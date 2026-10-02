// Command sirajctl performs the administrative actions that have no safe route
// through the web UI — most importantly creating the very first admin, since
// is_admin defaults to false and nothing in the app can grant it from nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/bilelzarai/siraj/internal/config"
	"github.com/bilelzarai/siraj/internal/database"
)

const usage = `sirajctl — administrative commands for the Sirāj quiz server

Usage:
  sirajctl <command> [arguments]

Commands:
  users                  List every account with its role
  promote <username>     Grant admin rights
  demote  <username>     Revoke admin rights
  admins                 List every admin account
  whois   <username>     Show one account
  passwd  <username>     Set a password, read from stdin
  stats                  Print platform totals
  seed                   Load the bundled question bank into the database

Passwords are stored as bcrypt hashes and cannot be read back — not by this
tool, not by an admin, not from the database. passwd sets a new one:

  read -rs NEWPASS && printf '%s' "$NEWPASS" | sirajctl passwd someone

Reading it from stdin rather than an argument keeps it out of shell history
and out of the process list, where any other user on the box could see it.

seed loads internal/database/seed/questions.json. It is idempotent and will
not overwrite a translation that something else has edited since, so it is safe
to re-run after editing the file. Run it once on a new database — the server
does not seed on boot unless SEED_ON_START says so, and it is not meant to.

Every command reads DATABASE_URL from the environment or from .env.
`

func main() {
	flag.Usage = func() { _, _ = os.Stderr.WriteString(usage) }
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(args[0], args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(command string, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch command {
	case "users":
		return listUsers(ctx, pool)
	case "passwd":
		return setPassword(ctx, pool, args)
	case "promote":
		return setAdmin(ctx, pool, args, true)
	case "demote":
		return setAdmin(ctx, pool, args, false)
	case "admins":
		return listAdmins(ctx, pool)
	case "whois":
		return whois(ctx, pool, args)
	case "stats":
		return stats(ctx, pool)
	case "seed":
		return seedBank(ctx, pool)
	default:
		flag.Usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func setAdmin(ctx context.Context, pool *pgxpool.Pool, args []string, admin bool) error {
	if len(args) != 1 {
		return errors.New("expected exactly one username")
	}
	username := strings.TrimSpace(args[0])

	// Resolve the account first, so a typo reports "no such user" rather than
	// tripping the last-admin guard below with a confusing message.
	var wasAdmin bool
	err := pool.QueryRow(ctx,
		`SELECT role = 'admin' FROM users WHERE username = $1`, username).Scan(&wasAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no account with username %q", username)
	}
	if err != nil {
		return err
	}

	// Refuse to remove the last admin: that would lock everyone out of the
	// admin area with no way back except this tool.
	if !admin && wasAdmin {
		var remaining int
		err := pool.QueryRow(ctx,
			`SELECT count(*) FROM users WHERE role = 'admin' AND username <> $1`, username).Scan(&remaining)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return errors.New("refusing to demote the last admin; promote someone else first")
		}
	}

	if wasAdmin == admin {
		state := "already an admin"
		if !admin {
			state = "not an admin"
		}
		fmt.Printf("%s is %s; nothing to do\n", username, state)
		return nil
	}

	var id, name string
	err = pool.QueryRow(ctx, `
		UPDATE users
		   SET role = CASE WHEN $2 THEN 'admin'::user_role ELSE 'player'::user_role END,
		       updated_at = now()
		 WHERE username = $1
		 RETURNING id::text, display_name`, username, admin).Scan(&id, &name)

	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no account with username %q", username)
	}
	if err != nil {
		return err
	}

	verb := "promoted to admin"
	if !admin {
		verb = "demoted to a regular account"
	}
	fmt.Printf("%s (%s) %s\n", name, username, verb)
	return nil
}

// listUsers answers "who is on this server and what can they do". The admin
// area at /admin/users shows the same thing with search and filters; this is
// for when you are not signed in as an admin yet.
func listUsers(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `
		SELECT username, display_name, email, role::text, status::text,
		       locale, created_at, last_seen_at
		  FROM users
		 ORDER BY CASE role WHEN 'admin' THEN 0 WHEN 'moderator' THEN 1 ELSE 2 END,
		          username`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tDISPLAY NAME\tEMAIL\tROLE\tSTATUS\tLANG\tJOINED\tLAST SEEN")

	n := 0
	for rows.Next() {
		var username, name, email, role, status, locale string
		var created, seen time.Time
		if err := rows.Scan(&username, &name, &email, &role, &status,
			&locale, &created, &seen); err != nil {
			return err
		}
		n++
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			username, name, email, role, status, locale,
			created.Format("2006-01-02"), seen.Format("2006-01-02 15:04"))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Printf("\n%d account(s). Passwords are bcrypt hashes and cannot be read;\n"+
		"set a new one with:  sirajctl passwd <username>\n", n)
	return nil
}

// setPassword replaces one account's password. There is no way to recover the
// old one, so this is the only route back into a locked-out account.
func setPassword(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	if len(args) != 1 {
		return errors.New("expected exactly one username")
	}
	username := strings.TrimSpace(args[0])

	// Stat rather than golang.org/x/term: one dependency fewer for one bool.
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		_, _ = os.Stderr.WriteString(
			"Reading the new password from stdin. Pipe it in so it does not land\n" +
				"in your shell history:\n\n" +
				"  read -rs NEWPASS && printf '%s' \"$NEWPASS\" | sirajctl passwd " + username + "\n\n")
	}

	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
	if err != nil {
		return fmt.Errorf("read password from stdin: %w", err)
	}
	// Trailing newlines come from echo and from typing; a password made only of
	// whitespace is a mistake, but one that merely contains spaces is fine.
	password := strings.TrimRight(string(raw), "\r\n")
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters, same rule as the sign-up form")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	var name string
	err = pool.QueryRow(ctx, `
		UPDATE users SET password_hash = $2, updated_at = now()
		 WHERE username = $1
		 RETURNING display_name`, username, string(hash)).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no account with username %q", username)
	}
	if err != nil {
		return err
	}

	// Every other device is now holding a session that outlived the password
	// it was granted under, which is exactly what a rotation is meant to end.
	tag, err := pool.Exec(ctx,
		`DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = $1)`, username)
	if err != nil {
		return err
	}

	fmt.Printf("password set for %s (%s); %d active session(s) signed out\n",
		name, username, tag.RowsAffected())
	return nil
}

func listAdmins(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `
		SELECT username, display_name, email, created_at, last_seen_at
		  FROM users WHERE role = 'admin' ORDER BY created_at`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tDISPLAY NAME\tEMAIL\tJOINED\tLAST SEEN")

	found := false
	for rows.Next() {
		var username, name, email string
		var created, seen time.Time
		if err := rows.Scan(&username, &name, &email, &created, &seen); err != nil {
			return err
		}
		found = true
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			username, name, email,
			created.Format("2006-01-02"), seen.Format("2006-01-02 15:04"))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	w.Flush()

	if !found {
		fmt.Println("No admins yet. Create one with:  sirajctl promote <username>")
	}
	return nil
}

func whois(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	if len(args) != 1 {
		return errors.New("expected exactly one username")
	}

	var (
		id, username, name, email, locale, country, role string
		xp, played, won                                  int
		created, seen                                    time.Time
	)
	err := pool.QueryRow(ctx, `
		SELECT id::text, username, display_name, email, locale, country,
		       role::text, xp, games_played, games_won, created_at, last_seen_at
		  FROM users WHERE username = $1`, strings.TrimSpace(args[0]),
	).Scan(&id, &username, &name, &email, &locale, &country,
		&role, &xp, &played, &won, &created, &seen)

	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no account with username %q", args[0])
	}
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, row := range [][2]string{
		{"id", id},
		{"username", username},
		{"display name", name},
		{"email", email},
		{"locale", locale},
		{"country", orDash(country)},
		{"role", role},
		{"xp", fmt.Sprint(xp)},
		{"rounds played", fmt.Sprint(played)},
		{"duels won", fmt.Sprint(won)},
		{"joined", created.Format(time.RFC3339)},
		{"last seen", seen.Format(time.RFC3339)},
	} {
		fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
	}
	return w.Flush()
}

func stats(ctx context.Context, pool *pgxpool.Pool) error {
	queries := []struct {
		label string
		sql   string
	}{
		{"users", `SELECT count(*) FROM users`},
		{"admins", `SELECT count(*) FROM users WHERE role = 'admin'`},
		{"moderators", `SELECT count(*) FROM users WHERE role = 'moderator'`},
		{"active today", `SELECT count(*) FROM users WHERE last_seen_at > now() - interval '1 day'`},
		{"categories", `SELECT count(*) FROM categories WHERE is_active`},
		{"questions", `SELECT count(*) FROM questions WHERE is_active`},
		{"translations", `SELECT count(*) FROM question_translations`},
		{"rounds finished", `SELECT count(*) FROM game_sessions WHERE status = 'finished'`},
		{"answers recorded", `SELECT count(*) FROM game_answers`},
		{"matches", `SELECT count(*) FROM challenges`},
		{"friendships", `SELECT count(*) FROM friendships WHERE status = 'accepted'`},
		{"messages", `SELECT count(*) FROM messages`},
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, q := range queries {
		var n int64
		if err := pool.QueryRow(ctx, q.sql).Scan(&n); err != nil {
			// A table from a later migration may not exist yet; report it
			// rather than aborting the whole report.
			fmt.Fprintf(w, "%s:\t—\n", q.label)
			continue
		}
		fmt.Fprintf(w, "%s:\t%d\n", q.label, n)
	}
	return w.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// seedBank loads the bundled question bank on demand.
//
// It is a command rather than a boot-time flag because loading content is
// something somebody does once, on purpose. A server that seeds on every
// restart is a server that can revert content between deploys — the upsert is
// careful about translations, but the decision to run it at all belongs to an
// operator and not to a process restart.
func seedBank(ctx context.Context, pool *pgxpool.Pool) error {
	// The bank references categories by slug, which migration 0002 creates. On
	// a database the server has never booted against, neither table exists yet,
	// and the error from the upsert names a missing category rather than the
	// missing schema.
	var categories int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM categories`).Scan(&categories); err != nil {
		return fmt.Errorf("no schema yet — start the server once so it migrates, then seed: %w", err)
	}

	if err := database.SeedQuestions(ctx, pool); err != nil {
		return err
	}

	var questions, translations int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM questions),
		       (SELECT count(*) FROM question_translations)`,
	).Scan(&questions, &translations); err != nil {
		return err
	}
	fmt.Printf("question bank loaded: %d questions, %d translations\n", questions, translations)
	return nil
}

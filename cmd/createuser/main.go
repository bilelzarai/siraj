// Sirāj account bootstrap: create (or re-point) one real staff account, so the first
// administrator can sign in and start entering real content in the admin console.
//
//	make admin email=you@example.com username=you password='a real password'
//	go run ./cmd/createuser -email=you@example.com -username=you -role=admin
//
// Nothing here is sample data: the command creates exactly the account you name, and
// refuses to invent one. Re-running with the same email keeps the user id, its games
// and its points, and only writes the password and the role again.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/config"
	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/modules/auth"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	email := flag.String("email", "", "the account's email address (required)")
	username := flag.String("username", "", "the account's username (required)")
	name := flag.String("name", "", "display name (defaults to the username)")
	role := flag.String("role", "admin", "admin, moderator, support, translator or player")
	lang := flag.String("lang", "en", "the account's language: ar, en or fr")
	password := flag.String("password", "", "the password; read from SIRAJ_ADMIN_PASSWORD when empty")
	flag.Parse()

	*email = strings.ToLower(strings.TrimSpace(*email))
	*username = strings.TrimSpace(*username)
	if *password == "" {
		*password = os.Getenv("SIRAJ_ADMIN_PASSWORD")
	}
	if *name == "" {
		*name = *username
	}
	if *email == "" || !strings.Contains(*email, "@") {
		fail("email", errors.New("pass -email=you@example.com"))
	}
	if *username == "" {
		fail("username", errors.New("pass -username=you"))
	}
	if len(*password) < auth.MinPasswordLength {
		fail("password", fmt.Errorf("pass -password (or SIRAJ_ADMIN_PASSWORD), at least %d characters", auth.MinPasswordLength))
	}
	userRole, err := parseRole(*role)
	if err != nil {
		fail("role", err)
	}

	cfg, err := config.Load()
	if err != nil {
		fail("config", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		fail("migrate", err)
	}
	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fail("db", err)
	}
	defer d.Close()

	svc := auth.NewService(d, auth.Deps{
		SessionTTL: cfg.SessionTTL,
		GuestTTL:   cfg.GuestTTL,
		BaseURL:    cfg.BaseURL,
	})
	state, err := upsert(ctx, d, svc, userRole, *username, *email, *name, *lang, *password)
	if err != nil {
		fail("create "+*username, err)
	}
	fmt.Printf("\n  %s %s (%s), role %s.\n  Sign in at %s/signin — email or username, both work.\n\n",
		state, *username, *email, userRole, cfg.BaseURL)
}

func parseRole(v string) (sqlc.UserRole, error) {
	switch sqlc.UserRole(strings.ToLower(strings.TrimSpace(v))) {
	case sqlc.UserRoleAdmin:
		return sqlc.UserRoleAdmin, nil
	case sqlc.UserRoleModerator:
		return sqlc.UserRoleModerator, nil
	case sqlc.UserRoleSupport:
		return sqlc.UserRoleSupport, nil
	case sqlc.UserRoleTranslator:
		return sqlc.UserRoleTranslator, nil
	case sqlc.UserRolePlayer:
		return sqlc.UserRolePlayer, nil
	}
	return "", fmt.Errorf("unknown role %q", v)
}

// upsert creates the account, or sets the password and the role of the one already
// there. It returns "created" or "updated" for the line printed at the end.
func upsert(ctx context.Context, d *db.DB, svc *auth.Service, role sqlc.UserRole, username, email, displayName, lang, password string) (string, error) {
	state := "created"
	_, err := svc.Register(ctx, auth.RegisterInput{
		DisplayName: displayName,
		Username:    username,
		Email:       email,
		Password:    password,
		Lang:        lang,
		AcceptTerms: true,
		Client:      "web",
	})
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrEmailTaken), errors.Is(err, auth.ErrUsernameTaken):
		state = "updated"
		hash, err := auth.HashPassword(password)
		if err != nil {
			return "", err
		}
		user, err := d.Q.GetUserByEmail(ctx, pgtype.Text{String: email, Valid: true})
		if err != nil {
			return "", err
		}
		if err := d.Q.CreateUserSettings(ctx, user.ID); err != nil {
			return "", err
		}
		if err := d.Q.UpsertIdentity(ctx, sqlc.UpsertIdentityParams{
			UserID: user.ID, Provider: "password", ProviderUid: email,
			PasswordHash: pgtype.Text{String: hash, Valid: true},
		}); err != nil {
			return "", err
		}
	default:
		return "", err
	}

	user, err := d.Q.GetUserByEmail(ctx, pgtype.Text{String: email, Valid: true})
	if err != nil {
		return "", err
	}
	if user.Role != role {
		if err := d.Q.SetUserRole(ctx, sqlc.SetUserRoleParams{Role: role, ID: user.ID}); err != nil {
			return "", err
		}
	}
	return state, nil
}

func fail(what string, err error) {
	slog.Error(what, "err", err)
	os.Exit(1)
}

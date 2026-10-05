package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bilelzarai/siraj/internal/models"
)

// Every column is a bare name or a cast, and deliberately so: prefixed() below
// re-qualifies this list by splitting it on commas, so a COALESCE(...) in here
// would be torn in half the moment a query needed a table alias.
const userColumns = `
	id, username, email, password_hash, display_name, bio, country, avatar_seed,
	locale, theme, xp, coins, games_played, games_won, best_streak,
	role::text, status, suspended_reason,
	last_seen_at, created_at, updated_at,
	is_temporary, host_user_id, guest_key, expires_at`

// accountsOnly is the predicate every list of people carries. A temporary
// player is a player — they answer questions and hold a place in a match — but
// they are not somebody you can befriend, rank, write to or find by name, and
// leaving them out of one query and not the next is how they leak into screens
// that have no way to describe them.
const accountsOnly = `NOT is_temporary`

func scanUser(row pgx.Row) (*models.User, error) {
	var u models.User
	// Nullable since temporary players exist: they have neither an address nor
	// a browser session of their own to be named by, depending on which kind
	// they are. The model keeps both as plain strings, empty when absent.
	var email, guestKey *string
	err := row.Scan(
		&u.ID, &u.Username, &email, &u.PasswordHash, &u.DisplayName, &u.Bio,
		&u.Country, &u.AvatarSeed, &u.Locale, &u.Theme, &u.XP, &u.Coins,
		&u.GamesPlayed, &u.GamesWon, &u.BestStreak,
		&u.Role, &u.Status, &u.SuspendedReason,
		&u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt,
		&u.IsTemporary, &u.HostUserID, &guestKey, &u.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if email != nil {
		u.Email = *email
	}
	if guestKey != nil {
		u.GuestKey = *guestKey
	}
	return &u, nil
}

// CreateUser inserts a new account, mapping unique violations to a typed
// error the handler can turn into a field message.
func (r *Repo) CreateUser(ctx context.Context, u *models.User) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, display_name, avatar_seed, locale)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at, last_seen_at`,
		u.Username, u.Email, u.PasswordHash, u.DisplayName, u.AvatarSeed, u.Locale,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.LastSeenAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch {
		case strings.Contains(pgErr.ConstraintName, "username"):
			return fmt.Errorf("%w: username", ErrConflict)
		case strings.Contains(pgErr.ConstraintName, "email"):
			return fmt.Errorf("%w: email", ErrConflict)
		}
		return ErrConflict
	}
	return err
}

func (r *Repo) UserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

// UserByEmail looks an account up by its address, which is unique.
func (r *Repo) UserByEmail(ctx context.Context, email string) (*models.User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(email::text) = lower($1) AND `+accountsOnly, email))
}

func (r *Repo) UserByUsername(ctx context.Context, username string) (*models.User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = $1`, username))
}

// UserByIdentifier accepts either a username or an email, which is what the
// login form asks for.
func (r *Repo) UserByIdentifier(ctx context.Context, identifier string) (*models.User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users
		  WHERE (username = $1 OR email = $1) AND `+accountsOnly, identifier))
}

func (r *Repo) UpdateProfile(ctx context.Context, id uuid.UUID, displayName, bio, country string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users
		   SET display_name = $2, bio = $3, country = $4, updated_at = now()
		 WHERE id = $1`, id, displayName, bio, country)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) UpdatePreferences(ctx context.Context, id uuid.UUID, locale, theme string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET locale = $2, theme = $3, updated_at = now() WHERE id = $1`,
		id, locale, theme)
	return err
}

func (r *Repo) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, hash)
	return err
}

func (r *Repo) TouchLastSeen(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET last_seen_at = now() WHERE id = $1`, id)
	return err
}

func (r *Repo) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

// SearchUsers does a trigram-backed fuzzy search, excluding the searcher.
func (r *Repo) SearchUsers(ctx context.Context, viewerID uuid.UUID, query string, limit int) ([]*models.UserCard, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	// The LEFT JOIN resolves each result to one of the Relation* constants, so
	// the search list can offer the action that actually applies to that person
	// instead of "Add friend" for everyone.
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country,
		       u.xp, u.last_seen_at, u.is_temporary,
		       COALESCE(
		         CASE
		           WHEN f.status = 'blocked'  THEN 'blocked'
		           WHEN f.status = 'accepted' THEN 'friends'
		           WHEN f.status = 'pending' AND f.requester_id = $1 THEN 'outgoing'
		           WHEN f.status = 'pending' THEN 'incoming'
		         END, 'none')
		  FROM users u
		  LEFT JOIN friendships f
		         ON (f.requester_id = $1 AND f.addressee_id = u.id)
		         OR (f.addressee_id = $1 AND f.requester_id = u.id)
		 WHERE u.id <> $1
		   AND NOT u.is_temporary
		   AND (u.username ILIKE '%' || $2 || '%' OR u.display_name ILIKE '%' || $2 || '%')
		 ORDER BY (u.username ILIKE $2 || '%') DESC, u.xp DESC
		 LIMIT $3`, viewerID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserCardsWithRelation(rows)
}

func collectUserCards(rows pgx.Rows) ([]*models.UserCard, error) {
	var out []*models.UserCard
	for rows.Next() {
		var c models.UserCard
		if err := rows.Scan(&c.ID, &c.Username, &c.DisplayName, &c.AvatarSeed,
			&c.Country, &c.XP, &c.LastSeenAt, &c.IsTemporary); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// collectUserCardsWithRelation is the same scan plus the viewer's relationship,
// for the queries that select it.
func collectUserCardsWithRelation(rows pgx.Rows) ([]*models.UserCard, error) {
	var out []*models.UserCard
	for rows.Next() {
		var c models.UserCard
		if err := rows.Scan(&c.ID, &c.Username, &c.DisplayName, &c.AvatarSeed,
			&c.Country, &c.XP, &c.LastSeenAt, &c.IsTemporary, &c.Relation); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

func (r *Repo) UserCardByUsername(ctx context.Context, username string) (*models.UserCard, error) {
	var c models.UserCard
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, display_name, avatar_seed, country, xp, last_seen_at
		  FROM users WHERE username = $1 AND NOT is_temporary`, username,
	).Scan(&c.ID, &c.Username, &c.DisplayName, &c.AvatarSeed, &c.Country, &c.XP, &c.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// UserCardInMyRoom resolves a name among the people in the viewer's room,
// temporary players included.
//
// UserCardByUsername deliberately cannot see a guest: a temporary player is
// invisible to the rest of the site, which is what stops them turning up in
// search, on the leaderboard or in a stranger's friend list. Inside a room the
// opposite is true — the people in it are exactly who you came to find, and in
// a guest's room all of them are guests. So the lookup that has to see them is
// a separate one, scoped to the room, rather than a flag loosening the lookup
// that must not.
func (r *Repo) UserCardInMyRoom(ctx context.Context, viewerID uuid.UUID, username string) (*models.UserCard, error) {
	var c models.UserCard
	err := r.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at, u.is_temporary
		  FROM conversation_members mine
		  JOIN conversation_members peer ON peer.conversation_id = mine.conversation_id
		  JOIN users u ON u.id = peer.user_id
		 WHERE mine.user_id = $1 AND mine.is_room
		   AND peer.user_id <> $1 AND u.username = $2`, viewerID, username,
	).Scan(&c.ID, &c.Username, &c.DisplayName, &c.AvatarSeed, &c.Country, &c.XP,
		&c.LastSeenAt, &c.IsTemporary)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// --------------------------------------------------------------- sessions --

func (r *Repo) CreateSession(ctx context.Context, s *models.Session) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		s.ID, s.UserID, s.UserAgent, s.IP, s.ExpiresAt)
	return err
}

// SessionUser resolves a session id to its user in one round trip, and
// rejects expired sessions.
func (r *Repo) SessionUser(ctx context.Context, sessionID string) (*models.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `
		SELECT `+prefixed(userColumns, "u")+`
		  FROM sessions s
		  JOIN users u ON u.id = s.user_id
		 WHERE s.id = $1 AND s.expires_at > now()`, sessionID))
}

func (r *Repo) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

func (r *Repo) DeleteOtherSessions(ctx context.Context, userID uuid.UUID, keepID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, userID, keepID)
	return err
}

func (r *Repo) ListSessions(ctx context.Context, userID uuid.UUID) ([]*models.Session, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, user_agent, ip, expires_at, created_at
		  FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Session
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.UserAgent, &s.IP,
			&s.ExpiresAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// PurgeExpiredSessions is called periodically by the background janitor.
func (r *Repo) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	ct, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// ExpireStaleChallenges flips past-due pending duels so they stop showing up
// as actionable.
func (r *Repo) ExpireStaleChallenges(ctx context.Context) (int64, error) {
	var affected int64
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `
			UPDATE challenges SET status = 'expired'
			 WHERE status IN ('pending', 'accepted') AND expires_at < now()`)
		if err != nil {
			return err
		}
		affected = ct.RowsAffected()

		// A match whose host is out cannot happen, and one nobody accepted has
		// nothing to happen. Both are cancelled rather than left standing: the
		// screen for a lobby in either state invites people to accept a game
		// that will never start.
		//
		// The handlers close these the moment they are caused. This is the net
		// underneath — for a row that got there some other way, and for the
		// ones already in the table before cancelling existed.
		if _, err := tx.Exec(ctx, `
			UPDATE challenges c
			   SET status = 'cancelled',
			       cancelled_at = COALESCE(c.cancelled_at, now()),
			       cancelled_by = COALESCE(c.cancelled_by, c.host_id)
			 WHERE c.status IN ('pending', 'accepted')
			   AND (
			     EXISTS (SELECT 1 FROM challenge_players p
			              WHERE p.challenge_id = c.id AND p.is_host
			                AND p.state IN ('declined', 'eliminated'))
			     OR NOT EXISTS (SELECT 1 FROM challenge_players p
			                     WHERE p.challenge_id = c.id AND NOT p.is_host
			                       AND p.state IN ('invited', 'joined', 'played')))`); err != nil {
			return err
		}

		// And let go of everyone any closed match was holding. A player is
		// inside one match at a time, and "inside" is the `joined` state — so a
		// match that has ended and left its players joined has locked every one
		// of them out of ever entering another.
		_, err = tx.Exec(ctx, `
			UPDATE challenge_players p
			   SET state = 'eliminated'
			  FROM challenges c
			 WHERE c.id = p.challenge_id
			   AND p.state IN ('invited', 'joined')
			   AND c.status IN ('expired', 'completed', 'declined', 'cancelled')`)
		return err
	})
	return affected, err
}

// prefixed rewrites a bare column list into a table-qualified one.
func prefixed(cols, alias string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// ------------------------------------------------------------ password reset --

// CreatePasswordReset stores a hashed token and invalidates every earlier one
// for the same account, so only the newest link in an inbox can be redeemed.
func (r *Repo) CreatePasswordReset(ctx context.Context, tokenHash string,
	userID uuid.UUID, expires time.Time, clientKey string) error {

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM password_resets WHERE user_id = $1 AND used_at IS NULL`,
			userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO password_resets (token_hash, user_id, expires_at, requested_ip)
			VALUES ($1, $2, $3, $4)`, tokenHash, userID, expires, truncateIP(clientKey))
		return err
	})
}

// PasswordResetUser resolves a live token to its account. A token that is
// missing, expired or already spent is reported the same way, so probing tells
// an attacker nothing about which case they hit.
func (r *Repo) PasswordResetUser(ctx context.Context, tokenHash string, now time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT user_id FROM password_resets
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2`,
		tokenHash, now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return id, err
}

// RedeemPasswordReset spends the token, sets the password and signs out every
// device in one transaction, so a half-applied reset cannot leave the old
// sessions alive alongside a new password.
func (r *Repo) RedeemPasswordReset(ctx context.Context, tokenHash string,
	userID uuid.UUID, passwordHash string) error {

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `
			UPDATE password_resets SET used_at = now()
			 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()`, tokenHash)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			// Someone redeemed it between the check and here.
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`,
			userID, passwordHash); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
		return err
	})
}

// truncateIP keeps the request origin for abuse triage without storing a full
// address indefinitely.
func truncateIP(clientKey string) string {
	if len(clientKey) > 64 {
		return clientKey[:64]
	}
	return clientKey
}

// SetAvatarSeed changes what a person's avatar is drawn from: a photo they
// uploaded, or empty to go back to the generated gradient.
func (r *Repo) SetAvatarSeed(ctx context.Context, userID uuid.UUID, seed string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET avatar_seed = $2 WHERE id = $1`, userID, seed)
	return err
}

package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// Players who are not accounts.
//
// Two kinds, both temporary, both rows in `users` so that every foreign key
// meaning "a player" keeps working without a second identity system:
//
//   - the anonymous visitor, who owns themselves and is tied to a browser
//     session by guest_key;
//   - a local player, made by somebody else to sit at the same device and
//     tied to them by host_user_id.
//
// Everything either of them does is deleted when expires_at passes, which is
// what "anonymous progress is temporary" means when it is written down rather
// than promised.

// GuestLifetime is how long a temporary player lives without being used. Every
// request they make pushes it out again, so a round in progress is never swept
// from under somebody; an abandoned one is gone by the next day.
const GuestLifetime = 24 * time.Hour

// MaxLocalPlayers is how many guests one host may have at their device. It is
// the match ceiling less the host, because the only reason to make one is to
// give them a seat in a match.
const MaxLocalPlayers = models.MaxChallengePlayers - 1

// guestUsername is a name nobody chose and nobody sees: temporary players are
// shown by display name everywhere, and this exists only because username is
// unique and NOT NULL. The prefix keeps them recognisable in the database.
func guestUsername() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "guest_" + hex.EncodeToString(buf), nil
}

// newGuestAvatarSeed gives a temporary player the same kind of generated
// avatar an account gets, so a scoreboard of four guests is four faces rather
// than four identical placeholders.
func newGuestAvatarSeed() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "guest"
	}
	return hex.EncodeToString(buf)
}

// CreateGuest inserts a temporary player.
//
// hostID names the account responsible for it — a local player at somebody's
// device. guestKey names the browser session instead, for the anonymous visitor
// themselves. Exactly one of the two is expected; the database refuses a
// temporary player with neither.
func (r *Repo) CreateGuest(ctx context.Context, hostID *uuid.UUID, guestKey, displayName, locale string) (*models.User, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil, ErrInvalid
	}
	if len([]rune(displayName)) > 40 {
		displayName = string([]rune(displayName)[:40])
	}

	// A handful of attempts, because the name is random and the only way it
	// collides is bad luck.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		username, err := guestUsername()
		if err != nil {
			return nil, err
		}
		u := &models.User{
			Username:    username,
			DisplayName: displayName,
			Locale:      locale,
			Role:        models.RolePlayer,
			Status:      models.StatusUserActive,
			IsTemporary: true,
			HostUserID:  hostID,
			GuestKey:    guestKey,
		}
		err = r.pool.QueryRow(ctx, `
			INSERT INTO users
				(username, email, password_hash, display_name, avatar_seed, locale,
				 is_temporary, host_user_id, guest_key, expires_at)
			VALUES ($1, NULL, '', $2, $3, $4, true, $5, NULLIF($6, ''), now() + $7::interval)
			RETURNING id, created_at, updated_at, last_seen_at, expires_at`,
			username, displayName, newGuestAvatarSeed(), locale, hostID, guestKey, GuestLifetime,
		).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.LastSeenAt, &u.ExpiresAt)
		if err == nil {
			return u, nil
		}
		if !isUniqueViolation(err) {
			return nil, err
		}
		// A clash on guest_key is not bad luck — somebody else already made
		// the anonymous player for this browser session, and theirs is the
		// one to use.
		if guestKey != "" {
			if existing, lookupErr := r.GuestByKey(ctx, guestKey); lookupErr == nil {
				return existing, nil
			}
		}
		lastErr = err
	}
	return nil, lastErr
}

// GuestByKey finds the anonymous player belonging to a browser session.
func (r *Repo) GuestByKey(ctx context.Context, guestKey string) (*models.User, error) {
	if strings.TrimSpace(guestKey) == "" {
		return nil, ErrNotFound
	}
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users
		  WHERE guest_key = $1 AND is_temporary AND expires_at > now()`, guestKey))
}

// LocalPlayers is everyone a host has sitting at their device, oldest first so
// the order on screen does not move around between visits.
func (r *Repo) LocalPlayers(ctx context.Context, hostID uuid.UUID) ([]*models.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users
		  WHERE host_user_id = $1 AND is_temporary AND expires_at > now()
		  ORDER BY created_at`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SearchLocalPlayers is the guests at one device, filtered by name and paged —
// the same shape the other two scopes answer in, so the dialog does not need to
// know which one it is showing.
func (r *Repo) SearchLocalPlayers(ctx context.Context, hostID uuid.UUID, query string, limit, offset int) ([]*models.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+userColumns+` FROM users
		 WHERE host_user_id = $1 AND is_temporary AND expires_at > now()
		   AND ($2 = '' OR display_name ILIKE '%' || $2 || '%')
		 ORDER BY created_at, id
		 LIMIT $3 OFFSET $4`,
		hostID, strings.TrimSpace(query), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// LocalPlayerCount is what the "add another" button is checked against.
func (r *Repo) LocalPlayerCount(ctx context.Context, hostID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM users
		 WHERE host_user_id = $1 AND is_temporary AND expires_at > now()`,
		hostID).Scan(&n)
	return n, err
}

// LocalPlayer loads one guest, but only if the caller is the person who made
// it. This is the whole access rule for playing as somebody else: you may act
// for a player you created, and for nobody else.
func (r *Repo) LocalPlayer(ctx context.Context, hostID, playerID uuid.UUID) (*models.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users
		  WHERE id = $1 AND host_user_id = $2 AND is_temporary AND expires_at > now()`,
		playerID, hostID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// DeleteLocalPlayer removes a guest and everything they did. Refused while they
// are inside a match, because the other players are waiting on a score that
// would never arrive.
func (r *Repo) DeleteLocalPlayer(ctx context.Context, hostID, playerID uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var inMatch bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM challenge_players
			                WHERE user_id = $1 AND state = 'joined')`,
			playerID).Scan(&inMatch); err != nil {
			return err
		}
		if inMatch {
			return ErrConflict
		}
		ct, err := tx.Exec(ctx,
			`DELETE FROM users WHERE id = $1 AND host_user_id = $2 AND is_temporary`,
			playerID, hostID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// TouchGuest pushes a temporary player's expiry out, and their host's with it.
//
// Both, because a guest outliving the person who made them is a row nothing can
// reach, and a host expiring while their guests do not is the same problem the
// other way up.
func (r *Repo) TouchGuest(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users
		   SET expires_at = now() + $2::interval
		 WHERE is_temporary
		   AND (id = $1
		     OR host_user_id = $1
		     OR id = (SELECT host_user_id FROM users WHERE id = $1))`,
		userID, GuestLifetime)
	return err
}

// PurgeExpiredGuests deletes temporary players whose time is up, and
// everything they left anywhere.
//
// Most of it goes by cascade: rounds, answers, places in a match, sessions,
// room memberships. Two things do not, and they are the reason this is a
// transaction rather than one statement. A question comment and an authored
// question both point at their writer with ON DELETE SET NULL, because for an
// account that is right — somebody closing their account should not silently
// erase a discussion other people took part in, and anonymising the author
// keeps the thread readable.
//
// For a guest it is wrong. "Nothing is saved" is a promise made on the way in,
// and a comment that outlives its author by forever, attached to no one, is
// that promise quietly broken. So the rows a guest wrote are deleted with
// them, before the cascade takes the rest.
func (r *Repo) PurgeExpiredGuests(ctx context.Context) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		const expiring = `SELECT id FROM users WHERE is_temporary AND expires_at < now()`

		if _, err := tx.Exec(ctx,
			`DELETE FROM question_comments WHERE user_id IN (`+expiring+`)`); err != nil {
			return err
		}
		// Their questions go too, and everything hanging off them — answers,
		// ratings and comments — by cascade. A question written for one
		// evening's match has no life after the match.
		if _, err := tx.Exec(ctx,
			`DELETE FROM questions WHERE author_id IN (`+expiring+`)`); err != nil {
			return err
		}

		ct, err := tx.Exec(ctx, `DELETE FROM users WHERE is_temporary AND expires_at < now()`)
		if err != nil {
			return err
		}
		n = ct.RowsAffected()
		return nil
	})
	return n, err
}

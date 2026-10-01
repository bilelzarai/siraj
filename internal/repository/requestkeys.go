package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// One press, one thing created.
//
// A disabled button stops the second click and nothing else. The second tab,
// the retried request, the browser's "resend?" after a back-and-forward, and
// the form still open on a phone in a pocket all arrive as separate, honest
// POSTs — and every one of them used to make another group, another match,
// another player.
//
// The form carries a key it generated. The first request to claim it wins and
// records where it ended up; every later request with the same key is handed
// that result instead of doing the work again. Nothing is stored beyond the key
// and a destination, so this is not a response cache — it is a way of asking
// "has this already happened".

// RequestKeyTTL is how long a claim is remembered. Long enough to cover a
// retry, a reload and a confused back button; short enough that the table stays
// small.
const RequestKeyTTL = 6 * time.Hour

// MaxRequestKeyLength bounds what a client can put in the primary key.
const MaxRequestKeyLength = 80

// ClaimRequestKey takes a key for this user and scope.
//
// It returns claimed=true when the caller is the first and should go ahead.
// claimed=false means somebody already did: `result` is whatever that first
// caller recorded, or empty when it has not finished yet — either way the
// answer is "do not do it again".
//
// An empty key claims nothing and always returns true, so a form that carries
// no key behaves exactly as it did before this existed.
func (r *Repo) ClaimRequestKey(ctx context.Context, key string, userID uuid.UUID, scope string) (bool, string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return true, "", nil
	}
	if len(key) > MaxRequestKeyLength {
		key = key[:MaxRequestKeyLength]
	}

	// The insert's effect is invisible to the SELECT beside it — a
	// data-modifying CTE does not show its rows to the rest of the statement —
	// so exactly one of the two branches can produce a row for a key nobody had
	// yet, and the loser reads the winner's row from the snapshot.
	var claimed bool
	var result string
	err := r.pool.QueryRow(ctx, `
		WITH ins AS (
		    INSERT INTO request_keys (key, user_id, scope)
		    VALUES ($1, $2, $3)
		    ON CONFLICT (key) DO NOTHING
		    RETURNING key
		),
		answer AS (
		    SELECT 1 AS ord, true AS claimed, ''::text AS result FROM ins
		    UNION ALL
		    SELECT 2, false, k.result FROM request_keys k WHERE k.key = $1
		)
		SELECT claimed, result FROM answer ORDER BY ord LIMIT 1`,
		key, userID, scope).Scan(&claimed, &result)

	if errors.Is(err, pgx.ErrNoRows) {
		// The row vanished between the insert and the read — a purge, or a
		// rolled-back claim. Treat it as ours: doing the work once more is
		// better than silently doing nothing.
		return true, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return claimed, result, nil
}

// CompleteRequestKey records where a claimed request ended up, so a repeat of
// it can be sent to the same place instead of being told nothing happened.
func (r *Repo) CompleteRequestKey(ctx context.Context, key, result string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if len(key) > MaxRequestKeyLength {
		key = key[:MaxRequestKeyLength]
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE request_keys SET result = $2 WHERE key = $1`, key, result)
	return err
}

// ReleaseRequestKey gives a key back after the work it claimed failed. Without
// it a refused match — not enough questions, nobody left to invite — would take
// its key down with it and the corrected resubmission would be told it had
// already happened.
func (r *Repo) ReleaseRequestKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if len(key) > MaxRequestKeyLength {
		key = key[:MaxRequestKeyLength]
	}
	_, err := r.pool.Exec(ctx,
		`DELETE FROM request_keys WHERE key = $1 AND result = ''`, key)
	return err
}

// PurgeRequestKeys drops claims older than the window. Nothing reads them after
// that, and a table that only grows is a table that eventually matters.
func (r *Repo) PurgeRequestKeys(ctx context.Context) (int64, error) {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM request_keys WHERE created_at < now() - $1::interval`, RequestKeyTTL)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

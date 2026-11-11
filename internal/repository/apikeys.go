package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// The credential behind the public endpoint. Everything here works on the hash
// of a key and never on the key itself: the plaintext exists for the length of
// one CLI command and is never written down, which is what makes "shown once"
// true rather than a promise.

// CreateAPIKey stores a minted key. The caller holds the plaintext; this layer
// only ever sees what it hashes to.
func (r *Repo) CreateAPIKey(ctx context.Context, label string, hash []byte, by *uuid.UUID) (*models.APIKey, error) {
	k := &models.APIKey{Label: label}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO api_keys (label, key_hash, created_by)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`, label, hash, by).Scan(&k.ID, &k.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return k, nil
}

// APIKeyByHash finds a live key. A revoked one is returned too, with its
// RevokedAt set, so the caller can answer 403 rather than 401 — "this key is
// finished" and "I do not know this key" are different things to be told.
func (r *Repo) APIKeyByHash(ctx context.Context, hash []byte) (*models.APIKey, error) {
	var k models.APIKey
	err := r.pool.QueryRow(ctx, `
		SELECT id, label, created_at, last_used_at, revoked_at
		  FROM api_keys WHERE key_hash = $1`, hash).
		Scan(&k.ID, &k.Label, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// TouchAPIKey records that a key answered a request. Operators need to know
// which credentials are still in use before revoking one, and a key nobody has
// used for a year is the easiest kind to retire.
func (r *Repo) TouchAPIKey(ctx context.Context, id int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id)
	return err
}

// APIKeys lists what exists, newest first. Never the key itself.
func (r *Repo) APIKeys(ctx context.Context) ([]*models.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, label, created_at, last_used_at, revoked_at
		  FROM api_keys ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.Label, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

// RevokeAPIKey stops a key answering, without deleting the row: which key was
// used, and when it was withdrawn, is the trail an operator needs afterwards.
// Revoking an already-revoked key is not an error — it is the state asked for.
func (r *Repo) RevokeAPIKey(ctx context.Context, id int) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

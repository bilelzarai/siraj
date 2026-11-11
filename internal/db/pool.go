// Package db owns the connection pool, migrations and the sqlc-generated queries.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"siraj/internal/db/sqlc"
)

// DB bundles the pool and the generated queries.
type DB struct {
	Pool *pgxpool.Pool
	Q    *sqlc.Queries
}

func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("cannot reach PostgreSQL (is `docker compose up -d` running?): %w", err)
	}
	return &DB{Pool: pool, Q: sqlc.New(pool)}, nil
}

func (d *DB) Close() { d.Pool.Close() }

// WithTx runs fn in one transaction: commit on nil, rollback on error or panic.
func (d *DB) WithTx(ctx context.Context, fn func(q *sqlc.Queries) error) (err error) {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(d.Q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

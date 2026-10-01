package database

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed all:migrations
var migrationFS embed.FS

//go:embed seed/questions.json
var seedFS embed.FS

// Connect opens a pooled connection and verifies it is reachable.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies every embedded .sql file exactly once, in filename order,
// recording each in schema_migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx,
				`INSERT INTO schema_migrations (version) VALUES ($1)`, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		slog.Info("migration applied", "version", name)
	}
	return nil
}

// seedQuestion mirrors one entry of seed/questions.json.
type seedQuestion struct {
	ID         int    `json:"id"`
	Category   string `json:"category"`
	Difficulty int    `json:"difficulty"`
	Points     int    `json:"points"`
	Correct    int    `json:"correct"`
	Source     string `json:"source"`
	T          map[string]struct {
		Prompt      string   `json:"prompt"`
		Choices     []string `json:"choices"`
		Explanation string   `json:"explanation"`
	} `json:"t"`
}

// SeedQuestions upserts the bundled question bank. It is idempotent: editing
// the JSON and restarting updates the rows in place.
func SeedQuestions(ctx context.Context, pool *pgxpool.Pool) error {
	raw, err := seedFS.ReadFile("seed/questions.json")
	if err != nil {
		return fmt.Errorf("read seed file: %w", err)
	}

	var questions []seedQuestion
	if err := json.Unmarshal(raw, &questions); err != nil {
		return fmt.Errorf("parse seed file: %w", err)
	}

	categoryIDs := map[string]int{}
	rows, err := pool.Query(ctx, `SELECT slug, id FROM categories`)
	if err != nil {
		return fmt.Errorf("load categories: %w", err)
	}
	for rows.Next() {
		var slug string
		var id int
		if err := rows.Scan(&slug, &id); err != nil {
			rows.Close()
			return err
		}
		categoryIDs[slug] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// kept counts translations left alone because something other than the
	// seeder had edited them.
	kept := 0

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, q := range questions {
			catID, ok := categoryIDs[q.Category]
			if !ok {
				return fmt.Errorf("question %d references unknown category %q", q.ID, q.Category)
			}
			points := q.Points
			if points == 0 {
				points = q.Difficulty * 10
			}

			_, err := tx.Exec(ctx, `
				INSERT INTO questions (id, category_id, difficulty, points, correct_index, source)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (id) DO UPDATE SET
					category_id   = EXCLUDED.category_id,
					difficulty    = EXCLUDED.difficulty,
					points        = EXCLUDED.points,
					correct_index = EXCLUDED.correct_index,
					source        = EXCLUDED.source`,
				q.ID, catID, q.Difficulty, points, q.Correct, q.Source)
			if err != nil {
				return fmt.Errorf("upsert question %d: %w", q.ID, err)
			}

			for locale, t := range q.T {
				if len(t.Choices) != 4 {
					return fmt.Errorf("question %d locale %s must have exactly 4 choices, got %d",
						q.ID, locale, len(t.Choices))
				}
				// Only overwrite a translation that is still the seeder's own.
				//
				// The seeder runs on every boot by default, so an unconditional
				// upsert silently reverted any correction made since — an
				// imported fix, a reviewed machine translation — with no warning
				// and no audit entry. A row whose source is no longer 'seed' was
				// touched by a human or a tool, and the file does not outrank it.
				tag, err := tx.Exec(ctx, `
					INSERT INTO question_translations
						(question_id, locale, prompt, choices, explanation, source)
					VALUES ($1, $2, $3, $4, $5, 'seed')
					ON CONFLICT (question_id, locale) DO UPDATE SET
						prompt      = EXCLUDED.prompt,
						choices     = EXCLUDED.choices,
						explanation = EXCLUDED.explanation
					WHERE question_translations.source = 'seed'`,
					q.ID, locale, t.Prompt, t.Choices, t.Explanation)
				if err != nil {
					return fmt.Errorf("upsert translation %d/%s: %w", q.ID, locale, err)
				}
				if tag.RowsAffected() == 0 {
					kept++
				}
			}
		}

		// Keep the sequence ahead of the explicit IDs we just inserted.
		_, err := tx.Exec(ctx,
			`SELECT setval('questions_id_seq', GREATEST((SELECT max(id) FROM questions), 1))`)
		return err
	})
	if err != nil {
		return err
	}

	slog.Info("question bank seeded", "count", len(questions), "locally_edited_kept", kept)
	return nil
}

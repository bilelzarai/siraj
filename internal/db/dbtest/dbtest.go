// Package dbtest gives tests a real PostgreSQL 18 database: the same migrations as
// production, in a database of their own (siraj_test), emptied between tests.
//
// Tests skip themselves with a clear message when PostgreSQL is not running, so
// `go test ./...` still works on a machine without Docker.
package dbtest

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"siraj/internal/config"
	"siraj/internal/db"
)

// testDB is the database this test binary owns. Every package gets its own, named
// after its test binary, because each one now writes the content it needs (Seed)
// instead of sharing a seeded catalog: two packages running at the same time — what
// `go test ./...` does — must not empty each other's tables.
var testDB = databaseName()

func databaseName() string {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		}
		return '_'
	}, base)
	if clean == "" || clean == "_" {
		return "siraj_test"
	}
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return "siraj_test_" + clean
}

var (
	once     sync.Once
	readyURL string
	readyErr error
	// What the migrations install and every test may rely on. No catalog and no
	// question bank: those are real content, written in the admin console, and a test
	// that needs something to play with calls Seed.
	seedTable = []string{
		// content_types is configuration, not content: the app cannot run without
		// knowing a question is a kind of thing, and migration 23 installs the set.
		// Truncating it between tests would leave the platform unable to store
		// anything at all.
		"content_types",
		"languages", "achievements", "achievement_translations",
	}
)

// URL is the connection string of the test database, created and migrated once per run.
func URL(t *testing.T) string {
	t.Helper()
	once.Do(func() { readyURL, readyErr = prepare() })
	if readyErr != nil {
		t.Skipf("PostgreSQL is not available for tests (%v). Start it with `make db-up`.", readyErr)
	}
	return readyURL
}

// Open returns a pool on the test database. Every test gets a clean slate.
func Open(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), URL(t))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(d.Close)
	Clean(t, d)
	return d
}

// Clean empties every table except the ones the migrations install (languages and
// the achievement definitions).
//
// One TRUNCATE for the whole list: the catalog and the question bank are content
// now, so a test's own domain is deleted with everything that points at it, and
// deleting table by table would trip over the RESTRICT between categories and
// domains. The partitions of game_answers go with their parent.
func Clean(t *testing.T, d *db.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := d.Pool.Query(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'schema_migrations'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if !contains(seedTable, name) && !strings.HasPrefix(name, "game_answers_") {
			tables = append(tables, `"`+name+`"`)
		}
	}
	rows.Close()
	if len(tables) == 0 {
		return
	}
	if _, err := d.Pool.Exec(ctx, "TRUNCATE TABLE "+strings.Join(tables, ", ")+" RESTART IDENTITY"); err != nil {
		t.Fatalf("clean: %v", err)
	}
}

func prepare() (string, error) {
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		base = cfg.DatabaseURL
	}
	target, admin, err := splitURL(base)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return "", err
	}
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", testDB).Scan(&exists); err != nil {
		_ = conn.Close(ctx)
		return "", err
	}
	if !exists {
		if _, err := conn.Exec(ctx, `CREATE DATABASE "`+testDB+`"`); err != nil {
			_ = conn.Close(ctx)
			return "", err
		}
	}
	_ = conn.Close(ctx)

	if err := db.Migrate(target); err != nil {
		return "", err
	}
	return target, nil
}

// splitURL returns the test database URL and an URL on the maintenance database.
func splitURL(raw string) (target, admin string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	t := *u
	t.Path = "/" + testDB
	a := *u
	a.Path = "/postgres"
	return t.String(), a.String(), nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

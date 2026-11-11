package repository_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bilelzarai/siraj/internal/database"
	"github.com/bilelzarai/siraj/internal/repository"
)

// These tests run against a real PostgreSQL, because what they are testing is
// SQL. Every statement in this package was previously verified only by a human
// loading a page — which is how NearDuplicates spent ten seconds a call for
// months without anybody noticing.
//
// Each run builds its own database from the migrations and drops it again, so
// the tests never touch development data and cannot leave anything behind. With
// no server reachable they skip rather than fail: a contributor without Docker
// still gets a green `go test ./...`.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	adminURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if adminURL == "" {
		adminURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if adminURL == "" {
		adminURL = dotenvDatabaseURL()
	}
	if adminURL == "" {
		fmt.Println("repository tests skipped: no DATABASE_URL or TEST_DATABASE_URL")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	code, err := runSuite(ctx, adminURL, m)
	if err != nil {
		fmt.Println("repository tests skipped:", err)
		os.Exit(0)
	}
	os.Exit(code)
}

func runSuite(ctx context.Context, adminURL string, m *testing.M) (int, error) {
	// A name per process, so two runs in parallel do not share a database.
	name := fmt.Sprintf("siraj_test_%d", os.Getpid())

	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		return 0, err
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		return 0, err
	}
	sweepLeakedDatabases(ctx, admin, "siraj_test_")

	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteIdent(name)); err != nil {
		admin.Close()
		return 0, err
	}
	defer func() {
		// Terminate stragglers first: a leaked connection would keep the
		// database alive and the next run would find the name taken.
		_, _ = admin.Exec(ctx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
		_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdent(name))
		admin.Close()
	}()

	pool, err := pgxpool.New(ctx, replaceDatabase(adminURL, name))
	if err != nil {
		return 0, err
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool); err != nil {
		return 0, err
	}

	testPool = pool
	return m.Run(), nil
}

// repo hands a test the repository, skipping when there is no database.
func repo(t *testing.T) *repository.Repo {
	t.Helper()
	if testPool == nil {
		t.Skip("no test database")
	}
	return repository.New(testPool)
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// replaceDatabase swaps the database name in a connection URL, keeping every
// other parameter — sslmode in particular — exactly as configured.
func replaceDatabase(raw, name string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Path = "/" + name
	return u.String()
}

// dotenvDatabaseURL is the last resort: the developer's own .env. `go test`
// runs each package in its own directory, so the file is looked for up the
// tree rather than in the working directory.
func dotenvDatabaseURL() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		body, err := os.ReadFile(filepath.Join(dir, ".env"))
		if err == nil {
			for _, line := range strings.Split(string(body), "\n") {
				if after, ok := strings.CutPrefix(strings.TrimSpace(line), "DATABASE_URL="); ok {
					return strings.Trim(strings.TrimSpace(after), `"'`)
				}
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// sweepLeakedDatabases drops what earlier runs left behind.
//
// The harness drops its own database in a deferred call, and a deferred call
// is exactly what a kill signal skips: ^C during a slow test, a CI job
// cancelled, an editor stopping a run. Each one leaves a database behind, and
// they accumulated to 385 of them — 3.9 GB — before anybody noticed, because
// nothing ever looked.
//
// Swept at the start rather than the end: a run that is killed cannot clean up
// after itself by definition, so the only reliable moment is the next one.
// Anything still connected is left alone — that is another run in progress,
// not a leak.
func sweepLeakedDatabases(ctx context.Context, admin *pgxpool.Pool, prefix string) {
	rows, err := admin.Query(ctx, `
		SELECT datname FROM pg_database
		 WHERE datname LIKE $1
		   AND NOT EXISTS (
		         SELECT 1 FROM pg_stat_activity WHERE datname = pg_database.datname)`,
		prefix+"%")
	if err != nil {
		fmt.Println("could not look for leaked test databases:", err)
		return
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			names = append(names, name)
		}
	}
	rows.Close()

	var swept int
	for _, name := range names {
		if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdent(name)); err == nil {
			swept++
		}
	}
	// Said out loud: a sweep that reports nothing looks identical to a sweep
	// that never ran.
	if swept > 0 {
		fmt.Printf("swept %d leaked test database(s) left by an interrupted run\n", swept)
	}
}

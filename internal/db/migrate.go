package db

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies every pending migration. It refuses to run on a dirty state,
// so a half-applied migration has to be fixed by hand before the server starts.
func Migrate(databaseURL string) error {
	src, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, toPgx5URL(databaseURL))
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer m.Close()

	if _, dirty, verr := m.Version(); verr == nil && dirty {
		return errors.New("database is in a dirty migration state: fix it, then run `migrate force <version>`")
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// golang-migrate's pgx v5 driver uses the pgx5:// scheme.
func toPgx5URL(u string) string {
	for _, p := range []string{"postgresql://", "postgres://"} {
		if strings.HasPrefix(u, p) {
			return "pgx5://" + strings.TrimPrefix(u, p)
		}
	}
	return u
}

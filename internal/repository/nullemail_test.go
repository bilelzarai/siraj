package repository_test

import (
	"errors"
	"testing"

	"github.com/bilelzarai/siraj/internal/repository"
)

// Every read path that touches users.email, exercised against an account that
// has none.
//
// Migration 0025 made the column nullable so a temporary player can exist
// without an address, and pgx refuses to scan NULL into a string — so a query
// that selects the column bare fails outright, for every row in the result,
// the moment one guest has ever played. Three sirajctl commands did exactly
// that, including the one you reach for before you have an admin account to
// sign in with.
//
// Asserted by behaviour rather than by reading the SQL: userColumns in
// users.go selects the column bare and is correct, because it scans into a
// *string. What matters is not how a query is written but whether it survives
// the row, so this creates the row and reads it everywhere.
func TestEveryReadSurvivesAnAccountWithNoEmail(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	repo := repository.New(testPool)
	ctx := t.Context()

	// A temporary player: no address, which the schema permits only for these.
	guest, err := repo.CreateGuest(ctx, nil, "nullemail-key", "ضيف بلا بريد", "ar")
	if err != nil {
		t.Fatalf("creating a temporary player: %v", err)
	}

	t.Run("the account itself", func(t *testing.T) {
		u, err := repo.UserByID(ctx, guest.ID)
		if err != nil {
			t.Fatalf("UserByID: %v", err)
		}
		if u.Email != "" {
			t.Errorf("an account with no address came back with %q", u.Email)
		}
	})

	t.Run("the admin directory", func(t *testing.T) {
		// Temporary players are excluded from the directory by design, so what
		// is being checked is that the query runs at all with one in the table.
		if _, _, err := repo.AdminUsers(ctx, repository.AdminUserFilter{Limit: 50}); err != nil {
			t.Errorf("AdminUsers: %v", err)
		}
	})

	t.Run("one account read on its own", func(t *testing.T) {
		if _, err := repo.AdminUser(ctx, guest.ID); err != nil {
			t.Errorf("AdminUser: %v", err)
		}
	})

	t.Run("the console search", func(t *testing.T) {
		if _, err := repo.AdminSearch(ctx, "ضيف", "ar", true, 5); err != nil {
			t.Errorf("AdminSearch: %v", err)
		}
	})

	t.Run("looking one up by address", func(t *testing.T) {
		// An empty identifier must not match the account with no address —
		// which is what a bare comparison against NULL would do if the column
		// were ever coalesced in a WHERE clause.
		if _, err := repo.UserByEmail(ctx, ""); err == nil {
			t.Error("an empty address matched an account")
		}
		if _, err := repo.UserByIdentifier(ctx, ""); err == nil {
			t.Error("an empty identifier matched an account")
		}
	})

	t.Run("the user card", func(t *testing.T) {
		// Not found is the right answer and the documented one: a temporary
		// player is invisible to the rest of the site, which is what keeps
		// them out of search, the leaderboard and a stranger's friend list.
		// What this asserts is that the query reached that answer rather than
		// failing to scan on the way — a NULL in the select list fails before
		// the row filter is ever considered.
		if _, err := repo.UserCardByUsername(ctx, guest.Username); !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("UserCardByUsername: %v, want ErrNotFound", err)
		}
	})
}

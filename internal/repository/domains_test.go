package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// The writing half of the level above a category. The reading half — that a
// retired domain leaves the draw and the count together — is in taxonomy_test.
//
// These mirror the category tests one level up, which is the point: the two
// are the same shape of thing, and anything true of one that is not true of
// the other is a drift worth seeing.

func TestADomainIsWrittenReadBackAndTranslated(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	id, err := r.UpsertDomain(ctx, &models.DomainDraft{
		// Slugs here are the writing half's own: taxonomy_test claims "sport"
		// and "football" in this same database.
		Slug: "sport-write", Icon: "⚽", Color: "#1d4ed8", SortOrder: 20, IsActive: true,
		Names: map[string]models.NameDraft{
			"en": {Name: "Sport", Description: "Football, handball, judo", Source: "human"},
			"ar": {Name: "الرياضة", Description: "كرة القدم وكرة اليد والجودو", Source: "human"},
		},
	})
	if err != nil {
		t.Fatalf("writing a domain: %v", err)
	}

	// The player's list, in a language the domain has.
	domains, err := r.Domains(ctx, "en")
	if err != nil {
		t.Fatalf("reading domains: %v", err)
	}
	var found *models.Domain
	for _, d := range domains {
		if d.ID == id {
			found = d
		}
	}
	if found == nil {
		t.Fatal("the domain just written is not in the list")
	}
	if found.Name != "Sport" {
		t.Errorf("name resolved to %q, want %q", found.Name, "Sport")
	}

	// And in one it does not: Arabic is the fallback everything resolves to,
	// so French reads the Arabic name rather than the slug.
	domains, err = r.Domains(ctx, "fr")
	if err != nil {
		t.Fatalf("reading domains in French: %v", err)
	}
	for _, d := range domains {
		if d.ID == id && d.Name != "الرياضة" {
			t.Errorf("French fell back to %q, want the Arabic name", d.Name)
		}
	}
}

func TestADomainSlugIsNotShared(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	draft := func() *models.DomainDraft {
		return &models.DomainDraft{
			Slug: "history", Icon: "🏛️", Color: "#0f766e", SortOrder: 30, IsActive: true,
			Names: map[string]models.NameDraft{"en": {Name: "History", Source: "human"}},
		}
	}
	if _, err := r.UpsertDomain(ctx, draft()); err != nil {
		t.Fatalf("writing the first: %v", err)
	}
	_, err := r.UpsertDomain(ctx, draft())
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("reusing a slug → %v, want ErrConflict", err)
	}
}

// One level up from "a category holding questions is not deleted", and for the
// same reason: the key is RESTRICT, so the database refuses it either way —
// this is what turns that refusal into something a screen can say.
func TestADomainHoldingCategoriesIsNotDeleted(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	id, err := r.UpsertDomain(ctx, &models.DomainDraft{
		Slug: "geography", Icon: "🗺️", Color: "#b45309", SortOrder: 40, IsActive: true,
		Names: map[string]models.NameDraft{"en": {Name: "Geography", Source: "human"}},
	})
	if err != nil {
		t.Fatalf("writing a domain: %v", err)
	}

	var categoryID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO categories (slug, icon, color, sort_order, domain_id)
		VALUES ('capitals','🏙️','#b45309',91,$1) RETURNING id`, id).Scan(&categoryID); err != nil {
		t.Fatalf("filing a category under it: %v", err)
	}

	if err := r.DeleteDomain(ctx, id); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("deleting a domain holding a category → %v, want ErrConflict", err)
	}

	// Empty it, and the same call goes through — the refusal is about what is
	// filed under it, not about the domain being special.
	if _, err := testPool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID); err != nil {
		t.Fatalf("emptying it: %v", err)
	}
	if err := r.DeleteDomain(ctx, id); err != nil {
		t.Fatalf("deleting an empty domain: %v", err)
	}
	if err := r.DeleteDomain(ctx, id); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleting it twice → %v, want ErrNotFound", err)
	}
}

// Retiring is the reversible half: the domain leaves the player's list and
// stays in the admin's, exactly as a retired category does.
func TestARetiredDomainLeavesThePlayersListAndStaysInTheAdmins(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	id, err := r.UpsertDomain(ctx, &models.DomainDraft{
		Slug: "science", Icon: "🔬", Color: "#4338ca", SortOrder: 50, IsActive: true,
		Names: map[string]models.NameDraft{"en": {Name: "Science", Source: "human"}},
	})
	if err != nil {
		t.Fatalf("writing a domain: %v", err)
	}

	listed := func(t *testing.T) bool {
		t.Helper()
		domains, err := r.Domains(ctx, "en")
		if err != nil {
			t.Fatalf("reading domains: %v", err)
		}
		for _, d := range domains {
			if d.ID == id {
				return true
			}
		}
		return false
	}

	if !listed(t) {
		t.Fatal("an active domain is missing from the player's list")
	}
	if err := r.SetDomainActive(ctx, id, false); err != nil {
		t.Fatalf("retiring: %v", err)
	}
	if listed(t) {
		t.Error("a retired domain is still offered to players")
	}

	admin, err := r.AdminDomains(ctx, "en")
	if err != nil {
		t.Fatalf("reading the admin list: %v", err)
	}
	var seen bool
	for _, d := range admin {
		if d.ID == id {
			seen = true
		}
	}
	if !seen {
		t.Error("a retired domain vanished from the admin list, so it could never be brought back")
	}

	if err := r.SetDomainActive(ctx, id, true); err != nil {
		t.Fatalf("restoring: %v", err)
	}
	if !listed(t) {
		t.Error("a restored domain did not come back")
	}
}

// The counts the delete button and the retire warning are built from.
func TestAnAdminDomainCountsWhatHangsBelowIt(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	domains, err := r.AdminDomains(ctx, "en")
	if err != nil {
		t.Fatalf("reading the admin list: %v", err)
	}
	var islamic *models.AdminDomain
	for _, d := range domains {
		if d.Slug == "islamic" {
			islamic = d
		}
	}
	if islamic == nil {
		t.Fatal("the seeded domain is missing")
	}
	if islamic.Categories == 0 {
		t.Error("the seeded domain reports no categories, but every category was filed under it")
	}
	if !islamic.Complete() {
		t.Errorf("the seeded domain has %v, want all three languages", islamic.PresentLocales)
	}
}

// The filter the setup screen will use: a category list narrowed to one
// subject area, and the same list unnarrowed when nobody asked.
func TestCategoriesCanBeNarrowedToOneDomain(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	id, err := r.UpsertDomain(ctx, &models.DomainDraft{
		Slug: "sport-filter", Icon: "⚽", Color: "#1d4ed8", SortOrder: 60, IsActive: true,
		Names: map[string]models.NameDraft{"en": {Name: "Sport", Source: "human"}},
	})
	if err != nil {
		t.Fatalf("writing a domain: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO categories (slug, icon, color, sort_order, domain_id)
		VALUES ('handball','🤾','#1d4ed8',92,$1)`, id); err != nil {
		t.Fatalf("filing a category under it: %v", err)
	}

	all, err := r.Categories(ctx, "en", 0)
	if err != nil {
		t.Fatalf("every category: %v", err)
	}
	narrowed, err := r.Categories(ctx, "en", id)
	if err != nil {
		t.Fatalf("one domain: %v", err)
	}

	if len(narrowed) != 1 || narrowed[0].Slug != "handball" {
		t.Fatalf("narrowing returned %d categories, want the one filed under the domain", len(narrowed))
	}
	if narrowed[0].DomainID != id || narrowed[0].DomainName != "Sport" {
		t.Errorf("the category does not carry its domain: id=%d name=%q",
			narrowed[0].DomainID, narrowed[0].DomainName)
	}
	if len(all) <= len(narrowed) {
		t.Errorf("zero narrowed to %d, so it is not meaning 'every domain'", len(all))
	}
}

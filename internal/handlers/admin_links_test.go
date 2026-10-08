package handlers_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// Every link the console emits has to lead somewhere. A dead link in an admin
// area is invisible until somebody presses it, and the design kit's own files
// link to *.html pages that do not exist here — so this walks what the server
// actually rendered rather than what the templates look like.
func TestEveryAdminLinkResolves(t *testing.T) {
	a := newApp(t)
	a.register("linkwalker")
	a.promote("linkwalker", models.RoleAdmin)

	paths := []string{
		"/admin", "/admin/audit", "/admin/audit/export.csv",
		"/admin/categories", "/admin/categories/1/edit", "/admin/categories/new",
		"/admin/comments", "/admin/domains", "/admin/domains/1/edit", "/admin/domains/new",
		"/admin/integrity", "/admin/questions", "/admin/questions/export.csv",
		"/admin/questions/import", "/admin/questions/import/template.csv",
		"/admin/questions/new", "/admin/rated", "/admin/review", "/admin/support",
		"/admin/users", "/admin/users/export.csv", "/admin/users/new",
		"/admin/search?q=a",
		"/app", "/notifications", "/u/linkwalker",
	}
	sort.Strings(paths)
	for _, p := range paths {
		if status, _ := a.get(p); status != 200 {
			t.Errorf("%-42s → %d", p, status)
		}
	}
}

// A moderator reaches the content half of the console. The four admin-only
// destinations must answer 404 for them — and, just as importantly, must not
// be linked from the pages they can open, because a link that 404s on click
// tells them the route is there.
func TestModeratorSeesNoAdminOnlyLinks(t *testing.T) {
	a := newApp(t)
	a.register("modwalker")
	a.promote("modwalker", models.RoleModerator)

	for _, p := range []string{"/admin/users", "/admin/audit", "/admin/domains"} {
		if status, _ := a.get(p); status != 404 {
			t.Errorf("a moderator opening %s → %d, want 404", p, status)
		}
	}

	for _, p := range []string{"/admin", "/admin/categories", "/admin/questions",
		"/admin/review", "/admin/comments", "/admin/rated", "/admin/integrity",
		"/admin/support"} {
		status, body := a.get(p)
		if status != 200 {
			t.Fatalf("a moderator opening %s → %d", p, status)
		}
		for _, forbidden := range []string{`href="/admin/users"`, `href="/admin/audit"`, `href="/admin/domains"`} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s offers a moderator %s", p, forbidden)
			}
		}
	}
}

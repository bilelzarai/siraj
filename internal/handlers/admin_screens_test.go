package handlers_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

var adminPaths = map[string]string{
	"dashboard":  "/admin",
	"users":      "/admin/users",
	"newuser":    "/admin/users/new",
	"domains":    "/admin/domains",
	"domform":    "/admin/domains/new",
	"categories": "/admin/categories",
	"catform":    "/admin/categories/new",
	"questions":  "/admin/questions",
	"qform":      "/admin/questions/new",
	"import":     "/admin/questions/import",
	"review":     "/admin/review",
	"comments":   "/admin/comments",
	"rated":      "/admin/rated",
	"integrity":  "/admin/integrity",
	"audit":      "/admin/audit",
	"support":    "/admin/support",
}

// Every admin screen, checked for the things a redesign breaks without a
// trace: a page that slipped out of the console shell, a script layer that
// stopped being linked, or an icon the sprite does not define — which renders
// as an empty box and nothing else.
func TestEveryAdminScreenIsWiredToTheConsole(t *testing.T) {
	a := newApp(t)
	a.register("auditor")
	a.promote("auditor", models.RoleAdmin)

	for name, path := range adminPaths {
		status, body := a.get(path)
		if status != 200 {
			t.Errorf("%-11s %s → %d", name, path, status)
			continue
		}
		// Both script layers have to be on every admin page: the kit's, and
		// the application's — the confirmations, the bulk bar and the Alpine
		// components all live in the second one.
		for _, want := range []string{"/static/admin/admin.js", "assets/alpine-", "assets/app-"} {
			if !strings.Contains(body, want) {
				t.Errorf("%-11s is missing %s", name, want)
			}
		}
		// A page that links the sprite but never uses it, or uses a symbol
		// the sprite does not define, renders empty boxes.
		for _, m := range regexp.MustCompile(`icons\.svg[^#]*#(i-[a-z-]+)`).FindAllStringSubmatch(body, -1) {
			if !spriteHas(t, m[1]) {
				t.Errorf("%-11s references %s, which the sprite does not define", name, m[1])
			}
		}
		// One main region per document, and the console shell around it.
		if got := strings.Count(body, "<main"); got != 1 {
			t.Errorf("%-11s has %d <main> elements, want 1", name, got)
		}
		if !strings.Contains(body, `<div class="app">`) {
			t.Errorf("%-11s is not inside the console shell", name)
		}
		// The player's own chrome must not come with it: two navigations for
		// two different jobs, stacked, is what the console shell replaced.
		for _, playerChrome := range []string{`class="navbar`, `class="tabbar`} {
			if strings.Contains(body, playerChrome) {
				t.Errorf("%-11s still carries the player's %s", name, playerChrome)
			}
		}
		// And not the player's stylesheet. The two sheets share thirty-nine
		// class names, so loading both is decided by rule order rather than by
		// design — app.css's .nav supplied `height: var(--nav-h)` to the
		// console's sidebar and clipped every navigation item below the first.
		if regexp.MustCompile(`<link[^>]+dist/assets/app-[^"]*\.css`).MatchString(body) {
			t.Errorf("%-11s links the player stylesheet; the console must not", name)
		}
	}
}

var sprite string

func spriteHas(t *testing.T, id string) bool {
	t.Helper()
	if sprite == "" {
		b, err := os.ReadFile(repoRoot(t) + "/static/admin/icons.svg")
		if err != nil {
			t.Fatalf("reading the sprite: %v", err)
		}
		sprite = string(b)
	}
	return strings.Contains(sprite, `id="`+id+`"`)
}

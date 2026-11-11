package views

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A class in a template that no stylesheet defines.
//
// This is how the "open a room" screen shipped unreadable: it was written with
// class="container container--narrow", a name that exists in a great many
// other projects and in none of this one's CSS. Nothing failed. The page
// rendered, every element was present and correct, and the whole of it sat
// flush against the left edge of the window with no width, no centring and no
// padding — because the three classes carrying its layout styled nothing at
// all.
//
// That is the fault worth a test: a missing rule has no error, no warning and
// no console message. It looks exactly like a page somebody meant to leave
// bare, and the only way to find it is to look at it.

// unstyled is the set of class names that are deliberately in the markup
// without a rule behind them: hooks left for a style that was never needed.
// Each has been checked against both the stylesheet and the script, and none
// of them carries layout — they sit on elements that are already positioned by
// a parent (.sets is the grid; .sets__list and .sets__body are merely its two
// children) or are decorative names on an element the cascade already reaches.
//
// This list may shrink and may not grow. A new name here means a class was
// written that does nothing, which is the bug above.
var unstyled = map[string]bool{
	"chat__emoji-open":   true,
	"chat__tool--mic":    true,
	"dropdown__note":     true,
	"input--sm":          true,
	"reveal":             true,
	"section-title":      true,
	"sets__body":         true,
	"sets__list":         true,
	"stack--tight":       true,
	"tabs":               true,
	"ticket-stage__what": true,
}

// Which sheets a template may draw on.
//
// The console document links the two admin sheets and not the player's. That
// is not a preference: the two share thirty-nine class names — .nav, .brand,
// .btn, .table, .panel, .field, .page-head, .avatar — and loading both cannot
// be made safe by ordering, because a later rule only wins the properties it
// declares. The kit's .nav sets flex, overflow and padding; app.css's .nav
// kept supplying position, background and `height: var(--nav-h)`, which
// clamped the whole sidebar navigation to the height of the player's top bar.
//
// So an admin template that reaches for an app.css class is a class that will
// not be there, which is exactly what this test exists to catch.
var adminTemplates = map[string]bool{
	"admin.templ":               true,
	"admin_categories.templ":    true,
	"admin_compare.templ":       true,
	"admin_domains.templ":       true,
	"admin_import.templ":        true,
	"admin_overview.templ":      true,
	"admin_questions.templ":     true,
	"admin_review.templ":        true,
	"admin_search.templ":        true,
	"admin_taxonomy_form.templ": true,
	"admin_userpanel.templ":     true,
	"admin_users.templ":         true,
}

// These two hold a player screen and an admin screen side by side, so they
// legitimately draw on both and are checked against the union. Splitting them
// into four files to tighten this check is not worth it; what the check is
// really protecting is the list above, where it is exact.
var mixedTemplates = map[string]bool{
	"comments.templ": true,
	"support.templ":  true,
}

var (
	playerSheets = []string{"web/src/css/app.css"}
	adminSheets  = []string{
		"static/admin/admin.css",
		"static/admin/admin-app.css",
	}
	mixedSheets = []string{
		"web/src/css/app.css",
		"static/admin/admin.css",
		"static/admin/admin-app.css",
	}
)

// classesDefinedIn is every selector token in the given sheets, which covers
// compound selectors (.a.b), descendants and anything inside a media query — a
// class is defined if its name appears after a dot anywhere in a rule.
func classesDefinedIn(t *testing.T, root string, sheets []string) map[string]bool {
	t.Helper()
	defined := map[string]bool{}
	for _, sheet := range sheets {
		body, err := os.ReadFile(filepath.Join(root, sheet))
		if err != nil {
			t.Fatalf("reading %s: %v", sheet, err)
		}
		for _, m := range regexp.MustCompile(`\.([a-zA-Z][a-zA-Z0-9_-]*)`).
			FindAllStringSubmatch(string(body), -1) {
			defined[m[1]] = true
		}
	}
	if len(defined) < 100 {
		t.Fatalf("only %d classes found in %v; the check would pass vacuously",
			len(defined), sheets)
	}
	return defined
}

func TestEveryClassInATemplateIsStyledOrKnownNotToBe(t *testing.T) {
	root := repoRoot(t)

	player := classesDefinedIn(t, root, playerSheets)
	admin := classesDefinedIn(t, root, adminSheets)
	mixed := classesDefinedIn(t, root, mixedSheets)

	js := clientSources(t)
	// The admin script is not part of the bundled client tree — it is served
	// as-is from static/admin — so it has to be read separately or every
	// behavioural hook in the kit reads as a class that styles nothing.
	adminJS, err := os.ReadFile(filepath.Join(root, "static/admin/admin.js"))
	if err != nil {
		t.Fatalf("reading the admin script: %v", err)
	}
	adminScripts := js + "\n" + string(adminJS)

	// The plain class="..." attributes, and the binding attributes a converted
	// component uses instead.
	//
	// The second half is the whole point of this list. An Alpine conversion
	// moves a class out of class="…" and into :class or x-bind:class, where a
	// checker that only knows the first pattern stops seeing it — so the class
	// goes unchecked and the test goes quiet rather than red. Silence is the
	// failure mode worth designing against: a red test is noticed.
	//
	// A templ class expression builds its names from Go, and those are
	// deliberately out of scope: this is a check over what is written by hand.
	walkTempl(t, filepath.Join(root, "internal/views"), func(name, body string) {
		defined, scripts, where := player, js, "app.css"
		switch {
		case adminTemplates[name]:
			defined, scripts, where = admin, adminScripts, "the console stylesheets"
		case mixedTemplates[name]:
			defined, scripts, where = mixed, adminScripts, "app.css or the console stylesheets"
		}
		for _, class := range classesIn(body) {
			if defined[class] || unstyled[class] {
				continue
			}
			// A class the script hangs behaviour on is a class that does
			// something, even with no rule behind it.
			if strings.Contains(scripts, `"`+class+`"`) ||
				strings.Contains(scripts, "."+class+`"`) {
				continue
			}
			t.Errorf("%s uses class %q, which no rule in %s defines "+
				"and no script reads: it styles nothing", name, class, where)
		}
	})
}

// And the reverse of the list above: a file that no longer renders inside the
// admin shell must come off it, or it keeps a wider set of classes allowed
// than it can actually load.
func TestEveryAdminTemplateListedExists(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal/views")
	seen := map[string]bool{}
	walkTempl(t, root, func(name, _ string) { seen[name] = true })

	for _, list := range []map[string]bool{adminTemplates, mixedTemplates} {
		for name := range list {
			if !seen[name] {
				t.Errorf("%q is on a template list but no such template exists", name)
			}
		}
	}
}

var (
	// Preceded by whitespace, so `x-bind:class="…"` is not also read as a
	// plain attribute and counted twice.
	plainClass = regexp.MustCompile(`\sclass="([^"{}]+)"`)
	boundClass = regexp.MustCompile(`(?::|x-bind:)class="([^"{}]+)"`)
)

// classesIn is every class name written by hand in one template, wherever a
// conversion can put it.
func classesIn(body string) []string {
	var out []string
	for _, m := range plainClass.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.Fields(m[1])...)
	}
	for _, m := range boundClass.FindAllStringSubmatch(body, -1) {
		out = append(out, literalClasses(m[1])...)
	}
	return out
}

// literalClasses pulls the names out of a binding's expression.
//
// A binding is an expression, not a list: `open ? 'is-on' : 'is-off'` names two
// classes and `sheet.state` names none this test can see. The quoted literals
// are the checkable part; everything else is left alone rather than guessed at,
// because a checker that reports what it cannot read is one people learn to
// ignore.
func literalClasses(value string) []string {
	var out []string
	var quote rune
	var current strings.Builder
	for _, r := range value {
		switch {
		case quote == 0 && (r == '\'' || r == '"' || r == '`'):
			quote = r
		case quote != 0 && r == quote:
			out = append(out, strings.Fields(current.String())...)
			current.Reset()
			quote = 0
		case quote != 0:
			current.WriteRune(r)
		}
	}
	return out
}

// And the reverse, for the list above: a name that has since been given a
// rule should come off it, or the list stops meaning anything.
func TestTheUnstyledListHasNoStaleEntries(t *testing.T) {
	sheet, err := os.ReadFile(filepath.Join(repoRoot(t), "web/src/css/app.css"))
	if err != nil {
		t.Fatalf("reading the stylesheet: %v", err)
	}
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`\.([a-zA-Z][a-zA-Z0-9_-]*)`).
		FindAllStringSubmatch(string(sheet), -1) {
		defined[m[1]] = true
	}
	for class := range unstyled {
		if defined[class] {
			t.Errorf("%q is styled now and should come off the unstyled list", class)
		}
	}
}

// The checker must see a class wherever a conversion can put it. Before this,
// moving a name from class="…" into a binding attribute took it out of the
// check silently — the test went quiet rather than red, which is the failure
// nobody notices.
func TestTheClassCheckerReadsBindingAttributesToo(t *testing.T) {
	cases := []struct {
		name   string
		markup string
		want   []string
	}{
		{"a plain attribute", `<div class="card card--pad">`, []string{"card", "card--pad"}},
		{"a shorthand binding", `<div :class="'is-on'">`, []string{"is-on"}},
		{"a long-form binding", `<div x-bind:class="'chip-radio__face'">`, []string{"chip-radio__face"}},
		{"both branches of a conditional", `<div :class="open ? 'is-open' : 'is-shut'">`,
			[]string{"is-open", "is-shut"}},
		{"an expression naming nothing literal", `<div :class="sheet.state">`, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := classesIn(tc.markup)
			if strings.Join(seen, " ") != strings.Join(tc.want, " ") {
				t.Errorf("read %v, want %v", seen, tc.want)
			}
		})
	}
}

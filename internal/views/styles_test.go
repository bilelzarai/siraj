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

func TestEveryClassInATemplateIsStyledOrKnownNotToBe(t *testing.T) {
	root := repoRoot(t)

	sheet, err := os.ReadFile(filepath.Join(root, "static/css/app.css"))
	if err != nil {
		t.Fatalf("reading the stylesheet: %v", err)
	}
	// Every selector token in the file, which covers compound selectors
	// (.a.b), descendants and anything inside a media query — a class is
	// defined if its name appears after a dot anywhere in a rule.
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`\.([a-zA-Z][a-zA-Z0-9_-]*)`).
		FindAllStringSubmatch(string(sheet), -1) {
		defined[m[1]] = true
	}
	if len(defined) < 100 {
		t.Fatalf("only %d classes found in the stylesheet; the check would pass vacuously",
			len(defined))
	}

	script, err := os.ReadFile(filepath.Join(root, "static/js/app.js"))
	if err != nil {
		t.Fatalf("reading the script: %v", err)
	}
	js := string(script)

	// Only the plain class="..." attributes. A templ class expression builds
	// its names from Go, and this is a check over what is written by hand.
	attr := regexp.MustCompile(`class="([^"{}]+)"`)

	walkTempl(t, filepath.Join(root, "internal/views"), func(name, body string) {
		for _, m := range attr.FindAllStringSubmatch(body, -1) {
			for _, class := range strings.Fields(m[1]) {
				if defined[class] || unstyled[class] {
					continue
				}
				// A class the script hangs behaviour on is a class that does
				// something, even with no rule behind it.
				if strings.Contains(js, `"`+class+`"`) ||
					strings.Contains(js, "."+class+`"`) {
					continue
				}
				t.Errorf("%s uses class %q, which no rule in app.css defines "+
					"and no script reads: it styles nothing", name, class)
			}
		}
	})
}

// And the reverse, for the list above: a name that has since been given a
// rule should come off it, or the list stops meaning anything.
func TestTheUnstyledListHasNoStaleEntries(t *testing.T) {
	sheet, err := os.ReadFile(filepath.Join(repoRoot(t), "static/css/app.css"))
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

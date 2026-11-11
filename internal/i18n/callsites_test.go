package i18n_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/i18n"
)

// A translation with a %d in it, printed by a call site that passes nothing, is
// a button that says "Send challenge (%d)" to a person.
//
// The existing catalogue tests compare the locales to each other: every locale
// has every key, and the verbs match across them. Neither of those notices a
// key whose text takes an argument and a call site that gives it none — which
// is exactly how that button reached a screen. This closes that gap by reading
// the call sites.
func TestEveryFormatKeyIsCalledWithArguments(t *testing.T) {
	bundle, err := i18n.New(i18n.DefaultLocale)
	if err != nil {
		t.Fatalf("loading catalogs: %v", err)
	}

	// c.T("some.key") with nothing after the key. A call that passes arguments
	// has a comma before the closing bracket, so this matches only the bare
	// form.
	bare := regexp.MustCompile(`\bc\.T\(\s*"([^"]+)"\s*\)`)
	// Anything Sprintf would consume. %% is an escaped percent and takes no
	// argument.
	verb := regexp.MustCompile(`%[^%]`)

	root := repoRoot(t)
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			name := info.Name()
			if !strings.HasSuffix(name, ".templ") && !strings.HasSuffix(name, ".go") {
				return nil
			}
			if strings.HasSuffix(name, "_templ.go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range bare.FindAllStringSubmatch(string(body), -1) {
				key := m[1]
				// Every shipped language, not only the fallback: a verb left
				// in the French text is printed to everybody reading French,
				// and checking one catalogue would not see it.
				for _, loc := range i18n.Supported {
					text := bundle.Printer(loc.Code).T(key)
					// A key the catalogue does not define comes back as
					// itself; the completeness test owns that case.
					if text == key || !verb.MatchString(text) {
						continue
					}
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s calls c.T(%q) with no arguments, but the %s text is %q — "+
						"the verb is printed to the reader", rel, key, loc.Code, text)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

// repoRoot climbs out of the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the module root")
	return ""
}

// And the reverse: a key with no format verb, called with arguments.
//
// fmt appends "%!(EXTRA string=…)" to the rendered string, so the screen shows
// the label followed by a parser error. It has happened twice — a "Points"
// column label called with a number, and a "Joined" label called with a date —
// because the catalogue is flat and a key that reads like a sentence and one
// that reads like a column header look the same at the call site.
func TestNoPlainKeyIsCalledWithArguments(t *testing.T) {
	bundle, err := i18n.New(i18n.DefaultLocale)
	if err != nil {
		t.Fatalf("loading catalogs: %v", err)
	}

	// c.T("some.key", …) — a call that passes at least one argument.
	withArgs := regexp.MustCompile(`\bc\.T\(\s*"([^"]+)"\s*,`)
	verb := regexp.MustCompile(`%[^%]`)

	root := repoRoot(t)
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			name := info.Name()
			if !strings.HasSuffix(name, ".templ") && !strings.HasSuffix(name, ".go") {
				return nil
			}
			if strings.HasSuffix(name, "_templ.go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range withArgs.FindAllStringSubmatch(string(body), -1) {
				key := m[1]
				for _, loc := range i18n.Supported {
					text := bundle.Printer(loc.Code).T(key)
					// A key the catalogue does not define comes back as
					// itself; the completeness test owns that case.
					if text == key {
						continue
					}
					if !verb.MatchString(text) {
						t.Errorf("%s calls %q with arguments, but its %s text is %q — "+
							"fmt appends a parser error to it",
							name, key, loc.Code, text)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

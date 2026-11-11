package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLocalesHaveTheSameKeys(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range Langs {
		if missing := b.MissingKeys(lang); len(missing) > 0 {
			t.Errorf("%s.json is missing %d keys: %v", lang, len(missing), missing)
		}
		if extra := b.ExtraKeys(lang); len(extra) > 0 {
			t.Errorf("%s.json has %d keys en.json does not: %v", lang, len(extra), extra)
		}
	}
}

func TestFallbackAndPlaceholders(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := b.For("ar").T("nope.missing"); got != "nope.missing" {
		t.Errorf("missing key should return itself, got %q", got)
	}
	if got := b.For("zz").Lang; got != Fallback {
		t.Errorf("unknown language should fall back to %s, got %s", Fallback, got)
	}
	if got := b.For("en").T("home.greeting.member", "name", "Aisha"); got != "Welcome back, Aisha" {
		t.Errorf("placeholder not replaced: %q", got)
	}
	if Dir("ar") != "rtl" || Dir("en") != "ltr" {
		t.Error("direction is wrong")
	}
}

// Every key a template or a handler asks for has to be in the locale files, or the
// screen prints the key itself. Keys built by joining a prefix to a value at run
// time ("notif." + kind) end in a dot or an underscore and are skipped: only the
// whole ones written in the source can be checked here.
func TestEveryKeyTheCodeAsksForExists(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	asked := regexp.MustCompile(`Tr\("([a-z0-9_.]+)"`)
	seen := map[string][]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "siraj-v2-dev-kit", "var", "tmp":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
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
		for _, m := range asked.FindAllSubmatch(body, -1) {
			key := string(m[1])
			rel, _ := filepath.Rel(root, path)
			seen[key] = append(seen[key], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	en := b.For(Fallback)
	for key, where := range seen {
		if strings.HasSuffix(key, ".") || strings.HasSuffix(key, "_") {
			continue // a prefix joined to something at run time
		}
		if en.T(key) == key {
			t.Errorf("no locale has %q, asked for in %s", key, strings.Join(where, ", "))
		}
	}
}

package views

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The policy-safe build evaluates nothing: a directive carries the name of
// something on the component and never a snippet of JavaScript. A template
// that writes an expression works on a developer's machine with the default
// build and fails silently under the one this application ships — silently,
// because an expression Alpine cannot evaluate simply does nothing.
//
// This is the check that keeps a conversion inside the dialect.
func TestEveryAlpineDirectiveNamesSomethingRatherThanEvaluatingIt(t *testing.T) {
	root := repoRoot(t)

	// x-on and x-bind take a name; x-show, x-if and x-text take a property.
	directive := regexp.MustCompile(`\b(?:x-on:[a-z.]+|@[a-z.]+|x-bind:[a-z-]+|:[a-z-]+|x-show|x-text|x-html|x-model)="([^"]*)"`)
	// A bare name, a dotted path, or a negated one. Anything else — a call, an
	// operator, a literal, a ternary — is an expression.
	plain := regexp.MustCompile(`^!?[A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)

	walkTempl(t, filepath.Join(root, "internal/views"), func(name, body string) {
		for _, m := range directive.FindAllStringSubmatch(body, -1) {
			value := strings.TrimSpace(m[1])
			// templ writes its own attributes with { } expressions, and those
			// are Go, resolved before the browser ever sees them.
			if value == "" || strings.Contains(value, "{") {
				continue
			}
			if !plain.MatchString(value) {
				t.Errorf("%s: %s is an expression, which the policy-safe build "+
					"cannot evaluate — it has to name something on the component",
					name, m[0])
			}
		}
	})
}

// Every component a template summons has to be registered, or the markup is
// inert and nothing says so.
func TestEveryComponentSummonedIsRegistered(t *testing.T) {
	root := repoRoot(t)

	registry, err := os.ReadFile(filepath.Join(root, "web/src/js/alpine.js"))
	if err != nil {
		t.Fatalf("reading the registry: %v", err)
	}
	summoned := regexp.MustCompile(`x-data="([A-Za-z_$][A-Za-z0-9_$]*)"`)

	seen := map[string]bool{}
	walkTempl(t, filepath.Join(root, "internal/views"), func(name, body string) {
		for _, m := range summoned.FindAllStringSubmatch(body, -1) {
			seen[m[1]] = true
			if !strings.Contains(string(registry), `Alpine.data("`+m[1]+`"`) {
				t.Errorf("%s summons %q, which alpine.js never registers: the markup is inert",
					name, m[1])
			}
		}
	})

	// And the reverse: a registration nothing summons is weight on every page.
	registered := regexp.MustCompile(`Alpine\.data\("([A-Za-z_$][A-Za-z0-9_$]*)"`)
	for _, m := range registered.FindAllStringSubmatch(string(registry), -1) {
		if !seen[m[1]] {
			t.Errorf("alpine.js registers %q, which no template summons", m[1])
		}
	}
}

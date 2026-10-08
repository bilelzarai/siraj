package views

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A page says which events make it out of date. Every one of those has to be an
// event the server actually publishes — a page waiting for `friend.accept`
// while the hub sends `friend.accepted` waits forever, and the failure is
// silent: the screen simply stays wrong, which is the bug this mechanism
// exists to fix.
func TestLiveEventsAreOnesTheServerSends(t *testing.T) {
	root := repoRoot(t)

	// What the server can publish, read from the one place the names are
	// declared. Scanning call sites instead would miss the ones assigned to a
	// variable first — which is how this check first reported two events as
	// missing that the server sends on every settled match.
	published := map[string]bool{}
	walkGo(t, filepath.Join(root, "internal/service"), func(body string) {
		for _, m := range regexp.MustCompile(`Event[A-Za-z]+\s+=\s+"([a-z.]+)"`).FindAllStringSubmatch(body, -1) {
			published[m[1]] = true
		}
	})
	if len(published) == 0 {
		t.Fatal("found no declared events; the check would pass vacuously")
	}

	// What the templates listen for.
	declared := map[string][]string{}
	walkTempl(t, filepath.Join(root, "internal/views"), func(name, body string) {
		for _, m := range regexp.MustCompile(`data-live="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			for _, event := range strings.Fields(m[1]) {
				declared[event] = append(declared[event], name)
			}
		}
	})
	if len(declared) == 0 {
		t.Fatal("no page declares what makes it stale")
	}

	for event, pages := range declared {
		if !published[event] {
			t.Errorf("%s waits for %q, which nothing publishes", strings.Join(pages, ", "), event)
		}
	}
}

// The browser subscribes by name too. An event the server publishes and the
// script never listens for is a page that stays stale for the one reason the
// mechanism cannot see: nobody was listening.
func TestTheClientSubscribesToEveryEvent(t *testing.T) {
	root := repoRoot(t)

	published := map[string]bool{}
	walkGo(t, filepath.Join(root, "internal/service"), func(body string) {
		for _, m := range regexp.MustCompile(`Event[A-Za-z]+\s+=\s+"([a-z.]+)"`).FindAllStringSubmatch(body, -1) {
			published[m[1]] = true
		}
	})

	listening := clientSources(t)

	for event := range published {
		if !strings.Contains(listening, `"`+event+`"`) {
			t.Errorf("the server publishes %q and the browser never subscribes to it", event)
		}
	}
}

func walkGo(t *testing.T, dir string, fn func(body string)) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		fn(string(body))
	}
}

func walkTempl(t *testing.T, dir string, fn func(name, body string)) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".templ") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		fn(e.Name(), string(body))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("no go.mod above the working directory")
	return ""
}

// A dropdown is styled by its class and wired by its attribute, and nothing
// complains when a template has only one of them.
//
// The conversation menu shipped as class="dropdown" with no data-dropdown, so
// the script never bound it: a button that looked exactly right and did
// nothing at all when pressed.
func TestEveryDropdownIsWiredAndNotJustStyled(t *testing.T) {
	walkTempl(t, filepath.Join(repoRoot(t), "internal/views"), func(name, body string) {
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, `class="dropdown"`) &&
				!strings.Contains(line, `class="dropdown `) {
				continue
			}
			if !strings.Contains(line, "data-dropdown") {
				t.Errorf("%s: a dropdown with no data-dropdown, which the script binds on:\n  %s",
					name, strings.TrimSpace(line))
			}
		}
	})
}

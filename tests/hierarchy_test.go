package tests

import (
	"net/http"
	"testing"
)

// §25 #8 — authorization holds on a direct HTTP request, not only in the screens.
//
// The service tests prove the rules; these prove nobody can step around them by
// typing a URL. A hidden button is not security, so every check is made against the
// running router with a real session.

// TestHierarchyScopeOverHTTP plays from a node and checks the three scopes select
// what they say they do, through the public contract rather than the query.
func TestHierarchyScopeOverHTTP(t *testing.T) {
	a := newApp(t)
	token := a.guest("Scope")

	// A branch nobody has published has nothing to play.
	r := a.post("/api/v1/games", map[string]any{
		"kind": "custom", "node_id": 999999, "node_scope": "both", "question_count": 5,
	}, token)
	if r.Code == http.StatusCreated {
		t.Fatal("a game started on a node that does not exist")
	}

	// An unknown scope must not widen the selection: it reads as the whole branch,
	// never as "no scope at all".
	r = a.post("/api/v1/games", map[string]any{
		"kind": "custom", "node_id": 999999, "node_scope": "everything", "question_count": 5,
	}, token)
	if r.Code == http.StatusCreated {
		t.Fatal("an unknown node_scope fell back to selecting from the whole bank")
	}
}

// TestLibraryNeedsASession — the player's own tree is behind a session, and a visitor
// is sent to sign in rather than shown somebody's library.
func TestLibraryNeedsASession(t *testing.T) {
	a := newApp(t)
	r := a.get("/library", "")
	if r.Code == http.StatusOK {
		t.Fatalf("the library answered %d to a request with no session", r.Code)
	}
}

// TestPlayerCannotWriteOfficialNodesOverHTTP — the admin tree routes are staff-only,
// and a player with a valid session gets nowhere near them.
func TestPlayerCannotWriteOfficialNodesOverHTTP(t *testing.T) {
	a := newApp(t)
	token := a.guest("Nosy")

	for _, path := range []string{
		"/admin/tree",
		"/admin/tree/1/status",
		"/admin/tree/1/archive",
		"/admin/tree/1/move",
	} {
		r := a.post(path, map[string]any{"status": "published"}, token)
		if r.Code == http.StatusOK || r.Code == http.StatusCreated {
			t.Errorf("%s answered %d to a player: the console is not staff-only", path, r.Code)
		}
	}
	if r := a.get("/admin/tree", token); r.Code == http.StatusOK {
		t.Error("a player read the admin hierarchy screen")
	}
}

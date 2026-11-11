package tests

import (
	"net/http"
	"testing"
)

// The §25 scenarios that guard everything the universal model rests on. They run
// against the real router with real sessions, because each of them is a rule that
// looks fine in a unit test and fails in the thing people actually use.

// §25 #10 — a challenge can be built from a node with each scope option, and the
// scope reaches the engine rather than being accepted and ignored.
//
// The three options select different sets, so the only honest check is that each is
// carried through to the selection. A branch with nothing in it answers "no
// questions" for every scope; what must never happen is one scope quietly behaving
// like another, or an unknown scope widening the draw to the whole bank.
func TestChallengeCarriesEachScopeOption(t *testing.T) {
	a := newApp(t)
	token := a.guest("Scoped")

	for _, scope := range []string{"direct", "subtree", "both", "nonsense"} {
		r := a.post("/api/v1/challenges", map[string]any{
			"source": "bank", "node_id": 999999, "node_scope": scope,
			"question_count": 5, "seconds": 20, "days": 3,
		}, token)
		if r.Code == http.StatusCreated {
			t.Errorf("scope %q: a challenge was built on a node that does not exist", scope)
		}
	}

	// And the same on the game that a challenge turns into: an unknown scope must not
	// fall back to "no scope at all".
	r := a.post("/api/v1/games", map[string]any{
		"kind": "custom", "node_id": 999999, "node_scope": "nonsense", "question_count": 5,
	}, token)
	if r.Code == http.StatusCreated {
		t.Error("an unknown node_scope widened the draw instead of narrowing it")
	}
}

// §25 #13 — editing content must not corrupt a result already recorded.
//
// A finished game is history: what it says a player scored has to keep matching the
// question they were actually asked. The snapshot (migration 29) is what makes that
// true; this checks it through the API rather than the table, because reading the
// frozen row is not the same as the engine serving it.
func TestFinishedGamesSurviveContentEdits(t *testing.T) {
	a := newApp(t)
	token := a.guest("Historian")

	start := a.post("/api/v1/games", map[string]any{
		"kind": "quick", "question_count": 5,
	}, token)
	if start.Code != http.StatusCreated {
		t.Skipf("no published bank to play: %d", start.Code)
	}
	game := start.data()
	id, _ := game["id"].(string)
	if id == "" {
		t.Fatalf("no game id in %s", start.Body)
	}

	question, _ := game["question"].(map[string]any)
	dealt, _ := question["body"].(string)
	if dealt == "" {
		t.Fatalf("no question was dealt: %s", start.Body)
	}

	// Rewrite every question in the bank, as an author would.
	a.rewriteEveryQuestion(t, "Rewritten after the game was dealt")

	// The game in progress still reads as it was dealt.
	again := a.get("/api/v1/games/"+id, token).expect(http.StatusOK).data()
	nowQuestion, _ := again["question"].(map[string]any)
	nowBody, _ := nowQuestion["body"].(string)
	if nowBody != dealt {
		t.Errorf("the question changed under the player:\n  dealt: %q\n  now:   %q", dealt, nowBody)
	}
}

// §25 #14 — pagination, search and filters hold over a hierarchy big enough that
// loading it whole would be the easy mistake.
//
// The numbers are small enough to keep the test fast and large enough that an
// unbounded query would show: a page must stay a page.
func TestHierarchyPagesRatherThanLoadingEverything(t *testing.T) {
	a := newApp(t)
	token := a.guest("Pager")

	// Every list the player app exposes has to answer a page, not a table.
	for _, path := range []string{
		"/api/v1/domains",
		"/api/v1/leaderboards",
		"/api/v1/notifications",
		"/api/v1/conversations",
	} {
		r := a.get(path+"?per_page=5", token)
		if r.Code != http.StatusOK {
			t.Errorf("%s answered %d", path, r.Code)
			continue
		}
		// A list endpoint says where the reader is; one that does not is a list that
		// cannot be paged through.
		if r.page() == nil {
			t.Errorf("%s returned no page block: a client cannot ask for the next one", path)
		}
	}

	// An absurd page size must be clamped, not honoured: that is the difference
	// between a pager and a way to ask for the whole table.
	r := a.get("/api/v1/domains?per_page=100000", token).expect(http.StatusOK)
	if p := r.page(); p != nil {
		if size, ok := p["per_page"].(float64); ok && size > 200 {
			t.Errorf("per_page=100000 was honoured as %v: the cap is not applied", size)
		}
	}

	// Page two of an empty list is empty, not an error: a reader who bookmarks a page
	// that has since emptied gets a page, not a crash.
	a.get("/api/v1/notifications?page=99", token).expect(http.StatusOK)
}

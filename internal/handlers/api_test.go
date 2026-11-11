package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/handlers/api"
	"github.com/bilelzarai/siraj/internal/models"
)

// The one way into this system that is not a browser session. What matters
// most here is not that it answers — it is what it refuses to answer with, and
// what it will not serve at all.

func (a *app) mintKey(t *testing.T, label string) string {
	t.Helper()
	plaintext, hash, err := api.Mint()
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if _, err := a.repo.CreateAPIKey(t.Context(), label, hash, nil); err != nil {
		t.Fatalf("storing the key: %v", err)
	}
	return plaintext
}

func (a *app) apiGet(t *testing.T, path, key string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, a.server.URL+path, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	return res.StatusCode, readAll(res)
}

func TestTheEndpointAnswersOnlyAKeyItKnows(t *testing.T) {
	a := newApp(t)
	key := a.mintKey(t, "known")

	for _, tc := range []struct {
		name   string
		header string
		want   int
	}{
		{"no header at all", "", http.StatusUnauthorized},
		{"a key nobody minted", "siraj_" + strings.Repeat("A", 43), http.StatusUnauthorized},
		{"a minted key", key, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := a.apiGet(t, "/api/v1/questions?locale=en", tc.header)
			if status != tc.want {
				t.Errorf("→ %d, want %d", status, tc.want)
			}
		})
	}

	// An unknown key and no key answer the same way on purpose: a different
	// answer would make this endpoint a way to find out which keys exist.
	noHeader, _ := a.apiGet(t, "/api/v1/questions", "")
	unknown, _ := a.apiGet(t, "/api/v1/questions", "siraj_"+strings.Repeat("B", 43))
	if noHeader != unknown {
		t.Errorf("no key → %d but an unknown key → %d; the difference tells a caller which keys exist",
			noHeader, unknown)
	}
}

func TestARevokedKeyIsToldSoAndLastUseIsRecorded(t *testing.T) {
	a := newApp(t)
	plaintext := a.mintKey(t, "to be revoked")

	if status, _ := a.apiGet(t, "/api/v1/questions?locale=en", plaintext); status != http.StatusOK {
		t.Fatalf("a live key → %d", status)
	}

	keys, err := a.repo.APIKeys(t.Context())
	if err != nil {
		t.Fatalf("listing keys: %v", err)
	}
	var id int
	for _, k := range keys {
		if k.Label == "to be revoked" {
			id = k.ID
			if k.LastUsedAt == nil {
				t.Error("a key answered a request and its last use was not recorded")
			}
		}
	}

	if err := a.repo.RevokeAPIKey(t.Context(), id); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	// 403, not 401: "this credential is finished" is a different thing to be
	// told than "I do not know you", and a consumer needs to tell them apart.
	if status, _ := a.apiGet(t, "/api/v1/questions?locale=en", plaintext); status != http.StatusForbidden {
		t.Errorf("a revoked key → %d, want 403", status)
	}
}

// The rule the whole project is built on, seen from outside: machine output
// and imports are held until a human approves them, and this surface inherits
// that rather than restating it.
func TestTheEndpointServesNothingAwaitingReview(t *testing.T) {
	a := newApp(t)
	key := a.mintKey(t, "review check")
	ctx := t.Context()

	cats, err := a.repo.Categories(ctx, "en", 0)
	if err != nil || len(cats) == 0 {
		t.Fatalf("no category to file a question under: %v", err)
	}
	id, err := a.repo.UpsertQuestion(ctx, &models.QuestionDraft{
		ID: 970001, CategoryID: cats[0].ID, Difficulty: 1, Points: 10,
		CorrectIndex: 0, IsActive: true,
		Translations: map[string]models.TranslationDraft{
			// Reviewed in English, awaiting review in French — the same
			// question, servable in one language and held in the other.
			"en": {Prompt: "Reviewed in English", Choices: []string{"a", "b", "c", "d"},
				Source: "human", NeedsReview: false},
			"fr": {Prompt: "En attente de relecture", Choices: []string{"a", "b", "c", "d"},
				Source: "machine", NeedsReview: true},
		},
	})
	if err != nil {
		t.Fatalf("writing the question: %v", err)
	}

	served := func(t *testing.T, locale string) bool {
		t.Helper()
		status, body := a.apiGet(t, "/api/v1/questions?limit=200&locale="+locale, key)
		if status != http.StatusOK {
			t.Fatalf("locale %s → %d", locale, status)
		}
		var page struct {
			Data []struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		for _, q := range page.Data {
			if q.ID == id {
				return true
			}
		}
		return false
	}

	if !served(t, "en") {
		t.Error("a reviewed translation is not served")
	}
	if served(t, "fr") {
		t.Error("a translation awaiting review reached the public endpoint")
	}
}

// A page, a cursor, and a total that agrees with them.
func TestThePageAndItsCursorAgreeWithTheTotal(t *testing.T) {
	a := newApp(t)
	key := a.mintKey(t, "paging")
	seedQuestions(a, 960000, 7)

	status, body := a.apiGet(t, "/api/v1/questions?locale=en&limit=3", key)
	if status != http.StatusOK {
		t.Fatalf("first page → %d", status)
	}
	var first struct {
		Data []struct {
			ID       int                   `json:"id"`
			Category struct{ Slug string } `json:"category"`
			Domain   struct{ Slug string } `json:"domain"`
		} `json:"data"`
		NextCursor string `json:"next_cursor"`
		Total      int    `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(first.Data) != 3 || first.NextCursor == "" {
		t.Fatalf("got %d rows and cursor %q, want a full page and a cursor",
			len(first.Data), first.NextCursor)
	}
	if first.Total < len(first.Data) {
		t.Errorf("total %d is smaller than the page it came with", first.Total)
	}
	// Every row says which subject area it belongs to — the field the endpoint
	// was made to carry from its first version rather than gain later.
	for _, q := range first.Data {
		if q.Domain.Slug == "" {
			t.Errorf("question %d is served without its subject area", q.ID)
		}
	}

	_, body = a.apiGet(t, "/api/v1/questions?locale=en&limit=3&cursor="+first.NextCursor, key)
	var second struct {
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &second); err != nil {
		t.Fatalf("decoding the second page: %v", err)
	}
	for _, a := range first.Data {
		for _, b := range second.Data {
			if a.ID == b.ID {
				t.Errorf("question %d is on both pages, so the cursor skips or repeats", a.ID)
			}
		}
	}
}

func TestTheEndpointRefusesWhatItCannotActOn(t *testing.T) {
	a := newApp(t)
	key := a.mintKey(t, "refusals")

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{"a locale nobody ships", "/api/v1/questions?locale=de", http.StatusBadRequest},
		{"a difficulty off the scale", "/api/v1/questions?difficulty=9", http.StatusBadRequest},
		{"a cursor it did not issue", "/api/v1/questions?cursor=not-a-cursor", http.StatusBadRequest},
		{"a route that is not there", "/api/v1/answers", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status, _ := a.apiGet(t, tc.path, key); status != tc.want {
				t.Errorf("→ %d, want %d", status, tc.want)
			}
		})
	}

	// Read-only is the decision, so a write is refused by the surface itself
	// rather than by a handler that happens not to exist.
	req, _ := http.NewRequest(http.MethodPost, a.server.URL+"/api/v1/questions", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST → %d, want 405", res.StatusCode)
	}
	_ = fmt.Sprint()
}

// The two prefixes mean opposite things and must not be reachable from each
// other's side. /ui is the interface fetching for itself, inside the session
// chain; /api/v1 is mounted above that chain with a bearer key and reads no
// cookie.
func TestTheTwoSurfacesCannotBeReachedWithEachOthersCredential(t *testing.T) {
	a := newApp(t)
	a.register("surfaces")
	key := a.mintKey(t, "surfaces")

	// A session reaches the interface endpoints.
	for _, path := range []string{"/ui/counts", "/ui/people?scope=friends&q="} {
		if status, _ := a.get(path); status != http.StatusOK {
			t.Errorf("GET %s with a session → %d, want 200", path, status)
		}
	}

	// A bearer key does not: those routes are behind the session layer, so a
	// key is simply not a credential they understand.
	for _, path := range []string{"/ui/counts", "/ui/people?scope=friends&q="} {
		anon := newAppSharing(t, a)
		if status, _ := anon.apiGet(t, path, key); status == http.StatusOK {
			t.Errorf("GET %s answered a bearer key, so the two surfaces overlap", path)
		}
	}

	// And the old prefix is gone, rather than quietly still working.
	for _, path := range []string{"/api/people?scope=friends&q=", "/api/counts"} {
		if status, _ := a.get(path); status == http.StatusOK {
			t.Errorf("GET %s still answers; the move left a second way in", path)
		}
	}
}

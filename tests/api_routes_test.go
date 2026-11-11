package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every endpoint of docs/backend-architecture.md, in the shape gin registers it.
// The list is the contract: a module that stops mounting one of these fails here
// rather than in an app (T-52).
var contract = []string{
	// Identity 1–16
	"POST /api/v1/auth/register", "POST /api/v1/auth/login", "POST /api/v1/auth/google",
	"POST /api/v1/auth/guest", "POST /api/v1/auth/guest/upgrade", "POST /api/v1/auth/refresh",
	"POST /api/v1/auth/logout", "POST /api/v1/auth/password/forgot", "POST /api/v1/auth/password/reset",
	"GET /api/v1/auth/username-available", "GET /api/v1/me", "PATCH /api/v1/me",
	"PATCH /api/v1/me/settings", "POST /api/v1/me/onboarding", "POST /api/v1/me/devices",
	"DELETE /api/v1/me",
	// Catalog 17–23, 114–117
	"GET /api/v1/domains", "GET /api/v1/domains/:id", "GET /api/v1/domains/:id/categories",
	"GET /api/v1/categories/:id", "GET /api/v1/search",
	"GET /api/v1/daily-question", "POST /api/v1/daily-question/answer",
	"GET /api/v1/me/domains", "PUT /api/v1/me/domains",
	"POST /api/v1/me/domains/:id", "DELETE /api/v1/me/domains/:id",
	// Games 24–31
	"POST /api/v1/games", "GET /api/v1/games/:id", "POST /api/v1/games/:id/answers",
	"POST /api/v1/games/:id/fifty", "POST /api/v1/games/:id/skip", "POST /api/v1/games/:id/finish",
	"GET /api/v1/games/:id/results", "GET /api/v1/me/games",
	// Questions 32–35
	"PUT /api/v1/questions/:id/rating", "GET /api/v1/questions/:id/comments",
	"POST /api/v1/questions/:id/comments", "POST /api/v1/questions/:id/reports",
	// Rooms 36–46, 119
	"POST /api/v1/rooms", "GET /api/v1/rooms/:code", "POST /api/v1/rooms/:code/join",
	"POST /api/v1/rooms/:code/leave", "PATCH /api/v1/rooms/:code", "POST /api/v1/rooms/:code/ready",
	"DELETE /api/v1/rooms/:code/players/:userId", "POST /api/v1/rooms/:code/start",
	"GET /api/v1/rooms/:code/messages", "POST /api/v1/rooms/:code/messages",
	"GET /api/v1/rooms/:code/podium", "POST /api/v1/rooms/:code/teams/shuffle",
	// Duels 47–50
	"POST /api/v1/duels/queue", "DELETE /api/v1/duels/queue", "GET /api/v1/duels/:id",
	"GET /api/v1/me/duel-ratings",
	// Challenges 51–62, 118
	"GET /api/v1/challenges", "GET /api/v1/challenges/stats", "POST /api/v1/challenges",
	"GET /api/v1/challenges/:id", "POST /api/v1/challenges/:id/accept",
	"POST /api/v1/challenges/:id/decline", "DELETE /api/v1/challenges/:id",
	"POST /api/v1/challenges/:id/remind", "POST /api/v1/challenges/:id/rematch",
	"GET /api/v1/challenge-links/:code", "POST /api/v1/challenges/random-opponent",
	"POST /api/v1/local-rounds", "POST /api/v1/challenges/:id/close",
	// Social 63–74
	"GET /api/v1/friends", "DELETE /api/v1/friends/:userId", "GET /api/v1/friend-requests",
	"POST /api/v1/friend-requests", "POST /api/v1/friend-requests/:id/accept",
	"POST /api/v1/friend-requests/:id/decline", "GET /api/v1/users/search",
	"GET /api/v1/users/:username", "POST /api/v1/blocks", "DELETE /api/v1/blocks/:userId",
	"POST /api/v1/invites",
	// Messages 75–86
	"GET /api/v1/conversations", "POST /api/v1/conversations", "GET /api/v1/conversations/:id",
	"PATCH /api/v1/conversations/:id", "POST /api/v1/conversations/:id/read",
	"POST /api/v1/conversations/:id/unread", "DELETE /api/v1/conversations/:id",
	"GET /api/v1/conversations/:id/messages", "POST /api/v1/conversations/:id/messages",
	"DELETE /api/v1/messages/:id", "POST /api/v1/messages/:id/reports", "POST /api/v1/uploads",
	// Notifications 87–89
	"GET /api/v1/notifications", "POST /api/v1/notifications/read",
	"GET /api/v1/notifications/unread-count",
	// Progress 90–95
	"GET /api/v1/me/progress", "GET /api/v1/me/mastery", "GET /api/v1/me/mastery/domains/:id",
	"GET /api/v1/me/achievements", "GET /api/v1/me/activity", "GET /api/v1/leaderboards",
	// Sets 96–105
	"GET /api/v1/sets", "POST /api/v1/sets", "GET /api/v1/sets/:id", "PATCH /api/v1/sets/:id",
	"DELETE /api/v1/sets/:id", "POST /api/v1/sets/:id/questions",
	"PATCH /api/v1/sets/:id/questions/:qid", "DELETE /api/v1/sets/:id/questions/:qid",
	"POST /api/v1/sets/:id/questions/:qid/duplicate", "POST /api/v1/sets/:id/suggest",
	// Support 106–110
	"GET /api/v1/support/topics", "POST /api/v1/support/tickets", "GET /api/v1/support/tickets",
	"GET /api/v1/support/tickets/:id", "POST /api/v1/support/tickets/:id/messages",
	// App 112–113, and the live stream that stands in for 111 until /ws exists
	"GET /api/v1/config", "GET /api/v1/health", "GET /api/v1/events",
}

// Every endpoint the app documents is mounted, and answers as itself rather than as
// the 404 page.
func TestEveryDocumentedEndpointIsMounted(t *testing.T) {
	a := newApp(t)
	mounted := map[string]bool{}
	for _, r := range a.Router.Routes() {
		mounted[r.Method+" "+r.Path] = true
	}
	var missing []string
	for _, want := range contract {
		if !mounted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d endpoints are not mounted:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}

// A JSON client that is not signed in is told so in the envelope, never with a page.
func TestPrivateEndpointsRefuseAnonymously(t *testing.T) {
	a := newApp(t)
	for _, path := range []string{
		"/api/v1/conversations", "/api/v1/challenges", "/api/v1/me/duel-ratings",
		"/api/v1/rooms", "/api/v1/me/progress",
	} {
		r := a.get(path, "")
		if r.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401 without a token, got %d: %s", path, r.Code, r.Body)
		}
	}
}

// What an external client meets before it meets a handler: the wrong verb, a browser
// preflight, and the cookies an answer should not carry.
func TestAPIBehavesLikeAnExternalAPI(t *testing.T) {
	a := newApp(t)

	// A real path with the wrong verb is 405 with Allow, not 404.
	r := a.do(http.MethodPut, "/api/v1/health", nil, "")
	if r.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT /health: want 405, got %d", r.Code)
	}
	if allow := r.Hdr.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("405 should name the verbs the route takes, Allow=%q", allow)
	}
	if code := r.errorCode(); code != "method_not_allowed" {
		t.Errorf("405 keeps the error envelope, got %q", code)
	}

	// An unknown path under /api is JSON, never the 404 page.
	r = a.get("/api/v1/nope", "")
	if r.Code != http.StatusNotFound || r.errorCode() != "not_found" {
		t.Errorf("unknown API path: %d %s", r.Code, r.Body)
	}

	// A token client's answers carry no cookies: nothing for it to store, and no
	// ambient credential to protect (the CSRF cookie belongs to the website).
	token := a.guest("Mobile")
	if set := a.get("/api/v1/me", token).Hdr.Get("Set-Cookie"); set != "" {
		t.Errorf("an API answer to a Bearer client should set no cookie, got %q", set)
	}
	if set := a.get("/api/v1/config", "").Hdr.Get("Set-Cookie"); set != "" {
		t.Errorf("a signed-out API call should set no cookie, got %q", set)
	}
	// …and it needs no CSRF token to write.
	a.post("/api/v1/notifications/read", map[string]any{"all": true}, token).expect(http.StatusNoContent)
}

// CORS is off unless origins are configured, and a preflight from an allowed origin
// is answered here rather than falling through to the router's 404.
func TestCORSPreflight(t *testing.T) {
	a := newAppWithOrigins(t, []string{"https://app.example"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/me", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: want 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("allowed origin not echoed: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("a Bearer client must be allowed to send Authorization: %q", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("the answer varies by origin: %q", got)
	}

	// An origin nobody allowed gets no header, so the browser blocks it.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Origin", "https://stranger.example")
	rec = httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("an unlisted origin must not be allowed: %q", got)
	}
}

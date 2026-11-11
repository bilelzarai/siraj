package handlers_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// register signs a new account up and leaves the browser signed in as it.
func (a *app) register(username string) {
	a.t.Helper()
	a.get("/register") // for the CSRF cookie
	status, _ := a.post("/register", url.Values{
		"username":         {username},
		"email":            {username + "@example.com"},
		"display_name":     {strings.ToUpper(username[:1]) + username[1:]},
		"password":         {"correct horse battery"},
		"password_confirm": {"correct horse battery"},
	})
	if status != http.StatusSeeOther {
		a.t.Fatalf("register %s: status %d, want a redirect into the app", username, status)
	}
}

// promote gives the signed-in account a role, for the screens that need one.
func (a *app) promote(username, role string) {
	a.t.Helper()
	user, err := a.repo.UserByUsername(a.t.Context(), username)
	if err != nil {
		a.t.Fatalf("load %s: %v", username, err)
	}
	if err := a.repo.SetUserRole(a.t.Context(), user.ID, role); err != nil {
		a.t.Fatalf("promote %s: %v", username, err)
	}
}

// Every page a signed-in player can reach, walked in one session. A 500 here is
// a screen that is broken for everybody; a 404 is a route that does not exist.
func TestEveryPlayerScreenAnswers(t *testing.T) {
	a := newApp(t)
	a.register("walker")

	pages := []string{
		"/app", "/play", "/challenges", "/challenges?tab=outgoing",
		"/challenges?tab=finished", "/challenges/new", "/messages",
		"/friends", "/friends?tab=requests", "/friends?tab=search&q=walk",
		"/leaderboard", "/leaderboard?scope=friends", "/notifications",
		"/support", "/support/new", "/profile/edit", "/u/walker",
		"/history", "/history?mode=solo", "/history?mode=daily", "/settings",
		// The pagination added this week, on every list that has it.
		"/history?size=5", "/notifications?size=5&page=2", "/history?page=999",
	}
	for _, page := range pages {
		status, body := a.get(page)
		switch {
		case status >= 500:
			t.Errorf("GET %s → %d (server error)", page, status)
		case status == http.StatusNotFound:
			t.Errorf("GET %s → 404 (no such route)", page)
		case status == http.StatusOK && strings.Contains(body, "error.500"):
			t.Errorf("GET %s rendered the error page", page)
		case status != http.StatusOK && status != http.StatusSeeOther:
			t.Errorf("GET %s → %d", page, status)
		}
	}
}

// The same for an administrator, whose screens are the ones with the most
// moving parts and the least traffic to shake them out.
func TestEveryAdminScreenAnswers(t *testing.T) {
	a := newApp(t)
	a.register("walkadmin")
	a.promote("walkadmin", models.RoleAdmin)

	pages := []string{
		"/admin", "/admin/users", "/admin/users?role=admin", "/admin/users/new",
		"/admin/domains", "/admin/domains/new", "/admin/domains/1/edit",
		"/admin/categories", "/admin/categories/new", "/admin/categories/1/edit",
		"/admin/questions", "/admin/questions?review=1", "/admin/questions/new",
		"/admin/questions/import", "/admin/questions/import/template.csv",
		"/admin/review", "/admin/rated", "/admin/integrity",
		"/admin/integrity?loc=en", "/admin/integrity?loc=fr",
		"/admin/comments", "/admin/support", "/admin/audit",
		"/admin/users?size=5", "/admin/questions?size=10&page=1",
		"/admin/audit?size=5", "/admin/rated?size=5", "/admin/comments?size=5",
	}
	for _, page := range pages {
		status, body := a.get(page)
		switch {
		case status >= 500:
			t.Errorf("GET %s → %d (server error)", page, status)
		case status == http.StatusNotFound:
			t.Errorf("GET %s → 404 (no such route)", page)
		case status == http.StatusOK && strings.Contains(body, "Page not found"):
			t.Errorf("GET %s rendered the not-found page", page)
		case status != http.StatusOK && status != http.StatusSeeOther:
			t.Errorf("GET %s → %d", page, status)
		}
	}
}

// A moderator may do content work and may not touch accounts. The admin area
// answers 404 rather than 403 to someone without the role, deliberately.
func TestModeratorIsKeptOutOfAccountManagement(t *testing.T) {
	a := newApp(t)
	a.register("walkmod")
	a.promote("walkmod", models.RoleModerator)

	if status, _ := a.get("/admin/questions"); status != http.StatusOK {
		t.Errorf("a moderator cannot reach the question bank: %d", status)
	}
	if status, _ := a.get("/admin/users"); status != http.StatusNotFound {
		t.Errorf("GET /admin/users as a moderator → %d, want 404", status)
	}
	if status, _ := a.get("/admin/audit"); status != http.StatusNotFound {
		t.Errorf("GET /admin/audit as a moderator → %d, want 404", status)
	}
	// The taxonomy's second level is structural, so it is drawn on the same
	// line as account management rather than beside the category screens a
	// moderator does reach (D11).
	if status, _ := a.get("/admin/domains"); status != http.StatusNotFound {
		t.Errorf("GET /admin/domains as a moderator → %d, want 404", status)
	}
	if status, _ := a.get("/admin/domains/new"); status != http.StatusNotFound {
		t.Errorf("GET /admin/domains/new as a moderator → %d, want 404", status)
	}
	// Bulk question actions moved behind full admin this week.
	status, _ := a.post("/admin/questions/bulk", url.Values{
		"scope": {"filter"}, "action": {"delete"},
	})
	if status != http.StatusNotFound {
		t.Errorf("bulk delete as a moderator → %d, want 404", status)
	}
}

// Signed out, every private screen sends you to the login page and none of them
// answers with content.
func TestSignedOutIsRedirectedNotServed(t *testing.T) {
	a := newApp(t)
	for _, page := range []string{"/app", "/play", "/messages", "/admin", "/settings", "/notifications"} {
		status, _ := a.get(page)
		if status != http.StatusSeeOther {
			t.Errorf("GET %s signed out → %d, want a redirect to login", page, status)
		}
	}
}

// A round, played from the setup screen to the result page.
func TestPlayingARoundWorksEndToEnd(t *testing.T) {
	a := newApp(t)
	a.register("walkplayer")
	seedQuestions(a, 950000, 12)

	if status, loc := a.post("/play/start", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
	}); status != http.StatusSeeOther || loc != "/play/round" {
		t.Fatalf("start → %d %q, want a redirect to /play/round", status, loc)
	}

	status, body := a.get("/play/round")
	if status != http.StatusOK {
		t.Fatalf("the round screen → %d", status)
	}
	if !strings.Contains(body, "data-question") && !strings.Contains(body, "answer") {
		t.Error("the round screen rendered without a question on it")
	}

	if status, _ := a.post("/play/quit", url.Values{}); status != http.StatusSeeOther {
		t.Errorf("quitting a round → %d", status)
	}
}

// seedQuestions puts enough content in the bank for a round to be drawable.
func seedQuestions(a *app, from, n int) {
	a.t.Helper()
	for i := 0; i < n; i++ {
		_, err := a.repo.UpsertQuestion(a.t.Context(), &models.QuestionDraft{
			ID: from + i, CategoryID: 1, Difficulty: 1, Points: 10,
			CorrectIndex: 0, IsActive: true,
			Translations: map[string]models.TranslationDraft{
				"en": {Prompt: fmt.Sprintf("Walk question %d?", from+i),
					Choices: []string{"a", "b", "c", "d"}, Source: "human"},
			},
		})
		if err != nil {
			a.t.Fatalf("seed question: %v", err)
		}
	}
}

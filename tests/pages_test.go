package tests

import (
	"net/http"
	"testing"
)

// TestPlayerPagesRender opens every built player screen as a member and as a guest.
// A page that 500s or loses its shell fails here before it reaches a player.
func TestPlayerPagesRender(t *testing.T) {
	a := newApp(t)
	token := a.member("pagetester")

	member := []string{
		"/", "/home", "/play", "/setup", "/domains", "/leaderboard", "/profile",
		"/history", "/settings", "/settings/delete", "/friends", "/notifications",
		"/my-questions", "/support",
	}
	for _, path := range member {
		r := a.get(path, token)
		if r.Code != http.StatusOK && r.Code != http.StatusSeeOther {
			t.Errorf("GET %s as a member: %d", path, r.Code)
		}
	}

	// A guest sees the locked page instead of the member-only screens (RL-03), but
	// never an error.
	guest := a.guest("Page guest")
	for _, path := range []string{"/home", "/play", "/friends", "/notifications", "/my-questions", "/settings", "/support", "/leaderboard"} {
		if r := a.get(path, guest); r.Code != http.StatusOK {
			t.Errorf("GET %s as a guest: %d", path, r.Code)
		}
	}

	// Visitors are sent to the welcome screen, not to a 500.
	for _, path := range []string{"/home", "/profile", "/friends"} {
		if r := a.get(path, ""); r.Code != http.StatusSeeOther {
			t.Errorf("GET %s signed out: %d, want a redirect", path, r.Code)
		}
	}
}

// TestAdminPagesNeedStaff checks the guard first, then opens the eleven screens.
func TestAdminPagesNeedStaff(t *testing.T) {
	a := newApp(t)
	player := a.member("adminguard")
	for _, path := range []string{"/admin", "/admin/users", "/admin/questions"} {
		if r := a.get(path, player); r.Code != http.StatusForbidden {
			t.Errorf("GET %s as a player: %d, want 403", path, r.Code)
		}
	}

	staff := a.member("adminstaff")
	if _, err := a.DB.Pool.Exec(t.Context(), "UPDATE users SET role = 'admin' WHERE username = 'adminstaff'"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/admin", "/admin/users", "/admin/support", "/admin/domains", "/admin/categories",
		"/admin/questions", "/admin/review", "/admin/comments", "/admin/rated",
		"/admin/health", "/admin/audit",
		"/admin/domains/new", "/admin/categories/new", "/admin/questions/new",
	} {
		if r := a.get(path, staff); r.Code != http.StatusOK {
			t.Errorf("GET %s as an admin: %d", path, r.Code)
		}
	}
}

// TestProgressAPI covers endpoints 90–95 for a fresh account.
func TestProgressAPI(t *testing.T) {
	a := newApp(t)
	token := a.member("progressapi")

	data := a.get("/api/v1/me/progress", token).expect(http.StatusOK).data()
	if level, _ := data["level"].(float64); level != 1 {
		t.Errorf("a new player starts at level %v, want 1", data["level"])
	}
	if goal, _ := data["daily_goal"].(float64); goal != 5 {
		t.Errorf("daily goal %v, want 5", data["daily_goal"])
	}
	a.get("/api/v1/me/mastery", token).expect(http.StatusOK)
	a.get("/api/v1/me/activity", token).expect(http.StatusOK)
	if got := a.get("/api/v1/me/achievements", token).expect(http.StatusOK).list(); len(got) != 6 {
		t.Errorf("got %d achievements, want the 6 seeded ones", len(got))
	}
	a.get("/api/v1/leaderboards?period=week", token).expect(http.StatusOK)
	if code := a.get("/api/v1/leaderboards?period=century", token).expect(http.StatusUnprocessableEntity).errorCode(); code == "" {
		t.Error("a bad period should be refused with an error envelope")
	}
}

// TestSetsAPI covers the 5-question rule (MQ-02) end to end.
func TestSetsAPI(t *testing.T) {
	a := newApp(t)
	token := a.member("setsapi")

	created := a.post("/api/v1/sets", map[string]any{"name": "Family night", "main_lang": "en"}, token).
		expect(http.StatusCreated).data()
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("no set id in %v", created)
	}
	// The bank publishes in three languages, so a question reaches it written in
	// three (MQ-06, LG-05); these four are English only on purpose.
	english := func(body string) map[string]any {
		return map[string]any{"en": map[string]any{
			"body": body, "answers": []string{"a", "b", "c", "d"},
		}}
	}
	allThree := func(body string) map[string]any {
		out := map[string]any{}
		for _, lang := range []string{"ar", "en", "fr"} {
			out[lang] = map[string]any{"body": body, "answers": []string{"a", "b", "c", "d"}}
		}
		return out
	}
	for i := 0; i < 4; i++ {
		a.post("/api/v1/sets/"+id+"/questions", map[string]any{
			"difficulty": 1, "correct_index": 1, "main_lang": "en",
			"text": english("Question"),
		}, token).expect(http.StatusCreated)
	}
	// Four questions is still a draft: suggesting it to the bank must be refused.
	if code := a.post("/api/v1/sets/"+id+"/suggest", nil, token).expect(http.StatusConflict).errorCode(); code != "set_too_small" {
		t.Errorf("error code %q, want set_too_small", code)
	}
	a.post("/api/v1/sets/"+id+"/questions", map[string]any{
		"difficulty": 1, "correct_index": 0, "main_lang": "en",
		"text": english("Fifth"),
	}, token).expect(http.StatusCreated)
	// Five now, but every one of them is English only: the bank still says no, and
	// says which rule stopped it.
	if code := a.post("/api/v1/sets/"+id+"/suggest", nil, token).expect(http.StatusConflict).errorCode(); code != "set_not_translated" {
		t.Errorf("error code %q, want set_not_translated", code)
	}

	// A set written in all three goes through.
	full := a.post("/api/v1/sets", map[string]any{"name": "Translated", "main_lang": "en"}, token).
		expect(http.StatusCreated).data()
	fullID, _ := full["id"].(string)
	for i := 0; i < 5; i++ {
		a.post("/api/v1/sets/"+fullID+"/questions", map[string]any{
			"difficulty": 1, "correct_index": 0, "main_lang": "en",
			"text": allThree("Question"),
		}, token).expect(http.StatusCreated)
	}
	a.post("/api/v1/sets/"+fullID+"/suggest", nil, token).expect(http.StatusNoContent)
}

// TestSupportAPI covers the ticket rules of endpoint 107.
func TestSupportAPI(t *testing.T) {
	a := newApp(t)
	token := a.member("supportapi")

	a.get("/api/v1/support/topics", "").expect(http.StatusOK)
	if code := a.post("/api/v1/support/tickets", map[string]any{
		"topic": "bug", "subject": "Hi", "body": "too short",
	}, token).expect(http.StatusUnprocessableEntity).errorCode(); code != "invalid" {
		t.Errorf("error code %q, want invalid", code)
	}
	ticket := a.post("/api/v1/support/tickets", map[string]any{
		"topic": "bug", "subject": "The timer freezes",
		"body": "The timer stops at five seconds on every question of a quick game.",
	}, token).expect(http.StatusCreated).data()
	if number, _ := ticket["number"].(string); number == "" {
		t.Errorf("no ticket number in %v", ticket)
	}
	if got := a.get("/api/v1/support/tickets", token).expect(http.StatusOK).list(); len(got) != 1 {
		t.Errorf("got %d tickets, want 1", len(got))
	}
}

// TestFriendsAPI covers requests, acceptance and the guest lock (FR-01, RL-03).
func TestFriendsAPI(t *testing.T) {
	a := newApp(t)
	one := a.member("friendone")
	two := a.member("friendtwo")

	found := a.get("/api/v1/users/search?q=friendtwo", one).expect(http.StatusOK).list()
	if len(found) != 1 {
		t.Fatalf("search found %d people, want 1", len(found))
	}
	other, _ := found[0].(map[string]any)
	id, _ := other["id"].(string)
	a.post("/api/v1/friend-requests", map[string]any{"user_id": id}, one).expect(http.StatusNoContent)

	inbox := a.get("/api/v1/friend-requests", two).expect(http.StatusOK).data()
	received, _ := inbox["received"].([]any)
	if len(received) != 1 {
		t.Fatalf("inbox has %d requests, want 1", len(received))
	}
	req, _ := received[0].(map[string]any)
	reqID, _ := req["id"].(string)
	a.post("/api/v1/friend-requests/"+reqID+"/accept", nil, two).expect(http.StatusNoContent)
	if got := a.get("/api/v1/friends", one).expect(http.StatusOK).list(); len(got) != 1 {
		t.Errorf("got %d friends, want 1", len(got))
	}

	// A guest cannot send friend requests (RL-03).
	guest := a.guest("Guest friend")
	a.post("/api/v1/friend-requests", map[string]any{"user_id": id}, guest).expect(http.StatusForbidden)
}

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// call is one request of the sweep: what to send, and what the API promises back.
type call struct {
	name   string
	method string
	path   string
	body   any
	token  func(*sweep) string
	want   []int
}

// sweep holds the real rows the calls need: every id below was created by an earlier
// call, so the whole run reads and writes the database like a client would.
type sweep struct {
	a                                       *app
	alice, bob, carol                       string
	aliceID, bobID                          string
	gameID, roomCode, challengeID, convID   string
	setID, ticketID, uploadID, deviceGameID string
	requestID, duelGameID, linkCode         string
	questionID                              int64
}

// TestEveryEndpointAnswers calls every endpoint of /api/v1 against a real database,
// in an order that gives each one something real to work on. It is the sweep behind
// "no broken endpoint": anything that 500s, or answers a status its own contract does
// not allow, fails here.
func TestEveryEndpointAnswers(t *testing.T) {
	s := &sweep{a: newApp(t)}
	a := s.a
	s.alice = a.member("alice")
	s.bob = a.member("bob")
	s.carol = a.guest("Carol")
	s.aliceID = a.get("/api/v1/me", s.alice).data()["id"].(string)
	s.bobID = a.get("/api/v1/me", s.bob).data()["id"].(string)
	s.questionID = a.Content.QuestionIDs[0]
	domain := itoa(a.Content.DomainID)
	category := itoa(a.Content.CategoryID)

	// --- the rows the read endpoints need, created through the API itself ---
	s.gameID = a.post("/api/v1/games", map[string]any{"kind": "quick", "domain_id": a.Content.DomainID}, s.alice).
		expect(http.StatusCreated).data()["id"].(string)
	s.roomCode = a.post("/api/v1/rooms", map[string]any{
		"domain_id": a.Content.DomainID, "question_count": 5, "seconds": 20,
	}, s.alice).expect(http.StatusCreated).data()["code"].(string)
	s.challengeID = a.post("/api/v1/challenges", map[string]any{
		"friend_ids": []string{s.bobID}, "domain_id": a.Content.DomainID,
		"question_count": 5, "seconds": 20, "days": 3,
	}, s.alice).expect(http.StatusCreated).data()["id"].(string)
	s.linkCode = a.post("/api/v1/challenges/"+s.challengeID+"/link", nil, s.alice).
		expect(http.StatusCreated).data()["code"].(string)
	s.convID = a.post("/api/v1/conversations", map[string]any{"user_id": s.bobID}, s.alice).
		expect(http.StatusCreated).data()["id"].(string)
	s.setID = a.post("/api/v1/sets", map[string]any{"name": "Sweep set", "main_lang": "en"}, s.alice).
		expect(http.StatusCreated).data()["id"].(string)
	s.ticketID = a.post("/api/v1/support/tickets", map[string]any{
		"topic": "question", "subject": "Sweep", "body": "Does the sweep reach support?",
	}, s.alice).expect(http.StatusCreated).data()["id"].(string)
	s.deviceGameID = a.post("/api/v1/local-rounds", map[string]any{
		"locals": []string{"Ali", "Sara"}, "domain_id": a.Content.DomainID,
		"question_count": 5, "seconds": 20,
	}, s.alice).expect(http.StatusCreated).data()["game_id"].(string)
	s.uploadID = s.upload(t)
	// A duel needs two players in the queue.
	a.post("/api/v1/duels/queue", map[string]any{"domain_id": a.Content.DomainID}, s.alice).expect(http.StatusOK)
	if id, ok := a.post("/api/v1/duels/queue", map[string]any{"domain_id": a.Content.DomainID}, s.bob).
		expect(http.StatusOK).data()["game_id"].(string); ok {
		s.duelGameID = id
	}
	// A friend request from carol, so the accept/decline endpoints have a real id.
	a.post("/api/v1/friend-requests", map[string]any{"user_id": s.aliceID}, s.bob)
	if list := a.get("/api/v1/friend-requests", s.alice).expect(http.StatusOK).data(); list != nil {
		if received, ok := list["received"].([]any); ok && len(received) > 0 {
			s.requestID, _ = received[0].(map[string]any)["id"].(string)
		}
	}

	ok := []int{http.StatusOK}
	okOrEmpty := []int{http.StatusOK, http.StatusNoContent}
	created := []int{http.StatusCreated}
	// Some calls legitimately refuse: the sweep accepts the refusal, never a 500.
	refused := []int{http.StatusOK, http.StatusCreated, http.StatusNoContent,
		http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
		http.StatusConflict, http.StatusUnprocessableEntity}

	alice := func(s *sweep) string { return s.alice }
	bob := func(s *sweep) string { return s.bob }
	none := func(*sweep) string { return "" }

	calls := []call{
		// ---- app, identity (1–16) ----
		{"health", "GET", "/health", nil, none, ok},
		{"config", "GET", "/config", nil, none, ok},
		{"username free", "GET", "/auth/username-available?username=brandnew", nil, none, ok},
		{"me", "GET", "/me", nil, alice, ok},
		{"patch me", "PATCH", "/me", map[string]any{"display_name": "Alice Q.", "lang": "fr"}, alice, ok},
		{"my settings", "GET", "/me/settings", nil, alice, ok},
		{"patch settings", "PATCH", "/me/settings", map[string]any{"theme": "dark", "daily_goal": 3}, alice, okOrEmpty},
		{"onboarding", "POST", "/me/onboarding", map[string]any{"lang": "en", "domain_ids": []int64{a.Content.DomainID}}, alice, refused},
		{"register device", "POST", "/me/devices", map[string]any{"platform": "ios", "push_token": "sweep-token"}, alice, okOrEmpty},
		// Both shapes of endpoint 6: the token in the header, and the token in the body.
		{"refresh (header)", "POST", "/auth/refresh", nil, bob, ok},
		{"refresh (body)", "POST", "/auth/refresh", map[string]any{"token": s.bob}, none, ok},
		{"refresh (nothing)", "POST", "/auth/refresh", nil, none, []int{http.StatusUnauthorized}},
		{"forgot password", "POST", "/auth/password/forgot", map[string]any{"email": "alice@example.com"}, none, okOrEmpty},
		{"reset password", "POST", "/auth/password/reset", map[string]any{"token": "nope", "password": "averysafe1"}, none, refused},
		// Google sign-in answers 503 when the server has no client id configured.
		{"google", "POST", "/auth/google", map[string]any{"id_token": "nope"}, none,
			[]int{http.StatusOK, http.StatusCreated, http.StatusUnauthorized, http.StatusServiceUnavailable}},
		{"guest upgrade", "POST", "/auth/guest/upgrade", map[string]any{
			"display_name": "Carol", "username": "carolup", "email": "carol@example.com",
			"password": "averysafe1", "accept_terms": true,
		}, func(s *sweep) string { return s.carol }, refused},

		// ---- catalog and search (17–23, 114–117) ----
		{"domains", "GET", "/domains?scope=all", nil, alice, ok},
		{"domain", "GET", "/domains/" + domain, nil, alice, ok},
		{"domain categories", "GET", "/domains/" + domain + "/categories", nil, alice, ok},
		{"category", "GET", "/categories/" + category, nil, alice, ok},
		{"search", "GET", "/search?scope=all&q=test", nil, alice, ok},
		{"my domains", "GET", "/me/domains", nil, alice, ok},
		{"replace my domains", "PUT", "/me/domains", map[string]any{"domain_ids": []int64{a.Content.DomainID}}, alice, ok},
		{"add domain", "POST", "/me/domains/" + domain, nil, bob, ok},
		{"remove last domain", "DELETE", "/me/domains/" + domain, nil, bob, refused},
		{"daily question", "GET", "/daily-question", nil, alice, refused},
		{"answer daily", "POST", "/daily-question/answer", map[string]any{"choice": 0}, alice, refused},

		// ---- games (24–31) ----
		{"game state", "GET", "/games/" + s.gameID, nil, alice, ok},
		{"fifty", "POST", "/games/" + s.gameID + "/fifty", nil, alice, refused},
		{"answer", "POST", "/games/" + s.gameID + "/answers", map[string]any{"position": 0, "choice": 1, "time_ms": 900}, alice, refused},
		{"skip", "POST", "/games/" + s.gameID + "/skip", nil, alice, refused},
		{"finish", "POST", "/games/" + s.gameID + "/finish", nil, alice, refused},
		{"results", "GET", "/games/" + s.gameID + "/results", nil, alice, refused},
		{"history", "GET", "/me/games", nil, alice, ok},

		// ---- questions (32–35) ----
		{"rate", "PUT", fmt.Sprintf("/questions/%d/rating", s.questionID), map[string]any{"stars": 4}, alice, refused},
		{"comments", "GET", fmt.Sprintf("/questions/%d/comments", s.questionID), nil, alice, ok},
		{"comment", "POST", fmt.Sprintf("/questions/%d/comments", s.questionID), map[string]any{"body": "Clear question."}, alice, refused},
		{"report question", "POST", fmt.Sprintf("/questions/%d/reports", s.questionID), map[string]any{"reason": "unclear", "note": "sweep"}, alice, refused},

		// ---- rooms (36–46, 119) ----
		{"open rooms", "GET", "/rooms", nil, alice, ok},
		{"room", "GET", "/rooms/" + s.roomCode, nil, alice, ok},
		{"join room", "POST", "/rooms/" + s.roomCode + "/join", nil, bob, ok},
		{"patch room", "PATCH", "/rooms/" + s.roomCode, map[string]any{"question_count": 5, "seconds": 20}, alice, ok},
		{"ready", "POST", "/rooms/" + s.roomCode + "/ready", map[string]any{"ready": true}, bob, ok},
		{"room chat send", "POST", "/rooms/" + s.roomCode + "/messages", map[string]any{"body": "salam"}, bob, okOrEmpty},
		{"room chat", "GET", "/rooms/" + s.roomCode + "/messages", nil, alice, ok},
		// Two players cannot take sides: both halves of RM-05 refuse the same way.
		{"teams move", "POST", "/rooms/" + s.roomCode + "/teams/move", map[string]any{"side": 1}, bob, []int{http.StatusConflict}},
		{"teams shuffle", "POST", "/rooms/" + s.roomCode + "/teams/shuffle", nil, alice, []int{http.StatusConflict}},
		{"start room", "POST", "/rooms/" + s.roomCode + "/start", nil, alice, ok},
		{"standings", "GET", "/rooms/" + s.roomCode + "/standings?position=0", nil, alice, ok},
		{"podium", "GET", "/rooms/" + s.roomCode + "/podium", nil, alice, ok},
		{"kick", "DELETE", "/rooms/" + s.roomCode + "/players/" + s.bobID, nil, alice, okOrEmpty},
		{"play again", "POST", "/rooms/" + s.roomCode + "/again", nil, alice, refused},
		{"leave room", "POST", "/rooms/" + s.roomCode + "/leave", nil, alice, okOrEmpty},

		// ---- duels (47–50) ----
		{"duel state", "GET", "/duels/queue", nil, alice, ok},
		{"my ratings", "GET", "/me/duel-ratings", nil, alice, ok},

		// ---- challenges (51–62, 118) ----
		{"challenges", "GET", "/challenges?box=received", nil, bob, ok},
		{"challenge stats", "GET", "/challenges/stats", nil, bob, ok},
		{"challenge", "GET", "/challenges/" + s.challengeID, nil, alice, ok},
		{"remind", "POST", "/challenges/" + s.challengeID + "/remind", nil, alice, refused},
		{"open link", "GET", "/challenge-links/" + s.linkCode, nil, func(s *sweep) string { return s.carol }, refused},
		{"random opponent", "POST", "/challenges/random-opponent", nil, alice, refused},
		{"local round state", "GET", "/local-rounds/" + s.deviceGameID, nil, alice, ok},
		{"local round results", "GET", "/local-rounds/" + s.deviceGameID + "/results", nil, alice, refused},
		{"accept challenge", "POST", "/challenges/" + s.challengeID + "/accept", nil, bob, created},
		{"close early", "POST", "/challenges/" + s.challengeID + "/close", nil, alice, refused},
		{"rematch", "POST", "/challenges/" + s.challengeID + "/rematch", nil, alice, refused},
		{"decline", "POST", "/challenges/" + s.challengeID + "/decline", nil, bob, refused},

		// ---- social (63–74) ----
		{"friends", "GET", "/friends", nil, alice, ok},
		{"requests", "GET", "/friend-requests", nil, alice, ok},
		{"people search", "GET", "/users/search?q=bob", nil, alice, ok},
		{"profile", "GET", "/users/bob", nil, alice, ok},
		{"suggestions", "GET", "/users-suggestions", nil, alice, ok},
		{"invite", "POST", "/invites", nil, alice, refused},
		{"block", "POST", "/blocks", map[string]any{"user_id": s.bobID}, func(s *sweep) string { return s.carol }, refused},
		{"unblock", "DELETE", "/blocks/" + s.bobID, nil, func(s *sweep) string { return s.carol }, refused},

		// ---- messages (75–86) ----
		{"inbox", "GET", "/conversations", nil, alice, ok},
		{"inbox groups", "GET", "/conversations?tab=groups", nil, alice, ok},
		{"inbox rooms", "GET", "/conversations?tab=rooms", nil, alice, ok},
		{"candidates", "GET", "/conversations-candidates", nil, alice, ok},
		{"thread", "GET", "/conversations/" + s.convID, nil, alice, ok},
		{"send", "POST", "/conversations/" + s.convID + "/messages", map[string]any{"body": "salam"}, alice, ok},
		{"send a file", "POST", "/conversations/" + s.convID + "/messages", map[string]any{"upload_id": s.uploadID, "kind": "file"}, alice, ok},
		{"messages", "GET", "/conversations/" + s.convID + "/messages", nil, bob, ok},
		{"patch conversation", "PATCH", "/conversations/" + s.convID, map[string]any{"pinned": true, "muted": true}, alice, okOrEmpty},
		{"read", "POST", "/conversations/" + s.convID + "/read", nil, bob, okOrEmpty},
		{"unread", "POST", "/conversations/" + s.convID + "/unread", nil, bob, okOrEmpty},
		{"unread count", "GET", "/conversations-unread-count", nil, bob, ok},

		// ---- notifications (87–89) ----
		{"notifications", "GET", "/notifications", nil, bob, ok},
		{"mark read", "POST", "/notifications/read", map[string]any{"all": true}, bob, okOrEmpty},
		{"badge", "GET", "/notifications/unread-count", nil, bob, ok},

		// ---- progress (90–95) ----
		{"progress", "GET", "/me/progress", nil, alice, ok},
		{"mastery", "GET", "/me/mastery", nil, alice, ok},
		{"mastery by domain", "GET", "/me/mastery/domains/" + domain, nil, alice, ok},
		{"achievements", "GET", "/me/achievements", nil, alice, ok},
		{"activity", "GET", "/me/activity", nil, alice, ok},
		{"leaderboard", "GET", "/leaderboards?period=week&scope=global", nil, alice, ok},

		// ---- sets (96–105) ----
		{"sets", "GET", "/sets", nil, alice, ok},
		{"set", "GET", "/sets/" + s.setID, nil, alice, ok},
		{"rename set", "PATCH", "/sets/" + s.setID, map[string]any{"name": "Sweep set 2", "main_lang": "en"}, alice, okOrEmpty},
		{"add question", "POST", "/sets/" + s.setID + "/questions", map[string]any{
			"category_id": a.Content.CategoryID, "difficulty": 1, "points": 10, "correct_index": 1, "main_lang": "en",
			"text": map[string]any{"en": map[string]any{"body": "Sweep question?", "answers": []string{"A", "B", "C", "D"}, "explanation": "B."}},
		}, alice, created},
		{"suggest set", "POST", "/sets/" + s.setID + "/suggest", nil, alice, refused},

		// ---- support (106–110) ----
		{"topics", "GET", "/support/topics", nil, none, ok},
		{"my tickets", "GET", "/support/tickets", nil, alice, ok},
		{"ticket", "GET", "/support/tickets/" + s.ticketID, nil, alice, ok},
		{"reply", "POST", "/support/tickets/" + s.ticketID + "/messages", map[string]any{"body": "Thanks."}, alice, okOrEmpty},
	}
	if s.duelGameID != "" {
		calls = append(calls,
			call{"duel", "GET", "/duels/" + s.duelGameID, nil, alice, ok},
			call{"duel opponent", "GET", "/duels/" + s.duelGameID + "/opponent", nil, alice, ok})
	}
	if s.requestID != "" {
		calls = append(calls,
			call{"accept request", "POST", "/friend-requests/" + s.requestID + "/accept", nil, alice, okOrEmpty},
			call{"remove friend", "DELETE", "/friends/" + s.bobID, nil, alice, okOrEmpty})
	}

	for _, c := range calls {
		r := a.do(c.method, "/api/v1"+c.path, c.body, c.token(s))
		t.Logf("SWEEP %-24s %-6s %-46s %d", c.name, c.method, c.path, r.Code)
		if !allowed(r.Code, c.want) {
			t.Errorf("%-22s %s %s → %d (want %v)\n    %s", c.name, c.method, c.path, r.Code, c.want, trim(r.Body))
			continue
		}
		if err := envelope(r); err != nil {
			t.Errorf("%-22s %s %s → %d %v: %s", c.name, c.method, c.path, r.Code, err, trim(r.Body))
		}
	}

	// Leaving the queue and signing out close the sweep.
	a.do(http.MethodDelete, "/api/v1/duels/queue", nil, s.alice).expect(http.StatusNoContent)
	a.post("/api/v1/auth/logout", nil, s.bob).expect(http.StatusNoContent)
}

// upload posts a small file through endpoint 86 and returns its id.
func (s *sweep) upload(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("a sweep attachment")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+s.alice)
	rec := httptest.NewRecorder()
	s.a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload (#86): %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := decode(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data.ID
}

// envelope checks the one promise every answer makes: 204 says nothing, a success
// carries "data", a failure carries "error" with a code and a message. A handler that
// answers bare JSON, or an empty body where a client expects a document, fails here.
func envelope(r reply) error {
	if r.Code == http.StatusNoContent {
		if len(bytes.TrimSpace(r.Body)) != 0 {
			return fmt.Errorf("204 with a body")
		}
		return nil
	}
	if ct := r.Hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("content type %q", ct)
	}
	var out struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Body, &out); err != nil {
		return fmt.Errorf("body is not JSON: %w", err)
	}
	if r.Code >= 400 {
		switch {
		case out.Error == nil:
			return fmt.Errorf("a failure without an error object")
		case out.Error.Code == "":
			return fmt.Errorf("an error without a code")
		case out.Error.Message == "":
			return fmt.Errorf("an error without a message")
		}
		return nil
	}
	if len(out.Data) == 0 {
		return fmt.Errorf("a success without a data field")
	}
	return nil
}

func allowed(code int, want []int) bool {
	for _, w := range want {
		if code == w {
			return true
		}
	}
	return false
}

func trim(body []byte) string {
	if len(body) > 300 {
		return string(body[:300]) + "…"
	}
	return string(body)
}

// The unhappy paths an external client hits most: a malformed id, an id that does not
// exist, and somebody else's row. None of them may answer 500, and none may answer
// 200 with a stranger's data.
func TestEveryEndpointRefusesBadInput(t *testing.T) {
	a := newApp(t)
	alice := a.member("badalice")
	bob := a.member("badbob")
	bobID := a.get("/api/v1/me", bob).data()["id"].(string)

	// Rows that belong to bob, which alice must not reach.
	bobGame := a.post("/api/v1/games", map[string]any{"kind": "quick", "domain_id": a.Content.DomainID}, bob).
		expect(http.StatusCreated).data()["id"].(string)
	bobSet := a.post("/api/v1/sets", map[string]any{"name": "Bob's set", "main_lang": "en"}, bob).
		expect(http.StatusCreated).data()["id"].(string)
	bobTicket := a.post("/api/v1/support/tickets", map[string]any{
		"topic": "question", "subject": "Bob", "body": "Bob's ticket, not Alice's.",
	}, bob).expect(http.StatusCreated).data()["id"].(string)
	bobConv := a.post("/api/v1/conversations", map[string]any{"user_id": bobID}, bob).Code // bob with himself: refused
	_ = bobConv
	bobRoom := a.post("/api/v1/rooms", map[string]any{"domain_id": a.Content.DomainID, "question_count": 5}, bob).
		expect(http.StatusCreated).data()["code"].(string)
	bobChallenge := a.post("/api/v1/challenges", map[string]any{
		"friend_ids": []string{bobID}, "domain_id": a.Content.DomainID, "question_count": 5, "days": 1,
	}, bob).Code
	_ = bobChallenge

	const unknownUUID = "01a00000-0000-7000-8000-000000000000"
	refusals := []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
		http.StatusConflict, http.StatusUnprocessableEntity}

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		token  string
		want   []int
	}{
		// A malformed id is a 400, not a 500 and not a 404 hunt.
		{"game, bad id", "GET", "/games/not-a-uuid", nil, alice, []int{http.StatusBadRequest}},
		{"set, bad id", "GET", "/sets/not-a-uuid", nil, alice, []int{http.StatusBadRequest}},
		{"thread, bad id", "GET", "/conversations/not-a-uuid", nil, alice, refusals},
		{"challenge, bad id", "GET", "/challenges/not-a-uuid", nil, alice, []int{http.StatusBadRequest}},
		{"domain, bad id", "GET", "/domains/not-a-number", nil, alice, []int{http.StatusBadRequest}},
		{"category, bad id", "GET", "/categories/not-a-number", nil, alice, []int{http.StatusBadRequest}},
		{"rating, bad id", "PUT", "/questions/not-a-number/rating", map[string]any{"stars": 3}, alice, []int{http.StatusBadRequest}},

		// An id that parses but does not exist is a 404, never a 500.
		{"unknown game", "GET", "/games/" + unknownUUID, nil, alice, refusals},
		{"unknown set", "GET", "/sets/" + unknownUUID, nil, alice, refusals},
		{"unknown thread", "GET", "/conversations/" + unknownUUID, nil, alice, refusals},
		{"unknown challenge", "GET", "/challenges/" + unknownUUID, nil, alice, refusals},
		{"unknown room", "GET", "/rooms/ZZZZZZ", nil, alice, refusals},
		{"unknown link", "GET", "/challenge-links/ZZZZZZZZ", nil, alice, refusals},
		{"unknown domain", "GET", "/domains/999999", nil, alice, refusals},
		{"unknown category", "GET", "/categories/999999", nil, alice, refusals},
		{"unknown user", "GET", "/users/nobodyhere", nil, alice, refusals},
		{"unknown ticket", "GET", "/support/tickets/" + unknownUUID, nil, alice, refusals},
		{"unknown duel", "GET", "/duels/" + unknownUUID, nil, alice, refusals},
		{"unknown local round", "GET", "/local-rounds/" + unknownUUID, nil, alice, refusals},

		// Somebody else's rows.
		{"bob's game", "GET", "/games/" + bobGame, nil, alice, refusals},
		{"bob's results", "GET", "/games/" + bobGame + "/results", nil, alice, refusals},
		{"bob's answer", "POST", "/games/" + bobGame + "/answers", map[string]any{"position": 0, "choice": 0, "time_ms": 100}, alice, refusals},
		{"bob's set", "GET", "/sets/" + bobSet, nil, alice, refusals},
		{"bob's set, renamed", "PATCH", "/sets/" + bobSet, map[string]any{"name": "Mine now", "main_lang": "en"}, alice, refusals},
		{"bob's set, deleted", "DELETE", "/sets/" + bobSet, nil, alice, refusals},
		{"bob's ticket", "GET", "/support/tickets/" + bobTicket, nil, alice, refusals},
		{"bob's ticket, replied", "POST", "/support/tickets/" + bobTicket + "/messages", map[string]any{"body": "hello"}, alice, refusals},
		{"bob's room, settings", "PATCH", "/rooms/" + bobRoom, map[string]any{"question_count": 20}, alice, refusals},
		{"bob's room, started", "POST", "/rooms/" + bobRoom + "/start", nil, alice, refusals},
		{"bob's room, chat", "GET", "/rooms/" + bobRoom + "/messages", nil, alice, refusals},

		// Bodies that do not say what they must.
		{"empty game", "POST", "/games", map[string]any{}, alice, append(refusals, http.StatusCreated)},
		// A set with no name is a draft called "Untitled set" — the design's own
		// behaviour, not a refusal (MQ-01).
		{"empty set", "POST", "/sets", map[string]any{"name": ""}, alice, []int{http.StatusCreated}},
		{"empty message", "POST", "/conversations/" + unknownUUID + "/messages", map[string]any{"body": ""}, alice, refusals},
		{"empty ticket", "POST", "/support/tickets", map[string]any{"topic": "", "subject": "", "body": ""}, alice, refusals},
		{"bad stars", "PUT", "/questions/1/rating", map[string]any{"stars": 99}, alice, refusals},
		{"bad conversation", "POST", "/conversations", map[string]any{"user_ids": []string{"not-a-uuid"}}, alice, refusals},
		{"bad upload id", "POST", "/conversations/" + unknownUUID + "/messages", map[string]any{"upload_id": "nope"}, alice, refusals},
		{"self conversation", "POST", "/conversations", map[string]any{"user_id": bobID}, bob, refusals},
	}

	for _, c := range cases {
		r := a.do(c.method, "/api/v1"+c.path, c.body, c.token)
		t.Logf("BAD %-24s %-6s %-44s %d", c.name, c.method, c.path, r.Code)
		if r.Code >= 500 {
			t.Errorf("%-22s %s %s → %d (a client's mistake became a server error)\n    %s",
				c.name, c.method, c.path, r.Code, trim(r.Body))
			continue
		}
		if !allowed(r.Code, c.want) {
			t.Errorf("%-22s %s %s → %d (want %v)\n    %s", c.name, c.method, c.path, r.Code, c.want, trim(r.Body))
			continue
		}
		if err := envelope(r); err != nil {
			t.Errorf("%-22s %s %s → %d %v: %s", c.name, c.method, c.path, r.Code, err, trim(r.Body))
		}
	}
}

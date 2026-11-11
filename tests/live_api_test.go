package tests

import (
	"net/http"
	"testing"
)

// A room over the API: hosted, joined, started, played against real questions, and
// read back from the database on every call (RM-01…RM-07, endpoints 36–46).
func TestRoomAPIEndToEnd(t *testing.T) {
	a := newApp(t)
	host := a.member("roomhost")
	guest := a.member("roomguest")

	created := a.post("/api/v1/rooms", map[string]any{
		"domain_id": a.Content.DomainID, "question_count": 5, "seconds": 20, "max_players": 4,
	}, host).expect(http.StatusCreated).data()
	code, _ := created["code"].(string)
	if code == "" {
		t.Fatalf("no room code in %v", created)
	}

	joined := a.post("/api/v1/rooms/"+code+"/join", nil, guest).expect(http.StatusOK).data()
	if int(joined["player_count"].(float64)) != 2 {
		t.Fatalf("the room should hold two players: %v", joined["player_count"])
	}

	// 41: ready counts come from the room's members, not from the client.
	ready := a.post("/api/v1/rooms/"+code+"/ready", map[string]any{"ready": true}, guest).
		expect(http.StatusOK).data()
	if int(ready["ready_count"].(float64)) != 1 {
		t.Errorf("one player is ready, got %v", ready["ready_count"])
	}

	// 40: only the host may change the round.
	a.do(http.MethodPatch, "/api/v1/rooms/"+code, map[string]any{"question_count": 10}, guest).
		expect(http.StatusForbidden)
	patched := a.do(http.MethodPatch, "/api/v1/rooms/"+code, map[string]any{"question_count": 10, "seconds": 30}, host).
		expect(http.StatusOK).data()
	if set := patched["settings"].(map[string]any); int(set["question_count"].(float64)) != 10 {
		t.Errorf("the host's settings were not saved: %v", set)
	}

	// 45 / 44: chat is stored and read back.
	a.post("/api/v1/rooms/"+code+"/messages", map[string]any{"body": "bismillah"}, guest).
		expect(http.StatusNoContent)
	lines := a.get("/api/v1/rooms/"+code+"/messages", host).expect(http.StatusOK).list()
	if len(lines) == 0 {
		t.Fatal("the room chat came back empty")
	}

	// 43: one game for the whole room.
	start := a.post("/api/v1/rooms/"+code+"/start", nil, host).expect(http.StatusOK).data()
	gameID, _ := start["game_id"].(string)
	if gameID == "" {
		t.Fatalf("the room did not start a game: %v", start)
	}
	// Both players answer the same game.
	for _, token := range []string{host, guest} {
		state := a.get("/api/v1/games/"+gameID, token).expect(http.StatusOK).data()
		if state["question"] == nil {
			t.Fatalf("no question for a room player: %v", state)
		}
	}
	board := a.get("/api/v1/rooms/"+code+"/standings?position=0", host).expect(http.StatusOK).data()
	if len(board["rows"].([]any)) != 2 {
		t.Errorf("the live board should list both players: %v", board["rows"])
	}
	a.get("/api/v1/rooms/"+code+"/podium", host).expect(http.StatusOK)
}

// A challenge over the API, with the one rule the module exists for: another
// player's score never leaves the server while the challenge is open (CH-09).
func TestChallengeAPIHidesScoresUntilItCloses(t *testing.T) {
	a := newApp(t)
	sender := a.member("chsender")
	other := a.member("chfriend")
	otherID := a.get("/api/v1/me", other).expect(http.StatusOK).data()["id"].(string)

	created := a.post("/api/v1/challenges", map[string]any{
		"friend_ids": []string{otherID}, "domain_id": a.Content.DomainID,
		"question_count": 5, "seconds": 20, "message": "yalla", "days": 1,
	}, sender).expect(http.StatusCreated).data()
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("no challenge id in %v", created)
	}

	// 55: accepting deals this player's copy of the frozen questions.
	game := a.post("/api/v1/challenges/"+id+"/accept", nil, sender).expect(http.StatusCreated).data()
	gameID, _ := game["game_id"].(string)
	for position := 0; position < 5; position++ {
		a.post("/api/v1/games/"+gameID+"/answers",
			map[string]any{"position": position, "choice": 0, "time_ms": 400}, sender).
			expect(http.StatusOK)
	}
	a.post("/api/v1/games/"+gameID+"/finish", nil, sender).expect(http.StatusOK)

	// The other player has not played: their line carries no score, and neither does
	// the sender's as seen by them.
	detail := a.get("/api/v1/challenges/"+id, other).expect(http.StatusOK).data()
	for _, raw := range detail["players"].([]any) {
		p := raw.(map[string]any)
		if p["is_me"] == true {
			continue
		}
		if p["score"] != nil {
			t.Errorf("an open challenge leaked a score: %v", p)
		}
	}

	// 58 and 52 read real rows.
	a.post("/api/v1/challenges/"+id+"/remind", nil, sender).expect(http.StatusOK)
	stats := a.get("/api/v1/challenges/stats", other).expect(http.StatusOK).data()
	if int(stats["received"].(float64)) != 1 {
		t.Errorf("the other player has one challenge waiting: %v", stats)
	}

	// 60: the share link resolves to the same challenge and joins whoever opens it.
	link := a.post("/api/v1/challenges/"+id+"/link", nil, sender).expect(http.StatusCreated).data()
	code, _ := link["code"].(string)
	third := a.member("chthird")
	opened := a.get("/api/v1/challenge-links/"+code, third).expect(http.StatusOK).data()
	if opened["id"] != id {
		t.Errorf("the link opened another challenge: %v", opened["id"])
	}

	// 56: declining is a real state change.
	a.post("/api/v1/challenges/"+id+"/decline", nil, other).expect(http.StatusNoContent)
}

// 47–50: the queue pairs two players inside one transaction, and the rating comes
// from the database.
func TestDuelQueuePairsTwoPlayers(t *testing.T) {
	a := newApp(t)
	one := a.member("duelone")
	two := a.member("dueltwo")

	first := a.post("/api/v1/duels/queue", map[string]any{"domain_id": a.Content.DomainID}, one).
		expect(http.StatusOK).data()
	if first["searching"] != true {
		t.Fatalf("the first player should be searching: %v", first)
	}
	second := a.post("/api/v1/duels/queue", map[string]any{"domain_id": a.Content.DomainID}, two).
		expect(http.StatusOK).data()
	gameID, _ := second["game_id"].(string)
	if gameID == "" {
		t.Fatalf("the second player should have been paired: %v", second)
	}
	// The first player now sees the same game.
	state := a.get("/api/v1/duels/queue", one).expect(http.StatusOK).data()
	if state["game_id"] != gameID {
		t.Errorf("the two sides are not on the same game: %v vs %s", state["game_id"], gameID)
	}
	detail := a.get("/api/v1/duels/"+gameID, one).expect(http.StatusOK).data()
	if detail["opponent"].(map[string]any)["name"] != "dueltwo" {
		t.Errorf("the opponent is read from the game: %v", detail["opponent"])
	}
	a.get("/api/v1/me/duel-ratings", one).expect(http.StatusOK)
	a.do(http.MethodDelete, "/api/v1/duels/queue", nil, two).expect(http.StatusNoContent)
}

// 75–86: a thread, its messages and its marks, all stored.
func TestMessagesAPIEndToEnd(t *testing.T) {
	a := newApp(t)
	one := a.member("msgone")
	two := a.member("msgtwo")
	twoID := a.get("/api/v1/me", two).expect(http.StatusOK).data()["id"].(string)

	created := a.post("/api/v1/conversations", map[string]any{"user_id": twoID}, one).
		expect(http.StatusCreated).data()
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("no conversation id in %v", created)
	}

	sent := a.post("/api/v1/conversations/"+id+"/messages", map[string]any{"body": "salam"}, one).
		expect(http.StatusOK).list()
	if len(sent) != 1 {
		t.Fatalf("the thread should hold one message: %v", sent)
	}

	// The other side sees it as unread until they open it (MS-06).
	badge := a.get("/api/v1/conversations-unread-count", two).expect(http.StatusOK).data()
	if int(badge["unread"].(float64)) != 1 {
		t.Errorf("one unread thread expected, got %v", badge["unread"])
	}
	a.post("/api/v1/conversations/"+id+"/read", nil, two).expect(http.StatusNoContent)
	badge = a.get("/api/v1/conversations-unread-count", two).expect(http.StatusOK).data()
	if int(badge["unread"].(float64)) != 0 {
		t.Errorf("after reading, the badge is empty: %v", badge["unread"])
	}

	// 78: pin and mute are written on the member row.
	a.do(http.MethodPatch, "/api/v1/conversations/"+id, map[string]any{"pinned": true, "muted": true}, one).
		expect(http.StatusNoContent)
	rows := a.get("/api/v1/conversations", one).expect(http.StatusOK).list()
	if len(rows) != 1 || rows[0].(map[string]any)["pinned"] != true {
		t.Errorf("the pin did not survive the round trip: %v", rows)
	}

	// 76 with several people makes a group, 78 renames it.
	group := a.post("/api/v1/conversations", map[string]any{
		"user_ids": []string{twoID}, "group": true, "title": "Study circle",
	}, one).expect(http.StatusCreated).data()
	groupID := group["id"].(string)
	a.do(http.MethodPatch, "/api/v1/conversations/"+groupID, map[string]any{"title": "Halaqa"}, one).
		expect(http.StatusNoContent)
	groups := a.get("/api/v1/conversations?tab=groups", one).expect(http.StatusOK).list()
	if len(groups) != 1 || groups[0].(map[string]any)["name"] != "Halaqa" {
		t.Errorf("the group was not renamed: %v", groups)
	}
}

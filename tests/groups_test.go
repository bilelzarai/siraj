package tests

import (
	"net/http"
	"strings"
	"testing"
)

// A group from the "+" menu: started with friends, managed by whoever started it,
// and left by anybody (MS-01).
func TestGroupLifecycle(t *testing.T) {
	a := newApp(t)
	owner := a.member("groupowner")
	friend := a.member("groupfriend")
	third := a.member("groupthird")
	friendID := a.get("/api/v1/me", friend).data()["id"].(string)
	thirdID := a.get("/api/v1/me", third).data()["id"].(string)

	group := a.post("/api/v1/conversations", map[string]any{
		"group": true, "title": "Study circle", "user_ids": []string{friendID},
	}, owner).expect(http.StatusCreated).data()
	id := group["id"].(string)

	// The panel: two people, and the owner is the one who started it.
	panel := a.get("/api/v1/conversations/"+id+"/members", owner).expect(http.StatusOK).data()
	members := panel["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("a group of two: %v", members)
	}
	if panel["i_am_owner"] != true {
		t.Error("whoever started the group owns it")
	}

	// Adding a third.
	after := a.post("/api/v1/conversations/"+id+"/members", map[string]any{
		"user_ids": []string{thirdID},
	}, owner).expect(http.StatusOK).data()
	if len(after["members"].([]any)) != 3 {
		t.Errorf("the third should be in: %v", after["members"])
	}

	// A member may not remove somebody else…
	a.do(http.MethodDelete, "/api/v1/conversations/"+id+"/members/"+thirdID, nil, friend).
		expect(http.StatusForbidden)
	// …but may leave.
	a.post("/api/v1/conversations/"+id+"/leave", nil, friend).expect(http.StatusNoContent)
	if rows := a.get("/api/v1/conversations?tab=groups", friend).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Errorf("after leaving, the group is gone from their inbox: %v", rows)
	}
	// The owner may remove.
	a.do(http.MethodDelete, "/api/v1/conversations/"+id+"/members/"+thirdID, nil, owner).
		expect(http.StatusNoContent)
	left := a.get("/api/v1/conversations/"+id+"/members", owner).expect(http.StatusOK).data()
	if len(left["members"].([]any)) != 1 {
		t.Errorf("only the owner is left: %v", left["members"])
	}

	// Renaming is the owner's too, and shows in the inbox.
	a.do(http.MethodPatch, "/api/v1/conversations/"+id, map[string]any{"title": "Halaqa"}, owner).
		expect(http.StatusNoContent)
	rows := a.get("/api/v1/conversations?tab=groups", owner).expect(http.StatusOK).list()
	if len(rows) != 1 || rows[0].(map[string]any)["name"] != "Halaqa" {
		t.Errorf("the new name should be in the inbox: %v", rows)
	}
}

// A challenge posted into a thread: the card carries the round, and tapping it
// opens the challenge rather than any score (MS-02, CH-09).
func TestChallengeCardInAThread(t *testing.T) {
	a := newApp(t)
	sender := a.member("cardsender")
	friend := a.member("cardfriend")
	friendID := a.get("/api/v1/me", friend).data()["id"].(string)

	challenge := a.post("/api/v1/challenges", map[string]any{
		"friend_ids": []string{friendID}, "domain_id": a.Content.DomainID,
		"question_count": 5, "seconds": 20, "days": 3,
	}, sender).expect(http.StatusCreated).data()
	challengeID := challenge["id"].(string)

	// It is offered to the composer…
	offered := a.get("/api/v1/challenges-shareable", sender).expect(http.StatusOK).list()
	if len(offered) == 0 {
		t.Fatal("an open challenge should be shareable")
	}

	conversation := a.post("/api/v1/conversations", map[string]any{"user_id": friendID}, sender).
		expect(http.StatusCreated).data()["id"].(string)
	thread := a.post("/api/v1/conversations/"+conversation+"/challenge", map[string]any{
		"challenge_id": challengeID,
	}, sender).expect(http.StatusOK).list()

	if len(thread) != 1 {
		t.Fatalf("the card should be the only message: %v", thread)
	}
	card := thread[0].(map[string]any)
	if card["kind"] != "challenge" || card["challenge_id"] != challengeID {
		t.Errorf("that is not a challenge card: %v", card)
	}
	// The other side sees it too, and can open the challenge from it.
	theirs := a.get("/api/v1/conversations/"+conversation+"/messages", friend).expect(http.StatusOK).list()
	if len(theirs) != 1 || theirs[0].(map[string]any)["challenge_id"] != challengeID {
		t.Errorf("the card did not reach the other side: %v", theirs)
	}
	a.post("/api/v1/challenges/"+challengeID+"/accept", nil, friend).expect(http.StatusCreated)
}

// The screens behind all of it: the "+" menu, the group dialog, the members panel
// and the challenge picker.
func TestMessagesScreensOfferTheActions(t *testing.T) {
	a := newApp(t)
	owner := a.member("screenowner")
	friend := a.member("screenfriend")
	friendID := a.get("/api/v1/me", friend).data()["id"].(string)

	page := string(a.get("/messages", owner).expect(http.StatusOK).Body)
	for _, want := range []string{"new=person", "new=group", "/rooms/join", `class="mi"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the + menu is missing %s", want)
		}
	}
	if !strings.Contains(string(a.get("/messages?new=group", owner).Body), "Group name") {
		t.Error("the group dialog should ask for a name")
	}

	group := a.post("/api/v1/conversations", map[string]any{
		"group": true, "title": "Screens", "user_ids": []string{friendID},
	}, owner).expect(http.StatusCreated).data()["id"].(string)

	thread := string(a.get("/messages?tab=groups&with="+group, owner).expect(http.StatusOK).Body)
	if !strings.Contains(thread, "panel=1") {
		t.Error("a group thread should offer its members panel")
	}
	panel := string(a.get("/messages?tab=groups&with="+group+"&panel=1", owner).expect(http.StatusOK).Body)
	// The panel lists the people and the way out. "Add people" only appears when
	// there is somebody to add — a friend who is not in the group yet — which
	// these two are not.
	for _, want := range []string{"Members", "Leave group", "screenfriend", "started the group", "Group name"} {
		if !strings.Contains(panel, want) {
			t.Errorf("the members panel is missing %s", want)
		}
	}
	picker := string(a.get("/messages?with="+group+"&pick=1", owner).expect(http.StatusOK).Body)
	if !strings.Contains(picker, "Send a challenge") {
		t.Error("the composer should open a challenge picker")
	}
}

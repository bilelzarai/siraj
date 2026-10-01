package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// openRoom creates a room as the signed-in browser and answers with its id.
func openRoom(t *testing.T, a *app, title, topic string) string {
	t.Helper()
	a.get("/messages/new/room")
	code, loc := a.post("/messages/new", url.Values{
		"kind": {"room"}, "title": {title}, "topic": {topic},
	})
	if code != http.StatusSeeOther || !strings.HasPrefix(loc, "/messages/") {
		t.Fatalf("opening a room → %d %q", code, loc)
	}
	return strings.TrimPrefix(loc, "/messages/")
}

// A group is people who were chosen. Everyone in it sees what anyone says,
// which is the whole difference between a group and three separate threads.
func TestAGroupReachesEveryoneInIt(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	c := newAppSharing(t, a)

	pair(t, a, b, "gowner", "gtwo")
	// The third has to be a friend too: you cannot put a stranger in a group.
	c.register("gthree")
	if code, _ := a.post("/friends/request", url.Values{"username": {"gthree"}}); code != http.StatusSeeOther {
		t.Fatalf("friend request → %d", code)
	}
	if code, _ := c.post("/friends/accept", url.Values{"username": {"gowner"}}); code != http.StatusSeeOther {
		t.Fatalf("accept → %d", code)
	}

	a.get("/messages/new/group")
	code, loc := a.post("/messages/new", url.Values{
		"kind": {"group"}, "title": {"Study circle"},
		"member": {"gtwo", "gthree"},
	})
	if code != http.StatusSeeOther {
		t.Fatalf("creating a group → %d", code)
	}
	group := strings.TrimPrefix(loc, "/messages/")

	if code, _ := a.post("/messages/"+group, url.Values{"body": {"we meet on Thursday"}}); code != http.StatusSeeOther {
		t.Fatalf("sending into the group → %d", code)
	}

	// Both of the others can read it, and it says who wrote it.
	for _, reader := range []*app{b, c} {
		code, body := reader.get("/messages/" + group + "/poll?after=0")
		if code != http.StatusOK {
			t.Fatalf("poll → %d", code)
		}
		var said struct {
			Messages []struct {
				Body   string `json:"body"`
				Author *struct {
					Name string `json:"name"`
				} `json:"author"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &said); err != nil {
			t.Fatalf("poll answered %q", body)
		}
		if len(said.Messages) != 1 || said.Messages[0].Body != "we meet on Thursday" {
			t.Fatalf("a member of the group did not get the message: %s", body)
		}
		if said.Messages[0].Author == nil {
			t.Error("a group message arrived with nobody's name on it")
		}
	}
}

// You cannot put somebody in a group you do not know. An invitation into a
// conversation arrives uninvited, and that is only welcome from a friend.
func TestAStrangerCannotBePutInAGroup(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	a.register("grpowner")
	b.register("stranger")

	a.get("/messages/new/group")
	code, loc := a.post("/messages/new", url.Values{
		"kind": {"group"}, "title": {"Uninvited"}, "member": {"stranger"},
	})
	if code != http.StatusSeeOther {
		t.Fatalf("status %d", code)
	}
	if strings.HasPrefix(loc, "/messages/") && loc != "/messages/new/group" {
		t.Errorf("a group was made with a stranger in it: %q", loc)
	}
}

// A room is open: anybody can find it and walk in. That is the one listing
// that shows a thread to somebody who is not in it.
func TestARoomIsOpenToAnybody(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	a.register("roomowner")
	b.register("passerby")

	room := openRoom(t, a, "Tajweed practice", "Reading together on Fridays")

	// It is listed to somebody who has never been in it.
	code, body := b.get("/messages?tab=room")
	if code != http.StatusOK {
		t.Fatalf("rooms tab → %d", code)
	}
	if !strings.Contains(body, "Tajweed practice") {
		t.Fatal("an open room was not listed to somebody outside it")
	}

	a.post("/messages/"+room, url.Values{"body": {"we start at maghrib"}})

	// The link the directory offers has to lead somewhere. It leads to the
	// room — its name, and the way in — and answering "no such page" to a room
	// the same screen is inviting you to join was the listing contradicting
	// itself.
	code, door := b.get("/messages/" + room)
	if code != http.StatusOK {
		t.Fatalf("a room advertised to everybody answered %d when opened", code)
	}
	if !strings.Contains(door, "Tajweed practice") {
		t.Error("the room's own page does not name it")
	}

	// What is inside it is still not theirs to read. That is the part joining
	// is for, and it is the part that matters.
	if strings.Contains(door, "we start at maghrib") {
		t.Error("a room's messages were readable before joining it")
	}
	if strings.Contains(door, "data-compose") {
		t.Error("a room offered a composer to somebody who is not in it")
	}
	if code, _ := b.get("/messages/" + room + "/poll?after=0"); code == http.StatusOK {
		t.Error("a non-member could poll a room for its messages")
	}
	if code, _ := b.post("/messages/"+room, url.Values{"body": {"hello"}}); code == http.StatusSeeOther {
		t.Error("a non-member could write into a room")
	}

	if code, _ := b.post("/messages/"+room+"/join", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("joining → %d", code)
	}
	if code, _ := b.get("/messages/" + room); code != http.StatusOK {
		t.Error("a room could not be read after joining it")
	}

	// And now what is said in it reaches them.
	_, poll := b.get("/messages/" + room + "/poll?after=0")
	if !strings.Contains(poll, "we start at maghrib") {
		t.Errorf("a room message did not reach a member: %s", poll)
	}
}

// Leaving is how you stop being reachable somewhere. What was said stays where
// it is; you are simply no longer in the room it was said in.
func TestLeavingARoomClosesIt(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	a.register("stayer")
	b.register("leaver")

	room := openRoom(t, a, "Open floor", "")
	b.post("/messages/"+room+"/join", url.Values{})
	if code, _ := b.get("/messages/" + room); code != http.StatusOK {
		t.Fatalf("joined but cannot read")
	}

	a.post("/messages/"+room, url.Values{"body": {"said while they were here"}})

	if code, _ := b.post("/messages/"+room+"/leave", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("leaving → %d", code)
	}

	// The room is still a place they can look at and walk back into — it is
	// open, and they have not been barred. What they have lost is what is
	// inside it.
	code, door := b.get("/messages/" + room)
	if code != http.StatusOK {
		t.Fatalf("an open room answered %d after somebody left it", code)
	}
	if strings.Contains(door, "said while they were here") {
		t.Error("a room's messages were still readable after leaving it")
	}
	if code, _ := b.get("/messages/" + room + "/poll?after=0"); code == http.StatusOK {
		t.Error("somebody who left could still poll the room")
	}
}

// Nobody can write into a thread they are not in. This is the rule that stops
// a message reaching everybody in the app.
func TestAnOutsiderCannotWriteIntoAThread(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	c := newAppSharing(t, a)
	pair(t, a, b, "insider", "insidertwo")
	c.register("outsider")

	a.get("/messages/new/group")
	_, loc := a.post("/messages/new", url.Values{
		"kind": {"group"}, "title": {"Closed"}, "member": {"insidertwo"},
	})
	group := strings.TrimPrefix(loc, "/messages/")

	code, _ := c.post("/messages/"+group, url.Values{"body": {"let me in"}})
	if code == http.StatusSeeOther {
		// A redirect is what a successful send answers with, so check the
		// thread rather than trusting the status.
		_, body := a.get("/messages/" + group + "/poll?after=0")
		if strings.Contains(body, "let me in") {
			t.Fatal("somebody outside the group wrote into it")
		}
	}
	if code, _ := c.get("/messages/" + group); code == http.StatusOK {
		t.Error("somebody outside the group could read it")
	}
}

// A group counts unread per member. read_at on the message holds one answer,
// which is enough for one other reader and wrong for four.
func TestUnreadInAGroupIsCountedPerMember(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	c := newAppSharing(t, a)
	pair(t, a, b, "counter", "countertwo")
	c.register("counterthree")
	a.post("/friends/request", url.Values{"username": {"counterthree"}})
	c.post("/friends/accept", url.Values{"username": {"counter"}})

	a.get("/messages/new/group")
	_, loc := a.post("/messages/new", url.Values{
		"kind": {"group"}, "title": {"Counting"}, "member": {"countertwo", "counterthree"},
	})
	group := strings.TrimPrefix(loc, "/messages/")
	a.post("/messages/"+group, url.Values{"body": {"one"}})
	a.post("/messages/"+group, url.Values{"body": {"two"}})

	unread := func(x *app) int {
		code, body := x.get("/api/counts")
		if code != http.StatusOK {
			t.Fatalf("counts → %d", code)
		}
		var counts map[string]int
		if err := json.Unmarshal([]byte(body), &counts); err != nil {
			t.Fatalf("counts answered %q", body)
		}
		return counts["messages"]
	}

	if got := unread(b); got != 2 {
		t.Errorf("one member sees %d unread, want 2", got)
	}
	if got := unread(c); got != 2 {
		t.Errorf("the other member sees %d unread, want 2", got)
	}

	// One of them reads it. The other's count must not move.
	if code, _ := b.postBody("/messages/"+group+"/read", nil); code != http.StatusOK {
		t.Fatalf("read → %d", code)
	}
	if got := unread(b); got != 0 {
		t.Errorf("the member who read it still sees %d unread", got)
	}
	if got := unread(c); got != 2 {
		t.Errorf("one member reading it took the count from the other: %d, want 2", got)
	}
}

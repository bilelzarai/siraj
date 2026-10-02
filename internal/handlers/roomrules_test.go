package handlers_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Opening a room is not entering it.
//
// It used to be both, and the two together were a contradiction: a person is
// in one room at a time, so being put inside the room you had just named made
// opening a second one impossible — the one-room index refused the insert and
// the screen answered "leave the room you are in before opening another" to
// somebody who had not asked to go anywhere.
func TestOpeningARoomDoesNotEnterIt(t *testing.T) {
	a := newApp(t)
	a.register("open_not_enter")
	me, err := a.repo.UserByUsername(t.Context(), "open_not_enter")
	if err != nil {
		t.Fatal(err)
	}

	first := openRoom(t, a, "First place", "")
	if _, err := a.repo.CurrentRoom(t.Context(), me.ID); err == nil {
		t.Error("opening a room put its creator inside it")
	}
	if members, err := a.repo.Members(t.Context(), mustUUID(t, first)); err != nil || len(members) != 0 {
		t.Errorf("a new room holds %d people (err %v), want nobody", len(members), err)
	}

	// So a second one is an ordinary thing to open, and both are listed.
	second := openRoom(t, a, "Second place", "")
	_, body := a.get("/messages?tab=room")
	for _, name := range []string{"First place", "Second place"} {
		if !strings.Contains(body, name) {
			t.Errorf("the directory does not list %q", name)
		}
	}

	// Walking in is a separate, deliberate press — and the one that is
	// governed by the one-room rule.
	if status, _ := a.post("/messages/"+first+"/join", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the owner could not walk into their own room")
	}
	if status, _ := a.post("/messages/"+second+"/join", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("switching rooms was refused")
	}
	current, err := a.repo.CurrentRoom(t.Context(), me.ID)
	if err != nil {
		t.Fatalf("after switching they are in no room: %v", err)
	}
	if current.ID.String() != second {
		t.Error("switching did not land them in the room they chose")
	}
	if members, err := a.repo.Members(t.Context(), mustUUID(t, first)); err != nil || len(members) != 0 {
		t.Errorf("switching left %d behind in the old room (err %v)", len(members), err)
	}
}

// Rooms have no page of their own. The form lives beside the list it fills.
func TestThereIsNoPageForOpeningARoom(t *testing.T) {
	a := newApp(t)
	a.register("no_room_page")

	if status, _ := a.get("/messages/new/room"); status != http.StatusNotFound {
		t.Errorf("GET /messages/new/room → %d, want 404 — the page was removed", status)
	}
	// And the form that replaced it is on the list screen.
	_, body := a.get("/messages?tab=room")
	if !strings.Contains(body, `action="/messages/new"`) {
		t.Error("the rooms list carries no way to open one")
	}
	// A group still has its screen: choosing people from a list is the larger
	// half of making one.
	if status, _ := a.get("/messages/new/group"); status != http.StatusOK {
		t.Errorf("GET /messages/new/group → %d, want the group screen", status)
	}
}

// Signing up mid-session leaves the temporary room behind.
//
// Clearing the cookie was not enough: the guest row lives on its own timer, so
// a name stood in a temporary room for the rest of the day and the room could
// not be collected while it was there.
func TestSigningUpLeavesTheTemporaryRoom(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 968000, 12)
	guest := startGuest(t, newAppSharing(t, a))

	room := enterRoom(t, guest, "Left behind", "")
	was := guestID(t, guest)

	guest.register("upgraded_player")

	if _, err := a.repo.CurrentRoom(t.Context(), was); err == nil {
		t.Error("the temporary player is still standing in their old room")
	}
	members, err := a.repo.Members(t.Context(), mustUUID(t, room))
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 0 {
		t.Errorf("the room still holds %d after its only guest signed up", len(members))
	}

	// And the account starts in no room at all.
	now, err := a.repo.UserByUsername(t.Context(), "upgraded_player")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.CurrentRoom(t.Context(), now.ID); err == nil {
		t.Error("the new account inherited the guest's room")
	}
}

// Every name in a list leads somewhere, or is not a link.
//
// A temporary player has no public profile — that is the rule — so a row that
// offered one answered Not Found. In a guest's room every row did.
func TestNoListOffersALinkToAPageThatIsNotThere(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 969000, 12)

	host := startGuest(t, newAppSharing(t, a))
	visitor := startGuest(t, newAppSharing(t, a))
	room := enterRoom(t, host, "No dead links", "")
	if status, _ := visitor.post("/messages/"+room+"/join", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the visitor could not join")
	}
	them, err := a.repo.UserByID(t.Context(), guestID(t, visitor))
	if err != nil {
		t.Fatal(err)
	}

	// The member list is rendered three ways — in the thread, as a fragment
	// and as its own page — and all three go through the same component, so
	// all three are checked.
	for _, page := range []string{
		"/messages/" + room,
		"/messages/" + room + "/members",
		"/challenges/new",
		"/challenges/new?group=room",
	} {
		status, body := host.get(page)
		if status != http.StatusOK {
			continue
		}
		if strings.Contains(body, `"/u/`+them.Username) {
			t.Errorf("%s links to a profile page that answers 404", page)
		}
		if strings.Contains(body, "/messages/with/"+them.Username) {
			t.Errorf("%s offers a private thread with somebody who cannot have one", page)
		}
	}

	// Pressing Challenge beside them has to reach the setup screen, not 404.
	status, body := host.get("/challenges/new?opponent=" + them.Username + "&group=room")
	if status != http.StatusOK {
		t.Fatalf("challenging somebody in your room → %d", status)
	}
	if !strings.Contains(body, them.DisplayName) {
		t.Error("the setup screen does not show who the challenge is against")
	}
}

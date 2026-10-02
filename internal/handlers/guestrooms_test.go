package handlers_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/service"
)

// Rooms for players without an account.
//
// A guest could play and had nobody to play with: the one place in this
// application where strangers meet is a room, and rooms were behind an
// account. They are not any more, and these are the four things that keeps
// true — a guest can open one, a guest can join one, the two kinds of room
// never mix, and a guest is in one room at a time like everybody else.

// startGuest presses "play without an account" and returns the browser.
func startGuest(t *testing.T, a *app) *app {
	t.Helper()
	a.get("/") // for the CSRF cookie
	if status, _ := a.post("/guest", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("could not start as a guest")
	}
	return a
}

func TestAGuestCanOpenARoomAndItIsTemporary(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 990000, 12)
	guest := startGuest(t, a)

	id := openRoom(t, guest, "Guest room", "")

	status, body := guest.get("/messages/" + id)
	if status != http.StatusOK {
		t.Fatalf("the guest cannot reach the room they opened → %d", status)
	}
	if !strings.Contains(body, "Guest room") {
		t.Error("the room does not carry its own name")
	}

	// Temporary, and it says so rather than leaving it to be discovered.
	conv, err := a.repo.RoomPreview(t.Context(), mustUUID(t, id), guestID(t, a))
	if err != nil {
		t.Fatalf("reading the room back: %v", err)
	}
	if !conv.IsTemporary {
		t.Error("a room opened by a guest is not marked temporary")
	}
}

func TestAGuestRoomAndAnAccountRoomNeverMix(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 991000, 12)

	// An account's room.
	owner := newAppSharing(t, a)
	owner.register("room_owner")
	accountRoom := enterRoom(t, owner, "Account room", "")

	// A guest's room, in a different browser.
	guest := startGuest(t, newAppSharing(t, a))
	guestRoom := enterRoom(t, guest, "Temporary room", "")

	// Neither directory shows the other's rooms.
	if _, body := guest.get("/messages?tab=room"); strings.Contains(body, "Account room") {
		t.Error("the guest's room directory lists an account's room")
	}
	if _, body := owner.get("/messages?tab=room"); strings.Contains(body, "Temporary room") {
		t.Error("an account's room directory lists a guest's room")
	}

	// And a guessed id gets them no further than the listing did. The join
	// is refused, and the refusal is an answer rather than a fault.
	if status, _ := guest.post("/messages/"+accountRoom+"/join", url.Values{}); status >= 500 {
		t.Errorf("a guest at the door of an account's room → %d", status)
	}
	if status, _ := owner.post("/messages/"+guestRoom+"/join", url.Values{}); status >= 500 {
		t.Errorf("an account at the door of a guest's room → %d", status)
	}

	// Neither got in.
	for _, c := range []struct {
		who  *app
		room string
		name string
	}{{guest, accountRoom, "guest"}, {owner, guestRoom, "account"}} {
		members, err := a.repo.Members(t.Context(), mustUUID(t, c.room))
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 1 {
			t.Errorf("the %s got into a room of the other kind: %d members, want only the owner",
				c.name, len(members))
		}
	}
}

// The database refuses it too, with the service layer taken out of the way.
// The route is one way into a membership row and not the only one.
func TestTheDatabaseRefusesAMixedRoom(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 992000, 12)

	owner := newAppSharing(t, a)
	owner.register("mixed_owner")
	accountRoom := mustUUID(t, enterRoom(t, owner, "Members only", ""))

	guest := startGuest(t, newAppSharing(t, a))

	if _, err := testPool.Exec(t.Context(),
		`INSERT INTO conversation_members (conversation_id, user_id) VALUES ($1, $2)`,
		accountRoom, guestID(t, guest)); err == nil {
		t.Error("the database let a temporary player into a permanent room")
	}
}

func TestAGuestIsInOneRoomAtATime(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 993000, 12)

	first := startGuest(t, newAppSharing(t, a))
	second := startGuest(t, newAppSharing(t, a))

	mine := enterRoom(t, first, "First room", "")
	theirs := openRoom(t, second, "Second room", "")

	// Opening a room is not entering it, so the second guest is in none of
	// them and the first is in exactly one.
	if _, err := a.repo.CurrentRoom(t.Context(), guestID(t, second)); err == nil {
		t.Error("opening a room put its creator inside it")
	}

	// Walking into another is allowed, and it is a move rather than an
	// addition: in the new room, out of the old one, in one operation.
	if status, _ := first.post("/messages/"+theirs+"/join", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the guest could not join the other room")
	}
	current, err := a.repo.CurrentRoom(t.Context(), guestID(t, first))
	if err != nil {
		t.Fatalf("the guest is in no room at all: %v", err)
	}
	if current.ID.String() != theirs {
		t.Errorf("the guest is in %s, want the room they just joined", current.ID)
	}
	members, err := a.repo.Members(t.Context(), mustUUID(t, mine))
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 0 {
		t.Errorf("the room they left still holds %d of them", len(members))
	}
}

// Two guests in a room can reach each other: that is the whole point of
// letting them have one.
func TestGuestsInARoomCanReachEachOther(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 994000, 12)

	host := startGuest(t, newAppSharing(t, a))
	visitor := startGuest(t, newAppSharing(t, a))

	room := enterRoom(t, host, "Play together", "")
	if status, _ := visitor.post("/messages/"+room+"/join", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the second guest could not join")
	}

	// They are in the same room, which is what makes them reachable at all.
	shared, err := a.repo.ShareRoom(t.Context(), guestID(t, host), guestID(t, visitor))
	if err != nil {
		t.Fatal(err)
	}
	if !shared {
		t.Fatal("two guests in one room do not count as sharing it")
	}

	// And the room is a room: it carries what they say to each other.
	if status, _ := visitor.post("/messages/"+room, url.Values{"body": {"salam"}}); status >= 400 {
		t.Errorf("a guest writing in their own room → %d", status)
	}
	if _, body := host.get("/messages/" + room); !strings.Contains(body, "salam") {
		t.Error("the other guest in the room cannot see what was said")
	}
}

// The room outlives its owner's deletion and is collected afterwards, because
// a room is the people in it rather than the person who named it.
func TestAnEmptyGuestRoomIsSweptAndAFullOneIsNot(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 995000, 12)

	lonely := startGuest(t, newAppSharing(t, a))
	empty := mustUUID(t, enterRoom(t, lonely, "Nobody left", ""))

	busy := startGuest(t, newAppSharing(t, a))
	occupied := mustUUID(t, enterRoom(t, busy, "Still here", ""))

	// The first guest's time is up and they are swept, which takes their
	// membership with them and leaves the room standing and empty.
	if _, err := testPool.Exec(t.Context(),
		`UPDATE users SET expires_at = now() - interval '1 hour' WHERE id = $1`,
		guestID(t, lonely)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.PurgeExpiredGuests(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Named rooms rather than a count: every test in this package shares one
	// database, so the sweep legitimately collects other tests' leavings too.
	if _, err := a.repo.PurgeEmptyGuestRooms(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.RoomPreview(t.Context(), empty, guestID(t, busy)); err == nil {
		t.Error("the empty guest room is still there")
	}
	if _, err := a.repo.RoomPreview(t.Context(), occupied, guestID(t, busy)); err != nil {
		t.Errorf("the sweep took a room that still had somebody in it: %v", err)
	}
}

// A guest cannot make a group, which is the other half of "rooms and nothing
// else": the route is open now, so the kind has to be checked rather than
// assumed from who could reach it.
func TestAGuestCannotMakeAGroupThroughTheRoomRoute(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 996000, 12)
	guest := startGuest(t, a)

	status, _ := guest.post("/messages/new", url.Values{
		"kind": {"group"}, "title": {"Sneaky"}, "request_key": {"sneaky-1"},
	})
	if status >= 500 {
		t.Fatalf("a guest posting a group → %d", status)
	}
	var groups int
	if err := testPool.QueryRow(t.Context(),
		`SELECT count(*) FROM conversations WHERE kind = 'group' AND title = 'Sneaky'`).
		Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if groups != 0 {
		t.Errorf("a guest made %d groups", groups)
	}
}

// guestID is which temporary player this particular browser is.
//
// Read from the browser's own guest cookie rather than from the newest row in
// the table: these tests run several guests against one server, and "the last
// one created" is whichever goroutine got there last.
func guestID(t *testing.T, a *app) uuid.UUID {
	t.Helper()
	base, err := url.Parse(a.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range a.client.Jar.Cookies(base) {
		if c.Name != service.GuestCookie {
			continue
		}
		guest, err := a.repo.GuestByKey(t.Context(), c.Value)
		if err != nil {
			t.Fatalf("resolving the guest cookie: %v", err)
		}
		return guest.ID
	}
	t.Fatal("this browser is not a guest")
	return uuid.Nil
}

// The point of the whole thing: two guests who met in a room can play each
// other. A room that cannot produce a match is a chat room, and that is not
// what anonymous play was missing.
func TestTwoGuestsInARoomCanPlayEachOther(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 997000, 12)

	host := startGuest(t, newAppSharing(t, a))
	rival := startGuest(t, newAppSharing(t, a))

	room := enterRoom(t, host, "Match room", "")
	if status, _ := rival.post("/messages/"+room+"/join", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the second guest could not join")
	}

	// A random opponent comes from the room and from nowhere else, so this is
	// also the check that the room is the thing being drawn from.
	status, location := host.post("/challenges/random", url.Values{})
	if status != http.StatusSeeOther {
		t.Fatalf("a guest drawing an opponent from their room → %d", status)
	}
	if !strings.Contains(location, "opponent=") {
		t.Fatalf("the draw found nobody in the room: landed at %q", location)
	}

	them, err := a.repo.UserByID(t.Context(), guestID(t, rival))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(location, them.Username) {
		t.Errorf("the draw picked somebody other than the guest in the room: %q", location)
	}

	// And the match that setup form opens is a real one between the two.
	status, _ = host.post("/challenges/new", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "opponent": {them.Username},
		"player_source": {"room"}, "request_key": {"guest-room-match"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("opening the match → %d", status)
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), guestID(t, host), "en")
	if err != nil {
		t.Fatalf("no match came out of the room: %v", err)
	}
	if !match.HasPlayer(guestID(t, rival)) {
		t.Error("the match was not against the other guest in the room")
	}
}

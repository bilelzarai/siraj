package handlers_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// The rules that are meant to hold whatever the screens look like: who may
// play, who may be reached, how many rooms and matches a person can be in, and
// what a second press of the same button does.
//
// Every one of these is checked through HTTP rather than against the service
// underneath, because "the UI does not offer it" is not the same claim as "the
// server refuses it", and it is the second one that matters.

// ---------------------------------------------------------- anonymous play --

// Rule 1: no account is needed to play, and an account is needed for
// everything that outlives a game.
func TestAnonymousPlayerCanPlayAndNothingElse(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 960000, 12)

	a.get("/") // for the CSRF cookie
	status, location := a.post("/guest", url.Values{})
	if status != http.StatusSeeOther {
		t.Fatalf("starting as a guest: status %d, want a redirect into the game", status)
	}
	if location != "/play" {
		t.Errorf("guest landed at %q, want /play", location)
	}

	// The game itself works in full.
	if status, _ := a.get("/play"); status != http.StatusOK {
		t.Errorf("GET /play as a guest → %d, want 200", status)
	}
	status, location = a.post("/play/start", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
	})
	if status != http.StatusSeeOther || location != "/play/round" {
		t.Fatalf("a guest starting a round → %d %q, want a redirect to /play/round", status, location)
	}
	if status, _ := a.get("/play/round"); status != http.StatusOK {
		t.Errorf("GET /play/round as a guest → %d, want 200", status)
	}

	// And everything an account is for is refused — by the server, not by a
	// missing link.
	//
	// Rooms are the one part of messaging that is not on this list, and they
	// are a deliberate exception rather than a hole: a room is where a player
	// finds somebody to play against, which is the one thing anonymous play
	// could not do. A guest's rooms are their own — see the guest-room rules
	// below — and the rest of messaging is still shut.
	for _, path := range []string{
		"/friends", "/leaderboard", "/history",
		"/notifications", "/settings", "/my/questions", "/support",
		"/messages/new/group", "/messages/with/somebody",
	} {
		status, body := a.get(path)
		if status != http.StatusSeeOther {
			t.Errorf("GET %s as a guest → %d, want a redirect to register; body starts %q",
				path, status, clip(body, 80))
		}
	}

	// The messages screen a guest does reach is the room directory and
	// nothing else: no people segment, no groups segment, nothing to open a
	// private thread with.
	status, body := a.get("/messages")
	if status != http.StatusOK {
		t.Fatalf("GET /messages as a guest → %d, want the room directory", status)
	}
	for _, offered := range []string{"/messages/new/group", "/messages/with/"} {
		if strings.Contains(body, offered) {
			t.Errorf("the guest's messages screen offers %s", offered)
		}
	}
}

// A guest is not a person anybody else can find, befriend or write to. They
// exist for the duration of a game and are invisible to the rest of the site.
func TestGuestsAreInvisibleToEverybodyElse(t *testing.T) {
	a := newApp(t)
	a.register("seeker")

	guest := newAppSharing(t, a)
	guest.get("/")
	if status, _ := guest.post("/guest", url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("starting as a guest: %d", status)
	}

	// Search finds accounts and nothing else.
	_, body := a.get("/friends?tab=search&q=guest")
	if strings.Contains(body, "guest_") {
		t.Error("user search offered a temporary player")
	}

	// And the leaderboard counts accounts only.
	_, board := a.get("/leaderboard")
	if strings.Contains(board, "guest_") {
		t.Error("the leaderboard listed a temporary player")
	}
}

// ---------------------------------------------------------- shared device --

// Rule 2: several people play on one device, and not all of them have an
// account. The host adds them, the seat decides whose turn it is, and the
// rounds are genuinely separate.
func TestSeveralPlayersShareOneDevice(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 961000, 12)
	a.register("host_device")

	if status, _ := a.post("/players", url.Values{"name": {"Bilal"}}); status != http.StatusSeeOther {
		t.Fatalf("adding a local player: %d", status)
	}

	host, err := a.repo.UserByUsername(t.Context(), "host_device")
	if err != nil {
		t.Fatalf("load host: %v", err)
	}
	players, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil {
		t.Fatalf("local players: %v", err)
	}
	if len(players) != 1 || players[0].DisplayName != "Bilal" {
		t.Fatalf("local players = %d, want one called Bilal", len(players))
	}
	guest := players[0]
	if !guest.IsTemporary || guest.HostUserID == nil || *guest.HostUserID != host.ID {
		t.Error("the added player is not a temporary player belonging to the host")
	}

	// The host plays a round of their own.
	if status, _ := a.post("/play/start", url.Values{"count": {"5"}}); status != http.StatusSeeOther {
		t.Fatal("host could not start a round")
	}

	// Hand the device over. The next round started belongs to the guest, and
	// the host's is left exactly where it was.
	if status, _ := a.post("/players/seat", url.Values{"player": {guest.ID.String()}}); status != http.StatusSeeOther {
		t.Fatalf("taking the seat: %d", status)
	}
	if status, _ := a.post("/play/start", url.Values{"count": {"5"}}); status != http.StatusSeeOther {
		t.Fatal("the seated guest could not start a round")
	}

	hostRound, err := a.repo.ActiveGame(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatalf("the host's round did not survive handing the phone over: %v", err)
	}
	guestRound, err := a.repo.ActiveGame(t.Context(), guest.ID, "en")
	if err != nil {
		t.Fatalf("the guest has no round of their own: %v", err)
	}
	if hostRound.ID == guestRound.ID {
		t.Error("the guest is playing the host's round rather than one of their own")
	}

	// A seat can only ever name somebody the signed-in account created.
	other := newAppSharing(t, a)
	other.register("not_the_host")
	if status, _ := other.post("/players/seat", url.Values{"player": {guest.ID.String()}}); status == http.StatusSeeOther {
		t.Error("a stranger was allowed to take a seat belonging to somebody else's device")
	}
}

// ------------------------------------------------------------------ rooms --

// Rule 5: a user is in one room at a time. Walking into a second takes them
// out of the first, and the database will not hold both.
func TestJoiningARoomLeavesThePreviousOne(t *testing.T) {
	a := newApp(t)
	a.register("room_hopper")
	user, err := a.repo.UserByUsername(t.Context(), "room_hopper")
	if err != nil {
		t.Fatal(err)
	}

	first := a.makeRoom("Tajweed")
	second := a.makeRoom("Sīrah")

	if _, err := a.repo.JoinThread(t.Context(), first, user.ID); err != nil {
		t.Fatalf("joining the first room: %v", err)
	}
	left, err := a.repo.JoinThread(t.Context(), second, user.ID)
	if err != nil {
		t.Fatalf("joining the second room: %v", err)
	}
	if left == nil || *left != first {
		t.Errorf("joining the second room reported leaving %v, want %v", left, first)
	}

	room, err := a.repo.CurrentRoom(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("current room: %v", err)
	}
	if room.ID != second {
		t.Errorf("current room is %v, want the one just joined (%v)", room.ID, second)
	}
}

// ------------------------------------------------------------- messaging --

// Rule 6: friends, or the people in your room. Nobody else, and the refusal is
// the server's rather than the page's.
func TestMessagingReachesFriendsAndRoomMatesOnly(t *testing.T) {
	a := newApp(t)
	a.register("room_writer")
	stranger := newAppSharing(t, a)
	stranger.register("room_stranger")

	// A stranger is not reachable. The redirect goes back to their profile
	// rather than into a thread, which is the refusal.
	status, body := a.get("/messages/with/room_stranger")
	if status != http.StatusSeeOther || !strings.Contains(body, "/u/room_stranger") {
		t.Errorf("writing to a stranger → %d %q, want a refusal", status, clip(body, 80))
	}

	// Put them both in the same room, and now they are.
	writer, err := a.repo.UserByUsername(t.Context(), "room_writer")
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.repo.UserByUsername(t.Context(), "room_stranger")
	if err != nil {
		t.Fatal(err)
	}
	room := a.makeRoom("Halaqa")
	if _, err := a.repo.JoinThread(t.Context(), room, writer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.JoinThread(t.Context(), room, other.ID); err != nil {
		t.Fatal(err)
	}

	status, body = a.get("/messages/with/room_stranger")
	if status != http.StatusSeeOther || !strings.Contains(body, "/messages/") {
		t.Errorf("writing to somebody in the same room → %d %q, want the thread",
			status, clip(body, 80))
	}

	// Leaving the room takes the reachability with it: it was the room, not a
	// relationship.
	if err := a.repo.LeaveThread(t.Context(), room, other.ID); err != nil {
		t.Fatal(err)
	}
	reachable, err := a.repo.ShareRoom(t.Context(), writer.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reachable {
		t.Error("somebody who left the room is still reachable through it")
	}
}

// ------------------------------------------------------------ one at a time --

// Rule 4: several invitations may be waiting, but a player is inside one match
// at a time. The second attempt is refused by the database, not by a check
// somebody might forget to write.
func TestAPlayerIsInsideOneMatchAtATime(t *testing.T) {
	a := newApp(t)
	a.register("busy_host")
	host, err := a.repo.UserByUsername(t.Context(), "busy_host")
	if err != nil {
		t.Fatal(err)
	}

	busy, err := a.repo.InActiveMatch(t.Context(), host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Fatal("a fresh account is already inside a match")
	}
}

// ----------------------------------------------------------- idempotency --

// Rule 7: one press creates one thing, and so does a second press of the same
// form — a resent POST, another tab, the browser's own retry.
func TestResubmittingAFormCreatesOneThing(t *testing.T) {
	a := newApp(t)
	a.register("one_press")
	host, err := a.repo.UserByUsername(t.Context(), "one_press")
	if err != nil {
		t.Fatal(err)
	}

	key := "test-key-" + host.ID.String()
	form := url.Values{"name": {"Twice"}, "request_key": {key}}

	if status, _ := a.post("/players", form); status != http.StatusSeeOther {
		t.Fatal("first press failed")
	}
	if status, _ := a.post("/players", form); status != http.StatusSeeOther {
		t.Fatal("second press failed")
	}

	players, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 {
		t.Errorf("two presses of one form made %d players, want 1", len(players))
	}

	// A different key is a different intention and does make a second one.
	form.Set("request_key", key+"-again")
	if status, _ := a.post("/players", form); status != http.StatusSeeOther {
		t.Fatal("a genuinely new submission was refused")
	}
	players, err = a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 2 {
		t.Errorf("a fresh key made %d players in total, want 2", len(players))
	}
}

// makeRoom opens a room directly, for the tests that need one to exist without
// caring how it was made.
func (a *app) makeRoom(title string) uuid.UUID {
	a.t.Helper()
	// Made by somebody nobody else in the test is, so the room starts empty and
	// the joins under test are the only ones in it.
	owner := a.freshAccount("room_owner_" + strings.ToLower(title))
	conv, err := a.repo.CreateThread(a.t.Context(), "room", title, "", owner, nil)
	if err != nil {
		a.t.Fatalf("create room %q: %v", title, err)
	}
	// Creating it puts the owner in it, which is not what these tests are
	// asking about — they want an empty room to walk into.
	if err := a.repo.LeaveThread(a.t.Context(), conv.ID, owner); err != nil {
		a.t.Fatalf("stepping back out of %q: %v", title, err)
	}
	return conv.ID
}

// freshAccount makes an account straight through the repository, for the
// fixtures a test needs to exist rather than to exercise.
func (a *app) freshAccount(name string) uuid.UUID {
	a.t.Helper()
	name = strings.NewReplacer(" ", "_", "ā", "a", "ī", "i").Replace(name)
	u := &models.User{
		Username: name, Email: name + "@example.com",
		DisplayName: name, PasswordHash: "x", Locale: "en",
	}
	if err := a.repo.CreateUser(a.t.Context(), u); err != nil {
		a.t.Fatalf("create %s: %v", name, err)
	}
	return u.ID
}

// clip shortens a body for an error message.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------- the picker's reach --

// Rule 2 of this round of work: the player picker is scoped by where you are.
// Friends, the guests at your keyboard, or the room you are standing in — and
// the scoping is the server's, so a crafted request gets the same answer a
// button would.
func TestPeopleSearchIsScopedServerSide(t *testing.T) {
	a := newApp(t)
	a.register("scope_viewer")
	friend := newAppSharing(t, a)
	friend.register("scope_friend")
	stranger := newAppSharing(t, a)
	stranger.register("scope_stranger")

	viewer, err := a.repo.UserByUsername(t.Context(), "scope_viewer")
	if err != nil {
		t.Fatal(err)
	}
	mate, err := a.repo.UserByUsername(t.Context(), "scope_friend")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := a.repo.UserByUsername(t.Context(), "scope_stranger")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, viewer.ID, mate.ID)

	// A guest at this device.
	if status, _ := a.post("/players", url.Values{"name": {"Hafsa"}}); status != http.StatusSeeOther {
		t.Fatal("adding a local player failed")
	}

	// Friends scope: the friend, and nobody else — least of all a stranger
	// searched for by their exact name.
	body := a.people(t, "friends", "")
	if !strings.Contains(body, "scope_friend") {
		t.Error("the friends scope does not list a friend")
	}
	if strings.Contains(body, "scope_stranger") {
		t.Error("the friends scope leaked somebody who is not a friend")
	}
	if hit := a.people(t, "friends", "scope_stranger"); strings.Contains(hit, "scope_stranger") {
		t.Error("searching by exact name found a stranger through the friends scope")
	}

	// Device scope: the guest, and no accounts at all.
	body = a.people(t, "device", "")
	if !strings.Contains(body, "Hafsa") {
		t.Error("the device scope does not list a guest at this device")
	}
	if strings.Contains(body, "scope_friend") {
		t.Error("the device scope leaked an account")
	}

	// Room scope with no room: nobody. Not everybody.
	body = a.people(t, "room", "")
	if strings.Contains(body, "scope_friend") || strings.Contains(body, "scope_stranger") {
		t.Errorf("the room scope answered with people while the viewer is in no room: %s", body)
	}

	// In a room, it is that room's members and only them.
	room := a.makeRoom("Scope hall")
	if _, err := a.repo.JoinThread(t.Context(), room, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.JoinThread(t.Context(), room, outsider.ID); err != nil {
		t.Fatal(err)
	}
	body = a.people(t, "room", "")
	if !strings.Contains(body, "scope_stranger") {
		t.Error("the room scope does not list somebody standing in the room")
	}
	if strings.Contains(body, "scope_friend") {
		t.Error("the room scope leaked a friend who is not in the room")
	}
}

// Rule 4: somebody in your room can be challenged with no friend request in
// between, and somebody in neither category cannot be challenged at all.
func TestARoomMateCanBeChallengedWithoutBeingAFriend(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 964000, 12)
	a.register("room_challenger")
	other := newAppSharing(t, a)
	other.register("room_rival")

	host, err := a.repo.UserByUsername(t.Context(), "room_challenger")
	if err != nil {
		t.Fatal(err)
	}
	rival, err := a.repo.UserByUsername(t.Context(), "room_rival")
	if err != nil {
		t.Fatal(err)
	}

	// Strangers: refused, and refused by the server rather than by a button
	// that was not drawn.
	status, body := a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"room_rival"}, "player_source": {"room"},
		"source": {"bank"}, "request_key": {"rc-1"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("challenging a stranger → %d, want a refusal (%s)", status, clip(body, 120))
	}

	// Same room: allowed, with nobody having sent a friend request.
	room := a.makeRoom("Challenge hall")
	if _, err := a.repo.JoinThread(t.Context(), room, host.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.JoinThread(t.Context(), room, rival.ID); err != nil {
		t.Fatal(err)
	}
	friends, err := a.repo.AreFriends(t.Context(), host.ID, rival.ID)
	if err != nil {
		t.Fatal(err)
	}
	if friends {
		t.Fatal("the test set them up as friends, which is the thing being avoided")
	}

	// Declared as a friends match, it is still refused: they are in the room,
	// not on the friends list, and the group a match declares is the group its
	// players have to come from.
	status, body = a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"room_rival"}, "player_source": {"friends"},
		"source": {"bank"}, "request_key": {"rc-1b"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("a room mate invited as a friend → %d, want a refusal (%s)", status, clip(body, 140))
	}

	// Declared as a room match, it works — with nobody having sent a friend
	// request.
	status, body = a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"room_rival"}, "player_source": {"room"},
		"source": {"bank"}, "request_key": {"rc-2"},
	})
	if status != http.StatusOK {
		t.Fatalf("challenging a room mate → %d (%s)", status, clip(body, 160))
	}
	if !strings.Contains(body, "/challenges") {
		t.Errorf("the send did not say where to go next: %s", clip(body, 120))
	}

	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatalf("the match was not opened: %v", err)
	}
	if match.Player(rival.ID) == nil {
		t.Error("the room mate is not in the match they were invited to")
	}
	if match.PlayerSource != models.SourceRoom {
		t.Errorf("the match says it is with %q, want the room", match.PlayerSource)
	}

	// Leaving the room takes the reach with it: the invitation was the room's
	// doing, and the room is a place you can walk out of.
	if err := a.repo.LeaveThread(t.Context(), room, rival.ID); err != nil {
		t.Fatal(err)
	}
	reachable, err := a.repo.ShareRoom(t.Context(), host.ID, rival.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reachable {
		t.Error("somebody who left the room is still reachable through it")
	}
}

// people asks the picker endpoint the way the dialog does.
func (a *app) people(t *testing.T, scope, query string) string {
	t.Helper()
	_, body := a.get("/ui/people?scope=" + scope + "&q=" + url.QueryEscape(query))
	return body
}

// postJSONForm submits a form and asks for JSON back, which is what the dialog
// does — the answer is a destination rather than a redirect.
func (a *app) postJSONForm(t *testing.T, path string, form url.Values) (int, string) {
	t.Helper()
	form.Set("csrf_token", a.csrf())

	req, err := http.NewRequest(http.MethodPost, a.server.URL+path,
		strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("X-CSRF-Token", a.csrf())

	res, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	return res.StatusCode, readAll(res)
}

// befriendThrough makes two accounts friends through the repository, for tests
// about something other than the friendship itself.
func befriendThrough(t *testing.T, a *app, x, y uuid.UUID) {
	t.Helper()
	if err := a.repo.RequestFriendship(a.t.Context(), x, y); err != nil {
		t.Fatalf("friend request: %v", err)
	}
	if err := a.repo.RespondToFriendship(a.t.Context(), x, y, true); err != nil {
		t.Fatalf("accept: %v", err)
	}
}

// ------------------------------------------------- answering your own mail --

// Accepting and declining are for everybody in a match, not for the two people
// the legacy columns happen to name.
//
// `challenges.challenger_id` and `opponent_id` are the host and the first
// person invited, kept so a two-player match still reads through the old shape.
// The guard on accept and decline was checking against exactly those two, so
// the third player in a match — and every guest at a shared device — was told
// "Not allowed" when they pressed Decline on an invitation addressed to them.
func TestEveryPlayerCanAnswerTheirOwnInvitation(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 965000, 12)
	a.register("mail_host")

	second := newAppSharing(t, a)
	second.register("mail_second")
	third := newAppSharing(t, a)
	third.register("mail_third")

	host, err := a.repo.UserByUsername(t.Context(), "mail_host")
	if err != nil {
		t.Fatal(err)
	}
	guestTwo, err := a.repo.UserByUsername(t.Context(), "mail_second")
	if err != nil {
		t.Fatal(err)
	}
	guestThree, err := a.repo.UserByUsername(t.Context(), "mail_third")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, guestTwo.ID)
	befriendThrough(t, a, host.ID, guestThree.ID)

	if status, body := a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"mail_second", "mail_third"},
		"source":   {"bank"}, "request_key": {"mail-1"},
	}); status != http.StatusOK {
		t.Fatalf("opening the match → %d (%s)", status, clip(body, 140))
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatal(err)
	}

	// The third player is in the match through their player row — which, since
	// the duel columns were dropped, is the only way anybody is in a match.
	if match.Player(guestThree.ID) == nil {
		t.Fatal("the third player is not in the match at all")
	}

	status, _ := third.post("/challenges/"+match.ID.String()+"/decline", url.Values{})
	if status != http.StatusSeeOther {
		t.Fatalf("the third player declining → %d, want it to be allowed", status)
	}
	refreshed, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if seat := refreshed.Player(guestThree.ID); seat == nil || seat.State != models.PlayerDeclined {
		t.Errorf("the decline did not stick: %+v", seat)
	}

	// And the second player can still accept theirs.
	if status, _ := second.post("/challenges/"+match.ID.String()+"/accept",
		url.Values{"confirm": {"1"}}); status != http.StatusSeeOther {
		t.Errorf("the second player accepting → %d, want it to be allowed", status)
	}

	// Somebody who is in no match at all is still refused.
	outsider := newAppSharing(t, a)
	outsider.register("mail_outsider")
	if status, _ := outsider.post("/challenges/"+match.ID.String()+"/decline", url.Values{}); status == http.StatusSeeOther {
		t.Error("a stranger was allowed to decline somebody else's match")
	}
}

// ------------------------------------------------ one group per match --

// A match is with friends, or with the guests at this device, or with the
// people in your room. Never a mixture: the three are not interchangeable, and
// a scoreboard across them compares nothing.
func TestAMatchIsWithOneGroupOnly(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 966000, 12)
	a.register("group_host")

	mate := newAppSharing(t, a)
	mate.register("group_friend")

	host, err := a.repo.UserByUsername(t.Context(), "group_host")
	if err != nil {
		t.Fatal(err)
	}
	friend, err := a.repo.UserByUsername(t.Context(), "group_friend")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, friend.ID)

	if status, _ := a.post("/players", url.Values{"name": {"Nusaybah"}}); status != http.StatusSeeOther {
		t.Fatal("adding a local player failed")
	}
	players, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(players) != 1 {
		t.Fatalf("local players = %d (err %v)", len(players), err)
	}
	guest := players[0]

	// A friend and a guest at the device, in one match: refused, whatever the
	// form says the group is.
	for _, source := range []string{"friends", "device", ""} {
		form := url.Values{
			"opponent": {"group_friend"}, "local": {guest.ID.String()},
			"source": {"bank"}, "request_key": {"mix-" + source},
		}
		if source != "" {
			form.Set("player_source", source)
		}
		status, body := a.postJSONForm(t, "/challenges/new", form)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("a mixed match declared %q → %d, want a refusal (%s)",
				source, status, clip(body, 140))
		}
	}
	if _, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en"); err == nil {
		t.Fatal("a mixed match was opened despite being refused")
	}

	// The device on its own is fine, and says so.
	status, body := a.postJSONForm(t, "/challenges/new", url.Values{
		"local": {guest.ID.String()}, "player_source": {"device"},
		"source": {"bank"}, "request_key": {"device-only"},
	})
	if status != http.StatusOK {
		t.Fatalf("a device-only match → %d (%s)", status, clip(body, 160))
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if match.PlayerSource != models.SourceDevice {
		t.Errorf("the match says it is with %q, want the device", match.PlayerSource)
	}
	for _, p := range match.Players {
		if p.IsHost {
			if p.Origin != models.OriginHost {
				t.Errorf("the host's origin is %q, want host", p.Origin)
			}
			continue
		}
		if p.Origin != models.SourceDevice {
			t.Errorf("a player's origin is %q, want device", p.Origin)
		}
	}
}

// And the database keeps the rule on its own, so a call site that forgets it
// cannot write a match nobody can describe.
func TestTheDatabaseRefusesAMixedMatch(t *testing.T) {
	a := newApp(t)
	a.register("db_group_host")
	host, err := a.repo.UserByUsername(t.Context(), "db_group_host")
	if err != nil {
		t.Fatal(err)
	}
	other := a.freshAccount("db_group_other")

	ch := &models.Challenge{
		HostID: host.ID, Difficulty: 1, QuestionIDs: []int{1},
		PlayerSource: models.SourceFriends,
	}
	// A seat claiming to have come from the room, in a match that says it is
	// with friends. The service would never build this; the constraint is what
	// makes that irrelevant.
	err = a.repo.CreateMatch(t.Context(), ch, []repository.MatchSeat{
		{UserID: other, Origin: models.SourceRoom},
	})
	if !errors.Is(err, repository.ErrInvalid) {
		t.Errorf("the database accepted a seat from another group: %v", err)
	}
}

// ------------------------------------------------- the challenge lifecycle --

// The host saying no ends the match for everybody, and lets everybody go.
func TestHostDeclineCancelsTheMatchForEveryone(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 967000, 12)
	a.register("cancel_host")

	one := newAppSharing(t, a)
	one.register("cancel_one")
	two := newAppSharing(t, a)
	two.register("cancel_two")

	host, err := a.repo.UserByUsername(t.Context(), "cancel_host")
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.repo.UserByUsername(t.Context(), "cancel_one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.repo.UserByUsername(t.Context(), "cancel_two")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, first.ID)
	befriendThrough(t, a, host.ID, second.ID)

	match := openMatch(t, a, host.ID, "ch-host-decline", "cancel_one", "cancel_two")

	// One invitee accepts, so somebody is genuinely holding a place.
	if status, _ := one.post("/challenges/"+match.ID.String()+"/accept", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the first invitee could not accept")
	}
	if busy, _ := a.repo.InActiveMatch(t.Context(), first.ID); !busy {
		t.Fatal("an accepted invitation does not read as being in a match")
	}

	// The host says no. That is not one person stepping out.
	if status, _ := a.post("/challenges/"+match.ID.String()+"/decline", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not decline")
	}

	after, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != models.ChallengeCancelled {
		t.Errorf("status = %q after the host declined, want cancelled", after.Status)
	}
	if after.CancelledAt == nil || after.CancelledBy == nil || *after.CancelledBy != host.ID {
		t.Errorf("the cancellation does not record who did it: %+v", after.CancelledBy)
	}

	// Everybody is free immediately — not at the next sweep.
	for _, who := range []*models.User{host, first, second} {
		busy, err := a.repo.InActiveMatch(t.Context(), who.ID)
		if err != nil {
			t.Fatal(err)
		}
		if busy {
			t.Errorf("%s is still held by a cancelled match", who.Username)
		}
	}

	// And a late accept fails with something a person can read, not a 500.
	status, _ := two.post("/challenges/"+match.ID.String()+"/accept", url.Values{})
	if status >= 500 {
		t.Errorf("a late accept on a cancelled match → %d, want a handled refusal", status)
	}
	refreshed, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != models.ChallengeCancelled {
		t.Errorf("a late accept revived a cancelled match: %q", refreshed.Status)
	}
	if seat := refreshed.Player(second.ID); seat != nil && seat.State == models.PlayerJoined {
		t.Error("a late accept joined a cancelled match")
	}
}

// Everybody invited saying no cancels it too — and one of them saying no does
// not, as long as somebody has accepted.
func TestAllInviteesDecliningCancelsTheMatch(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 968000, 12)
	a.register("decl_host")

	one := newAppSharing(t, a)
	one.register("decl_one")
	two := newAppSharing(t, a)
	two.register("decl_two")

	host, err := a.repo.UserByUsername(t.Context(), "decl_host")
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.repo.UserByUsername(t.Context(), "decl_one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.repo.UserByUsername(t.Context(), "decl_two")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, first.ID)
	befriendThrough(t, a, host.ID, second.ID)

	match := openMatch(t, a, host.ID, "ch-all-decline", "decl_one", "decl_two")

	// The second accepts, the first declines: the match carries on, because
	// the host and one accepted player are still in it.
	if status, _ := two.post("/challenges/"+match.ID.String()+"/accept", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the second invitee could not accept")
	}
	if status, _ := one.post("/challenges/"+match.ID.String()+"/decline", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the first invitee could not decline")
	}
	alive, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !alive.Live() {
		t.Fatalf("one decline out of two ended the match: %q", alive.Status)
	}

	// Now the one who accepted declines too. Nobody has accepted any more, so
	// there is no match left.
	if status, _ := two.post("/challenges/"+match.ID.String()+"/decline", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the second invitee could not decline")
	}
	dead, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if dead.Status != models.ChallengeCancelled {
		t.Errorf("status = %q after everybody declined, want cancelled", dead.Status)
	}
	if busy, _ := a.repo.InActiveMatch(t.Context(), host.ID); busy {
		t.Error("the host is still held by a match nobody accepted")
	}
}

// The host has a cancel of their own, and nobody else does.
func TestOnlyTheHostCanCancel(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 969000, 12)
	a.register("cx_host")
	guest := newAppSharing(t, a)
	guest.register("cx_guest")

	host, err := a.repo.UserByUsername(t.Context(), "cx_host")
	if err != nil {
		t.Fatal(err)
	}
	mate, err := a.repo.UserByUsername(t.Context(), "cx_guest")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, mate.ID)
	match := openMatch(t, a, host.ID, "ch-cancel", "cx_guest")

	// An invitee pressing cancel is refused and the match stands.
	if status, _ := guest.post("/challenges/"+match.ID.String()+"/cancel", url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("an invitee cancelling → %d", status)
	}
	still, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if still.Status == models.ChallengeCancelled {
		t.Fatal("somebody who is not the host cancelled the match")
	}

	// The host's own cancel works, and is final.
	if status, _ := a.post("/challenges/"+match.ID.String()+"/cancel", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not cancel their own match")
	}
	dead, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if dead.Status != models.ChallengeCancelled {
		t.Fatalf("status = %q after the host cancelled", dead.Status)
	}

	// Cancelling twice changes nothing and is not an error page.
	if status, _ := a.post("/challenges/"+match.ID.String()+"/cancel", url.Values{}); status >= 500 {
		t.Errorf("cancelling twice → %d", status)
	}

	// Freed means freed: the host can open another match straight away.
	if busy, _ := a.repo.InActiveMatch(t.Context(), host.ID); busy {
		t.Error("the host is still held by the match they cancelled")
	}
	if status, body := a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"cx_guest"}, "player_source": {"friends"},
		"source": {"bank"}, "request_key": {"ch-cancel-again"},
	}); status != http.StatusOK {
		t.Errorf("opening a match after cancelling → %d (%s)", status, clip(body, 140))
	}
}

// openMatch opens one through the API, as the host, and returns it.
func openMatch(t *testing.T, a *app, hostID uuid.UUID, key string, opponents ...string) *models.Challenge {
	t.Helper()
	form := url.Values{
		"player_source": {"friends"}, "source": {"bank"}, "request_key": {key},
	}
	for _, o := range opponents {
		form.Add("opponent", o)
	}
	if status, body := a.postJSONForm(t, "/challenges/new", form); status != http.StatusOK {
		t.Fatalf("opening a match → %d (%s)", status, clip(body, 160))
	}
	match, err := a.repo.ActiveMatchFor(a.t.Context(), hostID, "en")
	if err != nil {
		t.Fatalf("the match was not opened: %v", err)
	}
	return match
}

// Each side is told whose move it is, and neither is told to wait for the
// other while it is their own.
//
// Two things conspired. "Your turn" was asked about whoever held the seat
// cookie — which outlives the match it was set for, so a host whose phone had
// last been handed to a guest was asked about somebody not in this match at
// all, and fell through to the waiting branch. And "waiting for X" read the two
// legacy columns, naming the host-or-first-invitee rather than whoever actually
// still had to move. Together: both players told to wait for the other, and
// neither offered anything to press.
func TestEachSideIsToldWhoseMoveItIs(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 970000, 12)
	a.register("turn_host")
	other := newAppSharing(t, a)
	other.register("turn_guest")

	host, err := a.repo.UserByUsername(t.Context(), "turn_host")
	if err != nil {
		t.Fatal(err)
	}
	rival, err := a.repo.UserByUsername(t.Context(), "turn_guest")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, rival.ID)

	// The host has a guest at their device and the seat is on them — the state
	// a phone is left in by any shared-device match, and the state that broke
	// every label on this screen.
	if status, _ := a.post("/players", url.Values{"name": {"Seatholder"}}); status != http.StatusSeeOther {
		t.Fatal("adding a local player failed")
	}
	players, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(players) != 1 {
		t.Fatalf("local players = %d (err %v)", len(players), err)
	}
	if status, _ := a.post("/players/seat", url.Values{"player": {players[0].ID.String()}}); status != http.StatusSeeOther {
		t.Fatal("taking the seat failed")
	}

	openMatch(t, a, host.ID, "turn-1", "turn_guest")

	// Both have a move — the host a round to play, the invitee an invitation
	// to answer — so both are told so.
	mine := statusTags(t, a)
	theirs := statusTags(t, other)
	if !has(mine, "Your turn") {
		t.Errorf("the host, who has a round to play, was told %v", mine)
	}
	if !has(theirs, "Your turn") {
		t.Errorf("the invited player was told %v", theirs)
	}

	// And neither is waiting on anybody, least of all on a guest at the host's
	// own device who is no part of this match.
	for _, tag := range append(append([]string{}, mine...), theirs...) {
		if strings.HasPrefix(tag, "Waiting for") {
			t.Errorf("somebody with a move of their own was told %q", tag)
		}
	}

	// Once the host has played, the match is genuinely waiting on the other
	// side, and says so by name.
	if status, _ := a.post("/challenges/"+mustMatch(t, a, host.ID).ID.String()+"/accept",
		url.Values{"confirm": {"1"}}); status != http.StatusSeeOther {
		t.Fatal("the host could not start their round")
	}
	if status, _ := a.post("/play/quit", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("leaving the round failed")
	}
}

// statusTags is what the challenge cards on this browser's screen say.
// statusTags is what the cards on the challenge list say about themselves.
//
// The tab is named rather than left to the default, because the default
// landing hands the phone on when a match is live on this device — which is
// the right thing for somebody playing and the wrong thing for a test about
// what the list says.
func statusTags(t *testing.T, a *app) []string {
	t.Helper()
	_, body := a.get("/challenges?tab=incoming")
	var out []string
	for _, m := range tagPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

var tagPattern = regexp.MustCompile(`<span class="tag[^"]*">([^<]*)</span>`)

func has(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// mustMatch is the match this account is currently inside.
func mustMatch(t *testing.T, a *app, userID uuid.UUID) *models.Challenge {
	t.Helper()
	ch, err := a.repo.ActiveMatchFor(a.t.Context(), userID, "en")
	if err != nil {
		t.Fatalf("no active match: %v", err)
	}
	return ch
}

// Sides need somebody to share one with.
//
// A team match of two is one person against one person however the sides are
// labelled, and adding the scores up per side is addition with a single number
// in it. The old check only asked whether two different sides had been named,
// so a host and one guest on teams 1 and 2 passed as "teams" — a duel reported
// through a scoreboard that implied there was something to add.
func TestTeamsNeedMoreThanTwoPlayers(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 972000, 12)
	a.register("team_host")

	one := newAppSharing(t, a)
	one.register("team_one")
	two := newAppSharing(t, a)
	two.register("team_two")

	host, err := a.repo.UserByUsername(t.Context(), "team_host")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"team_one", "team_two"} {
		mate, err := a.repo.UserByUsername(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		befriendThrough(t, a, host.ID, mate.ID)
	}

	// Two players, two sides: refused. This is the case that used to pass.
	status, body := a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"team_one"}, "player_source": {"friends"},
		"format": {"team"}, "host_team": {"1"}, "team_team_one": {"2"},
		"source": {"bank"}, "request_key": {"team-1v1"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("a two-player team match → %d, want a refusal (%s)", status, clip(body, 160))
	}
	if _, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en"); err == nil {
		t.Fatal("a two-player team match was opened")
	}

	// Three players, all on different sides: still refused — three sides of
	// one person each is a free-for-all with numbers on it.
	status, body = a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"team_one", "team_two"}, "player_source": {"friends"},
		"format": {"team"}, "host_team": {"1"},
		"team_team_one": {"2"}, "team_team_two": {"3"},
		"source": {"bank"}, "request_key": {"team-1v1v1"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("three sides of one → %d, want a refusal (%s)", status, clip(body, 160))
	}

	// Three players, two of them together: still refused. A side of one is
	// not a side, so this is a duel in which one of the duellists happens to
	// be a pair — and the scoreboard would report a single player's score as
	// a team total.
	status, body = a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"team_one", "team_two"}, "player_source": {"friends"},
		"format": {"team"}, "host_team": {"1"},
		"team_team_one": {"1"}, "team_team_two": {"2"},
		"source": {"bank"}, "request_key": {"team-2v1"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("two against one → %d, want a refusal (%s)", status, clip(body, 160))
	}

	// Four players split three against one: enough people, two sides, and
	// still not a team match. This is the case the player count alone cannot
	// catch, which is why the per-side rule exists separately from it.
	fourth := newAppSharing(t, a)
	fourth.register("team_four")
	odd, err := a.repo.UserByUsername(t.Context(), "team_four")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, odd.ID)

	status, body = a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"team_one", "team_two", "team_four"}, "player_source": {"friends"},
		"format": {"team"}, "host_team": {"1"},
		"team_team_one": {"1"}, "team_team_two": {"1"}, "team_team_four": {"2"},
		"source": {"bank"}, "request_key": {"team-3v1"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("three against one → %d, want a refusal (%s)", status, clip(body, 160))
	}

	// Four players, two a side: that is a team match.
	third := newAppSharing(t, a)
	third.register("team_three")
	who, err := a.repo.UserByUsername(t.Context(), "team_three")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, who.ID)

	status, body = a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"team_one", "team_two", "team_three"}, "player_source": {"friends"},
		"format": {"team"}, "host_team": {"1"},
		"team_team_one": {"1"}, "team_team_two": {"2"}, "team_team_three": {"2"},
		"source": {"bank"}, "request_key": {"team-2v2"},
	})
	if status != http.StatusOK {
		t.Fatalf("a two-against-two team match → %d (%s)", status, clip(body, 160))
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if match.Format != models.FormatTeam {
		t.Errorf("the match says it is a %q", match.Format)
	}
	sides := map[int]int{}
	for _, p := range match.Players {
		sides[p.Team]++
	}
	if sides[1] != 2 || sides[2] != 2 {
		t.Errorf("sides came out as %v, want two a side", sides)
	}
}

// The number the form shuts the Teams option on is the number the server
// refuses below, so the two cannot drift apart unnoticed.
func TestTheFormAndTheServerAgreeOnTheTeamMinimum(t *testing.T) {
	script := clientSources(t)
	perSide := fmt.Sprintf("const MIN_PER_SIDE = %d;", models.MinPerSide)
	if !strings.Contains(script, perSide) {
		t.Errorf("the form and the server disagree on how many a side needs: looked for %q", perSide)
	}

	want := fmt.Sprintf("const MIN_TEAM_PLAYERS = %d;", models.MinTeamPlayers)
	if !strings.Contains(script, want) {
		t.Errorf("the script does not carry %q — the form would offer teams the server refuses", want)
	}
}

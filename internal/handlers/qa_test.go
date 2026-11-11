package handlers_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// The paths a careful tester walks that the rest of the suite does not: a match
// called off while somebody is in the middle of it, two people answering at the
// same instant, hostile input, and the whole thing read right-to-left.

// QA-01 — the host calls the match off while another player is mid-round.
//
// Cancelling before anybody starts is the easy case and is covered elsewhere.
// This is the one with a live round attached to it: the round has to stop being
// part of a match, the player has to be released, and nothing may credit a
// score to a match that was called off.
func TestCancellingDuringPlayStopsTheMatch(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 973000, 12)
	a.register("mid_host")
	other := newAppSharing(t, a)
	other.register("mid_rival")

	host, err := a.repo.UserByUsername(t.Context(), "mid_host")
	if err != nil {
		t.Fatal(err)
	}
	rival, err := a.repo.UserByUsername(t.Context(), "mid_rival")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, rival.ID)

	match := openMatch(t, a, host.ID, "mid-1", "mid_rival")

	// The rival accepts and the host sets it going; the rival now holds a
	// live round.
	if status, _ := other.post("/challenges/"+match.ID.String()+"/accept", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the rival could not accept")
	}
	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not start the match")
	}
	if _, err := a.repo.ActiveGame(t.Context(), rival.ID, "en"); err != nil {
		t.Fatalf("the rival has no live round: %v", err)
	}

	// The host calls it off mid-round.
	if status, _ := a.post("/challenges/"+match.ID.String()+"/cancel", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not cancel")
	}

	after, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != models.ChallengeCancelled {
		t.Errorf("status = %q after a mid-play cancel, want cancelled", after.Status)
	}
	for _, who := range []*models.User{host, rival} {
		busy, err := a.repo.InActiveMatch(t.Context(), who.ID)
		if err != nil {
			t.Fatal(err)
		}
		if busy {
			t.Errorf("%s is still held by a cancelled match", who.Username)
		}
	}

	// The rival finishing the round they were already in must not settle a
	// match that no longer exists.
	if status, _ := other.post("/play/finish", url.Values{}); status >= 500 {
		t.Errorf("finishing a round from a cancelled match → %d", status)
	}
	settled, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != models.ChallengeCancelled {
		t.Errorf("a finish revived a cancelled match as %q", settled.Status)
	}
}

// QA-02 — two invitees decline at the same instant.
//
// The decision "is there still a match here" is read and written in the same
// breath. Taken apart, each request sees the other as still present and neither
// closes the match; taken together under a row lock, exactly one of them does.
func TestSimultaneousDeclinesCloseTheMatchExactlyOnce(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 974000, 12)
	a.register("race_host")

	one := newAppSharing(t, a)
	one.register("race_one")
	two := newAppSharing(t, a)
	two.register("race_two")

	host, err := a.repo.UserByUsername(t.Context(), "race_host")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"race_one", "race_two"} {
		mate, err := a.repo.UserByUsername(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		befriendThrough(t, a, host.ID, mate.ID)
	}

	match := openMatch(t, a, host.ID, "race-1", "race_one", "race_two")

	// Both refuse at once. Nobody has accepted, so the match has nothing left
	// to be — however the two requests interleave.
	var wg sync.WaitGroup
	for _, browser := range []*app{one, two} {
		wg.Add(1)
		go func(b *app) {
			defer wg.Done()
			b.post("/challenges/"+match.ID.String()+"/decline", url.Values{})
		}(browser)
	}
	wg.Wait()

	after, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != models.ChallengeCancelled {
		t.Errorf("status = %q after both invitees declined at once, want cancelled", after.Status)
	}
	if busy, _ := a.repo.InActiveMatch(t.Context(), host.ID); busy {
		t.Error("the host is still held by a match both invitees refused")
	}
	// Exactly one cancellation, not two.
	if after.CancelledAt == nil {
		t.Error("the match was closed without recording when")
	}
}

// QA-03 — a double-pressed accept spends one attempt, not two.
func TestAcceptingTwiceAtOnceStartsOneRound(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 975500, 12)
	a.register("dbl_host")
	other := newAppSharing(t, a)
	other.register("dbl_rival")

	host, err := a.repo.UserByUsername(t.Context(), "dbl_host")
	if err != nil {
		t.Fatal(err)
	}
	rival, err := a.repo.UserByUsername(t.Context(), "dbl_rival")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, rival.ID)
	match := openMatch(t, a, host.ID, "dbl-1", "dbl_rival")

	// In, and under way: the round exists before the presses being tested.
	if status, _ := other.post("/challenges/"+match.ID.String()+"/accept", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the rival could not accept")
	}
	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not start the match")
	}

	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			codes[slot], _ = other.post("/challenges/"+match.ID.String()+"/accept",
				url.Values{"confirm": {"1"}})
		}(i)
	}
	wg.Wait()

	// One round, not four.
	var rounds int
	if err := testPool.QueryRow(t.Context(),
		`SELECT count(*) FROM game_sessions WHERE user_id = $1 AND challenge_id = $2`,
		rival.ID, match.ID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if rounds != 1 {
		t.Errorf("four simultaneous accepts opened %d rounds, want 1", rounds)
	}

	// And the one that exists is still playable. The damage was never only the
	// extra rows: Start abandoned the round in progress before making the next,
	// so the loser of the race was left holding a dead round and no way back
	// into the match.
	round, err := a.repo.ActiveGame(t.Context(), rival.ID, "en")
	if err != nil {
		t.Fatalf("the accepted round is not playable any more: %v", err)
	}
	if round.ChallengeID == nil || *round.ChallengeID != match.ID {
		t.Error("the surviving round does not belong to the match that was accepted")
	}

	// None of the presses may answer with a fault.
	for i, code := range codes {
		if code >= 500 {
			t.Errorf("press %d answered %d", i+1, code)
		}
	}
}

// QA-04 — hostile input reaches the database as a value, never as syntax.
func TestSearchTakesHostileInputAsText(t *testing.T) {
	a := newApp(t)
	a.register("inj_user")

	probes := []string{
		"'; DROP TABLE users; --",
		"%' OR '1'='1",
		"\\'; SELECT pg_sleep(5); --",
		"<script>alert(1)</script>",
		strings.Repeat("a", 500),
	}
	for _, scope := range []string{"friends", "room", "device", "message"} {
		for _, probe := range probes {
			status, body := a.get("/ui/people?scope=" + scope + "&q=" + url.QueryEscape(probe))
			if status != http.StatusOK {
				t.Errorf("scope %s with %q → %d", scope, clip(probe, 24), status)
			}
			if strings.Contains(body, "<script>") {
				t.Errorf("scope %s echoed a script tag back unescaped", scope)
			}
		}
	}
	// The table is still there, which is the point.
	if _, err := a.repo.UserByUsername(t.Context(), "inj_user"); err != nil {
		t.Fatalf("the account did not survive the probes: %v", err)
	}
}

// QA-05 — one person cannot read or act on another's match.
func TestAStrangerCannotTouchSomebodyElsesMatch(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 976000, 12)
	a.register("own_host")
	mate := newAppSharing(t, a)
	mate.register("own_rival")
	stranger := newAppSharing(t, a)
	stranger.register("own_stranger")

	host, err := a.repo.UserByUsername(t.Context(), "own_host")
	if err != nil {
		t.Fatal(err)
	}
	rival, err := a.repo.UserByUsername(t.Context(), "own_rival")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, rival.ID)
	match := openMatch(t, a, host.ID, "own-1", "own_rival")

	for _, action := range []string{"accept", "decline", "cancel"} {
		status, _ := stranger.post("/challenges/"+match.ID.String()+"/"+action, url.Values{})
		if status == http.StatusSeeOther {
			// A redirect here would mean it was carried out.
			after, err := a.repo.Challenge(t.Context(), match.ID, "en")
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != models.ChallengePending || after.Player(host.ID) == nil {
				t.Errorf("a stranger's %q changed somebody else's match", action)
			}
		}
	}

	// And a guest's round is not readable by an unrelated account.
	if status, _ := stranger.get("/play/result/" + match.ID.String()); status == http.StatusOK {
		t.Error("a stranger could open a result that is not theirs")
	}
}

// QA-06 — the Arabic build renders right-to-left on every challenge screen.
func TestTheChallengeScreensReadRightToLeftInArabic(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 977000, 12)
	a.register("rtl_user")

	// Somebody to challenge, so the setup screen has a populated group on it
	// as well as an empty one.
	mate := newAppSharing(t, a)
	mate.register("rtl_mate")
	me, err := a.repo.UserByUsername(t.Context(), "rtl_user")
	if err != nil {
		t.Fatal(err)
	}
	them, err := a.repo.UserByUsername(t.Context(), "rtl_mate")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, me.ID, them.ID)

	for _, page := range []string{
		"/app", "/challenges", "/challenges/new", "/play", "/messages", "/friends",
	} {
		status, body := a.get(page + "?lang=ar")
		if status != http.StatusOK {
			t.Errorf("GET %s in Arabic → %d", page, status)
			continue
		}
		if !strings.Contains(body, `dir="rtl"`) {
			t.Errorf("%s did not render right-to-left in Arabic", page)
		}
		if !strings.Contains(body, `lang="ar"`) {
			t.Errorf("%s did not declare Arabic", page)
		}
		// A screen still carrying its English strings means a key was never
		// translated; the catalogue tests cover the keys, this covers the page.
		if strings.Contains(body, "Set up your round") || strings.Contains(body, "New challenge") {
			t.Errorf("%s shows English text under an Arabic locale", page)
		}
	}
}

// QA-07 — a remote match starts for everybody at the same moment.
//
// Accepting used to be the whole of starting, so two people who had arranged
// to play each other answered the same questions on different days and the
// scoreboard compared two solitary rounds. Accepting is now "I am here"; the
// host presses start, and that one press opens every round at once.
func TestARemoteMatchStartsForEverybodyAtOnce(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 978000, 12)
	a.register("sync_host")
	other := newAppSharing(t, a)
	other.register("sync_rival")

	host, err := a.repo.UserByUsername(t.Context(), "sync_host")
	if err != nil {
		t.Fatal(err)
	}
	rival, err := a.repo.UserByUsername(t.Context(), "sync_rival")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, rival.ID)
	match := openMatch(t, a, host.ID, "sync-1", "sync_rival")

	// Nobody can play before the host says go — not the invitee, and not the
	// host either.
	if status, _ := other.post("/challenges/"+match.ID.String()+"/accept", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the rival could not accept")
	}
	for _, who := range []struct {
		name string
		app  *app
		id   uuid.UUID
	}{{"host", a, host.ID}, {"rival", other, rival.ID}} {
		if _, err := a.repo.RoundInMatch(t.Context(), who.id, match.ID, "en"); err == nil {
			t.Errorf("%s had a round before the match was started", who.name)
		}
	}

	// Only the host may start it.
	if status, _ := other.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status == http.StatusSeeOther {
		if started, _ := a.repo.MatchStarted(t.Context(), match.ID); started {
			t.Fatal("an invitee started somebody else's match")
		}
	}

	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not start the match")
	}

	// One press, a round each, on the same questions.
	var hostRound, rivalRound *models.GameSession
	if hostRound, err = a.repo.RoundInMatch(t.Context(), host.ID, match.ID, "en"); err != nil {
		t.Fatalf("the host has no round: %v", err)
	}
	if rivalRound, err = a.repo.RoundInMatch(t.Context(), rival.ID, match.ID, "en"); err != nil {
		t.Fatalf("the rival has no round: %v", err)
	}
	if len(hostRound.QuestionIDs) != len(rivalRound.QuestionIDs) {
		t.Fatal("the two rounds were drawn from different question sets")
	}
	for i := range hostRound.QuestionIDs {
		if hostRound.QuestionIDs[i] != rivalRound.QuestionIDs[i] {
			t.Fatalf("question %d differs between the two rounds", i)
		}
	}

	// And the match records that it began, once.
	started, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !started.Started() {
		t.Error("the match does not record that it started")
	}
	first := started.StartedAt

	// A second press changes nothing: no second round, no new timestamp.
	a.post("/challenges/"+match.ID.String()+"/start", url.Values{})
	again, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if again.StartedAt == nil || !again.StartedAt.Equal(*first) {
		t.Error("pressing start twice restarted the match")
	}
	var rounds int
	if err := testPool.QueryRow(t.Context(),
		`SELECT count(*) FROM game_sessions WHERE challenge_id = $1`, match.ID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if rounds != 2 {
		t.Errorf("the match has %d rounds, want one each", rounds)
	}
}

// QA-08 — a match cannot be started before enough people have accepted.
func TestAMatchCannotStartWithNobodyInIt(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 979000, 12)
	a.register("lone_host")
	other := newAppSharing(t, a)
	other.register("lone_rival")

	host, err := a.repo.UserByUsername(t.Context(), "lone_host")
	if err != nil {
		t.Fatal(err)
	}
	rival, err := a.repo.UserByUsername(t.Context(), "lone_rival")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, host.ID, rival.ID)
	match := openMatch(t, a, host.ID, "lone-1", "lone_rival")

	// The invitation is unanswered, so the host is the only person in it.
	a.post("/challenges/"+match.ID.String()+"/start", url.Values{})
	if started, _ := a.repo.MatchStarted(t.Context(), match.ID); started {
		t.Error("a match started with only its host in it")
	}
	if _, err := a.repo.RoundInMatch(t.Context(), host.ID, match.ID, "en"); err == nil {
		t.Error("a round was opened for a match that could not start")
	}
}

// QA-09 — a seat left on a player who is out does not speak for the device.
//
// A phone seated on a guest who declined made every card reason about that
// guest: "is it their turn" was no, so the card fell through to the waiting
// branch and counted everybody else as outstanding. A host sitting in the
// middle of their own match read "Waiting for 2 players" — about a match that
// was waiting on them. The scoreboard meanwhile highlighted the account holder,
// so one card carried two different ideas of who "me" was.
func TestASeatOnSomebodyWhoIsOutDoesNotSpeakForTheDevice(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 982000, 12)
	a.register("seat_host")

	// Two guests at this device, in a match with the host.
	for _, name := range []string{"Refuser", "Player"} {
		if status, _ := a.post("/players", url.Values{"name": {name}}); status != http.StatusSeeOther {
			t.Fatalf("adding %s failed", name)
		}
	}
	host, err := a.repo.UserByUsername(t.Context(), "seat_host")
	if err != nil {
		t.Fatal(err)
	}
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != 2 {
		t.Fatalf("local players = %d (err %v)", len(guests), err)
	}
	refuser, player := guests[0], guests[1]

	form := url.Values{
		"player_source": {"device"}, "source": {"bank"}, "request_key": {"seat-1"},
		"local": {refuser.ID.String(), player.ID.String()},
	}
	if status, body := a.postJSONForm(t, "/challenges/new", form); status != http.StatusOK {
		t.Fatalf("opening the match → %d (%s)", status, clip(body, 140))
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatal(err)
	}

	// One guest refuses, from their own seat — which is how a seat ends up on
	// somebody who is out of the match.
	if status, _ := a.post("/players/seat", url.Values{"player": {refuser.ID.String()}}); status != http.StatusSeeOther {
		t.Fatal("taking the seat failed")
	}
	if status, _ := a.post("/challenges/"+match.ID.String()+"/decline", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the guest could not decline")
	}

	// The match carries on — host and one guest are still in it.
	alive, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !alive.Live() {
		t.Fatalf("one guest declining ended the match: %q", alive.Status)
	}

	// The seat is still on the player who refused. The card must be about the
	// people who still have something to do, not about them.
	tags := statusTags(t, a)
	for _, tag := range tags {
		if strings.HasPrefix(tag, "Waiting for") {
			t.Errorf("the device was told %q while it still has a move to make", tag)
		}
	}
	if !has(tags, "Your turn") {
		t.Errorf("the device has a move and was told %v", tags)
	}
}

// QA-10 — the button answers for whoever the card said it was about.
//
// A card is drawn for one person and its buttons are answered for another, and
// the two used different rules for picking that person. With the seat left on a
// guest who had declined, the screen correctly skipped them and drew the host's
// Cancel button — and the handler, which still preferred the seat, answered
// "only the host can cancel a match" to the host pressing it.
func TestTheHostCanPressTheirOwnButtons(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 984000, 12)
	a.register("btn_host")

	for _, name := range []string{"Refuser", "Player"} {
		if status, _ := a.post("/players", url.Values{"name": {name}}); status != http.StatusSeeOther {
			t.Fatalf("adding %s failed", name)
		}
	}
	host, err := a.repo.UserByUsername(t.Context(), "btn_host")
	if err != nil {
		t.Fatal(err)
	}
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != 2 {
		t.Fatalf("local players = %d (err %v)", len(guests), err)
	}
	refuser, player := guests[0], guests[1]

	if status, body := a.postJSONForm(t, "/challenges/new", url.Values{
		"player_source": {"device"}, "source": {"bank"}, "request_key": {"btn-1"},
		"local": {refuser.ID.String(), player.ID.String()},
	}); status != http.StatusOK {
		t.Fatalf("opening the match → %d (%s)", status, clip(body, 140))
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatal(err)
	}

	// One guest declines from their own seat, and the seat stays on them.
	if status, _ := a.post("/players/seat", url.Values{"player": {refuser.ID.String()}}); status != http.StatusSeeOther {
		t.Fatal("taking the seat failed")
	}
	if status, _ := a.post("/challenges/"+match.ID.String()+"/decline", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the guest could not decline")
	}

	// The card is the host's: it offers start and cancel. So must the buttons
	// be. Starting first, because it is the one the host would reach for.
	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not start their own match")
	}
	started, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !started.Started() {
		t.Error("the host's start did nothing")
	}

	// And cancel, which is the press that was being refused.
	if status, _ := a.post("/challenges/"+match.ID.String()+"/cancel", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not cancel their own match")
	}
	after, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != models.ChallengeCancelled {
		t.Errorf("status = %q after the host cancelled, want cancelled", after.Status)
	}
	if after.CancelledBy == nil || *after.CancelledBy != host.ID {
		t.Error("the cancellation was not recorded against the host")
	}
}

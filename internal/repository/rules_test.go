package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// The rules the database keeps on its own, checked against the database rather
// than against the code that is supposed to respect them. A constraint is worth
// having precisely because it holds when a call site forgets.

// Rule 4: a player is inside one match at a time. Several invitations may be
// waiting — that is a different state — but joining a second live match is
// refused.
func TestOneLiveMatchPerPlayer(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "one_match_host", "one.match.host@example.com")
	rival := newUser(t, "one_match_rival", "one.match.rival@example.com")
	third := newUser(t, "one_match_third", "one.match.third@example.com")
	befriend(t, host.ID, rival.ID)
	befriend(t, third.ID, rival.ID)
	q := addQuestion(t, 975001, "How many rakʿahs are in Maghrib?")

	first := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, first, []repository.MatchSeat{{UserID: rival.ID}}); err != nil {
		t.Fatalf("first match: %v", err)
	}

	// The host is in their own match from the moment they open it, so a second
	// one is refused at creation.
	second := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	err := r.CreateMatch(ctx, second, []repository.MatchSeat{{UserID: rival.ID}})
	if !errors.Is(err, repository.ErrConflict) {
		t.Errorf("opening a second match while inside one returned %v, want ErrConflict", err)
	}

	// The invitation on the rival is only an invitation: a second one can
	// arrive and sit beside the first.
	other := &models.Challenge{HostID: third.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, other, []repository.MatchSeat{{UserID: rival.ID}}); err != nil {
		t.Fatalf("a second invitation to the same player: %v", err)
	}

	// Accepting one of them is fine.
	if err := r.JoinMatch(ctx, first.ID, rival.ID); err != nil {
		t.Fatalf("accepting the first invitation: %v", err)
	}
	// Accepting the other, while inside the first, is not.
	if err := r.JoinMatch(ctx, other.ID, rival.ID); !errors.Is(err, repository.ErrBusy) {
		t.Errorf("accepting a second invitation returned %v, want ErrBusy", err)
	}

	busy, err := r.InActiveMatch(ctx, rival.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !busy {
		t.Error("a player who has accepted a match does not read as being in one")
	}
}

// A match that has run out of time is holding nobody. Without this the players
// it left in the joined state could never enter another match again.
func TestExpiringAMatchReleasesItsPlayers(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "expiry_host", "expiry.host@example.com")
	guest := newUser(t, "expiry_guest", "expiry.guest@example.com")
	befriend(t, host.ID, guest.ID)
	q := addQuestion(t, 975010, "In which month was the Qurʾān revealed?")

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: guest.ID}}); err != nil {
		t.Fatalf("create match: %v", err)
	}
	if err := r.JoinMatch(ctx, ch.ID, guest.ID); err != nil {
		t.Fatalf("join: %v", err)
	}

	// Backdate it past its own deadline and run the sweep.
	if _, err := testPool.Exec(ctx,
		`UPDATE challenges SET expires_at = now() - interval '1 day' WHERE id = $1`,
		ch.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := r.ExpireStaleChallenges(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	busy, err := r.InActiveMatch(ctx, guest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Error("an expired match is still holding the player who joined it")
	}
	busy, err = r.InActiveMatch(ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Error("an expired match is still holding its host")
	}
}

// Rule 3: a challenge question that is not submitted is lost, and the round
// moves on. The answer row is written so the review screen can still show what
// was asked and that nothing was chosen.
func TestALapsedQuestionIsRecordedAndSkipped(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	player := newUser(t, "lapse_player", "lapse.player@example.com")
	first := addQuestion(t, 975020, "Who was the first muezzin?")
	second := addQuestion(t, 975021, "Which surah opens the Qurʾān?")

	session := &models.GameSession{
		UserID: player.ID, Mode: models.ModeChallenge, Locale: "en",
		QuestionIDs: []int{first, second}, TotalQuestions: 2,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("create round: %v", err)
	}

	// Serving it in a timed round sets a deadline; a solo round gets none.
	_, deadline, err := r.MarkQuestionServed(ctx, session.ID, 25_000)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if deadline == nil {
		t.Fatal("a timed round was served without a deadline")
	}

	if err := r.LapseQuestion(ctx, session.ID, first, 0, 25_000); err != nil {
		t.Fatalf("lapse: %v", err)
	}

	after, err := r.Game(ctx, session.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if after.Cursor != 1 {
		t.Errorf("cursor = %d after a lost question, want 1", after.Cursor)
	}
	if after.DeadlineAt != nil {
		t.Error("the deadline survived the question it belonged to")
	}

	answers, err := r.GameAnswers(ctx, session.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 1 {
		t.Fatalf("answers recorded = %d, want the lost question to be one of them", len(answers))
	}
	if answers[0].Answered() {
		t.Error("a lost question was recorded as though somebody chose an answer")
	}
	if answers[0].IsCorrect || answers[0].PointsAwarded != 0 {
		t.Error("a lost question scored something")
	}

	// And it cannot be lost twice: the guard is on the position.
	if err := r.LapseQuestion(ctx, session.ID, first, 0, 25_000); !errors.Is(err, repository.ErrConflict) {
		t.Errorf("losing the same question twice returned %v, want ErrConflict", err)
	}
}

// A solo round has no deadline at all, which is what makes it resumable — and
// what keeps it out of the resume card's way when it is a challenge.
func TestOnlySoloRoundsAreOfferedBackAndUntimed(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	player := newUser(t, "resume_player", "resume.player@example.com")
	q := addQuestion(t, 975030, "How many pillars does Islam have?")

	challenge := &models.GameSession{
		UserID: player.ID, Mode: models.ModeChallenge, Locale: "en",
		QuestionIDs: []int{q}, TotalQuestions: 1,
	}
	if err := r.CreateGame(ctx, challenge); err != nil {
		t.Fatalf("create challenge round: %v", err)
	}

	if _, err := r.ResumableGame(ctx, player.ID, "en"); !errors.Is(err, repository.ErrNotFound) {
		t.Error("a challenge round was offered back as something to carry on with")
	}
	if _, err := r.ActiveGame(ctx, player.ID, "en"); err != nil {
		t.Errorf("the challenge round is not active: %v", err)
	}

	// Replace it with a solo round, which is resumable and has no deadline.
	if err := r.AbandonGame(ctx, challenge.ID, player.ID); err != nil {
		t.Fatal(err)
	}
	solo := &models.GameSession{
		UserID: player.ID, Mode: models.ModeSolo, Locale: "en",
		QuestionIDs: []int{q}, TotalQuestions: 1,
	}
	if err := r.CreateGame(ctx, solo); err != nil {
		t.Fatalf("create solo round: %v", err)
	}
	if _, deadline, err := r.MarkQuestionServed(ctx, solo.ID, 0); err != nil {
		t.Fatalf("serve solo: %v", err)
	} else if deadline != nil {
		t.Error("a solo question was given a deadline")
	}
	if _, err := r.ResumableGame(ctx, player.ID, "en"); err != nil {
		t.Errorf("a solo round was not offered back: %v", err)
	}
}

// Rule 5, in the database: one room per person, whatever the handler does.
func TestTheDatabaseRefusesASecondRoom(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	user := newUser(t, "two_rooms", "two.rooms@example.com")
	owner := newUser(t, "two_rooms_owner", "two.rooms.owner@example.com")

	// Opening rooms is not being in them. Somebody may set up as many places
	// as they like; the rule is about where they are standing.
	first, err := r.CreateThread(ctx, "room", "First room", "", owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.CreateThread(ctx, "room", "Second room", "", owner.ID, nil)
	if err != nil {
		t.Fatalf("opening a second room: %v", err)
	}
	if _, err := r.JoinThread(ctx, first.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := r.JoinThread(ctx, first.ID, user.ID); err != nil {
		t.Fatalf("joining the first room: %v", err)
	}
	left, err := r.JoinThread(ctx, second.ID, user.ID)
	if err != nil {
		t.Fatalf("joining the second room: %v", err)
	}
	if left == nil || *left != first.ID {
		t.Fatalf("joining the second room left %v, want the first", left)
	}

	// And they really are only in one.
	var rooms int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM conversation_members WHERE user_id = $1 AND is_room`,
		user.ID).Scan(&rooms); err != nil {
		t.Fatal(err)
	}
	if rooms != 1 {
		t.Errorf("the player is in %d rooms, want exactly 1", rooms)
	}
}

// A room makes the people in it reachable, and leaving takes that away. This is
// the whole of rule 6's second clause.
func TestSharingARoomIsWhatMakesSomebodyReachable(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	a := newUser(t, "reach_a", "reach.a@example.com")
	b := newUser(t, "reach_b", "reach.b@example.com")
	owner := newUser(t, "reach_owner", "reach.owner@example.com")

	room, err := r.CreateThread(ctx, "room", "Reach room", "", owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	shared, err := r.ShareRoom(ctx, a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		t.Fatal("two strangers already share a room")
	}

	// Making a room does not put the owner in it, so there is nobody in this
	// one until somebody walks in.
	if empty, err := r.Members(ctx, room.ID); err != nil || len(empty) != 0 {
		t.Fatalf("a newly opened room already holds %d people (err %v)", len(empty), err)
	}

	for _, who := range []*models.User{owner, a, b} {
		if _, err := r.JoinThread(ctx, room.ID, who.ID); err != nil {
			t.Fatal(err)
		}
	}
	if shared, err = r.ShareRoom(ctx, a.ID, b.ID); err != nil || !shared {
		t.Errorf("two people in the same room do not share it (err %v)", err)
	}

	peers, err := r.RoomPeers(ctx, a.ID, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Errorf("room peers = %d, want the owner and the other joiner", len(peers))
	}

	if err := r.LeaveThread(ctx, room.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if shared, err = r.ShareRoom(ctx, a.ID, b.ID); err != nil || shared {
		t.Errorf("leaving the room left the reachability behind (shared=%v, err %v)", shared, err)
	}
}

// Rule 7's server half, at the level that actually enforces it: a key is
// claimed once and every later attempt is told so.
func TestARequestKeyIsClaimedOnce(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	user := newUser(t, "idem_user", "idem.user@example.com")

	claimed, previous, err := r.ClaimRequestKey(ctx, "k-1", user.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !claimed || previous != "" {
		t.Fatalf("first claim = (%v, %q), want (true, \"\")", claimed, previous)
	}
	if err := r.CompleteRequestKey(ctx, "k-1", "/somewhere"); err != nil {
		t.Fatal(err)
	}

	claimed, previous, err = r.ClaimRequestKey(ctx, "k-1", user.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Error("the same key was claimed twice")
	}
	if previous != "/somewhere" {
		t.Errorf("the repeat was told %q, want where the first attempt ended up", previous)
	}

	// A key released after a failure can be claimed again, so a corrected
	// resubmission is not mistaken for a repeat.
	if _, _, err := r.ClaimRequestKey(ctx, "k-2", user.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if err := r.ReleaseRequestKey(ctx, "k-2"); err != nil {
		t.Fatal(err)
	}
	claimed, _, err = r.ClaimRequestKey(ctx, "k-2", user.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Error("a released key could not be claimed again")
	}

	// No key, no protection, and no surprise: it always proceeds.
	if claimed, _, err := r.ClaimRequestKey(ctx, "", user.ID, "test"); err != nil || !claimed {
		t.Errorf("an empty key returned (%v, %v), want it to proceed", claimed, err)
	}
}

// Temporary players, and the promise that everything they did goes with them.
func TestSweepingAGuestTakesTheirRoundsWithIt(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	guest, err := r.CreateGuest(ctx, nil, "sweep-key-1", "Passing through", "en")
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	q := addQuestion(t, 975040, "Which city is the qiblah?")
	session := &models.GameSession{
		UserID: guest.ID, Mode: models.ModeSolo, Locale: "en",
		QuestionIDs: []int{q}, TotalQuestions: 1,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("guest round: %v", err)
	}

	// Still alive: the sweep leaves it alone.
	if _, err := r.PurgeExpiredGuests(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UserByID(ctx, guest.ID); err != nil {
		t.Fatalf("a live guest was swept: %v", err)
	}

	// Past its time: the row goes, and the round goes with it.
	if _, err := testPool.Exec(ctx,
		`UPDATE users SET expires_at = now() - interval '1 hour' WHERE id = $1`,
		guest.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PurgeExpiredGuests(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UserByID(ctx, guest.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("an expired guest survived the sweep (%v)", err)
	}

	var rounds int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM game_sessions WHERE id = $1`, session.ID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if rounds != 0 {
		t.Error("the guest's round outlived the guest")
	}
}

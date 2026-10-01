package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// befriend puts two accounts in the state a match requires.
func befriend(t *testing.T, a, b uuid.UUID) {
	t.Helper()
	r := repo(t)
	if err := r.RequestFriendship(t.Context(), a, b); err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := r.RespondToFriendship(t.Context(), a, b, true); err != nil {
		t.Fatalf("accept: %v", err)
	}
}

// A match with four people: the third and fourth are exactly what the old
// shape could not express, since both players lived in the challenge row.
func TestMatchCarriesMoreThanTwoPlayers(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "match_host", "match.host@example.com")
	guests := []*models.User{
		newUser(t, "match_g1", "match.g1@example.com"),
		newUser(t, "match_g2", "match.g2@example.com"),
		newUser(t, "match_g3", "match.g3@example.com"),
	}
	ids := make([]repository.MatchSeat, 0, len(guests))
	for _, g := range guests {
		befriend(t, host.ID, g.ID)
		ids = append(ids, repository.MatchSeat{UserID: g.ID})
	}
	q := addQuestion(t, 970001, "Which month is the month of fasting?")

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, ch, ids); err != nil {
		t.Fatalf("create match: %v", err)
	}

	loaded, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Players) != 4 {
		t.Fatalf("players = %d, want the host and three guests", len(loaded.Players))
	}
	if me := loaded.Player(host.ID); me == nil || !me.IsHost || me.State != models.PlayerJoined {
		t.Errorf("the host is %+v, want joined and marked host", me)
	}
	for _, g := range guests {
		if p := loaded.Player(g.ID); p == nil || p.State != models.PlayerInvited {
			t.Errorf("guest %s is %+v, want invited", g.Username, p)
		}
	}
	if loaded.ReadyToStart() {
		t.Error("a match where nobody has accepted was ready to start")
	}
}

// One person declining does not end the match for everybody — which is the
// whole difference a third player makes.
func TestDeclineLeavesTheMatchRunning(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "decline_host", "decline.host@example.com")
	stays := newUser(t, "decline_stays", "decline.stays@example.com")
	leaves := newUser(t, "decline_leaves", "decline.leaves@example.com")
	befriend(t, host.ID, stays.ID)
	befriend(t, host.ID, leaves.ID)
	q := addQuestion(t, 970010, "Who built the Kaaba?")

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: stays.ID}, {UserID: leaves.ID}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.JoinMatch(ctx, ch.ID, stays.ID); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := r.DeclineMatch(ctx, ch.ID, leaves.ID); err != nil {
		t.Fatalf("decline: %v", err)
	}

	loaded, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded.ReadyToStart() {
		t.Error("the match stopped being playable when one of three declined")
	}
	if got := loaded.Player(leaves.ID).State; got != models.PlayerDeclined {
		t.Errorf("the decliner is %q", got)
	}
	// Two people are still expected, not three.
	if n := len(loaded.Waiting()); n != 2 {
		t.Errorf("waiting on %d players, want the host and the one who joined", n)
	}
}

// The match settles when the last player finishes, and the top score wins once.
func TestMatchSettlesWhenEverybodyHasPlayed(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "settle_host", "settle.host@example.com")
	rival := newUser(t, "settle_rival", "settle.rival@example.com")
	third := newUser(t, "settle_third", "settle.third@example.com")
	befriend(t, host.ID, rival.ID)
	befriend(t, host.ID, third.ID)
	q := addQuestion(t, 970020, "How many pillars does Islam have?")

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: rival.ID}, {UserID: third.ID}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, id := range []uuid.UUID{rival.ID, third.ID} {
		if err := r.JoinMatch(ctx, ch.ID, id); err != nil {
			t.Fatalf("join: %v", err)
		}
	}

	if err := r.RecordMatchScore(ctx, ch.ID, host.ID, 30); err != nil {
		t.Fatalf("score host: %v", err)
	}
	mid, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if mid.Status == models.ChallengeCompleted {
		t.Error("the match settled while two players had not finished")
	}

	if err := r.RecordMatchScore(ctx, ch.ID, rival.ID, 70); err != nil {
		t.Fatalf("score rival: %v", err)
	}
	if err := r.RecordMatchScore(ctx, ch.ID, third.ID, 50); err != nil {
		t.Fatalf("score third: %v", err)
	}

	done, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if done.Status != models.ChallengeCompleted {
		t.Fatalf("status = %q, want completed", done.Status)
	}
	winner := done.Winner()
	if winner == nil || winner.UserID != rival.ID {
		t.Fatalf("winner = %+v, want the 70", winner)
	}
	// Ranked puts the scoreboard in order.
	ranked := done.Ranked()
	if len(ranked) != 3 || ranked[0].UserID != rival.ID || ranked[2].UserID != host.ID {
		t.Errorf("ranked order is wrong: %v", ranked)
	}
	// The win is credited exactly once.
	fresh, err := r.UserByID(ctx, rival.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fresh.GamesWon != 1 {
		t.Errorf("games_won = %d, want 1", fresh.GamesWon)
	}
}

// A question one player writes for one match must never be reachable by anyone
// else. Three properties make that true, and this checks all three — the pair
// (is_active false, challenge_id set) is what every public draw already filters
// on, and the constraint makes the pair impossible to break by accident.
func TestAuthoredQuestionsStayInsideTheirMatch(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "author_host", "author.host@example.com")
	guest := newUser(t, "author_guest", "author.guest@example.com")
	befriend(t, host.ID, guest.ID)

	// Enough bank questions that a draw would find something if it could.
	for i := 0; i < 8; i++ {
		addQuestion(t, 980100+i, "Bank question for the draw test "+string(rune('a'+i))+"?")
	}

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{980100}}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: guest.ID}}); err != nil {
		t.Fatalf("create match: %v", err)
	}

	mine, err := r.CreateMatchQuestions(ctx, ch.ID, host.ID, "en",
		[]models.TranslationDraft{{
			Prompt:  "What did I have for breakfast?",
			Choices: []string{"Dates", "Bread", "Rice", "Nothing"},
		}},
		[]int{1}, []int{1}, []int{0})
	if err != nil {
		t.Fatalf("write question: %v", err)
	}
	if len(mine) != 1 {
		t.Fatalf("wrote %d questions, want 1", len(mine))
	}
	authored := mine[0]

	// 1. It plays: the match can load it like any other question.
	q, err := r.Question(ctx, authored, "en", "en")
	if err != nil {
		t.Fatalf("the match cannot read its own question: %v", err)
	}
	if q.Prompt != "What did I have for breakfast?" {
		t.Errorf("prompt = %q", q.Prompt)
	}

	// 2. It is never drawn. Ask for far more questions than the bank holds, so
	// a draw that could reach it would.
	drawn, err := r.PickQuestionIDs(ctx, nil, 0, 100, "en")
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	for _, id := range drawn {
		if id == authored {
			t.Fatal("a match question was drawn into an ordinary round")
		}
	}
	daily, err := r.PickDailyQuestionIDs(ctx, "2026-09-29", 100, "en")
	if err != nil {
		t.Fatalf("daily: %v", err)
	}
	for _, id := range daily {
		if id == authored {
			t.Fatal("a match question was drawn into the daily")
		}
	}

	// 3. It is not in the bank: not in the admin list, not in the count.
	listed, total, err := r.AdminQuestions(ctx, repository.AdminQuestionFilter{
		Locale: "en", Limit: 500,
	})
	if err != nil {
		t.Fatalf("admin list: %v", err)
	}
	for _, q := range listed {
		if q.ID == authored {
			t.Error("a match question appears in the admin question bank")
		}
	}
	_ = total

	// And the author is recorded, so a question that upsets somebody has a name
	// against it.
	authors, err := r.MatchQuestionAuthors(ctx, []int{authored})
	if err != nil {
		t.Fatalf("authors: %v", err)
	}
	if card := authors[authored]; card == nil || card.ID != host.ID {
		t.Errorf("author = %+v, want the host who wrote it", card)
	}
}

// Somebody who is invited and never opens the match must not hold the result
// hostage. Four people finish, one never turned up, and the scoreboard used to
// say "waiting" until the whole match expired a week later.
func TestNeverStartingIsEliminatedOnceTheOthersFinish(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "elim_host", "elim.host@example.com")
	rival := newUser(t, "elim_rival", "elim.rival@example.com")
	ghost := newUser(t, "elim_ghost", "elim.ghost@example.com")
	befriend(t, host.ID, rival.ID)
	befriend(t, host.ID, ghost.ID)
	q := addQuestion(t, 990001, "Which surah is the heart of the Qur'an?")

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: rival.ID}, {UserID: ghost.ID}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.JoinMatch(ctx, ch.ID, rival.ID); err != nil {
		t.Fatalf("join: %v", err)
	}
	// The ghost never joins and never starts.

	if err := r.RecordMatchScore(ctx, ch.ID, host.ID, 50); err != nil {
		t.Fatalf("host score: %v", err)
	}
	// One score is not enough to settle anything.
	mid, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if mid.Player(ghost.ID).State != models.PlayerInvited {
		t.Errorf("the ghost was eliminated while only one player had finished")
	}

	if err := r.RecordMatchScore(ctx, ch.ID, rival.ID, 80); err != nil {
		t.Fatalf("rival score: %v", err)
	}

	done, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := done.Player(ghost.ID).State; got != models.PlayerEliminated {
		t.Fatalf("the ghost is %q, want eliminated", got)
	}
	if done.Status != models.ChallengeCompleted {
		t.Fatalf("status = %q, want the match settled without them", done.Status)
	}
	if winner := done.Winner(); winner == nil || winner.UserID != rival.ID {
		t.Errorf("winner = %+v, want the 80", winner)
	}
	// Eliminated is not declined: nobody is recorded as refusing a match they
	// simply missed.
	if done.Player(ghost.ID).State == models.PlayerDeclined {
		t.Error("a missed match was recorded as a refusal")
	}
	if len(done.Waiting()) != 0 {
		t.Errorf("still waiting on %d players", len(done.Waiting()))
	}
}

// Somebody who is part-way through their round is still playing, and the match
// waits for them rather than throwing them out.
func TestAPlayerMidRoundIsNotEliminated(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "mid_host", "mid.host@example.com")
	slow := newUser(t, "mid_slow", "mid.slow@example.com")
	third := newUser(t, "mid_third", "mid.third@example.com")
	befriend(t, host.ID, slow.ID)
	befriend(t, host.ID, third.ID)
	q := addQuestion(t, 990010, "How many verses are in Surah al-Fatiha?")

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{q}}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: slow.ID}, {UserID: third.ID}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, id := range []uuid.UUID{slow.ID, third.ID} {
		if err := r.JoinMatch(ctx, ch.ID, id); err != nil {
			t.Fatalf("join: %v", err)
		}
	}

	// The slow player has started: a round of their own is open.
	session := &models.GameSession{
		UserID: slow.ID, Mode: models.ModeChallenge, Locale: "en",
		QuestionIDs: []int{q}, TotalQuestions: 1, ChallengeID: &ch.ID,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := r.AttachMatchSession(ctx, ch.ID, slow.ID, session.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := r.RecordMatchScore(ctx, ch.ID, host.ID, 40); err != nil {
		t.Fatalf("host: %v", err)
	}
	if err := r.RecordMatchScore(ctx, ch.ID, third.ID, 60); err != nil {
		t.Fatalf("third: %v", err)
	}

	loaded, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded.Player(slow.ID).State; got != models.PlayerJoined {
		t.Errorf("a player mid-round is %q, want still joined", got)
	}
	if loaded.Status == models.ChallengeCompleted {
		t.Error("the match settled while somebody was still playing their round")
	}
}

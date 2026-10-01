package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

func TestLevelCurve(t *testing.T) {
	cases := []struct {
		xp    int
		level int
	}{
		{0, 1},
		{99, 1},
		{100, 2}, // 50*2*1
		{299, 2},
		{300, 3}, // 50*3*2
		{599, 3},
		{600, 4},    // 50*4*3
		{1000, 5},   // 50*5*4
		{9100, 14},  // 50*14*13
		{10000, 14}, // level 15 does not start until 10500
		{10500, 15}, // 50*15*14
	}
	for _, tc := range cases {
		u := &User{XP: tc.xp}
		if got := u.Level(); got != tc.level {
			t.Errorf("XP %d => level %d, want %d", tc.xp, got, tc.level)
		}
	}
}

func TestLevelIsMonotonic(t *testing.T) {
	last := 0
	for xp := 0; xp <= 20000; xp += 37 {
		u := &User{XP: xp}
		lvl := u.Level()
		if lvl < last {
			t.Fatalf("level went backwards at XP %d: %d after %d", xp, lvl, last)
		}
		last = lvl
	}
}

func TestLevelProgress(t *testing.T) {
	// Exactly at a level boundary: no progress into the new level yet.
	u := &User{XP: 300}
	earned, needed := u.LevelProgress()
	if earned != 0 {
		t.Errorf("earned = %d at a boundary, want 0", earned)
	}
	if needed != 300 { // level 4 starts at 600
		t.Errorf("needed = %d, want 300", needed)
	}
	if got := u.LevelPercent(); got != 0 {
		t.Errorf("percent = %d at a boundary, want 0", got)
	}

	// Halfway through level 3.
	half := &User{XP: 450}
	if got := half.LevelPercent(); got != 50 {
		t.Errorf("percent = %d halfway, want 50", got)
	}
}

func TestLevelPercentIsBounded(t *testing.T) {
	for xp := 0; xp < 5000; xp += 13 {
		u := &User{XP: xp}
		if p := u.LevelPercent(); p < 0 || p > 100 {
			t.Fatalf("XP %d gave an out-of-range percent: %d", xp, p)
		}
	}
}

// Accuracy is the one percentage on a session, and it has to survive an
// abandoned round that recorded no questions at all.
func TestAccuracyHandlesAnEmptyRound(t *testing.T) {
	if got := (&GameSession{}).Accuracy(); got != 0 {
		t.Errorf("accuracy with no questions = %d, want 0", got)
	}
	if got := (&GameSession{TotalQuestions: 8, CorrectCount: 6}).Accuracy(); got != 75 {
		t.Errorf("accuracy = %d, want 75", got)
	}
	if got := (&GameSession{TotalQuestions: 5, CorrectCount: 5}).Accuracy(); got != 100 {
		t.Errorf("a flawless round = %d, want 100", got)
	}
}

func TestInitialsHandlesUnicode(t *testing.T) {
	cases := []struct {
		name string
		user User
		want string
	}{
		{"latin", User{DisplayName: "Malek"}, "M"},
		{"arabic", User{DisplayName: "مالك"}, "م"},
		{"emoji", User{DisplayName: "🌙 Nur"}, "🌙"},
		{"falls back to username", User{Username: "malek"}, "m"},
		{"nothing at all", User{}, "?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.user.Initials(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGameSessionAccuracy(t *testing.T) {
	if got := (&GameSession{}).Accuracy(); got != 0 {
		t.Errorf("empty session accuracy = %d, want 0 (no divide by zero)", got)
	}
	if got := (&GameSession{TotalQuestions: 10, CorrectCount: 7}).Accuracy(); got != 70 {
		t.Errorf("accuracy = %d, want 70", got)
	}
}

func TestGameSessionIsComplete(t *testing.T) {
	if (&GameSession{Cursor: 4, TotalQuestions: 5}).IsComplete() {
		t.Error("a round on its last question is not complete yet")
	}
	if !(&GameSession{Cursor: 5, TotalQuestions: 5}).IsComplete() {
		t.Error("a round whose cursor reached the total is complete")
	}
}

func TestUserCardPresence(t *testing.T) {
	fresh := &UserCard{LastSeenAt: time.Now().Add(-30 * time.Second)}
	if !fresh.IsOnline() {
		t.Error("someone seen 30s ago should read as online")
	}
	stale := &UserCard{LastSeenAt: time.Now().Add(-10 * time.Minute)}
	if stale.IsOnline() {
		t.Error("someone seen 10 minutes ago should read as offline")
	}
}

func TestChallengeOutcome(t *testing.T) {
	me := mustUUID("11111111-1111-1111-1111-111111111111")
	them := mustUUID("22222222-2222-2222-2222-222222222222")

	t.Run("unfinished duel has no outcome", func(t *testing.T) {
		ch := &Challenge{Status: ChallengePending}
		if got := ch.Outcome(me); got != "" {
			t.Errorf("got %q, want an empty outcome", got)
		}
	})

	t.Run("winner sees a win, loser a loss", func(t *testing.T) {
		ch := &Challenge{
			Status: ChallengeCompleted, WinnerID: &me,
		}
		if got := ch.Outcome(me); got != "win" {
			t.Errorf("winner got %q, want win", got)
		}
		if got := ch.Outcome(them); got != "loss" {
			t.Errorf("loser got %q, want loss", got)
		}
	})

	t.Run("no winner is a draw for both", func(t *testing.T) {
		ch := &Challenge{Status: ChallengeCompleted}
		if got := ch.Outcome(me); got != "draw" {
			t.Errorf("the host got %q, want draw", got)
		}
		if got := ch.Outcome(them); got != "draw" {
			t.Errorf("the other player got %q, want draw", got)
		}
	})
}

func TestBadgeEarned(t *testing.T) {
	if (&Badge{}).Earned() {
		t.Error("a badge with no timestamp is not earned")
	}
	now := time.Now()
	if !(&Badge{EarnedAt: &now}).Earned() {
		t.Error("a badge with a timestamp is earned")
	}
}

package service

import (
	"testing"
	"time"
)

func TestScorePointsSpeedBonus(t *testing.T) {
	// An instant answer earns the full 50% bonus; using the whole clock
	// earns none. Both sit on the same base value.
	instant := scorePoints(20, 0, 1)
	slow := scorePoints(20, QuestionTimeLimitMS, 1)

	if instant != 30 {
		t.Errorf("instant answer = %d, want 30 (base 20 + 50%% bonus)", instant)
	}
	if slow != 20 {
		t.Errorf("last-second answer = %d, want 20 (base only)", slow)
	}
	if instant <= slow {
		t.Errorf("speed must pay: instant %d should beat slow %d", instant, slow)
	}
}

func TestScorePointsStreakMultiplier(t *testing.T) {
	const base, at = 20, QuestionTimeLimitMS // no speed bonus, isolate the streak

	cases := []struct {
		streak int
		want   int
	}{
		{1, 20},
		{2, 20},
		{3, 24}, // x1.2
		{4, 24},
		{5, 26}, // x1.3
		{7, 26},
		{8, 30}, // x1.5, the cap
		{50, 30},
	}

	for _, tc := range cases {
		if got := scorePoints(base, at, tc.streak); got != tc.want {
			t.Errorf("streak %d: got %d, want %d", tc.streak, got, tc.want)
		}
	}
}

func TestScorePointsDefaultsZeroBase(t *testing.T) {
	// A misconfigured question must still be worth something.
	if got := scorePoints(0, 0, 1); got <= 0 {
		t.Errorf("zero base scored %d, want a positive fallback", got)
	}
}

func TestClampTime(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"negative clock skew", -500, 0},
		{"normal", 4200, 4200},
		{"inside the grace window", QuestionTimeLimitMS + 1000, QuestionTimeLimitMS + 1000},
		{"beyond grace is capped", QuestionTimeLimitMS + GraceMS + 60000, QuestionTimeLimitMS},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampTime(tc.in); got != tc.want {
				t.Errorf("clampTime(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// A client reporting an implausibly large time must not be able to claim the
// speed bonus, nor to wrap the arithmetic into a negative score.
func TestScoreCannotBeGamedByInflatedTime(t *testing.T) {
	got := scorePoints(30, clampTime(1<<30), 1)
	if got != 30 {
		t.Errorf("inflated time scored %d, want the base 30", got)
	}
	if got < 0 {
		t.Errorf("score went negative: %d", got)
	}
}

func TestXPForRound(t *testing.T) {
	if got := xpForRound(0, 0); got != 0 {
		t.Errorf("empty round = %d XP, want 0", got)
	}
	// Half the score plus five per correct answer.
	if got := xpForRound(200, 8); got != 140 {
		t.Errorf("xpForRound(200, 8) = %d, want 140", got)
	}
	// More correct answers at the same score must never pay less.
	if xpForRound(200, 9) < xpForRound(200, 8) {
		t.Error("XP must be monotonic in correct answers")
	}
}

func TestPraiseThresholds(t *testing.T) {
	cases := []struct {
		accuracy int
		want     string
	}{
		{100, "result.praise.excellent"},
		{90, "result.praise.excellent"},
		{89, "result.praise.good"},
		{70, "result.praise.good"},
		{69, "result.praise.average"},
		{40, "result.praise.average"},
		{39, "result.praise.low"},
		{0, "result.praise.low"},
	}
	for _, tc := range cases {
		if got := Praise(tc.accuracy); got != tc.want {
			t.Errorf("Praise(%d) = %q, want %q", tc.accuracy, got, tc.want)
		}
	}
}

// The speed bonus used to be whatever the client said it was: a request with
// timeMs: 0 collected the full bonus on every question and nothing could tell.
// elapsedMS is what closed that, so each half of the rule gets a case.
func TestElapsedMSCannotBeUnderstated(t *testing.T) {
	now := time.Now()
	served := now.Add(-20 * time.Second) // the server saw 20s pass

	// A client claiming it answered instantly is held to what the server saw,
	// less the latency grace.
	got := elapsedMS(0, 0, &served, now)
	want := 20_000 - GraceMS
	if got != want {
		t.Errorf("a client claiming 0ms got %d, want the server floor %d", got, want)
	}
}

// Leaving a round banks the clock and clears the stamp, so the floor has to
// come from the banked time or a resumed question could be answered instantly
// for the full speed bonus.
func TestElapsedMSCountsBankedTimeWithoutAStamp(t *testing.T) {
	got := elapsedMS(0, 8_000, nil, time.Now())
	want := 8_000 - GraceMS
	if got != want {
		t.Errorf("a resumed question gave %d, want the banked floor %d", got, want)
	}
}

// A question picked back up and read on: the time banked before leaving and
// the time since resuming are the same question's clock, so they add.
func TestElapsedMSAddsBankedAndCurrentVisit(t *testing.T) {
	now := time.Now()
	served := now.Add(-5 * time.Second) // resumed five seconds ago

	got := elapsedMS(0, 8_000, &served, now)
	want := 13_000 - GraceMS
	if got != want {
		t.Errorf("got %d, want banked plus this visit %d", got, want)
	}
}

func TestElapsedMSTrustsAHonestSlowClient(t *testing.T) {
	now := time.Now()
	served := now.Add(-2 * time.Second)

	// The client took longer than the server's floor — believe it, so a slow
	// render is not silently rewarded.
	if got := elapsedMS(4_000, 0, &served, now); got != 4_000 {
		t.Errorf("got %d, want the client's own 4000ms", got)
	}
}

func TestElapsedMSWithoutAServerStamp(t *testing.T) {
	// Rounds started before served_at existed, and any round whose stamping
	// write failed, fall back to the old behaviour rather than scoring zero.
	if got := elapsedMS(3_000, 0, nil, time.Now()); got != 3_000 {
		t.Errorf("got %d, want the client's 3000ms", got)
	}

	var zero time.Time
	if got := elapsedMS(3_000, 0, &zero, time.Now()); got != 3_000 {
		t.Errorf("a zero stamp gave %d, want 3000ms", got)
	}
}

func TestElapsedMSClampsToTheLimit(t *testing.T) {
	now := time.Now()
	served := now.Add(-10 * time.Minute)

	// Someone who left the tab open for ten minutes timed out; they did not
	// earn a ten-minute duration on the record.
	if got := elapsedMS(0, 0, &served, now); got != QuestionTimeLimitMS {
		t.Errorf("got %d, want it clamped to the limit %d", got, QuestionTimeLimitMS)
	}
}

func TestElapsedMSNeverGoesNegative(t *testing.T) {
	now := time.Now()
	// Clock skew, or an answer arriving faster than the grace allows for.
	served := now.Add(-100 * time.Millisecond)
	if got := elapsedMS(0, 0, &served, now); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

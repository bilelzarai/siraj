package views

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAvatarStyleIsStable(t *testing.T) {
	// The same person must keep the same colours across pages and restarts.
	first := AvatarStyle("seed-abc")
	for i := 0; i < 50; i++ {
		if got := AvatarStyle("seed-abc"); got != first {
			t.Fatalf("avatar colour changed between calls: %q then %q", first, got)
		}
	}
	if !strings.Contains(first, "--a1:#") || !strings.Contains(first, "--a2:#") {
		t.Errorf("style %q should define both gradient stops", first)
	}
}

func TestAvatarStyleSpreadsAcrossPalette(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		seen[AvatarStyle(uuid.New().String())] = true
	}
	if len(seen) < len(avatarPalette) {
		t.Errorf("only %d of %d palette entries were used", len(seen), len(avatarPalette))
	}
}

func TestAvatarStyleHandlesEmptySeed(t *testing.T) {
	if got := AvatarStyle(""); !strings.Contains(got, "--a1:") {
		t.Errorf("empty seed produced %q, want a usable style", got)
	}
}

func TestAvatarSeedForPrecedence(t *testing.T) {
	id := uuid.New()

	if got := AvatarSeedFor(id, "stored", "malek"); got != "stored" {
		t.Errorf("a stored seed should win, got %q", got)
	}
	if got := AvatarSeedFor(id, "", "malek"); got != "malek" {
		t.Errorf("username should be the first fallback, got %q", got)
	}
	if got := AvatarSeedFor(id, "", ""); got != id.String() {
		t.Errorf("id should be the last resort, got %q", got)
	}
}

func TestAnswerKey(t *testing.T) {
	for i, want := range []string{"A", "B", "C", "D"} {
		if got := AnswerKey("en", i); got != want {
			t.Errorf("AnswerKey(en, %d) = %q, want %q", i, got, want)
		}
	}
	for i, want := range []string{"أ", "ب", "ج", "د"} {
		if got := AnswerKey("ar", i); got != want {
			t.Errorf("AnswerKey(ar, %d) = %q, want %q", i, got, want)
		}
	}
	// Out-of-range indices must not panic.
	if got := AnswerKey("ar", 9); got != "?" {
		t.Errorf("out-of-range Arabic key = %q, want ?", got)
	}
	if got := AnswerKey("en", -1); got != "?" {
		t.Errorf("negative key = %q, want ?", got)
	}
}

func TestPercentGuardsZero(t *testing.T) {
	if got := Percent(5, 0); got != 0 {
		t.Errorf("Percent(5, 0) = %d, want 0", got)
	}
	if got := Percent(3, 10); got != 30 {
		t.Errorf("Percent(3, 10) = %d, want 30", got)
	}
}

func TestBarHeightIsBounded(t *testing.T) {
	cases := []struct{ value, max, want int }{
		{0, 100, 3},  // nothing to show
		{5, 0, 3},    // no scale yet
		{1, 1000, 6}, // clamped to the visible minimum
		{50, 100, 50},
		{100, 100, 100},
		{200, 100, 100}, // never overflows the track
	}
	for _, tc := range cases {
		if got := BarHeight(tc.value, tc.max); got != tc.want {
			t.Errorf("BarHeight(%d, %d) = %d, want %d", tc.value, tc.max, got, tc.want)
		}
	}
}

func TestDifficultyKey(t *testing.T) {
	cases := map[int]string{
		0:  "game.difficulty.any",
		1:  "game.difficulty.1",
		2:  "game.difficulty.2",
		3:  "game.difficulty.3",
		9:  "game.difficulty.any",
		-1: "game.difficulty.any",
	}
	for in, want := range cases {
		if got := DifficultyKey(in); got != want {
			t.Errorf("DifficultyKey(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRankClass(t *testing.T) {
	if got := RankClass(1); got != "lb-rank lb-rank--1" {
		t.Errorf("got %q", got)
	}
	if got := RankClass(4); got != "lb-rank" {
		t.Errorf("rank 4 should get no medal class, got %q", got)
	}
	if got := RankClass(0); got != "lb-rank" {
		t.Errorf("rank 0 should get no medal class, got %q", got)
	}
}

func TestDayLabelCoversEveryWeekday(t *testing.T) {
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) // a Sunday
	for _, locale := range []string{"ar", "en", "fr"} {
		for i := 0; i < 7; i++ {
			if got := DayLabel(locale, base.AddDate(0, 0, i)); got == "" {
				t.Errorf("%s day %d produced an empty label", locale, i)
			}
		}
	}
}

func TestCtxActive(t *testing.T) {
	c := Ctx{Path: "/messages/abc"}
	if !c.Active("/messages") {
		t.Error("a nested path should mark its section active")
	}
	if c.Active("/friends") {
		t.Error("an unrelated section must not be active")
	}
	if c.AriaCurrent("/messages") != "page" {
		t.Error("the active section should report aria-current=page")
	}

	home := Ctx{Path: "/app"}
	if !home.Active("/") {
		t.Error("/app should light up the home entry")
	}
}

func TestInitialUppercases(t *testing.T) {
	if got := Initial("malek"); got != "M" {
		t.Errorf("got %q, want M", got)
	}
	if got := Initial("  spaced"); got != "S" {
		t.Errorf("leading space should be trimmed, got %q", got)
	}
	if got := Initial(""); got != "?" {
		t.Errorf("empty name = %q, want ?", got)
	}
}

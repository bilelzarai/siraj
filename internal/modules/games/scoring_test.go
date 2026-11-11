package games

import "testing"

// GP-04: base 10/20/30 by difficulty plus up to 10 for speed, integers only.
func TestPoints(t *testing.T) {
	cases := []struct {
		name       string
		difficulty int32
		timeMS     int32
		seconds    int32
		want       int32
	}{
		{"easy, instant", 1, 0, 20, 20},
		{"easy, half the time", 1, 10_000, 20, 15},
		{"easy, out of time", 1, 20_000, 20, 10},
		{"medium, instant", 2, 0, 20, 30},
		{"hard, instant", 3, 0, 15, 40},
		{"hard, 80% of the time used", 3, 12_000, 15, 32},
		{"no timer keeps the base", 3, 99_000, 0, 30},
		{"over the limit keeps the base", 2, 40_000, 20, 20},
		{"negative time is treated as instant", 2, -5, 20, 30},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Points(c.difficulty, c.timeMS, c.seconds); got != c.want {
				t.Errorf("Points(%d, %d, %d) = %d, want %d", c.difficulty, c.timeMS, c.seconds, got, c.want)
			}
		})
	}
}

func TestMaxPoints(t *testing.T) {
	if got := MaxPoints(2, 5, 20); got != 150 {
		t.Errorf("a 5-question medium game with a timer is worth 150, got %d", got)
	}
	if got := MaxPoints(2, 5, 0); got != 100 {
		t.Errorf("without a timer it is worth 100, got %d", got)
	}
}

func TestLevels(t *testing.T) {
	if XPForLevel(1) != 0 || XPForLevel(2) != 100 || XPForLevel(3) != 300 {
		t.Errorf("level thresholds are wrong: %d %d %d", XPForLevel(1), XPForLevel(2), XPForLevel(3))
	}
	for _, c := range []struct{ xp, level int32 }{{0, 1}, {99, 1}, {100, 2}, {299, 2}, {300, 3}} {
		if got := LevelFor(c.xp); got != c.level {
			t.Errorf("LevelFor(%d) = %d, want %d", c.xp, got, c.level)
		}
	}
}

func TestXPForGame(t *testing.T) {
	if got := XPForGame(150, 5, 5); got != 85 {
		t.Errorf("a perfect game of 150 points gives 85 XP, got %d", got)
	}
	if got := XPForGame(60, 3, 5); got != 30 {
		t.Errorf("60 points gives 30 XP, got %d", got)
	}
}

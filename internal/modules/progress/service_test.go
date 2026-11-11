package progress

import "testing"

// The level curve is 50 × (level − 1)², mirrored in AddUserXP; the two must agree or
// users.level drifts away from users.xp.
func TestLevelCurve(t *testing.T) {
	cases := []struct {
		xp    int32
		level int32
	}{{0, 1}, {49, 1}, {50, 2}, {199, 2}, {200, 3}, {450, 4}, {800, 5}}
	for _, c := range cases {
		if got := LevelForXP(c.xp); got != c.level {
			t.Errorf("LevelForXP(%d) = %d, want %d", c.xp, got, c.level)
		}
	}
	for level := int32(1); level <= 20; level++ {
		if got := LevelForXP(XPForLevel(level)); got != level {
			t.Errorf("level %d: XP %d maps back to %d", level, XPForLevel(level), got)
		}
	}
}

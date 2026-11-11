package rooms

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

// A room that has never been split still comes back even: the lobby shows two sides
// without the host touching anything (RM-05).
func TestSplitDealsAnUntouchedRoomEvenly(t *testing.T) {
	members := ids(4)
	got := Split(nil, members)
	if len(got[0]) != 2 || len(got[1]) != 2 {
		t.Fatalf("want 2 v 2, got %d v %d", len(got[0]), len(got[1]))
	}
}

// The host's arrangement survives; whoever left drops out and whoever arrived joins
// the shorter side.
func TestSplitKeepsTheArrangementAndPlacesNewcomers(t *testing.T) {
	m := ids(5)
	stored := [][]string{
		{m[0].String(), m[1].String(), uuid.New().String()}, // one of them has left
		{m[2].String()},
	}
	got := Split(stored, m)
	if got.Of(m[0]) != 0 || got.Of(m[1]) != 0 || got.Of(m[2]) != 1 {
		t.Fatalf("the stored sides moved: %v", got)
	}
	// Two newcomers, and the short side fills first.
	if got.Of(m[3]) != 1 {
		t.Fatalf("first newcomer should join the shorter side, got %d", got.Of(m[3]))
	}
	if len(got[0])+len(got[1]) != len(m) {
		t.Fatalf("everyone must be placed: %d of %d", len(got[0])+len(got[1]), len(m))
	}
	for _, side := range got {
		for _, id := range side {
			found := false
			for _, want := range m {
				found = found || want == id
			}
			if !found {
				t.Fatalf("a player who left is still on a side: %s", id)
			}
		}
	}
}

// Nobody is on two sides at once, even when the stored arrangement says so.
func TestSplitNeverPlacesAPlayerTwice(t *testing.T) {
	m := ids(4)
	stored := [][]string{{m[0].String(), m[1].String()}, {m[0].String(), m[2].String()}}
	got := Split(stored, m)
	if len(got[0])+len(got[1]) != 4 {
		t.Fatalf("want four places, got %d", len(got[0])+len(got[1]))
	}
	if got.Of(m[0]) != 0 {
		t.Fatalf("the first side should keep the duplicate, got %d", got.Of(m[0]))
	}
}

// Team totals are added up, never stored (§2 of 03-BACKEND-CHANGES).
func TestTeamTotalsAddTheSidesUp(t *testing.T) {
	rows := []Standing{
		{Team: 0, Score: 120, Gain: 20},
		{Team: 0, Score: 80, Gain: 0},
		{Team: 1, Score: 150, Gain: 24},
		{Team: -1, Score: 999, Gain: 99}, // a player with no side is not counted
	}
	totals, gains, counts := TeamTotals(rows)
	if totals != [2]int32{200, 150} {
		t.Fatalf("totals: %v", totals)
	}
	if gains != [2]int32{20, 24} {
		t.Fatalf("gains: %v", gains)
	}
	if counts != [2]int{2, 1} {
		t.Fatalf("counts: %v", counts)
	}
	if !HasTeams(rows) {
		t.Fatal("a game with both sides filled plays in teams")
	}
	if HasTeams([]Standing{{Team: 0}, {Team: -1}}) {
		t.Fatal("one side alone is not a team game")
	}
}

// A room that drops below four players plays for itself instead of two against one
// (RM-05).
func TestModeFallsBackWhenTheRoomIsShort(t *testing.T) {
	s := &Service{}
	set := Settings{Mode: ModeTeams}
	if got := s.Mode(set, 3); got != ModeFFA {
		t.Fatalf("want ffa under four players, got %s", got)
	}
	if got := s.Mode(set, 4); got != ModeTeams {
		t.Fatalf("want teams at four players, got %s", got)
	}
}

// The room settings panel only takes the rounds the rest of the app can play.
func TestUpdateSettingsRefusesARoundNobodyCanPlay(t *testing.T) {
	for _, bad := range []SettingsInput{
		{Count: 7, Seconds: 20},
		{Count: 10, Seconds: 45},
		{Count: 0, Seconds: 0},
	} {
		if allowedCounts[bad.Count] && allowedSeconds[bad.Seconds] {
			t.Errorf("%v should not be a playable round", bad)
		}
	}
	if !allowedCounts[10] || !allowedSeconds[20] {
		t.Error("the default round was refused")
	}
}

// The room's invite link becomes a square a phone can read; an empty link draws
// nothing rather than an empty box.
func TestQRDrawsTheLinkAndNothingElse(t *testing.T) {
	if got := QR(""); got != "" {
		t.Errorf("an empty link drew %q", got)
	}
	svg := QR("https://siraj.app/r/HX4M2P")
	for _, want := range []string{"<svg", "viewBox", "currentColor", "</svg>"} {
		if !strings.Contains(svg, want) {
			t.Errorf("the square is missing %q", want)
		}
	}
	// Nothing in it may carry a colour of its own: the page's ink paints it (BR-02).
	if strings.Contains(svg, "#") {
		t.Error("the square wrote a colour instead of taking the page's")
	}
}

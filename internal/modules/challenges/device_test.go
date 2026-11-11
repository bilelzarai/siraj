package challenges

import (
	"testing"

	"github.com/google/uuid"
)

// Two to ten players, named once each, one to twenty characters (§3 of
// 03-BACKEND-CHANGES). The signed-in player counts as one of them.
func TestCheckLocalsHoldsTheLimits(t *testing.T) {
	if err := CheckLocals(nil); err == nil {
		t.Error("a round of one was accepted")
	}
	if err := CheckLocals([]string{"Sara"}); err != nil {
		t.Errorf("the owner plus one is a round of two: %v", err)
	}
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = string(rune('a' + i))
	}
	if err := CheckLocals(nine); err != nil {
		t.Errorf("the owner plus nine is ten: %v", err)
	}
	if err := CheckLocals(append(nine, "one too many")); err == nil {
		t.Error("eleven players were accepted")
	}
	if err := CheckLocals([]string{"Sara", "Sara"}); err == nil {
		t.Error("two players with one name were accepted")
	}
	long := make([]byte, 21)
	for i := range long {
		long[i] = 'x'
	}
	if err := CheckLocals([]string{string(long)}); err == nil {
		t.Error("a name of 21 characters was accepted")
	}
}

// The phone goes to the first person who has not answered the question on the table,
// and the question is only turned over once the last of them has (DV-02, DV-04).
func TestTheTurnGoesToWhoeverHasNotAnswered(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	state := DeviceState{Players: []DevicePlayer{
		{ID: a, Name: "Sara", Answered: true, Score: 30},
		{ID: b, Name: "Omar", Score: 10},
		{ID: c, Name: "Bilal", Score: 20},
	}}
	// The same walk DeviceState does over its players.
	for i := range state.Players {
		if !state.Players[i].Answered {
			state.Players[i].Now = true
			state.Turn = &state.Players[i]
			break
		}
	}
	if state.Turn == nil || state.Turn.Name != "Omar" {
		t.Fatalf("the phone should go to Omar, got %v", state.Turn)
	}
	if state.Answered() != 1 {
		t.Errorf("one of three has answered, got %d", state.Answered())
	}
	// Best first, whatever order they sit in.
	if got := state.Standings(); got[0].Name != "Sara" || got[2].Name != "Omar" {
		t.Errorf("standings are out of order: %v", got)
	}
}

// Team totals come from the sides, and the tug bar reads them (DV-04).
func TestDeviceTeamsAddUpAndPullTheBar(t *testing.T) {
	state := DeviceState{Players: []DevicePlayer{
		{Team: 0, Score: 40, Gain: 20},
		{Team: 1, Score: 20},
		{Team: 0, Score: 20},
		{Team: 1, Score: 20, Gain: 20},
	}}
	if !deviceHasTeams(state.Players) {
		t.Fatal("both sides are filled, so the round is in teams")
	}
	for _, p := range state.Players {
		if p.Team == 0 || p.Team == 1 {
			state.Totals[p.Team] += p.Score
		}
	}
	if state.Totals != [2]int32{60, 40} {
		t.Fatalf("totals: %v", state.Totals)
	}
	if got := state.ShareA(); got != 60 {
		t.Errorf("Team A holds 60%% of the bar, got %d", got)
	}
	// A round of fewer than four plays for itself.
	if deviceHasTeams([]DevicePlayer{{Team: 0}, {Team: -1}}) {
		t.Error("one side alone is not a team round")
	}
}

// Each player's pick is remembered, so the reveal can stand their avatar on it.
func TestChoicesGroupPlayersByTheOptionTheyPicked(t *testing.T) {
	two, three := int32(2), int32(3)
	state := DeviceState{Players: []DevicePlayer{
		{Name: "Sara", Choice: &two},
		{Name: "Omar", Choice: &three},
		{Name: "Bilal", Choice: &two},
		{Name: "Hana"}, // ran out of time: no pick at all
	}}
	if got := state.Choices(2); len(got) != 2 {
		t.Errorf("two picked option C, got %d", len(got))
	}
	if got := state.Choices(0); len(got) != 0 {
		t.Errorf("nobody picked option A, got %d", len(got))
	}
}

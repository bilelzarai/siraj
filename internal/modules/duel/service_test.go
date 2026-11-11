package duel

import "testing"

// A player with no finished duel has no win rate: the card shows their level alone
// rather than a flat zero (DU-01).
func TestWinRateOnlyExistsOnceThereIsARecord(t *testing.T) {
	if _, ok := WinRate(0, 0); ok {
		t.Error("a player who has never duelled was given a win rate")
	}
	rate, ok := WinRate(5, 3)
	if !ok || rate != 62 {
		t.Errorf("5 wins in 8 duels is 62%%, got %d (ok=%v)", rate, ok)
	}
	if rate, _ := WinRate(3, 0); rate != 100 {
		t.Errorf("an unbeaten player is at 100%%, got %d", rate)
	}
}

// The rating moves the design prints on the results screen (DU-04).
func TestSettleMovesTheRatingTheWayTheDesignSays(t *testing.T) {
	if d, w, l, dr := settle(120, 90); d != 25 || w != 1 || l != 0 || dr != 0 {
		t.Errorf("a win: %d %d %d %d", d, w, l, dr)
	}
	if d, w, l, dr := settle(90, 120); d != -12 || w != 0 || l != 1 || dr != 0 {
		t.Errorf("a loss: %d %d %d %d", d, w, l, dr)
	}
	if d, w, l, dr := settle(100, 100); d != 5 || w != 0 || l != 0 || dr != 1 {
		t.Errorf("a draw: %d %d %d %d", d, w, l, dr)
	}
}

// A draw is a draw: Record.Won is only true when this player actually scored more.
func TestRecordWonIsStrict(t *testing.T) {
	if (Record{LastMine: 100, LastThem: 100}).Won() {
		t.Error("a level score was counted as a win")
	}
	if !(Record{LastMine: 118, LastThem: 96}).Won() {
		t.Error("118 to 96 is a win")
	}
}

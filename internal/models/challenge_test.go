package models

import (
	"testing"

	"github.com/google/uuid"
)

func player(state string, score int, host bool) *ChallengePlayer {
	p := &ChallengePlayer{UserID: uuid.New(), State: state, IsHost: host}
	if state == PlayerPlayed {
		s := score
		p.Score = &s
	}
	return p
}

// A match is not a match until two people are in it. One person answering
// questions alone is a solo round with a scoreboard that has nothing to compare
// against.
func TestReadyToStartNeedsTwo(t *testing.T) {
	host := player(PlayerJoined, 0, true)
	ch := &Challenge{Players: []*ChallengePlayer{host}}
	if ch.ReadyToStart() {
		t.Error("a match with only its host was ready to start")
	}

	ch.Players = append(ch.Players, player(PlayerInvited, 0, false))
	if ch.ReadyToStart() {
		t.Error("an invitation nobody accepted counted towards the minimum")
	}

	ch.Players = append(ch.Players, player(PlayerJoined, 0, false))
	if !ch.ReadyToStart() {
		t.Error("two joined players were not enough to start")
	}
}

// The scoreboard is everyone who has finished, best first — and it is not
// decided while anybody could still change it.
func TestStandingAndWinner(t *testing.T) {
	low := player(PlayerPlayed, 40, true)
	high := player(PlayerPlayed, 90, false)
	pending := player(PlayerJoined, 0, false)

	ch := &Challenge{Players: []*ChallengePlayer{low, high, pending}}
	standing := ch.Standing()
	if len(standing) != 2 || standing[0] != high {
		t.Fatalf("standing = %v, want the two who played with 90 first", standing)
	}
	if ch.Winner() != nil {
		t.Error("a winner was declared while somebody was still to play")
	}
	if names := ch.Waiting(); len(names) != 1 || names[0] != pending {
		t.Errorf("waiting = %v, want the one who has not played", names)
	}

	// Once the last player is in, the top score wins.
	pending.State = PlayerPlayed
	score := 10
	pending.Score = &score
	if ch.Winner() != high {
		t.Errorf("winner = %v, want the 90", ch.Winner())
	}

	// Two at the top is a draw, not a coin toss.
	tied := 90
	low.Score = &tied
	if ch.Winner() != nil {
		t.Error("a tie produced a winner")
	}
}

// Somebody who declined is not waited for, and does not count towards the
// minimum either.
func TestDeclinedPlayersAreNotWaitedFor(t *testing.T) {
	ch := &Challenge{Players: []*ChallengePlayer{
		player(PlayerJoined, 0, true),
		player(PlayerDeclined, 0, false),
	}}
	if len(ch.Waiting()) != 1 {
		t.Errorf("waiting = %d, want only the host who has not played", len(ch.Waiting()))
	}
	if ch.ReadyToStart() {
		t.Error("a declined invitation counted towards the minimum")
	}
}

// A set is playable when it can fill a round. The two numbers have to agree:
// offering a set and then refusing it is worse than not offering it.
func TestSetPlayability(t *testing.T) {
	cases := []struct {
		count    int
		playable bool
		needed   int
	}{
		{0, false, MinSetQuestions},
		{1, false, MinSetQuestions - 1},
		{MinSetQuestions - 1, false, 1},
		{MinSetQuestions, true, 0},
		{MinSetQuestions + 10, true, 0},
	}
	for _, tc := range cases {
		set := &QuestionSet{Count: tc.count}
		if set.Playable() != tc.playable {
			t.Errorf("a set of %d: playable = %v, want %v", tc.count, set.Playable(), tc.playable)
		}
		if set.Needed() != tc.needed {
			t.Errorf("a set of %d: needs %d more, want %d", tc.count, set.Needed(), tc.needed)
		}
	}
}

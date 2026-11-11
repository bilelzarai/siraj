package challenges

import (
	"testing"

	"siraj/internal/db/sqlc"
)

// score is a pointer helper: a nil score is one the viewer may not see yet.
func score(n int32) *int32 { return &n }

// A challenge the host may close: nobody is mid-game, and somebody never started
// (CH-07). While anyone is playing, the button stays shut — a player who started
// keeps the window the decision gives them (D-03).
func TestCloseEarlyOnlyWhenNobodyIsPlaying(t *testing.T) {
	cases := []struct {
		name    string
		players []Player
		host    bool
		want    bool
	}{
		{"host, one never started", []Player{
			{Status: string(sqlc.ParticipantStatusPlayed), IsMe: true},
			{Status: string(sqlc.ParticipantStatusPending)},
		}, true, true},
		{"somebody is mid-game", []Player{
			{Status: string(sqlc.ParticipantStatusPlayed), IsMe: true},
			{Status: string(sqlc.ParticipantStatusPlaying)},
			{Status: string(sqlc.ParticipantStatusPending)},
		}, true, false},
		{"everyone played, nothing to close", []Player{
			{Status: string(sqlc.ParticipantStatusPlayed), IsMe: true},
			{Status: string(sqlc.ParticipantStatusPlayed)},
		}, true, false},
		{"not the host", []Player{
			{Status: string(sqlc.ParticipantStatusPlayed), IsMe: true},
			{Status: string(sqlc.ParticipantStatusPending)},
		}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Detail{IsHost: tc.host}
			for _, p := range tc.players {
				switch p.Status {
				case string(sqlc.ParticipantStatusPlaying):
					d.Playing++
				case string(sqlc.ParticipantStatusPending):
					d.NotStart++
				}
			}
			got := d.IsHost && d.Playing == 0 && d.NotStart > 0
			if got != tc.want {
				t.Errorf("close available = %v, want %v", got, tc.want)
			}
		})
	}
}

// The rule the whole module exists for: before a challenge closes, nobody's score
// but your own leaves the server (CH-04).
func TestOpenChallengeCarriesNobodyElsesScore(t *testing.T) {
	open := Card{Status: string(sqlc.ChallengeStatusOpen), Players: []Player{
		{Name: "Me", IsMe: true, Score: score(84)},
		{Name: "Them", Score: nil},
	}}
	if open.Closed() {
		t.Fatal("an open challenge must not read as closed")
	}
	for _, p := range open.Players {
		if !p.IsMe && p.Score != nil {
			t.Errorf("%s's score left the server before the end: %d", p.Name, *p.Score)
		}
	}

	// Once closed, everyone's score is fair game.
	done := Card{Status: string(sqlc.ChallengeStatusClosed), Players: []Player{
		{Name: "Me", IsMe: true, Score: score(84)},
		{Name: "Them", Score: score(91)},
	}}
	if !done.Closed() {
		t.Fatal("a closed challenge should read as closed")
	}
	for _, p := range done.Players {
		if p.Score == nil {
			t.Errorf("%s has no score on a closed challenge", p.Name)
		}
	}
}

// A rematch keeps the window the first challenge gave, and never less than a day.
func TestDaysLeftFallsBackSensibly(t *testing.T) {
	if got := daysLeft(sqlc.Challenge{}); got != 3 {
		t.Errorf("a challenge with no deadline rematches on three days, got %d", got)
	}
}

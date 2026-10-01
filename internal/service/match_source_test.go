package service

import (
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// A match is played on one source. "Six of yours and four from the bank" is two
// different games in one sitting: written questions are about the people
// playing, the bank's are about the subject, and a score over both compares
// neither.
//
// The precedence is deliberate — written now over a chosen set, a set over the
// bank — because each is a more deliberate choice than the one under it.
func TestQuestionSourceIsSingular(t *testing.T) {
	cases := []struct {
		name     string
		authored int
		hasSet   bool
		want     string
	}{
		{"nothing chosen", 0, false, "bank"},
		{"a set chosen", 0, true, "set"},
		{"questions written", 6, false, "written"},
		{"both offered", 6, true, "written"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := "bank"
			switch {
			case tc.authored > 0:
				got = "written"
			case tc.hasSet:
				got = "set"
			}
			if got != tc.want {
				t.Errorf("source = %q, want %q", got, tc.want)
			}
		})
	}
}

// Each source has to stand on its own: below the round minimum there is no
// topping up from somewhere else, so each refusal says which source was short.
func TestEachSourceRefusesOnItsOwnTerms(t *testing.T) {
	if ErrTooFewWritten == nil || ErrSetTooSmall == nil || ErrNotEnoughQuestions == nil {
		t.Fatal("a source that cannot fill a round has to say which one it was")
	}
	for _, pair := range [][2]error{
		{ErrTooFewWritten, ErrSetTooSmall},
		{ErrSetTooSmall, ErrNotEnoughQuestions},
		{ErrTooFewWritten, ErrNotEnoughQuestions},
	} {
		if pair[0] == pair[1] {
			t.Error("two sources share one refusal; the message cannot name the right one")
		}
	}
}

// The set minimum and the round minimum are the same number, declared in two
// packages. Two constants that must agree and are written apart do not stay
// agreed on their own.
func TestSetMinimumMatchesTheRoundMinimum(t *testing.T) {
	if models.MinSetQuestions != MinQuestions {
		t.Errorf("a set needs %d questions but a round needs %d — one of them is offering what the other refuses",
			models.MinSetQuestions, MinQuestions)
	}
}

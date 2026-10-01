package repository_test

import (
	"context"
	"testing"

	"github.com/bilelzarai/siraj/internal/repository"
)

func TestExistingQuestionIDs(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	here := addQuestion(t, 910001, "Which surah opens the Qur'an?")

	got, err := r.ExistingQuestionIDs(ctx, []int{here, 999999})
	if err != nil {
		t.Fatalf("existing: %v", err)
	}
	if !got[here] {
		t.Errorf("%d is in the bank and was not reported", here)
	}
	if got[999999] {
		t.Error("999999 is not in the bank and was reported as present")
	}
	if empty, err := r.ExistingQuestionIDs(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("empty input = %v, %v; want an empty map and no error", empty, err)
	}
}

// One round trip has to answer for every row of a file, each row against its
// own exclusion, and say nothing at all about the rows with no near match.
func TestBestMatchesAnswersEveryProbeAtOnce(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	badr := addQuestion(t, 910010, "In which month did the Battle of Badr take place?")
	addQuestion(t, 910011, "Who was the first muezzin of Islam?")

	probes := []repository.PromptProbe{
		// Line 7 is word-for-word the Badr question: a match.
		{Key: 7, Prompt: "In which month did the Battle of Badr take place?"},
		// Line 8 resembles nothing in the bank.
		{Key: 8, Prompt: "What is the capital city of Morocco?"},
		// Line 9 is the Badr question again, but it *is* that question being
		// updated, so it must not be reported as its own duplicate.
		{Key: 9, Prompt: "In which month did the Battle of Badr take place?", ExcludeID: badr},
	}

	got, err := r.BestMatches(ctx, probes, "en", 0.85)
	if err != nil {
		t.Fatalf("best matches: %v", err)
	}

	if m, ok := got[7]; !ok || m.RightID != badr {
		t.Errorf("line 7 = %+v, want a match on %d", got[7], badr)
	}
	if _, ok := got[8]; ok {
		t.Errorf("line 8 matched %+v, want nothing", got[8])
	}
	if _, ok := got[9]; ok {
		t.Errorf("line 9 matched itself: %+v", got[9])
	}
	if m := got[7]; m != nil && m.Percent() != 100 {
		t.Errorf("identical prompts scored %d%%, want 100%%", m.Percent())
	}
}

// A pair a moderator has read and called different has to stop coming back.
// The list showed the same sixty every visit, including the ones already
// judged — and a list that cannot be worked through stops being read.
func TestMarkPairDistinctTakesThePairOffTheSweep(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	who := newUser(t, "verdict_mod", "verdict.mod@example.com")

	left := addQuestion(t, 960001, "Which companion is called the Sword of Allah?")
	right := addQuestion(t, 960002, "Which companion is called the Sword of God?")

	found := func() bool {
		pairs, err := r.NearDuplicates(ctx, 0.5, 200, "en")
		if err != nil {
			t.Fatalf("sweep: %v", err)
		}
		for _, p := range pairs {
			if (p.LeftID == left && p.RightID == right) || (p.LeftID == right && p.RightID == left) {
				return true
			}
		}
		return false
	}

	if !found() {
		t.Fatal("the near-identical pair was not raised to begin with")
	}

	// Recorded the other way round on purpose: the sweep surfaces a pair in
	// whichever order the language gives it, and one verdict has to settle both.
	if err := r.MarkPairDistinct(ctx, right, left, who.ID); err != nil {
		t.Fatalf("mark distinct: %v", err)
	}
	if found() {
		t.Error("the pair is still being raised after being judged different")
	}

	// And a decision taken by mistake can be undone.
	if err := r.ForgetPairVerdict(ctx, left, right); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !found() {
		t.Error("the pair did not come back after the verdict was withdrawn")
	}
}

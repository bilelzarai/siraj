package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

func writeSet(t *testing.T, owner uuid.UUID, name string, prompts ...string) uuid.UUID {
	t.Helper()
	r := repo(t)
	set, err := r.CreateQuestionSet(t.Context(), owner, name, "en")
	if err != nil {
		t.Fatalf("create set: %v", err)
	}
	if len(prompts) == 0 {
		return set.ID
	}

	drafts := make([]models.TranslationDraft, 0, len(prompts))
	cats := make([]int, 0, len(prompts))
	diffs := make([]int, 0, len(prompts))
	correct := make([]int, 0, len(prompts))
	for _, prompt := range prompts {
		drafts = append(drafts, models.TranslationDraft{
			Prompt: prompt, Choices: []string{"a", "b", "c", "d"}, Source: "player",
		})
		cats = append(cats, 1)
		diffs = append(diffs, 1)
		correct = append(correct, 0)
	}
	if _, err := r.AddToQuestionSet(t.Context(), set.ID, owner, "en", drafts, cats, diffs, correct); err != nil {
		t.Fatalf("add to set: %v", err)
	}
	return set.ID
}

// A player's own set is theirs: they can write into it, read it back, and
// nobody else can touch it.
func TestQuestionSetIsPrivateToItsOwner(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	mine := newUser(t, "set_mine", "set.mine@example.com")
	other := newUser(t, "set_other", "set.other@example.com")

	setID := writeSet(t, mine.ID, "Ramadan quiz", "Which night is Laylat al-Qadr in?")

	if _, err := r.QuestionSet(ctx, setID, mine.ID); err != nil {
		t.Fatalf("the owner cannot read their own set: %v", err)
	}
	if _, err := r.QuestionSet(ctx, setID, other.ID); err == nil {
		t.Error("somebody else read a private set")
	}
	if _, err := r.AddToQuestionSet(ctx, setID, other.ID, "en",
		[]models.TranslationDraft{{Prompt: "x", Choices: []string{"a", "b", "c", "d"}}},
		[]int{1}, []int{1}, []int{0}); err == nil {
		t.Error("somebody else wrote into a private set")
	}

	questions, err := r.SetQuestions(ctx, setID, mine.ID, "en")
	if err != nil {
		t.Fatalf("read set: %v", err)
	}
	if len(questions) != 1 || questions[0].Prompt != "Which night is Laylat al-Qadr in?" {
		t.Fatalf("set contains %+v", questions)
	}

	// And the containment rule still holds: a written question is not the bank.
	drawn, err := r.PickQuestionIDs(ctx, nil, nil, 0, 100, "en")
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	for _, id := range drawn {
		if id == questions[0].ID {
			t.Fatal("a question from somebody's private set was drawn into an ordinary round")
		}
	}
}

// Playing the same friend weekly from the same set should not ask them the same
// things every week.
func TestPickFromSetPrefersWhatTheOpponentHasNotSeen(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	host := newUser(t, "unseen_host", "unseen.host@example.com")
	rival := newUser(t, "unseen_rival", "unseen.rival@example.com")

	setID := writeSet(t, host.ID, "Weekly", "Seen one?", "Unseen two?", "Unseen three?")
	questions, err := r.SetQuestions(ctx, setID, host.ID, "en")
	if err != nil {
		t.Fatalf("read set: %v", err)
	}
	seen := questions[0]

	// The rival has already answered the first one, in a round of their own.
	session := &models.GameSession{
		UserID: rival.ID, Mode: models.ModeSolo, Locale: "en",
		QuestionIDs: []int{seen.ID}, TotalQuestions: 1,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := r.RecordAnswer(ctx, session.ID, &models.GameAnswer{
		QuestionID: seen.ID, Position: 0, SelectedIndex: 0, IsCorrect: true,
		TimeMS: 1000, PointsAwarded: 10,
	}, 1); err != nil {
		t.Fatalf("answer: %v", err)
	}

	// Asking for two should give the two they have not met.
	picked, err := r.PickFromSet(ctx, setID, host.ID, []uuid.UUID{rival.ID}, 2)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(picked) != 2 {
		t.Fatalf("picked %d, want 2", len(picked))
	}
	for _, id := range picked {
		if id == seen.ID {
			t.Error("a question the opponent has already answered was preferred over one they had not")
		}
	}

	// Asking for more than the set holds still returns what there is, repeats
	// included — a small set has to repeat, and repeating beats refusing.
	all, err := r.PickFromSet(ctx, setID, host.ID, []uuid.UUID{rival.ID}, 10)
	if err != nil {
		t.Fatalf("pick all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("picked %d from a set of three", len(all))
	}
}

// Writing a question once, while opening a match, and keeping it afterwards is
// the path most people will use.
func TestKeepingMatchQuestionsIntoASet(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	host := newUser(t, "keep_host", "keep.host@example.com")
	guest := newUser(t, "keep_guest", "keep.guest@example.com")
	befriend(t, host.ID, guest.ID)

	ch := &models.Challenge{HostID: host.ID, Difficulty: 1, QuestionIDs: []int{}}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: guest.ID}}); err != nil {
		t.Fatalf("create match: %v", err)
	}
	if _, err := r.CreateMatchQuestions(ctx, ch.ID, host.ID, "en",
		[]models.TranslationDraft{{Prompt: "Worth keeping?", Choices: []string{"a", "b", "c", "d"}}},
		[]int{1}, []int{1}, []int{0}); err != nil {
		t.Fatalf("write match question: %v", err)
	}

	setID := writeSet(t, host.ID, "Keepers")
	added, err := r.SaveMatchQuestionsToSet(ctx, ch.ID, setID, host.ID)
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if added != 1 {
		t.Fatalf("kept %d questions, want 1", added)
	}

	questions, err := r.SetQuestions(ctx, setID, host.ID, "en")
	if err != nil {
		t.Fatalf("read set: %v", err)
	}
	if len(questions) != 1 || questions[0].Prompt != "Worth keeping?" {
		t.Errorf("set holds %+v", questions)
	}

	// Somebody else's set is not a place to put them.
	stranger := newUser(t, "keep_stranger", "keep.stranger@example.com")
	if _, err := r.SaveMatchQuestionsToSet(ctx, ch.ID, setID, stranger.ID); err == nil {
		t.Error("a stranger saved into somebody else's set")
	}
	_ = repository.MaxSetQuestions
}

// A question can be corrected — until somebody has answered it. The review
// screen reads a question's text by joining, not from a copy taken when it was
// asked, so editing a played question changes what people are shown they were
// asked. A score nobody can make sense of is worse than a typo.
func TestEditingAQuestionStopsOnceItHasBeenPlayed(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	owner := newUser(t, "edit_owner", "edit.owner@example.com")

	setID := writeSet(t, owner.ID, "Editable", "Frist question?")
	questions, err := r.SetQuestions(ctx, setID, owner.ID, "en")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	q := questions[0]

	// Before anybody has played it, a typo is a typo.
	fixed := models.TranslationDraft{
		Prompt:  "First question?",
		Choices: []string{"w", "x", "y", "z"},
		Source:  "player",
	}
	if err := r.UpdateSetQuestion(ctx, setID, owner.ID, q.ID, "en", fixed, 2); err != nil {
		t.Fatalf("edit: %v", err)
	}
	after, err := r.SetQuestions(ctx, setID, owner.ID, "en")
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if after[0].Prompt != "First question?" || after[0].CorrectIndex != 2 {
		t.Fatalf("the edit did not take: %+v", after[0])
	}

	// Somebody answers it.
	player := newUser(t, "edit_player", "edit.player@example.com")
	session := &models.GameSession{
		UserID: player.ID, Mode: models.ModeSolo, Locale: "en",
		QuestionIDs: []int{q.ID}, TotalQuestions: 1,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("game: %v", err)
	}
	if err := r.RecordAnswer(ctx, session.ID, &models.GameAnswer{
		QuestionID: q.ID, Position: 0, SelectedIndex: 2, IsCorrect: true,
		TimeMS: 900, PointsAwarded: 10,
	}, 1); err != nil {
		t.Fatalf("answer: %v", err)
	}

	if played, err := r.SetQuestionPlayed(ctx, q.ID); err != nil || !played {
		t.Fatalf("played = %v, %v; want true", played, err)
	}
	rewrite := models.TranslationDraft{
		Prompt:  "Something else entirely?",
		Choices: []string{"a", "b", "c", "d"},
		Source:  "player",
	}
	if err := r.UpdateSetQuestion(ctx, setID, owner.ID, q.ID, "en", rewrite, 0); err == nil {
		t.Error("a question somebody had answered was rewritten under them")
	}

	// And it really is unchanged.
	final, err := r.SetQuestions(ctx, setID, owner.ID, "en")
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if final[0].Prompt != "First question?" {
		t.Errorf("the played question now reads %q", final[0].Prompt)
	}

	// Somebody else's question is not theirs to edit either.
	stranger := newUser(t, "edit_stranger", "edit.stranger@example.com")
	if err := r.UpdateSetQuestion(ctx, setID, stranger.ID, q.ID, "en", rewrite, 0); err == nil {
		t.Error("a stranger edited somebody else's question")
	}
}

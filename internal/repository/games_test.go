package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// A question the player answered can lack the language they are now reading in,
// and lack Arabic as well. The review page then asked PostgreSQL for a prompt
// that did not exist, got NULL, and failed to scan it into a string — a 500 on
// a page that was only ever going to show one round back to its own player.
func TestGameAnswersSurvivesAQuestionMissingBothFallbacks(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	// English only: no Arabic, and the reader below is on French.
	id, err := r.UpsertQuestion(ctx, &models.QuestionDraft{
		ID: 920001, CategoryID: 1, Difficulty: 1, Points: 10, CorrectIndex: 0, IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "English only, on purpose", Choices: []string{"a", "b", "c", "d"}, Source: "human"},
		},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	player := newUser(t, "review_probe", "review.probe@example.com")
	session := &models.GameSession{
		UserID: player.ID, Mode: models.ModeSolo, Locale: "fr",
		QuestionIDs: []int{id}, TotalQuestions: 1,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := r.RecordAnswer(ctx, session.ID, &models.GameAnswer{
		QuestionID: id, Position: 0, SelectedIndex: 0, IsCorrect: true,
		TimeMS: 1000, PointsAwarded: 10,
	}, 1); err != nil {
		t.Fatalf("record answer: %v", err)
	}

	answers, err := r.GameAnswers(ctx, session.ID, "fr")
	if err != nil {
		t.Fatalf("game answers in a language the question lacks: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("got %d answers, want 1", len(answers))
	}
	// It falls back to the language the question does have, rather than showing
	// an empty row that says nothing about what was asked.
	if answers[0].Prompt != "English only, on purpose" {
		t.Errorf("prompt = %q, want the English one it does have", answers[0].Prompt)
	}
	if len(answers[0].Choices) != 4 {
		t.Errorf("choices = %v, want the four it does have", answers[0].Choices)
	}
	_ = uuid.Nil
}

// Closing a round and paying for it have to be one write. Apart, a failure
// between them left the round finished and the experience unpaid, and the
// retry — which is idempotent by design — reported success without noticing.
func TestFinishGameCreditsThePlayerExactlyOnce(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	player := newUser(t, "finish_probe", "finish.probe@example.com")
	q := addQuestion(t, 920010, "How many gates does Paradise have?")

	session := &models.GameSession{
		UserID: player.ID, Mode: models.ModeSolo, Locale: "en",
		QuestionIDs: []int{q}, TotalQuestions: 1,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("create game: %v", err)
	}

	if _, err := r.FinishGame(ctx, session.ID, 40, "en", player.ID, 3, 5); err != nil {
		t.Fatalf("finish: %v", err)
	}
	after, err := r.UserByID(ctx, player.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if after.XP != 40 || after.Coins != 3 || after.GamesPlayed != 1 || after.BestStreak != 5 {
		t.Fatalf("after one finish: xp=%d coins=%d played=%d streak=%d, want 40/3/1/5",
			after.XP, after.Coins, after.GamesPlayed, after.BestStreak)
	}

	// Finishing again is a no-op: the round is already closed, so there is
	// nothing to pay for a second time.
	if _, err := r.FinishGame(ctx, session.ID, 40, "en", player.ID, 3, 5); err != nil {
		t.Fatalf("second finish: %v", err)
	}
	again, err := r.UserByID(ctx, player.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if again.XP != 40 || again.GamesPlayed != 1 {
		t.Errorf("after finishing twice: xp=%d played=%d, want it unchanged at 40/1",
			again.XP, again.GamesPlayed)
	}
}

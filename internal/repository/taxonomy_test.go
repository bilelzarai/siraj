package repository_test

import (
	"context"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// The filter set is five conditions since the taxonomy gained a level, and the
// fifth is the one with no screen behind it yet: a domain can be retired before
// anything in the interface can create one, and the draw has to honour that
// from the migration onward rather than from the screens onward.
//
// The count and the draw are asserted together on purpose. They are separate
// queries over the same rule, and a count that disagrees with the draw is worse
// than no count: it offers a player a round the bank cannot fill.
func TestRetiringADomainWithdrawsItsQuestionsFromBothTheDrawAndTheCount(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	// A second domain, with a category and a question of its own, so the
	// Islamic bank is never what is being measured.
	var domainID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO domains (slug, icon, color, sort_order) VALUES ('sport','⚽','#1d4ed8',2)
		RETURNING id`).Scan(&domainID); err != nil {
		t.Fatalf("creating a domain: %v", err)
	}
	var categoryID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO categories (slug, icon, color, sort_order, domain_id)
		VALUES ('football','🥅','#1d4ed8',90,$1) RETURNING id`, domainID).Scan(&categoryID); err != nil {
		t.Fatalf("creating a category: %v", err)
	}

	questionID, err := r.UpsertQuestion(ctx, &models.QuestionDraft{
		ID: 975900, CategoryID: categoryID, Difficulty: 1, Points: 10,
		CorrectIndex: 0, IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "How long is a football match?", Choices: []string{"90", "60", "45", "120"}, Source: "human"},
		},
	})
	if err != nil {
		t.Fatalf("creating a question: %v", err)
	}

	countsFor := func(t *testing.T) int {
		t.Helper()
		a, err := r.AvailableCounts(ctx, "en")
		if err != nil {
			t.Fatalf("availability: %v", err)
		}
		return a.ByCategory[categoryID][1]
	}
	drawnFor := func(t *testing.T) int {
		t.Helper()
		ids, err := r.PickQuestionIDs(ctx, &categoryID, nil, 1, 10, "en")
		if err != nil {
			t.Fatalf("draw: %v", err)
		}
		return len(ids)
	}
	offered := func(t *testing.T) bool {
		t.Helper()
		cats, err := r.Categories(ctx, "en", 0)
		if err != nil {
			t.Fatalf("categories: %v", err)
		}
		for _, c := range cats {
			if c.ID == categoryID {
				return true
			}
		}
		return false
	}

	if got := countsFor(t); got != 1 {
		t.Fatalf("an active domain counts %d of its questions, want 1", got)
	}
	if got := drawnFor(t); got != 1 {
		t.Fatalf("an active domain draws %d of its questions, want 1", got)
	}
	if !offered(t) {
		t.Fatal("a category in an active domain is not offered")
	}

	if _, err := testPool.Exec(ctx, `UPDATE domains SET is_active = false WHERE id = $1`, domainID); err != nil {
		t.Fatalf("retiring the domain: %v", err)
	}

	if got := countsFor(t); got != 0 {
		t.Errorf("a retired domain still counts %d questions: the setup screen would offer a round that cannot be filled", got)
	}
	if got := drawnFor(t); got != 0 {
		t.Errorf("a retired domain still draws %d questions: a half-built subject area is reaching players", got)
	}
	if offered(t) {
		t.Error("a category under a retired domain is still offered, and its availability can only ever be zero")
	}

	// Retiring is not deleting, one level up as well as one level down: the
	// question is withdrawn from play and still there.
	var alive bool
	if err := testPool.QueryRow(ctx,
		`SELECT is_active FROM questions WHERE id = $1`, questionID).Scan(&alive); err != nil {
		t.Fatalf("reading the question back: %v", err)
	}
	if !alive {
		t.Error("retiring a domain deactivated its questions; withdrawal must keep history")
	}
}

// The daily round is one shared set for everyone, drawn from the date. It reads
// the bank through the same filter set, so a retired domain must not surface
// there either — the one draw nobody chooses the category for.
func TestTheDailyDrawHonoursTheDomainToo(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	var domainID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO domains (slug, icon, color, sort_order, is_active)
		VALUES ('history-daily','🏺','#92400e',3,true) RETURNING id`).Scan(&domainID); err != nil {
		t.Fatalf("creating a domain: %v", err)
	}
	var categoryID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO categories (slug, icon, color, sort_order, domain_id)
		VALUES ('ancient','🗿','#92400e',91,$1) RETURNING id`, domainID).Scan(&categoryID); err != nil {
		t.Fatalf("creating a category: %v", err)
	}
	id, err := r.UpsertQuestion(ctx, &models.QuestionDraft{
		ID: 975901, CategoryID: categoryID, Difficulty: 1, Points: 10,
		CorrectIndex: 0, IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "Which city held the Library of Alexandria?", Choices: []string{"Alexandria", "Cairo", "Athens", "Rome"}, Source: "human"},
		},
	})
	if err != nil {
		t.Fatalf("creating a question: %v", err)
	}

	drawn := func(t *testing.T) bool {
		t.Helper()
		ids, err := r.PickDailyQuestionIDs(ctx, "2026-10-06", 500, "en")
		if err != nil {
			t.Fatalf("daily draw: %v", err)
		}
		for _, got := range ids {
			if got == id {
				return true
			}
		}
		return false
	}

	if !drawn(t) {
		t.Fatal("an active domain's question never appears in the daily draw")
	}
	if _, err := testPool.Exec(ctx, `UPDATE domains SET is_active = false WHERE id = $1`, domainID); err != nil {
		t.Fatalf("retiring the domain: %v", err)
	}
	if drawn(t) {
		t.Error("the daily round still draws from a retired domain")
	}
}

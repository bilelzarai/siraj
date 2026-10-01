package repository_test

import (
	"context"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// addQuestion writes one English question and returns its id.
func addQuestion(t *testing.T, id int, prompt string) int {
	t.Helper()
	newID, err := repo(t).UpsertQuestion(context.Background(), &models.QuestionDraft{
		ID:           id,
		CategoryID:   1,
		Difficulty:   1,
		Points:       10,
		CorrectIndex: 0,
		IsActive:     true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: prompt, Choices: []string{"a", "b", "c", "d"}, Source: "human"},
		},
	})
	if err != nil {
		t.Fatalf("upsert question %d: %v", id, err)
	}
	return newID
}

// The caller says how alike counts. Until it did, the query fetched everything
// pg_trgm's default threshold of 0.3 let through — 566 rows to return 14 on the
// real bank — and the caller discarded the rest in Go.
func TestSimilarPromptsHonoursTheThreshold(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	// Wording nothing else in the suite uses: the database is shared by every
	// test in this package, and a prompt another test also writes would make
	// this one fail depending on the order they ran in.
	const probe = "Which Zirconium expedition reached the Qaf mountains first?"
	near := addQuestion(t, 900001, "Which Zirconium expedition reached the Qaf mountains first?")
	addQuestion(t, 900002, "Which Zirconium expedition reached the Sinai mountains first?")
	addQuestion(t, 900003, "How many parasangs lie between Kufa and Basra?")

	got, err := r.SimilarPrompts(ctx, probe, "en", 0, 10, 0.95)
	if err != nil {
		t.Fatalf("similar: %v", err)
	}
	if len(got) != 1 || got[0].RightID != near {
		ids := make([]int, len(got))
		for i, d := range got {
			ids[i] = d.RightID
		}
		t.Fatalf("at 0.95 got %v, want only the identical prompt %d", ids, near)
	}

	// Lowering it lets the neighbouring battle back in, which is the whole
	// reason the number is a parameter and not a constant in the query.
	loose, err := r.SimilarPrompts(ctx, probe, "en", 0, 10, 0.5)
	if err != nil {
		t.Fatalf("similar loose: %v", err)
	}
	if len(loose) < 2 {
		t.Errorf("at 0.5 got %d rows, want the Uhud prompt as well", len(loose))
	}
}

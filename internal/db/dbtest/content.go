package dbtest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"siraj/internal/db"
)

// seeds numbers the content each test writes, so two calls in one test do not fight
// over the same slug.
var seeds atomic.Int64

// Content is what a test needs in order to have something to play: one domain, one
// category and a few published questions.
//
// No migration seeds a catalog or a question bank any more — real content is entered
// in the admin console — so tests write the little they need here instead.
type Content struct {
	DomainID    int64
	DomainSlug  string
	CategoryID  int64
	QuestionIDs []int64
}

// Seed writes one islamic domain with one category and n published questions in the
// three languages, and returns their ids. The right answer of question i is i%4, so a
// test that always picks the same choice still sees both right and wrong answers.
func Seed(t *testing.T, d *db.DB, n int) Content {
	t.Helper()
	ctx := context.Background()
	seq := seeds.Add(1)
	out := Content{DomainSlug: fmt.Sprintf("test-domain-%d", seq)}

	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO domains (slug, family, icon, color, motif, position, is_featured)
		VALUES ($1, 'islamic', 'mosque', '#0b7a5c', 'mosque', $2, true)
		RETURNING id`, out.DomainSlug, seq).Scan(&out.DomainID); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO categories (domain_id, slug, icon, color, position)
		VALUES ($1, $2, 'book', '#0d9488', 1)
		RETURNING id`, out.DomainID, fmt.Sprintf("test-category-%d", seq)).Scan(&out.CategoryID); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	for _, lang := range []string{"ar", "en", "fr"} {
		if _, err := d.Pool.Exec(ctx, `
			INSERT INTO domain_translations (domain_id, lang, name, description)
			VALUES ($1, $2, $3, $3)`, out.DomainID, lang, "Test domain ("+lang+")"); err != nil {
			t.Fatalf("seed domain text: %v", err)
		}
		if _, err := d.Pool.Exec(ctx, `
			INSERT INTO category_translations (category_id, lang, name)
			VALUES ($1, $2, $3)`, out.CategoryID, lang, "Test category ("+lang+")"); err != nil {
			t.Fatalf("seed category text: %v", err)
		}
	}

	for i := 0; i < n; i++ {
		var id int64
		if err := d.Pool.QueryRow(ctx, `
			INSERT INTO questions (category_id, difficulty, points, correct_index, status, main_lang, published_at)
			VALUES ($1, 1, 10, $2, 'published', 'en', now())
			RETURNING id`, out.CategoryID, i%4).Scan(&id); err != nil {
			t.Fatalf("seed question %d: %v", i, err)
		}
		for _, lang := range []string{"ar", "en", "fr"} {
			answers := []string{
				fmt.Sprintf("%s-A%d", lang, i), fmt.Sprintf("%s-B%d", lang, i),
				fmt.Sprintf("%s-C%d", lang, i), fmt.Sprintf("%s-D%d", lang, i),
			}
			if _, err := d.Pool.Exec(ctx, `
				INSERT INTO question_translations (question_id, lang, body, answers, explanation)
				VALUES ($1, $2, $3, $4, $5)`,
				id, lang,
				fmt.Sprintf("Test question %d (%s)?", i, lang),
				answers,
				fmt.Sprintf("Because %s-%c%d is the one.", lang, 'A'+rune(i%4), i),
			); err != nil {
				t.Fatalf("seed question text %d: %v", i, err)
			}
		}
		out.QuestionIDs = append(out.QuestionIDs, id)
	}
	return out
}

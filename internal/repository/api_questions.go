package repository

import (
	"context"

	"github.com/bilelzarai/siraj/internal/models"
)

// What the public endpoint serves. It is a projection of the same rows the
// game draws from, under the same conditions — the filter set is copied here
// from content.go rather than re-derived, because a second way of deciding
// what is drawable is a second place to forget that unreviewed text is held.

// APIQuestionFilter is what a caller may narrow by. Zero on every field means
// "everything this locale has".
type APIQuestionFilter struct {
	Locale     string
	Category   string // slug, empty for every category
	Domain     string // slug, empty for every subject area
	Difficulty int    // 0 for every difficulty
	// AfterID is keyset pagination: the last id the caller already has. An
	// offset would skip or repeat rows as the bank is edited between pages,
	// which for a consumer mirroring the bank means silent gaps.
	AfterID int
	Limit   int
}

// APIQuestions returns one page and the total the filter matches.
//
// The total is counted under the same conditions as the page, so a consumer
// can tell when it has everything without the two disagreeing.
func (r *Repo) APIQuestions(ctx context.Context, f APIQuestionFilter) ([]*models.APIQuestion, int, error) {
	const conditions = `
		  FROM questions q
		  JOIN categories c ON c.id = q.category_id AND c.is_active
		  JOIN domains d ON d.id = c.domain_id AND d.is_active
		  JOIN question_translations t ON t.question_id = q.id AND t.locale = $1
		                                 AND NOT t.needs_review
		  LEFT JOIN category_translations ct  ON ct.category_id  = c.id AND ct.locale  = $1 AND NOT ct.needs_review
		  LEFT JOIN category_translations cfb ON cfb.category_id = c.id AND cfb.locale = 'ar' AND NOT cfb.needs_review
		  LEFT JOIN domain_translations dt    ON dt.domain_id    = d.id AND dt.locale  = $1 AND NOT dt.needs_review
		  LEFT JOIN domain_translations dfb   ON dfb.domain_id   = d.id AND dfb.locale = 'ar' AND NOT dfb.needs_review
		 WHERE q.is_active
		   AND q.author_id IS NULL
		   AND ($2 = '' OR c.slug = $2)
		   AND ($3 = '' OR d.slug = $3)
		   AND ($4::int = 0 OR q.difficulty = $4)`

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*)`+conditions, f.Locale, f.Category, f.Domain, f.Difficulty).
		Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT q.id, q.difficulty, q.points, q.correct_index,
		       t.prompt, t.choices, t.explanation, t.locale,
		       c.slug, COALESCE(ct.name, cfb.name, c.slug), c.icon,
		       d.slug, COALESCE(dt.name, dfb.name, d.slug), d.icon`+conditions+`
		   AND q.id > $5
		 ORDER BY q.id
		 LIMIT $6`,
		f.Locale, f.Category, f.Domain, f.Difficulty, f.AfterID, f.Limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.APIQuestion
	for rows.Next() {
		var q models.APIQuestion
		if err := rows.Scan(&q.ID, &q.Difficulty, &q.Points, &q.CorrectIndex,
			&q.Prompt, &q.Choices, &q.Explanation, &q.Locale,
			&q.Category.Slug, &q.Category.Name, &q.Category.Icon,
			&q.Domain.Slug, &q.Domain.Name, &q.Domain.Icon); err != nil {
			return nil, 0, err
		}
		out = append(out, &q)
	}
	return out, total, rows.Err()
}

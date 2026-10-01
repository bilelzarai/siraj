package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// Categories returns every active category with names resolved for locale,
// falling back to the Arabic name when a translation is missing.
func (r *Repo) Categories(ctx context.Context, locale string) ([]*models.Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.slug, c.icon, c.color, c.sort_order,
		       COALESCE(t.name, fb.name, c.slug),
		       COALESCE(t.description, fb.description, '')
		  FROM categories c
		  LEFT JOIN category_translations t  ON t.category_id  = c.id AND t.locale  = $1 AND NOT t.needs_review
		  LEFT JOIN category_translations fb ON fb.category_id = c.id AND fb.locale = 'ar' AND NOT fb.needs_review
		 WHERE c.is_active
		 ORDER BY c.sort_order`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Slug, &c.Icon, &c.Color, &c.SortOrder,
			&c.Name, &c.Description); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// Availability is how many questions can actually be drawn, per category and
// difficulty, for one locale. Index 0 on either axis is the "any" total.
//
// It exists because the setup screen was offering combinations the bank cannot
// satisfy: eight categories times four difficulties, most of which held fewer
// than the minimum round length, so choosing a difficulty inside a category
// usually dead-ended on "not enough questions".
type Availability struct {
	// ByCategory[categoryID][difficulty] — difficulty 0 is every difficulty.
	ByCategory map[int]map[int]int
	// Any[difficulty] — across all categories. Index 0 is the grand total.
	Any map[int]int
}

// Count answers "how many questions could this round draw from", with 0
// meaning "any" on either axis.
func (a Availability) Count(categoryID, difficulty int) int {
	if a.ByCategory == nil {
		return 0
	}
	if categoryID <= 0 {
		return a.Any[difficulty]
	}
	return a.ByCategory[categoryID][difficulty]
}

// AvailableCounts totals the drawable questions per category and difficulty.
//
// The filters mirror PickQuestionIDs exactly — active question, translation
// present in this locale, translation not awaiting review — because a count
// that does not match what the draw will find is worse than no count at all.
func (r *Repo) AvailableCounts(ctx context.Context, locale string) (Availability, error) {
	out := Availability{ByCategory: map[int]map[int]int{}, Any: map[int]int{}}

	rows, err := r.pool.Query(ctx, `
		SELECT q.category_id, q.difficulty, count(*)
		  FROM questions q
		  JOIN question_translations t ON t.question_id = q.id AND t.locale = $1
		                                 AND NOT t.needs_review
		 WHERE q.is_active
		 GROUP BY q.category_id, q.difficulty`, locale)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var categoryID, difficulty, n int
		if err := rows.Scan(&categoryID, &difficulty, &n); err != nil {
			return out, err
		}
		if out.ByCategory[categoryID] == nil {
			out.ByCategory[categoryID] = map[int]int{}
		}
		out.ByCategory[categoryID][difficulty] += n
		out.ByCategory[categoryID][0] += n
		out.Any[difficulty] += n
		out.Any[0] += n
	}
	return out, rows.Err()
}

// PickQuestionIDs draws a random set of question ids for a new round.
// Selection happens in SQL so the whole bank is eligible without loading it.
func (r *Repo) PickQuestionIDs(ctx context.Context, categoryID *int, difficulty, count int, locale string) ([]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT q.id
		  FROM questions q
		  JOIN question_translations t ON t.question_id = q.id AND t.locale = $4
		                                 AND NOT t.needs_review
		 WHERE q.is_active
		   AND ($1::int IS NULL OR q.category_id = $1)
		   AND ($2::int = 0   OR q.difficulty  = $2)
		 ORDER BY random()
		 LIMIT $3`, categoryID, difficulty, count, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PickDailyQuestionIDs draws the set everyone plays today.
//
// The order comes from hashing the day with each question id, so it is the same
// for every player, stable for the whole day, and different tomorrow — without
// storing a schedule anywhere. setseed would be per-connection state on a
// pooled connection, which is not something to rely on.
func (r *Repo) PickDailyQuestionIDs(ctx context.Context, day string, count int, locale string) ([]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT q.id
		  FROM questions q
		  JOIN question_translations t ON t.question_id = q.id AND t.locale = $3
		                                 AND NOT t.needs_review
		 WHERE q.is_active
		 ORDER BY md5($1 || ':' || q.id::text)
		 LIMIT $2`, day, count, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PlayedDaily reports whether this player already started today's round. Both a
// finished and an abandoned one count: the point of a daily is one attempt.
func (r *Repo) PlayedDaily(ctx context.Context, userID uuid.UUID, day string) (bool, error) {
	var played bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM game_sessions
			 WHERE user_id = $1 AND mode = 'daily'
			   AND started_at >= $2::date AND started_at < $2::date + interval '1 day')`,
		userID, day).Scan(&played)
	return played, err
}

// Question loads one question fully hydrated, preferring locale and falling
// back to second before Arabic.
//
// Two preferences rather than one because a player can switch language midway
// through a round. The round was assembled in the language it started in, so
// every question is guaranteed to exist there; the newly chosen language is
// not. Dropping straight to Arabic when a French translation is missing would
// answer "show me this in French" with a script the player may not read at
// all, when the English they were already reading is right there. Pass the
// same value twice when there is nothing to fall back to.
func (r *Repo) Question(ctx context.Context, id int, locale, second string) (*models.Question, error) {
	var q models.Question
	err := r.pool.QueryRow(ctx, `
		SELECT q.id, q.category_id, c.slug, c.icon, q.difficulty, q.points,
		       q.correct_index, q.source,
		       COALESCE(t.prompt, s.prompt, fb.prompt),
		       COALESCE(t.choices, s.choices, fb.choices),
		       COALESCE(t.explanation, s.explanation, fb.explanation, ''),
		       COALESCE(ct.name, cs.name, cfb.name, c.slug),
		       -- Which language won, so the caller can tag the markup with it.
		       CASE WHEN t.prompt IS NOT NULL THEN $2
		            WHEN s.prompt IS NOT NULL THEN $3
		            ELSE 'ar' END
		  FROM questions q
		  JOIN categories c ON c.id = q.category_id
		  LEFT JOIN question_translations t   ON t.question_id   = q.id AND t.locale   = $2 AND NOT t.needs_review
		  LEFT JOIN question_translations s   ON s.question_id   = q.id AND s.locale   = $3 AND NOT s.needs_review
		  LEFT JOIN question_translations fb  ON fb.question_id  = q.id AND fb.locale  = 'ar' AND NOT fb.needs_review
		  LEFT JOIN category_translations ct  ON ct.category_id  = c.id AND ct.locale  = $2
		  LEFT JOIN category_translations cs  ON cs.category_id  = c.id AND cs.locale  = $3
		  LEFT JOIN category_translations cfb ON cfb.category_id = c.id AND cfb.locale = 'ar'
		 WHERE q.id = $1`, id, locale, second,
	).Scan(&q.ID, &q.CategoryID, &q.CategorySlug, &q.CategoryIcon, &q.Difficulty,
		&q.Points, &q.CorrectIndex, &q.Source, &q.Prompt, &q.Choices,
		&q.Explanation, &q.CategoryName, &q.Locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &q, err
}

// TotalQuestions powers the landing-page counter.
func (r *Repo) TotalQuestions(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM questions WHERE is_active AND author_id IS NULL`).Scan(&n)
	return n, err
}

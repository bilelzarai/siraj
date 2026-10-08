package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// ---------------------------------------------------------- comments --

// UpsertQuestionComment saves one person's remark about a question. A second
// submission replaces the first rather than adding a row, so the thread cannot
// be used to flood a question.
func (r *Repo) UpsertQuestionComment(ctx context.Context, questionID int64,
	userID uuid.UUID, locale, body string) error {

	_, err := r.pool.Exec(ctx, `
		INSERT INTO question_comments (question_id, user_id, locale, body)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (question_id, user_id) WHERE user_id IS NOT NULL
		DO UPDATE SET body = EXCLUDED.body, locale = EXCLUDED.locale,
		              created_at = now(), hidden = false, hidden_by = NULL`,
		questionID, userID, locale, body)
	return err
}

// QuestionComments lists the visible remarks on a question, newest first.
// Hidden rows never leave the database: a moderated comment stays for the
// audit trail but is not shown to anyone.
func (r *Repo) QuestionComments(ctx context.Context, questionID int64, limit int) ([]*models.QuestionComment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT qc.id, qc.question_id, qc.locale, qc.body, qc.created_at,
		       COALESCE(u.display_name, ''), COALESCE(u.username, ''),
		       COALESCE(u.avatar_seed, '')
		  FROM question_comments qc
		  LEFT JOIN users u ON u.id = qc.user_id
		 WHERE qc.question_id = $1 AND NOT qc.hidden
		 ORDER BY qc.created_at DESC
		 LIMIT $2`, questionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.QuestionComment
	for rows.Next() {
		var c models.QuestionComment
		if err := rows.Scan(&c.ID, &c.QuestionID, &c.Locale, &c.Body, &c.CreatedAt,
			&c.AuthorName, &c.AuthorUsername, &c.AuthorSeed); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// MyQuestionComment returns what this person already wrote, so the form opens
// with their own words rather than blank.
func (r *Repo) MyQuestionComment(ctx context.Context, questionID int64, userID uuid.UUID) (string, error) {
	var body string
	err := r.pool.QueryRow(ctx, `
		SELECT body FROM question_comments
		 WHERE question_id = $1 AND user_id = $2`, questionID, userID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return body, err
}

// CommentsForQuestions batches the visible comment counts for a review screen.
func (r *Repo) CommentCounts(ctx context.Context, questionIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(questionIDs))
	if len(questionIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT question_id, count(*) FROM question_comments
		 WHERE question_id = ANY($1) AND NOT hidden
		 GROUP BY question_id`, questionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// CommentFilter is the moderation queue's filter.
//
// State is the queue's own split: "open" is everything still waiting, which is
// what the screen opens on, "resolved" what has been dealt with, "hidden" what
// was taken down, and "" everything.
type CommentFilter struct {
	Query  string
	State  string
	Locale string
	Limit  int
	Offset int
}

// commentWhere builds the clause the count and the page share, so the pager
// can never be counting a different set than the one on screen.
// $1 is always the locale the category name is resolved in, so the clause's
// own placeholders start at $2. Numbering is by position in the argument
// slice, not by position in the statement, so the locale has to be seeded here
// rather than prepended by the caller — prepending it shifted every filter
// placeholder by one and the LIMIT ended up reading the search text.
func commentWhere(f CommentFilter) (string, []any) {
	where := []string{"$1::text IS NOT NULL"}
	args := []any{commentLocale(f)}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	switch f.State {
	case "open":
		where = append(where, "qc.resolved_at IS NULL AND NOT qc.hidden")
	case "resolved":
		where = append(where, "qc.resolved_at IS NOT NULL")
	case "hidden":
		where = append(where, "qc.hidden")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		add("qc.body ILIKE '%%' || $%d || '%%'", q)
	}
	if f.Locale != "" {
		add("qc.locale = $%d", f.Locale)
	}
	return strings.Join(where, " AND "), args
}

// CommentCountsByState is the number beside each of the queue's tabs. One
// query rather than four: the tabs are read together.
func (r *Repo) CommentCountsByState(ctx context.Context) (models.CommentCounts, error) {
	var c models.CommentCounts
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE resolved_at IS NULL AND NOT hidden)::int,
		       count(*) FILTER (WHERE resolved_at IS NOT NULL)::int,
		       count(*) FILTER (WHERE hidden)::int
		  FROM question_comments`).Scan(&c.Total, &c.Open, &c.Resolved, &c.Hidden)
	return c, err
}

// CountQuestionComments is how many remarks the filter matches.
func (r *Repo) CountQuestionComments(ctx context.Context, f CommentFilter) (int, error) {
	clause, args := commentWhere(f)
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM question_comments qc WHERE `+clause, args...).Scan(&n)
	return n, err
}

// QuestionCommentsPage is one page of the queue.
//
// The offset is used. It was accepted and dropped, so every page of the
// moderation queue was page one — the pager moved, the rows did not, and a
// queue longer than one page could not be worked through at all.
func (r *Repo) QuestionCommentsPage(ctx context.Context, f CommentFilter) ([]*models.QuestionComment, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	clause, args := commentWhere(f)
	args = append(args, f.Limit, f.Offset)

	rows, err := r.pool.Query(ctx, `
		SELECT qc.id, qc.question_id, qc.locale, qc.body, qc.created_at,
		       COALESCE(u.display_name, ''), COALESCE(u.username, ''),
		       COALESCE(u.avatar_seed, ''), qc.hidden,
		       qc.resolved_at, COALESCE(ru.display_name, ru.username, ''),
		       COALESCE(ct.name, c.slug, ''), COALESCE(c.icon, '')
		  FROM question_comments qc
		  LEFT JOIN users u  ON u.id = qc.user_id
		  LEFT JOIN users ru ON ru.id = qc.resolved_by
		  -- The question the remark is about, so a moderator reading it knows
		  -- which part of the bank it concerns without opening the question.
		  LEFT JOIN questions q  ON q.id = qc.question_id
		  LEFT JOIN categories c ON c.id = q.category_id
		  LEFT JOIN category_translations ct
		         ON ct.category_id = c.id AND ct.locale = $1
		 WHERE `+clause+`
		 ORDER BY qc.created_at DESC
		 LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.QuestionComment
	for rows.Next() {
		var c models.QuestionComment
		if err := rows.Scan(&c.ID, &c.QuestionID, &c.Locale, &c.Body, &c.CreatedAt,
			&c.AuthorName, &c.AuthorUsername, &c.AuthorSeed, &c.Hidden,
			&c.ResolvedAt, &c.ResolvedBy, &c.CategoryName, &c.CategoryIcon); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// SetQuestionCommentResolved marks a remark dealt with, or puts it back.
//
// The third state, beside visible and hidden. A remark that reported a real
// fault is not something to hide once it has been fixed — it was useful, and
// hiding it pretends it never arrived — so this is how the queue stops asking
// about it while it stays on the question.
func (r *Repo) SetQuestionCommentResolved(ctx context.Context, id int64, resolved bool, by uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE question_comments
		   SET resolved_at = CASE WHEN $2 THEN now() ELSE NULL END,
		       resolved_by = CASE WHEN $2 THEN $3::uuid ELSE NULL END
		 WHERE id = $1`, id, resolved, by)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetQuestionCommentHidden is the moderator action.
func (r *Repo) SetQuestionCommentHidden(ctx context.Context, id int64, hidden bool, by uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE question_comments
		   SET hidden = $2, hidden_by = CASE WHEN $2 THEN $3::uuid ELSE NULL END
		 WHERE id = $1`, id, hidden, by)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AnsweredQuestionAt resolves a position in a round to the question behind it,
// but only once that position has been answered. The play screen offers the
// comment box after the reveal, and this is what enforces that server-side.
func (r *Repo) AnsweredQuestionAt(ctx context.Context, sessionID uuid.UUID,
	position int, userID uuid.UUID) (int64, error) {

	var id int64
	err := r.pool.QueryRow(ctx, `
		SELECT ga.question_id
		  FROM game_answers ga
		  JOIN game_sessions gs ON gs.id = ga.session_id
		 WHERE ga.session_id = $1 AND ga.position = $2 AND gs.user_id = $3`,
		sessionID, position, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// HasAnsweredQuestion reports whether this person has ever been served this
// question. The comment thread is gated on it so it cannot be used to read
// questions before playing them.
func (r *Repo) HasAnsweredQuestion(ctx context.Context, userID uuid.UUID, questionID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM game_answers ga
		    JOIN game_sessions gs ON gs.id = ga.session_id
		   WHERE gs.user_id = $1 AND ga.question_id = $2)`,
		userID, questionID).Scan(&exists)
	return exists, err
}

// ---------------------------------------------------------------- ratings --

// RateQuestion records one player's stars for a question, replacing whatever
// they said before. One row per person per question, so a question cannot be
// voted down twice by the same player.
func (r *Repo) RateQuestion(ctx context.Context, questionID int64,
	userID uuid.UUID, stars int) error {

	_, err := r.pool.Exec(ctx, `
		INSERT INTO question_ratings (question_id, user_id, stars)
		VALUES ($1, $2, $3)
		ON CONFLICT (question_id, user_id)
		DO UPDATE SET stars = EXCLUDED.stars, updated_at = now()`,
		questionID, userID, stars)
	return err
}

// QuestionRating is what one player said and what everyone said, which the
// round needs together: the stars to light up, and the average to show beside
// them.
func (r *Repo) QuestionRating(ctx context.Context, questionID int64,
	userID uuid.UUID) (mine, count int, average float64, err error) {

	err = r.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(stars) FILTER (WHERE user_id = $2), 0),
		       count(*), COALESCE(avg(stars), 0)
		  FROM question_ratings WHERE question_id = $1`,
		questionID, userID).Scan(&mine, &count, &average)
	return mine, count, average, err
}

// PoorlyRatedQuestions lists the questions players have marked down: an
// average at or below the threshold, over at least minVotes ratings.
//
// minVotes exists because one person having a bad day is not evidence. A
// single one-star rating would otherwise put a good question at the top of the
// admin's alert list and keep it there.
func (r *Repo) PoorlyRatedQuestions(ctx context.Context, locale string,
	threshold float64, minVotes, limit, offset int) ([]*models.RatedQuestion, error) {

	rows, err := r.pool.Query(ctx, `
		SELECT q.id, COALESCE(t.prompt, any_t.prompt, ''),
		       COALESCE(ct.name, c.slug), c.icon,
		       count(qr.*)::int, avg(qr.stars)::float8
		  FROM questions q
		  JOIN categories c ON c.id = q.category_id
		  JOIN question_ratings qr ON qr.question_id = q.id
		  LEFT JOIN category_translations ct ON ct.category_id = c.id AND ct.locale = $1
		  LEFT JOIN question_translations t  ON t.question_id  = q.id AND t.locale  = $1
		                                        AND NOT t.needs_review
		  -- Same gate as every player-facing query: an admin judging a question
		  -- players marked down should not be reading machine text that has not
		  -- been reviewed, with nothing on screen saying so.
		  LEFT JOIN LATERAL (
		        SELECT prompt FROM question_translations
		         WHERE question_id = q.id AND NOT needs_review
		         ORDER BY locale LIMIT 1
		  ) any_t ON true
		 GROUP BY q.id, q.ratings_reviewed_at, t.prompt, any_t.prompt, ct.name, c.slug, c.icon
		HAVING count(qr.*) >= $3 AND avg(qr.stars) <= $2
		   -- Dismissed, and nobody has rated it since.
		   AND max(qr.updated_at) > COALESCE(q.ratings_reviewed_at, '-infinity'::timestamptz)
		 ORDER BY avg(qr.stars) ASC, count(qr.*) DESC
		 LIMIT $4 OFFSET $5`, locale, threshold, minVotes, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.RatedQuestion
	for rows.Next() {
		var q models.RatedQuestion
		if err := rows.Scan(&q.ID, &q.Prompt, &q.CategoryName, &q.CategoryIcon,
			&q.Votes, &q.Average); err != nil {
			return nil, err
		}
		out = append(out, &q)
	}
	return out, rows.Err()
}

// DismissRatings records that a moderator has read this question's ratings and
// judged it sound. It comes back only if somebody rates it again.
func (r *Repo) DismissRatings(ctx context.Context, questionID int) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE questions SET ratings_reviewed_at = now(), updated_at = now() WHERE id = $1`,
		questionID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountPoorlyRated is the badge on the admin nav: how many questions are
// currently flagged, without pulling every row to count them.
func (r *Repo) CountPoorlyRated(ctx context.Context, threshold float64, minVotes int) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT qr.question_id FROM question_ratings qr
			  JOIN questions q ON q.id = qr.question_id
			 GROUP BY qr.question_id, q.ratings_reviewed_at
			HAVING count(*) >= $2 AND avg(qr.stars) <= $1
			   AND max(qr.updated_at) > COALESCE(q.ratings_reviewed_at, '-infinity'::timestamptz)
		) flagged`, threshold, minVotes).Scan(&n)
	return n, err
}

// RatingSummary is how the bank is rated overall, for the figures above the
// poorly-rated list: a single bad question says nothing about whether the bank
// is in trouble, and these two numbers are what gives it a scale.
type RatingSummary struct {
	Votes int
	// Positive is how many of those were four or five stars. The screen above
	// this draws a thumbs split, and four-and-up is what "thumbs up" means on
	// a five-point scale.
	Positive int
	// Average across every rating ever given.
	Average float64
}

// Share is the positive proportion, as a percentage.
func (r RatingSummary) Share() int {
	if r.Votes == 0 {
		return 0
	}
	return r.Positive * 100 / r.Votes
}

// RatingOverview counts every rating in a window. A zero window means all of
// them — the screen offers "last 30 days" and "all time" and this answers both
// rather than growing a second query.
func (r *Repo) RatingOverview(ctx context.Context, within time.Duration) (RatingSummary, error) {
	var s RatingSummary
	var avg *float64

	// An interval of zero would exclude everything, so "all time" is expressed
	// as a null cutoff rather than as a zero one.
	var since *time.Time
	if within > 0 {
		t := time.Now().Add(-within)
		since = &t
	}

	err := r.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE stars >= 4)::int,
		       avg(stars)::float8
		  FROM question_ratings
		 WHERE $1::timestamptz IS NULL OR updated_at >= $1`, since,
	).Scan(&s.Votes, &s.Positive, &avg)
	if avg != nil {
		s.Average = *avg
	}
	return s, err
}

// commentLocale is the language the category name is resolved in. The filter's
// own locale when it has one, so a queue narrowed to French reads French
// category names; otherwise the bank's default.
func commentLocale(f CommentFilter) string {
	if f.Locale != "" {
		return f.Locale
	}
	return "ar"
}

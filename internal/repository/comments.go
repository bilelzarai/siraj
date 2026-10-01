package repository

import (
	"context"
	"errors"

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

// RecentQuestionComments feeds the moderation queue.
// CountQuestionComments is how many remarks the moderation queue holds.
func (r *Repo) CountQuestionComments(ctx context.Context, includeHidden bool) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM question_comments WHERE ($1 OR NOT hidden)`,
		includeHidden).Scan(&n)
	return n, err
}

// QuestionCommentsPage is one page of it.
func (r *Repo) QuestionCommentsPage(ctx context.Context, includeHidden bool, limit, offset int) ([]*models.QuestionComment, error) {
	return r.questionComments(ctx, includeHidden, limit, offset)
}

func (r *Repo) RecentQuestionComments(ctx context.Context, includeHidden bool, limit int) ([]*models.QuestionComment, error) {
	return r.questionComments(ctx, includeHidden, limit, 0)
}

func (r *Repo) questionComments(ctx context.Context, includeHidden bool, limit, offset int) ([]*models.QuestionComment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT qc.id, qc.question_id, qc.locale, qc.body, qc.created_at,
		       COALESCE(u.display_name, ''), COALESCE(u.username, ''),
		       COALESCE(u.avatar_seed, ''), qc.hidden
		  FROM question_comments qc
		  LEFT JOIN users u ON u.id = qc.user_id
		 WHERE ($1 OR NOT qc.hidden)
		 ORDER BY qc.created_at DESC
		 LIMIT $2`, includeHidden, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.QuestionComment
	for rows.Next() {
		var c models.QuestionComment
		if err := rows.Scan(&c.ID, &c.QuestionID, &c.Locale, &c.Body, &c.CreatedAt,
			&c.AuthorName, &c.AuthorUsername, &c.AuthorSeed, &c.Hidden); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
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

package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// MaxSetQuestions bounds one collection. Big enough for a season of matches,
// small enough that the screen listing it is still a screen.
const MaxSetQuestions = 100

// MaxSetsPerPlayer keeps "my questions" a shelf rather than a filing cabinet.
const MaxSetsPerPlayer = 20

// CreateQuestionSet opens a named collection for one player.
func (r *Repo) CreateQuestionSet(ctx context.Context, ownerID uuid.UUID, name, locale string) (*models.QuestionSet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrForbidden
	}

	var count int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM question_sets WHERE owner_id = $1`, ownerID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxSetsPerPlayer {
		return nil, ErrConflict
	}

	set := &models.QuestionSet{OwnerID: ownerID, Name: name, Locale: locale}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO question_sets (owner_id, name, locale)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`,
		ownerID, name, locale,
	).Scan(&set.ID, &set.CreatedAt, &set.UpdatedAt)
	return set, err
}

// QuestionSets lists what a player has written, with how much is in each.
func (r *Repo) QuestionSets(ctx context.Context, ownerID uuid.UUID) ([]*models.QuestionSet, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.owner_id, s.name, s.locale, s.created_at, s.updated_at,
		       (SELECT count(*) FROM question_set_items i WHERE i.set_id = s.id)
		  FROM question_sets s
		 WHERE s.owner_id = $1
		 ORDER BY s.updated_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.QuestionSet
	for rows.Next() {
		var s models.QuestionSet
		if err := rows.Scan(&s.ID, &s.OwnerID, &s.Name, &s.Locale,
			&s.CreatedAt, &s.UpdatedAt, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// QuestionSet loads one, scoped to its owner — a set is private, and the
// invitation to a match is what shares it.
func (r *Repo) QuestionSet(ctx context.Context, id, ownerID uuid.UUID) (*models.QuestionSet, error) {
	var s models.QuestionSet
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.owner_id, s.name, s.locale, s.created_at, s.updated_at,
		       (SELECT count(*) FROM question_set_items i WHERE i.set_id = s.id)
		  FROM question_sets s
		 WHERE s.id = $1 AND s.owner_id = $2`, id, ownerID,
	).Scan(&s.ID, &s.OwnerID, &s.Name, &s.Locale, &s.CreatedAt, &s.UpdatedAt, &s.Count)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// RenameQuestionSet changes what a collection is called.
func (r *Repo) RenameQuestionSet(ctx context.Context, id, ownerID uuid.UUID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrForbidden
	}
	ct, err := r.pool.Exec(ctx,
		`UPDATE question_sets SET name = $3, updated_at = now()
		  WHERE id = $1 AND owner_id = $2`, id, ownerID, name)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteQuestionSet removes a collection and the questions in it.
//
// The questions go with it — they were written for this and nothing else —
// except any that have been answered. A round's review reads the question text
// by joining, not from a copy taken at the time, so deleting a played question
// would blank it out of somebody's history. Those rows stay behind, orphaned
// from the set and reachable only by the rounds that used them.
func (r *Repo) DeleteQuestionSet(ctx context.Context, id, ownerID uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var owned bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM question_sets WHERE id = $1 AND owner_id = $2)`,
			id, ownerID).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return ErrNotFound
		}
		// Only the questions nobody has answered. One that has been played is
		// part of a round somebody can still review.
		if _, err := tx.Exec(ctx, `
			DELETE FROM questions q
			 WHERE q.author_id = $2
			   AND EXISTS (SELECT 1 FROM question_set_items i
			                WHERE i.set_id = $1 AND i.question_id = q.id)
			   AND NOT EXISTS (SELECT 1 FROM game_answers a WHERE a.question_id = q.id)`,
			id, ownerID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM question_sets WHERE id = $1`, id)
		return err
	})
}

// AddToQuestionSet writes new questions into a collection.
func (r *Repo) AddToQuestionSet(ctx context.Context, setID, ownerID uuid.UUID,
	locale string, drafts []models.TranslationDraft, categoryID, difficulty, correct []int) ([]int, error) {

	if len(drafts) == 0 {
		return nil, nil
	}

	var have int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM question_set_items WHERE set_id = $1`, setID).Scan(&have); err != nil {
		return nil, err
	}
	if have+len(drafts) > MaxSetQuestions {
		return nil, ErrConflict
	}

	ids := make([]int, 0, len(drafts))
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var owned bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM question_sets WHERE id = $1 AND owner_id = $2)`,
			setID, ownerID).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return ErrNotFound
		}

		for i, draft := range drafts {
			var id int
			// author_id and is_active=false are what keep it out of every
			// public draw; the constraint on the table makes the pair
			// unsettable by anything that does not know what it is looking at.
			err := tx.QueryRow(ctx, `
				INSERT INTO questions
					(category_id, difficulty, points, correct_index, source,
					 is_active, author_id, created_by)
				VALUES ($1, $2, $3, $4, 'player', false, $5, $5)
				RETURNING id`,
				categoryID[i], difficulty[i], difficulty[i]*10, correct[i], ownerID,
			).Scan(&id)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO question_translations
					(question_id, locale, prompt, choices, explanation, source, needs_review)
				VALUES ($1, $2, $3, $4, $5, 'player', false)`,
				id, locale, draft.Prompt, draft.Choices, draft.Explanation); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO question_set_items (set_id, question_id, position)
				VALUES ($1, $2, $3)`, setID, id, have+i); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		_, err := tx.Exec(ctx,
			`UPDATE question_sets SET updated_at = now() WHERE id = $1`, setID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// UpdateSetQuestion rewrites one question in a collection.
//
// Refused once anybody has answered it. A round's review reads the question by
// joining, not from a copy taken at the time, so editing a played question
// changes what people are shown they were asked — and a score they can no
// longer make sense of is worse than not being able to fix a typo. The screen
// offers to copy it instead, which leaves the played one alone.
func (r *Repo) UpdateSetQuestion(ctx context.Context, setID, ownerID uuid.UUID,
	questionID int, locale string, draft models.TranslationDraft, correct int) error {

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var mine bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM question_set_items i
				  JOIN question_sets s ON s.id = i.set_id
				  JOIN questions q ON q.id = i.question_id
				 WHERE i.set_id = $1 AND s.owner_id = $2 AND i.question_id = $3
				   AND q.author_id = $2)`, setID, ownerID, questionID).Scan(&mine); err != nil {
			return err
		}
		if !mine {
			return ErrNotFound
		}

		var answered bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM game_answers WHERE question_id = $1)`,
			questionID).Scan(&answered); err != nil {
			return err
		}
		if answered {
			return ErrConflict
		}

		if _, err := tx.Exec(ctx,
			`UPDATE questions SET correct_index = $2, updated_at = now() WHERE id = $1`,
			questionID, correct); err != nil {
			return err
		}
		ct, err := tx.Exec(ctx, `
			UPDATE question_translations
			   SET prompt = $3, choices = $4, explanation = $5
			 WHERE question_id = $1 AND locale = $2`,
			questionID, locale, draft.Prompt, draft.Choices, draft.Explanation)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			// Written in another language than the one being edited in.
			_, err = tx.Exec(ctx, `
				INSERT INTO question_translations
					(question_id, locale, prompt, choices, explanation, source, needs_review)
				VALUES ($1, $2, $3, $4, $5, 'player', false)`,
				questionID, locale, draft.Prompt, draft.Choices, draft.Explanation)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx,
			`UPDATE question_sets SET updated_at = now() WHERE id = $1`, setID)
		return err
	})
}

// SetQuestionPlayed reports whether anybody has answered it, which is what
// decides between editing it and copying it.
func (r *Repo) SetQuestionPlayed(ctx context.Context, questionID int) (bool, error) {
	var played bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM game_answers WHERE question_id = $1)`,
		questionID).Scan(&played)
	return played, err
}

// RemoveFromQuestionSet takes one question out of a collection.
func (r *Repo) RemoveFromQuestionSet(ctx context.Context, setID, ownerID uuid.UUID, questionID int) error {
	ct, err := r.pool.Exec(ctx, `
		DELETE FROM question_set_items i
		 USING question_sets s
		 WHERE i.set_id = s.id AND s.id = $1 AND s.owner_id = $2 AND i.question_id = $3`,
		setID, ownerID, questionID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetQuestions is everything in a collection, for the screen that lists it.
func (r *Repo) SetQuestions(ctx context.Context, setID, ownerID uuid.UUID, locale string) ([]*models.Question, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT q.id, q.category_id, q.difficulty, q.points, q.correct_index,
		       COALESCE(t.prompt, ''), COALESCE(t.choices, ARRAY[]::text[]),
		       COALESCE(t.explanation, '')
		  FROM question_set_items i
		  JOIN question_sets s ON s.id = i.set_id AND s.owner_id = $2
		  JOIN questions q ON q.id = i.question_id
		  LEFT JOIN LATERAL (
		        SELECT prompt, choices, explanation FROM question_translations
		         WHERE question_id = q.id
		         ORDER BY (locale = $3) DESC, locale LIMIT 1
		  ) t ON true
		 WHERE i.set_id = $1
		 ORDER BY i.position, q.id`, setID, ownerID, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Question
	for rows.Next() {
		var q models.Question
		if err := rows.Scan(&q.ID, &q.CategoryID, &q.Difficulty, &q.Points,
			&q.CorrectIndex, &q.Prompt, &q.Choices, &q.Explanation); err != nil {
			return nil, err
		}
		out = append(out, &q)
	}
	return out, rows.Err()
}

// PickFromSet draws questions for a match, preferring the ones these opponents
// have not answered before.
//
// A reusable set played weekly against the same friend would otherwise ask them
// the same things every week. Unseen first, then whatever is left — a set of
// eight played twice has to repeat, and repeating beats refusing.
func (r *Repo) PickFromSet(ctx context.Context, setID, ownerID uuid.UUID,
	opponents []uuid.UUID, count int) ([]int, error) {

	rows, err := r.pool.Query(ctx, `
		SELECT i.question_id
		  FROM question_set_items i
		  JOIN question_sets s ON s.id = i.set_id AND s.owner_id = $2
		 WHERE i.set_id = $1
		 ORDER BY EXISTS (
		           SELECT 1 FROM game_answers a
		            JOIN game_sessions g ON g.id = a.session_id
		            WHERE a.question_id = i.question_id
		              AND g.user_id = ANY($3)
		         ) ASC,
		         random()
		 LIMIT $4`, setID, ownerID, opponents, count)
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

// SaveMatchQuestionsToSet keeps the questions somebody wrote for one match, so
// the second match against the same friends does not mean writing them again.
func (r *Repo) SaveMatchQuestionsToSet(ctx context.Context, challengeID, setID, ownerID uuid.UUID) (int, error) {
	var added int
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var owned bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM question_sets WHERE id = $1 AND owner_id = $2)`,
			setID, ownerID).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return ErrNotFound
		}
		ct, err := tx.Exec(ctx, `
			INSERT INTO question_set_items (set_id, question_id)
			SELECT $1, q.id
			  FROM questions q
			 WHERE q.challenge_id = $3 AND q.author_id = $2
			ON CONFLICT DO NOTHING`, setID, ownerID, challengeID)
		if err != nil {
			return err
		}
		added = int(ct.RowsAffected())
		_, err = tx.Exec(ctx,
			`UPDATE question_sets SET updated_at = now() WHERE id = $1`, setID)
		return err
	})
	return added, err
}

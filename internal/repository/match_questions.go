package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// MaxAuthoredPerMatch bounds what one person can write for one match. Enough to
// make the match theirs, few enough that writing them is still a few minutes'
// work rather than a second career.
const MaxAuthoredPerMatch = 10

// CreateMatchQuestions writes a player's own questions for one match and
// returns their ids, in the order given.
//
// They are inactive and carry the match's id, which is what keeps them out of
// every public draw: `is_active` is already the filter every draw uses, and a
// table constraint makes the pair impossible to unset by accident. The author
// is recorded because a question that upsets somebody has to have a name
// against it.
func (r *Repo) CreateMatchQuestions(ctx context.Context, challengeID, authorID uuid.UUID,
	locale string, drafts []models.TranslationDraft, categoryID, difficulty, correct []int) ([]int, error) {

	if len(drafts) == 0 {
		return nil, nil
	}
	if len(drafts) > MaxAuthoredPerMatch {
		return nil, ErrForbidden
	}

	ids := make([]int, 0, len(drafts))
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		for i, draft := range drafts {
			var id int
			err := tx.QueryRow(ctx, `
				INSERT INTO questions
					(category_id, difficulty, points, correct_index, source,
					 is_active, challenge_id, author_id, created_by)
				VALUES ($1, $2, $3, $4, 'player', false, $5, $6, $6)
				RETURNING id`,
				categoryID[i], difficulty[i], difficulty[i]*10, correct[i],
				challengeID, authorID).Scan(&id)
			if err != nil {
				return err
			}

			// One language: the one the match is played in. A question written
			// by hand for four friends does not need translating, and offering
			// to would be asking somebody to write it three times.
			if _, err := tx.Exec(ctx, `
				INSERT INTO question_translations
					(question_id, locale, prompt, choices, explanation, source, needs_review)
				VALUES ($1, $2, $3, $4, $5, 'player', false)`,
				id, locale, draft.Prompt, draft.Choices, draft.Explanation); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// MatchQuestionAuthors maps each of these question ids to who wrote it, for the
// ones that were written rather than drawn. The review screen uses it to say so
// — a question somebody made up should be labelled as such when it is read back.
func (r *Repo) MatchQuestionAuthors(ctx context.Context, ids []int) (map[int]*models.UserCard, error) {
	out := map[int]*models.UserCard{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT q.id, u.id, u.username, u.display_name, u.avatar_seed,
		       u.country, u.xp, u.last_seen_at
		  FROM questions q
		  JOIN users u ON u.id = q.author_id
		 WHERE q.id = ANY($1) AND q.author_id IS NOT NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var questionID int
		var card models.UserCard
		if err := rows.Scan(&questionID, &card.ID, &card.Username, &card.DisplayName,
			&card.AvatarSeed, &card.Country, &card.XP, &card.LastSeenAt); err != nil {
			return nil, err
		}
		out[questionID] = &card
	}
	return out, rows.Err()
}

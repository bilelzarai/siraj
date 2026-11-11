-- Module 9 · My questions (MQ-01…MQ-03, endpoints 96–105).

-- name: ListSets :many
SELECT s.*, (SELECT count(*) FROM set_questions q WHERE q.set_id = s.id)::int AS question_count
FROM question_sets s
WHERE s.owner_id = @owner_id AND s.deleted_at IS NULL
ORDER BY s.updated_at DESC
LIMIT @lim OFFSET @off;

-- name: CountSets :one
SELECT count(*) FROM question_sets WHERE owner_id = @owner_id AND deleted_at IS NULL;

-- name: GetSet :one
SELECT s.*, (SELECT count(*) FROM set_questions q WHERE q.set_id = s.id)::int AS question_count
FROM question_sets s WHERE s.id = @id AND s.deleted_at IS NULL;

-- name: CreateSet :one
INSERT INTO question_sets (owner_id, name, main_lang) VALUES (@owner_id, @name, @main_lang) RETURNING *;

-- name: UpdateSet :one
UPDATE question_sets
SET name = COALESCE(sqlc.narg('name'), name),
    main_lang = COALESCE(sqlc.narg('main_lang'), main_lang)
WHERE id = @id AND owner_id = @owner_id AND deleted_at IS NULL
RETURNING *;

-- name: DeleteSet :execrows
UPDATE question_sets SET deleted_at = now() WHERE id = @id AND owner_id = @owner_id AND deleted_at IS NULL;

-- name: BumpSetPlays :exec
UPDATE question_sets SET plays = plays + 1 WHERE id = @id;

-- name: SetQuestions :many
SELECT q.id, q.position, q.category_id, q.difficulty, q.points, q.correct_index, q.main_lang,
       t.lang, t.body, t.answers, t.explanation
FROM set_questions q
LEFT JOIN set_question_translations t ON t.set_question_id = q.id
WHERE q.set_id = @set_id
ORDER BY q.position, t.lang;

-- name: NextSetPosition :one
SELECT COALESCE(max(position), 0)::int + 1 FROM set_questions WHERE set_id = @set_id;

-- name: AddSetQuestion :one
INSERT INTO set_questions (set_id, position, category_id, difficulty, points, correct_index, main_lang)
VALUES (@set_id, @position, sqlc.narg('category_id'), @difficulty, @points, @correct_index, @main_lang)
RETURNING *;

-- name: UpdateSetQuestion :one
UPDATE set_questions
SET category_id   = COALESCE(sqlc.narg('category_id'), category_id),
    difficulty    = COALESCE(sqlc.narg('difficulty'), difficulty),
    points        = COALESCE(sqlc.narg('points'), points),
    correct_index = COALESCE(sqlc.narg('correct_index'), correct_index)
WHERE id = @id AND set_id = @set_id
RETURNING *;

-- name: UpsertSetQuestionText :exec
INSERT INTO set_question_translations (set_question_id, lang, body, answers, explanation)
VALUES (@set_question_id, @lang, @body, @answers, sqlc.narg('explanation'))
ON CONFLICT (set_question_id, lang) DO UPDATE
SET body = EXCLUDED.body, answers = EXCLUDED.answers, explanation = EXCLUDED.explanation;

-- name: DeleteSetQuestionText :exec
DELETE FROM set_question_translations WHERE set_question_id = @set_question_id AND lang = @lang;

-- name: DeleteSetQuestion :execrows
DELETE FROM set_questions WHERE id = @id AND set_id = @set_id;

-- name: ResequenceSet :exec
UPDATE set_questions q
SET position = ordered.rn
FROM (SELECT sq.id, row_number() OVER (ORDER BY sq.position, sq.id) AS rn
      FROM set_questions sq WHERE sq.set_id = @set_id) ordered
WHERE q.id = ordered.id;

-- name: GetSetQuestion :one
SELECT * FROM set_questions WHERE id = @id AND set_id = @set_id;

-- name: SetQuestionTexts :many
SELECT * FROM set_question_translations WHERE set_question_id = @set_question_id;

-- name: SetQuestionsForPlay :many
-- A set used as the questions of a game or a challenge (one language, the player's).
SELECT q.id, q.position, q.category_id, q.difficulty, q.points, q.correct_index,
       COALESCE(t.body, tm.body) AS body,
       COALESCE(t.answers, tm.answers) AS answers,
       COALESCE(t.explanation, tm.explanation) AS explanation
FROM set_questions q
LEFT JOIN set_question_translations t  ON t.set_question_id = q.id AND t.lang = @lang
LEFT JOIN set_question_translations tm ON tm.set_question_id = q.id AND tm.lang = q.main_lang
WHERE q.set_id = @set_id
ORDER BY q.position;

-- name: SuggestSetToBank :exec
INSERT INTO bank_suggestions (set_question_id, user_id)
SELECT q.id, @user_id FROM set_questions q WHERE q.set_id = @set_id
ON CONFLICT DO NOTHING;

-- name: SetQuestionIDs :many
-- The questions of one of the player's own sets, in order, for a challenge (MQ-05).
SELECT id FROM set_questions WHERE set_id = @set_id ORDER BY position;

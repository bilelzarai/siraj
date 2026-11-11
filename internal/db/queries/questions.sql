-- name: RateQuestion :exec
-- GP-05 / RS-03: one rating per player per question; rating again replaces it.
INSERT INTO question_ratings (question_id, user_id, stars, reason)
VALUES (@question_id, @user_id, @stars, @reason)
ON CONFLICT (question_id, user_id)
DO UPDATE SET stars = EXCLUDED.stars, reason = EXCLUDED.reason;

-- name: MyRating :one
SELECT stars, reason FROM question_ratings WHERE question_id = @question_id AND user_id = @user_id;

-- name: ListComments :many
SELECT c.id, c.body, c.created_at, u.display_name, u.username
FROM question_comments c
LEFT JOIN users u ON u.id = c.user_id
WHERE c.question_id = @question_id AND c.status = 'visible'
ORDER BY c.created_at DESC
LIMIT @lim;

-- name: AddComment :one
INSERT INTO question_comments (question_id, user_id, body)
VALUES (@question_id, @user_id, @body)
RETURNING *;

-- name: ReportQuestion :exec
INSERT INTO question_reports (question_id, user_id, reason, note, lang)
VALUES (@question_id, @user_id, @reason, @note, @lang);

-- name: QuestionExists :one
SELECT EXISTS (SELECT 1 FROM questions WHERE id = @id AND deleted_at IS NULL) AS found;

-- name: TodaysQuestion :one
-- Endpoint 22: the question of the day, from one of the player's domains, rotating by
-- day (HM-02). Islamic-only keeps it inside the Islamic family (DM-09).
SELECT dq.question_id, dq.domain_id, q.difficulty, q.points, q.correct_index, q.source,
       d.slug AS domain_slug, d.color AS domain_color, d.icon AS domain_icon,
       d.motif AS domain_motif, d.family AS domain_family,
       COALESCE(dt.name, dtf.name) AS domain_name,
       COALESCE(ct.name, ctf.name) AS category_name,
       COALESCE(qt.body, qtf.body) AS body,
       COALESCE(qt.answers, qtf.answers)::text[] AS answers,
       COALESCE(qt.explanation, qtf.explanation) AS explanation,
       (qt.body IS NOT NULL)::bool AS in_language
FROM daily_questions dq
JOIN questions q  ON q.id = dq.question_id
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = dq.domain_id
LEFT JOIN question_translations qt  ON qt.question_id = q.id AND qt.lang = @lang
LEFT JOIN question_translations qtf ON qtf.question_id = q.id AND qtf.lang = 'en'
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf   ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE dq.day = @day
  AND (NOT @islamic_only::bool OR d.family = 'islamic')
  AND (NOT @mine_only::bool OR EXISTS (
        SELECT 1 FROM user_domains ud WHERE ud.user_id = @user_id AND ud.domain_id = dq.domain_id))
ORDER BY (dq.domain_id + @day_number::bigint) % 7, dq.domain_id
LIMIT 1;

-- name: EnsureDailyQuestion :exec
-- Picks today's question for a domain when the worker has not done it yet.
INSERT INTO daily_questions (day, domain_id, question_id)
SELECT @day, @domain_id, q.id
FROM questions q
JOIN categories c ON c.id = q.category_id
WHERE c.domain_id = @domain_id AND q.status = 'published' AND q.deleted_at IS NULL
ORDER BY (q.id + @day_number::bigint) % 97, q.id
LIMIT 1
ON CONFLICT DO NOTHING;

-- name: MyDailyAnswer :one
SELECT * FROM daily_answers WHERE day = @day AND user_id = @user_id;

-- name: AnswerDaily :one
-- One try a day (HM-02): the insert is the guard.
INSERT INTO daily_answers (day, user_id, question_id, choice, is_correct, points)
VALUES (@day, @user_id, @question_id, @choice, @is_correct, @points)
ON CONFLICT (day, user_id) DO NOTHING
RETURNING *;

-- name: DailyStats :one
-- "x% of players got this right" under the answered card.
SELECT count(*)::int AS answers,
       count(*) FILTER (WHERE is_correct)::int AS correct
FROM daily_answers WHERE question_id = @question_id;

-- name: DomainsWithQuestions :many
SELECT DISTINCT c.domain_id FROM questions q
JOIN categories c ON c.id = q.category_id
WHERE q.status = 'published' AND q.deleted_at IS NULL;

-- name: QuestionText :one
-- Just the prompt, for the report drawer's echo of the question (RS-04).
SELECT COALESCE(t.body, tf.body) AS body
FROM questions q
LEFT JOIN question_translations t  ON t.question_id = q.id AND t.lang = @lang
LEFT JOIN question_translations tf ON tf.question_id = q.id AND tf.lang = 'en'
WHERE q.id = @id;

-- name: PickQuestions :many
-- The heart of starting a game (GP-02): published questions inside the scope, filtered
-- by Your domains and by Islamic-only, optionally by category and difficulty, in a
-- random order. When fewer exist than asked for, the caller plays with what came back.
SELECT q.id, q.category_id, q.difficulty, q.points, q.correct_index, c.domain_id
FROM questions q
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
WHERE q.status = 'published'
  AND q.deleted_at IS NULL
  AND c.is_published
  AND d.is_published
  AND (NOT @islamic_only::bool OR d.family = 'islamic')
  AND (@domain_id::bigint = 0 OR c.domain_id = @domain_id)
  AND (cardinality(@category_ids::bigint[]) = 0 OR q.category_id = ANY (@category_ids))
  -- The universal tree (CUT-1). @node is a node to play from and @node_scope says how
  -- much of it counts: 'direct' is the content hanging on that node, 'subtree' is
  -- everything below it but not the node itself, 'both' is the branch entire (§10.2).
  -- `path <@` reads the GiST index, so a branch ten levels deep costs one comparison.
  AND (sqlc.narg('node')::bigint IS NULL OR CASE @node_scope::text
         WHEN 'direct'  THEN q.node_id = sqlc.narg('node')::bigint
         WHEN 'subtree' THEN q.node_id IN (
                SELECT n.id FROM nodes n
                 WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = sqlc.narg('node')::bigint)
                   AND n.id <> sqlc.narg('node')::bigint AND n.deleted_at IS NULL)
         ELSE q.node_id IN (
                SELECT n.id FROM nodes n
                 WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = sqlc.narg('node')::bigint)
                   AND n.deleted_at IS NULL)
       END)
  AND (NOT @mine_only::bool OR EXISTS (
        SELECT 1 FROM user_domains ud WHERE ud.user_id = @user_id AND ud.domain_id = c.domain_id))
  AND (@difficulty::smallint = 0 OR q.difficulty = @difficulty)
  AND q.id <> ALL (@exclude::bigint[])
ORDER BY random()
LIMIT @lim;

-- name: CountAvailableQuestions :one
-- The same filter without the limit: the setup screen says how many questions exist.
SELECT count(*)
FROM questions q
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
WHERE q.status = 'published'
  AND q.deleted_at IS NULL
  AND c.is_published
  AND d.is_published
  AND (NOT @islamic_only::bool OR d.family = 'islamic')
  AND (@domain_id::bigint = 0 OR c.domain_id = @domain_id)
  AND (cardinality(@category_ids::bigint[]) = 0 OR q.category_id = ANY (@category_ids))
  -- The universal tree (CUT-1). @node is a node to play from and @node_scope says how
  -- much of it counts: 'direct' is the content hanging on that node, 'subtree' is
  -- everything below it but not the node itself, 'both' is the branch entire (§10.2).
  -- `path <@` reads the GiST index, so a branch ten levels deep costs one comparison.
  AND (sqlc.narg('node')::bigint IS NULL OR CASE @node_scope::text
         WHEN 'direct'  THEN q.node_id = sqlc.narg('node')::bigint
         WHEN 'subtree' THEN q.node_id IN (
                SELECT n.id FROM nodes n
                 WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = sqlc.narg('node')::bigint)
                   AND n.id <> sqlc.narg('node')::bigint AND n.deleted_at IS NULL)
         ELSE q.node_id IN (
                SELECT n.id FROM nodes n
                 WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = sqlc.narg('node')::bigint)
                   AND n.deleted_at IS NULL)
       END)
  AND (NOT @mine_only::bool OR EXISTS (
        SELECT 1 FROM user_domains ud WHERE ud.user_id = @user_id AND ud.domain_id = c.domain_id))
  AND (@difficulty::smallint = 0 OR q.difficulty = @difficulty);

-- name: CreateGame :one
INSERT INTO games (mode, status, owner_id, scope, difficulty, question_count, seconds_per_question, room_id, challenge_id)
VALUES (@mode, 'playing', @owner_id, @scope, sqlc.narg('difficulty'), @question_count, @seconds_per_question,
        sqlc.narg('room_id'), sqlc.narg('challenge_id'))
RETURNING *;

-- name: AddGameQuestion :exec
-- A round names a v1 question, a question from a player's set, or a content item.
-- Migration 28's trigger pairs question_id and item_id either way, so a caller gives
-- whichever it has.
INSERT INTO game_questions (game_id, position, question_id, set_question_id, item_id)
VALUES (@game_id, @position, sqlc.narg('question_id'), sqlc.narg('set_question_id'),
        sqlc.narg('item_id'));

-- name: AddGamePlayer :one
INSERT INTO game_players (game_id, user_id, local_name, team)
VALUES (@game_id, sqlc.narg('user_id'), sqlc.narg('local_name'), sqlc.narg('team'))
RETURNING *;

-- name: GetGame :one
SELECT * FROM games WHERE id = @id;

-- name: GetGamePlayer :one
SELECT * FROM game_players WHERE game_id = @game_id AND user_id = @user_id;

-- name: GamePlayers :many
SELECT * FROM game_players WHERE game_id = @game_id ORDER BY score DESC, finished_at NULLS LAST;

-- name: ServeQuestion :one
-- Stamps the moment the player was shown this question; the first call wins, so
-- reloading the page does not reset the clock. Returns the stamp either way.
UPDATE game_questions SET served_at = now()
WHERE game_id = @game_id AND position = @position AND served_at IS NULL
RETURNING served_at;

-- name: QuestionServedAt :one
SELECT served_at FROM game_questions WHERE game_id = @game_id AND position = @position;

-- name: GameQuestionAt :one
-- One question of a game, in the player's language, with the English fallback (LG-05).
-- A game's questions come from the bank or, for a challenge on one of the player's own
-- sets, from set_questions (MQ-05); the union keeps the engine blind to the difference.
SELECT gq.position, q.id, q.category_id,
       COALESCE(fv.difficulty, q.difficulty), COALESCE(fv.points, q.points),
       COALESCE((fv.answer->>'index')::smallint, q.correct_index), q.source,
       c.domain_id, c.icon AS category_icon, c.color AS category_color,
       COALESCE(ct.name, ctf.name) AS category_name,
       d.slug AS domain_slug, d.color AS domain_color, d.icon AS domain_icon,
       d.motif AS domain_motif, d.family AS domain_family,
       COALESCE(dt.name, dtf.name) AS domain_name,
       -- What was frozen when this round was dealt comes first (§10.3, §25 #13).
       -- Every question is mirrored into content_items (migration 26) and frozen on
       -- deal (migration 29), so an edit to the bank cannot reach a game in progress
       -- or a result already recorded. Live text is the fallback for rounds dealt
       -- before snapshots existed.
       COALESCE(fv.texts -> sqlc.arg(lang)::text ->> 'body', fv.texts->'en'->>'body',
                qt.body, qtf.body) AS body,
       COALESCE(content_answer_list(fv.texts, sqlc.arg(lang)::text),
                qt.answers, qtf.answers)::text[] AS answers,
       COALESCE(fv.texts -> sqlc.arg(lang)::text ->> 'explanation', fv.texts->'en'->>'explanation',
                qt.explanation, qtf.explanation) AS explanation,
       (COALESCE(fv.texts -> sqlc.arg(lang)::text, to_jsonb(qt.body)) IS NOT NULL)::bool AS in_language
FROM game_questions gq
JOIN questions q  ON q.id = gq.question_id
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
LEFT JOIN question_translations qt  ON qt.question_id = q.id AND qt.lang = @lang
LEFT JOIN question_translations qtf ON qtf.question_id = q.id AND qtf.lang = 'en'
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf   ON dtf.domain_id = d.id AND dtf.lang = 'en'
LEFT JOIN content_item_versions fv  ON fv.item_id = gq.item_id AND fv.version = gq.item_version
WHERE gq.game_id = @game_id AND gq.position = @position
UNION ALL
SELECT gq.position, sq.id, COALESCE(sq.category_id, 0), sq.difficulty, sq.points, sq.correct_index, ''::text,
       COALESCE(c.domain_id, 0), COALESCE(c.icon, 'pen'), COALESCE(c.color, '#9a5b07'),
       COALESCE(ct.name, ctf.name, ''),
       COALESCE(d.slug, 'mine'), COALESCE(d.color, '#9a5b07'), COALESCE(d.icon, 'pen'),
       COALESCE(d.motif, 'pen'), COALESCE(d.family, 'general'),
       COALESCE(dt.name, dtf.name, ''),
       COALESCE(st.body, stm.body), COALESCE(st.answers, stm.answers)::text[],
       COALESCE(st.explanation, stm.explanation),
       (st.body IS NOT NULL)::bool
FROM game_questions gq
JOIN set_questions sq ON sq.id = gq.set_question_id
LEFT JOIN categories c ON c.id = sq.category_id
LEFT JOIN domains d    ON d.id = c.domain_id
LEFT JOIN set_question_translations st  ON st.set_question_id = sq.id AND st.lang = @lang
LEFT JOIN set_question_translations stm ON stm.set_question_id = sq.id AND stm.lang = sq.main_lang
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf   ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE gq.game_id = @game_id AND gq.position = @position
UNION ALL
-- CUT-4b: content born in the universal model, which has no v1 question behind it.
-- A mirrored question still comes through the first branch, so nothing is listed
-- twice and nothing about an existing game changes.
--
-- The theming a screen needs — icon, colour, the root it belongs to — comes off the
-- node rather than a category, which is what lets content ten levels down be played
-- and still look like the domain it lives in.
SELECT gq.position, i.id, 0::bigint,
       COALESCE(fv.difficulty, i.difficulty), COALESCE(fv.points, i.points),
       COALESCE((COALESCE(fv.answer, i.answer)->>'index')::smallint, -1), ''::text,
       0::bigint, COALESCE(NULLIF(n.icon, ''), 'book'), COALESCE(NULLIF(n.color, ''), '#0b7a5c'),
       COALESCE(nt.name, ntf.name, n.slug),
       COALESCE(rt_n.slug, n.slug), COALESCE(NULLIF(rt_n.color, ''), '#0b7a5c'),
       COALESCE(NULLIF(rt_n.icon, ''), 'book'), COALESCE(NULLIF(rt_n.motif, ''), 'book'),
       -- The first branch of this union yields the domain_family enum, so this one
       -- must too. A node's metadata is free-form, so only the value that means
       -- something to the design is honoured and everything else reads as general.
       CASE WHEN rt_n.metadata->>'family' = 'islamic' THEN 'islamic' ELSE 'general' END::domain_family,
       COALESCE(rt_t.name, rt_f.name, rt_n.slug, ''),
       -- What was frozen when this round was dealt comes first (§10.3). An edit to
       -- the source must not reach a match already in progress, or a result already
       -- recorded. The live text is the fallback for a round dealt before snapshots
       -- existed at all.
       COALESCE(fv.texts -> sqlc.arg(lang)::text ->> 'body', fv.texts->'en'->>'body',
                it.fields->>'body', itf.fields->>'body'),
       COALESCE(content_answer_list(fv.texts, sqlc.arg(lang)::text),
                string_to_array(COALESCE(it.fields->>'answers', itf.fields->>'answers', ''), '|'))::text[],
       COALESCE(fv.texts -> sqlc.arg(lang)::text ->> 'explanation', fv.texts->'en'->>'explanation',
                it.fields->>'explanation', itf.fields->>'explanation', ''),
       (COALESCE(fv.texts -> sqlc.arg(lang)::text, it.fields) IS NOT NULL)::bool
FROM game_questions gq
JOIN content_items i ON i.id = gq.item_id AND i.question_id IS NULL
JOIN nodes n         ON n.id = i.node_id
LEFT JOIN nodes rt_n ON rt_n.id = n.root_id
LEFT JOIN node_translations nt   ON nt.node_id = n.id AND nt.lang = @lang
LEFT JOIN node_translations ntf  ON ntf.node_id = n.id AND ntf.lang = 'en'
LEFT JOIN node_translations rt_t ON rt_t.node_id = rt_n.id AND rt_t.lang = @lang
LEFT JOIN node_translations rt_f ON rt_f.node_id = rt_n.id AND rt_f.lang = 'en'
LEFT JOIN content_item_translations it  ON it.item_id = i.id AND it.lang = @lang
LEFT JOIN content_item_translations itf ON itf.item_id = i.id AND itf.lang = 'en'
LEFT JOIN content_item_versions fv ON fv.item_id = i.id AND fv.version = gq.item_version
WHERE gq.game_id = @game_id AND gq.position = @position;

-- name: GameQuestions :many
-- Every question of a game, for the results review (RS-03).
SELECT gq.position, q.id, q.category_id, q.difficulty, q.points, q.correct_index, q.source,
       c.domain_id, c.icon AS category_icon, c.color AS category_color,
       COALESCE(ct.name, ctf.name) AS category_name,
       d.slug AS domain_slug, d.color AS domain_color, d.icon AS domain_icon,
       d.family AS domain_family,
       COALESCE(dt.name, dtf.name) AS domain_name,
       COALESCE(qt.body, qtf.body) AS body,
       COALESCE(qt.answers, qtf.answers)::text[] AS answers,
       COALESCE(qt.explanation, qtf.explanation) AS explanation
FROM game_questions gq
JOIN questions q  ON q.id = gq.question_id
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
LEFT JOIN question_translations qt  ON qt.question_id = q.id AND qt.lang = @lang
LEFT JOIN question_translations qtf ON qtf.question_id = q.id AND qtf.lang = 'en'
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf   ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE gq.game_id = @game_id
UNION ALL
SELECT gq.position, sq.id, COALESCE(sq.category_id, 0), sq.difficulty, sq.points, sq.correct_index, ''::text,
       COALESCE(c.domain_id, 0), COALESCE(c.icon, 'pen'), COALESCE(c.color, '#9a5b07'),
       COALESCE(ct.name, ctf.name, ''),
       COALESCE(d.slug, 'mine'), COALESCE(d.color, '#9a5b07'), COALESCE(d.icon, 'pen'),
       COALESCE(d.family, 'general'),
       COALESCE(dt.name, dtf.name, ''),
       COALESCE(st.body, stm.body), COALESCE(st.answers, stm.answers)::text[],
       COALESCE(st.explanation, stm.explanation)
FROM game_questions gq
JOIN set_questions sq ON sq.id = gq.set_question_id
LEFT JOIN categories c ON c.id = sq.category_id
LEFT JOIN domains d    ON d.id = c.domain_id
LEFT JOIN set_question_translations st  ON st.set_question_id = sq.id AND st.lang = @lang
LEFT JOIN set_question_translations stm ON stm.set_question_id = sq.id AND stm.lang = sq.main_lang
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf   ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE gq.game_id = @game_id
ORDER BY 1;

-- name: SaveAnswer :exec
-- One answer per player per question: a second submit is ignored, which also makes the
-- Idempotency-Key header safe.
INSERT INTO game_answers (game_id, player_id, position, choice, is_correct, time_ms, points, lifeline)
VALUES (@game_id, @player_id, @position, sqlc.narg('choice'), @is_correct, @time_ms, @points, sqlc.narg('lifeline'))
ON CONFLICT DO NOTHING;

-- name: PlayerAnswers :many
SELECT * FROM game_answers WHERE game_id = @game_id AND player_id = @player_id ORDER BY position;

-- name: AnswerAt :one
SELECT * FROM game_answers WHERE game_id = @game_id AND player_id = @player_id AND position = @position;

-- name: CountAnswers :one
SELECT count(*) FROM game_answers WHERE game_id = @game_id AND player_id = @player_id;

-- name: UsedLifelines :many
SELECT DISTINCT lifeline FROM game_answers
WHERE game_id = @game_id AND player_id = @player_id AND lifeline IS NOT NULL;

-- name: SetLifelineUsed :exec
-- 50/50 leaves a trace without answering: the row is written when the answer arrives,
-- so the engine keeps the used lifelines in the game row's scope bag instead.
UPDATE games SET scope = jsonb_set(scope, @path::text[], 'true'::jsonb, true) WHERE id = @id;

-- name: UpdatePlayerScore :exec
UPDATE game_players SET score = @score, correct = @correct WHERE id = @id;

-- name: FinishGamePlayer :exec
UPDATE game_players SET score = @score, correct = @correct, rank = @rank,
       xp_gained = @xp_gained, finished_at = now()
WHERE id = @id;

-- name: FinishGame :exec
UPDATE games SET status = @status, finished_at = now() WHERE id = @id AND finished_at IS NULL;

-- name: PlayersStillPlaying :one
-- How many of this game's players have not finished their own turn yet. A room game
-- and a pass-the-device round share one games row, so the game only closes once this
-- reaches zero (RM-04, RM-06).
SELECT count(*)::int FROM game_players WHERE game_id = @game_id AND finished_at IS NULL;

-- name: MyGames :many
-- Endpoint 31, the History screen (LB-03). mode = '' means every mode.
SELECT g.id, g.mode, g.question_count, g.finished_at, g.scope,
       p.score, p.correct, p.rank,
       dom.name AS domain_name, dom.color AS domain_color, dom.icon AS domain_icon
FROM games g
JOIN game_players p ON p.game_id = g.id AND p.user_id = @user_id
LEFT JOIN LATERAL (
  SELECT COALESCE(dt.name, dtf.name) AS name, d.color, d.icon
  FROM game_questions gq
  JOIN questions q  ON q.id = gq.question_id
  JOIN categories c ON c.id = q.category_id
  JOIN domains d    ON d.id = c.domain_id
  LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
  LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
  WHERE gq.game_id = g.id
  ORDER BY gq.position LIMIT 1
) dom ON true
WHERE g.status = 'finished'
  AND (@mode::text = '' OR g.mode::text = @mode)
ORDER BY g.finished_at DESC NULLS LAST
LIMIT @lim OFFSET @off;

-- name: CountMyGames :one
SELECT count(*) FROM games g
JOIN game_players p ON p.game_id = g.id AND p.user_id = @user_id
WHERE g.status = 'finished' AND (@mode::text = '' OR g.mode::text = @mode);

-- name: MyGameStats :one
-- The four tiles above the history list.
SELECT count(*)::int AS games,
       COALESCE(max(p.score), 0)::int AS best_score,
       COALESCE(sum(p.correct), 0)::int AS correct,
       COALESCE(sum(g.question_count), 0)::int AS answered,
       count(*) FILTER (WHERE g.finished_at >= date_trunc('week', current_date))::int AS this_week
FROM games g
JOIN game_players p ON p.game_id = g.id AND p.user_id = @user_id
WHERE g.status = 'finished';

-- name: ResultsByDomain :many
-- RS-02: a mixed game shows how the player did in each domain.
SELECT c.domain_id, COALESCE(dt.name, dtf.name) AS domain_name, d.color,
       count(*)::int AS answered,
       count(*) FILTER (WHERE a.is_correct)::int AS correct
FROM game_answers a
JOIN game_questions gq ON gq.game_id = a.game_id AND gq.position = a.position
JOIN questions q  ON q.id = gq.question_id
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE a.game_id = @game_id AND a.player_id = @player_id
GROUP BY c.domain_id, dt.name, dtf.name, d.color
ORDER BY answered DESC;

-- name: GamePlayersInOrder :many
-- A pass-the-device round plays in turn order: the host first, then each local name
-- in the order they were added (RM-06).
SELECT * FROM game_players WHERE game_id = @game_id ORDER BY joined_at, id;

-- name: ResetServedQuestions :exec
-- Each turn of a device round starts the clock again for everyone's own answers.
UPDATE game_questions SET served_at = NULL WHERE game_id = @game_id;

-- name: SetGamePlayerTeam :exec
-- Which side a player answers for. NULL in everyone-for-themselves (RM-05).
UPDATE game_players SET team = sqlc.narg('team') WHERE game_id = @game_id AND user_id = @user_id;

-- name: PlayerProgress :many
-- Everyone on a shared game, with how far each has got: the live board of a room and
-- the standings of its podium read the same row (RM-04, RM-05, RM-07).
SELECT p.id, p.user_id, p.local_name, p.team, p.score, p.correct, p.xp_gained, p.finished_at,
       count(a.position)::int                                        AS answered,
       COALESCE(round(avg(a.time_ms)), 0)::int                       AS avg_ms,
       COALESCE(min(a.time_ms) FILTER (WHERE a.is_correct), 0)::int  AS best_ms
FROM game_players p
LEFT JOIN game_answers a ON a.game_id = p.game_id AND a.player_id = p.id
WHERE p.game_id = @game_id
GROUP BY p.id
ORDER BY p.score DESC, p.joined_at;

-- name: AnswersAtPosition :many
-- What each player did with the question on screen: the "+points" the room sees once
-- the answer is revealed (RM-04, RM-06).
SELECT player_id, is_correct, points, time_ms
FROM game_answers WHERE game_id = @game_id AND position = @position;

-- name: GameStreaks :many
-- The longest run of right answers each player strung together, for the comeback
-- award on the podium (RM-07).
WITH runs AS (
  SELECT player_id,
         position - row_number() OVER (PARTITION BY player_id ORDER BY position) AS grp
  FROM game_answers WHERE game_id = @game_id AND is_correct
)
SELECT player_id, max(n)::int AS streak
FROM (SELECT player_id, grp, count(*)::int AS n FROM runs GROUP BY player_id, grp) t
GROUP BY player_id;

-- name: SetGamePlayerTeamByID :exec
-- A pass-the-device player has no account, so their side is set by row id (DV-01).
UPDATE game_players SET team = sqlc.narg('team') WHERE id = @id;

-- name: PlayerChoiceAt :many
-- What each player picked for one question, so the reveal can stand their avatars on
-- the options they chose (DV-04).
SELECT player_id, choice, is_correct, points, time_ms
FROM game_answers WHERE game_id = @game_id AND position = @position;

-- name: FinishDevicePlayer :exec
-- A pass-the-device player has no account: they get their place and nothing else, so
-- no XP is written for someone who cannot hold it (DV-06).
-- The owner's row is already finished by the engine, which is where their XP comes
-- from; this only settles the places, so every row of the round gets one.
UPDATE game_players SET score = @score, correct = @correct, rank = @rank,
       finished_at = COALESCE(finished_at, now())
WHERE id = @id;

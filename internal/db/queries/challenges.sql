-- Challenges and pass-the-device rounds (CH-01…CH-11, RM-06).
-- Other players' scores never leave the server until the challenge closes; the DTO
-- builder in the service enforces that, these statements only read what it needs.

-- name: CreateChallenge :one
INSERT INTO challenges (creator_id, source, scope, set_id, difficulty, question_count,
                        seconds_per_question, message, deadline_at, is_guest)
VALUES (@creator_id, @source, @scope, sqlc.narg('set_id'), sqlc.narg('difficulty'),
        @question_count, @seconds_per_question, sqlc.narg('message'),
        sqlc.narg('deadline_at'), @is_guest)
RETURNING *;

-- name: AddChallengeQuestion :exec
INSERT INTO challenge_questions (challenge_id, position, question_id, set_question_id)
VALUES (@challenge_id, @position, sqlc.narg('question_id'), sqlc.narg('set_question_id'));

-- name: ChallengeQuestionIDs :many
SELECT position, question_id, set_question_id
FROM challenge_questions WHERE challenge_id = @challenge_id ORDER BY position;

-- name: AddChallengeParticipant :exec
INSERT INTO challenge_participants (challenge_id, user_id, status)
VALUES (@challenge_id, @user_id, @status)
ON CONFLICT (challenge_id, user_id) DO NOTHING;

-- name: GetChallenge :one
SELECT * FROM challenges WHERE id = @id;

-- name: GetParticipant :one
SELECT * FROM challenge_participants WHERE challenge_id = @challenge_id AND user_id = @user_id;

-- name: ChallengeParticipants :many
SELECT p.*, u.display_name, u.username, u.level
FROM challenge_participants p
JOIN users u ON u.id = p.user_id
WHERE p.challenge_id = @challenge_id
ORDER BY p.score DESC NULLS LAST, p.created_at;

-- name: StartParticipant :exec
UPDATE challenge_participants
SET status = 'playing', game_id = @game_id, started_at = now()
WHERE challenge_id = @challenge_id AND user_id = @user_id;

-- name: FinishParticipant :exec
UPDATE challenge_participants
SET status = 'played', finished_at = now(), score = @score, correct = @correct
WHERE challenge_id = @challenge_id AND user_id = @user_id;

-- name: DeclineParticipant :exec
UPDATE challenge_participants SET status = 'declined'
WHERE challenge_id = @challenge_id AND user_id = @user_id AND status = 'pending';

-- name: CloseChallenge :exec
UPDATE challenges SET status = 'closed', closed_at = now(), close_reason = @close_reason
WHERE id = @id AND status = 'open';

-- name: RankChallenge :exec
-- Ranks everyone who played and writes won / lost / draw (CH-07).
WITH played AS (
  SELECT cp.id, cp.score FROM challenge_participants cp
  WHERE cp.challenge_id = @challenge_id AND cp.status = 'played'
), ranked AS (
  SELECT played.id,
         rank() OVER (ORDER BY played.score DESC) AS r,
         count(*) FILTER (WHERE played.score = (SELECT max(score) FROM played)) OVER () AS tied
  FROM played
)
UPDATE challenge_participants p
SET rank = ranked.r::smallint,
    result = CASE WHEN ranked.r > 1 THEN 'lost'::challenge_result
                  WHEN ranked.tied > 1 THEN 'draw'::challenge_result
                  ELSE 'won'::challenge_result END
FROM ranked WHERE ranked.id = p.id;

-- name: MyChallenges :many
-- CH-02: received (someone challenged me), sent (I created it) and finished.
SELECT c.*, p.status AS my_status, p.score AS my_score, p.game_id AS my_game_id,
       p.result AS my_result,
       u.display_name AS creator_name, u.username AS creator_username,
       (SELECT count(*) FROM challenge_participants x WHERE x.challenge_id = c.id)::int AS players,
       (SELECT count(*) FROM challenge_participants x
          WHERE x.challenge_id = c.id AND x.status = 'played')::int AS played
FROM challenges c
JOIN challenge_participants p ON p.challenge_id = c.id AND p.user_id = @user_id
JOIN users u ON u.id = c.creator_id
WHERE (@box::text = 'received' AND c.creator_id <> @user_id AND c.status = 'open' AND p.status = 'pending')
   OR (@box::text = 'sent'     AND c.creator_id = @user_id  AND c.status = 'open')
   OR (@box::text = 'finished' AND (c.status = 'closed' OR p.status = 'played'))
ORDER BY c.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountMyChallenges :one
SELECT count(*) FROM challenges c
JOIN challenge_participants p ON p.challenge_id = c.id AND p.user_id = @user_id
WHERE (@box::text = 'received' AND c.creator_id <> @user_id AND c.status = 'open' AND p.status = 'pending')
   OR (@box::text = 'sent'     AND c.creator_id = @user_id  AND c.status = 'open')
   OR (@box::text = 'finished' AND (c.status = 'closed' OR p.status = 'played'));

-- name: ChallengeBoxCounts :one
SELECT
  count(*) FILTER (WHERE c.creator_id <> @user_id AND c.status = 'open' AND p.status = 'pending')::int AS received,
  count(*) FILTER (WHERE c.creator_id = @user_id AND c.status = 'open')::int AS sent,
  count(*) FILTER (WHERE c.status = 'closed' OR p.status = 'played')::int AS finished
FROM challenges c
JOIN challenge_participants p ON p.challenge_id = c.id AND p.user_id = @user_id;

-- name: ChallengeScope :one
-- The category a challenge was built on, for the card's title (CH-02).
SELECT COALESCE(ct.name, ctf.name) AS category_name, c.icon, c.color
FROM challenge_questions cq
JOIN questions q ON q.id = cq.question_id
JOIN categories c ON c.id = q.category_id
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
WHERE cq.challenge_id = @challenge_id
ORDER BY cq.position LIMIT 1;

-- name: ExpireChallenges :exec
-- The worker closes open challenges past their deadline (CH-08).
UPDATE challenges SET status = 'closed', closed_at = now(), close_reason = 'deadline'
WHERE status = 'open' AND deadline_at IS NOT NULL AND deadline_at < now();

-- name: CancelChallenge :exec
-- CH-10: the creator can call a challenge off, but only while nobody else has
-- started playing it; after that the round belongs to everyone in it.
-- close_reason only accepts all_played / deadline / host (migration 9), and the
-- creator is the host, so a cancellation is recorded as 'host'.
UPDATE challenges SET status = 'cancelled', closed_at = now(), close_reason = 'host'
WHERE challenges.id = @id AND challenges.creator_id = @creator_id
  AND challenges.status = 'open'
  AND NOT EXISTS (
    SELECT 1 FROM challenge_participants p
    WHERE p.challenge_id = challenges.id AND p.user_id <> @creator_id
      AND p.status IN ('playing', 'played'));

-- name: ChallengeStillWaiting :many
-- CH-10 "remind": everyone who has not started their round yet, the creator aside.
SELECT user_id FROM challenge_participants
WHERE challenge_id = @challenge_id AND user_id <> @creator_id AND status = 'pending';

-- name: RemoveNonStarters :exec
-- CH-06 / CH-07: whoever never started leaves the challenge when it closes, so the
-- results belong to the players who actually played.
UPDATE challenge_participants SET status = 'removed'
WHERE challenge_id = @challenge_id AND status = 'pending';

-- name: ChallengesPastDeadline :many
-- Every open challenge whose deadline has gone by (CH-06). A player who started keeps
-- their window, so a challenge with somebody still mid-game waits for them (D-03).
SELECT c.id FROM challenges c
WHERE c.status = 'open' AND c.deadline_at IS NOT NULL AND c.deadline_at < now()
  AND NOT EXISTS (
    SELECT 1 FROM challenge_participants p
    WHERE p.challenge_id = c.id AND p.status = 'playing'
      AND p.started_at > now() - interval '30 minutes'
  );

-- name: CreateChallengeLink :one
-- CH-10: a share link that lasts 24 hours.
INSERT INTO challenge_links (code, challenge_id, expires_at)
VALUES (@code, @challenge_id, @expires_at)
RETURNING *;

-- name: ChallengeLinkFor :one
SELECT * FROM challenge_links
WHERE challenge_id = @challenge_id AND expires_at > now()
ORDER BY created_at DESC LIMIT 1;

-- name: GetChallengeLink :one
SELECT * FROM challenge_links WHERE code = @code AND expires_at > now();

-- name: UseChallengeLink :exec
UPDATE challenge_links SET uses = uses + 1 WHERE code = @code;

-- name: CountChallengeParticipants :one
SELECT count(*)::int FROM challenge_participants WHERE challenge_id = @challenge_id;

-- name: RandomOpponent :one
-- Endpoint 61: somebody real to play against — a friend first, and failing that a
-- player this one has already shared a game with. Blocked people never come back.
WITH friends AS (
  SELECT CASE WHEN f.user_a = @user_id THEN f.user_b ELSE f.user_a END AS id
  FROM friendships f
  WHERE @user_id IN (f.user_a, f.user_b)
), played AS (
  SELECT DISTINCT p2.user_id AS id
  FROM game_players p1
  JOIN game_players p2 ON p2.game_id = p1.game_id AND p2.user_id <> p1.user_id
  WHERE p1.user_id = @user_id AND p2.user_id IS NOT NULL
), pool AS (
  SELECT id, 0 AS rank FROM friends
  UNION
  SELECT id, 1 AS rank FROM played
)
SELECT u.id, u.display_name, u.username, u.level
FROM pool
JOIN users u ON u.id = pool.id
WHERE u.id <> @user_id
  AND u.kind = 'member'
  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = @user_id AND b.blocked_id = u.id)
                                            OR (b.blocker_id = u.id AND b.blocked_id = @user_id))
ORDER BY pool.rank, random()
LIMIT 1;

-- name: DeleteExpiredChallengeLinks :execrows
-- The worker sweeps share links once they are past their 24 hours (CH-10).
DELETE FROM challenge_links WHERE expires_at < now();

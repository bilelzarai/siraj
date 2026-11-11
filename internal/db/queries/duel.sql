-- 1v1 duels (DU-01…DU-04): the match, the game and the rating.

-- name: EnterDuelQueue :exec
INSERT INTO duel_queue (user_id, domain_id, is_guest, rating)
VALUES (@user_id, sqlc.narg('domain_id'), @is_guest, @rating)
ON CONFLICT (user_id) DO UPDATE
SET domain_id = EXCLUDED.domain_id, is_guest = EXCLUDED.is_guest,
    rating = EXCLUDED.rating, game_id = NULL, created_at = now();

-- name: LeaveDuelQueue :exec
DELETE FROM duel_queue WHERE user_id = @user_id;

-- name: MyDuelQueue :one
SELECT * FROM duel_queue WHERE user_id = @user_id;

-- name: FindOpponent :one
-- The player who has waited longest, of the same kind, closest in rating (DU-02).
SELECT * FROM duel_queue
WHERE user_id <> @user_id AND game_id IS NULL AND is_guest = @is_guest
ORDER BY abs(rating - @rating::int), created_at
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: MatchDuel :exec
UPDATE duel_queue SET game_id = @game_id WHERE user_id = ANY(@user_ids::uuid[]);

-- name: GetDuelRating :one
SELECT * FROM duel_ratings WHERE user_id = @user_id AND domain_id = @domain_id;

-- name: BumpDuelRating :exec
INSERT INTO duel_ratings (user_id, domain_id, rating, wins, losses, draws)
VALUES (@user_id, @domain_id, @rating, @wins, @losses, @draws)
ON CONFLICT (user_id, domain_id) DO UPDATE
SET rating = duel_ratings.rating + EXCLUDED.rating - 1200,
    wins   = duel_ratings.wins + EXCLUDED.wins,
    losses = duel_ratings.losses + EXCLUDED.losses,
    draws  = duel_ratings.draws + EXCLUDED.draws;

-- name: MyBestRating :one
SELECT COALESCE(max(rating), 1200)::int AS rating,
       COALESCE(sum(wins), 0)::int AS wins,
       COALESCE(sum(losses), 0)::int AS losses
FROM duel_ratings WHERE user_id = @user_id;

-- name: DuelHeadToHead :one
-- How the two of them stand after every duel they have finished together (DU-01).
WITH duels AS (
  SELECT g.id,
         max(p.score) FILTER (WHERE p.user_id = @user_id)  AS mine,
         max(p.score) FILTER (WHERE p.user_id = @other_id) AS theirs
  FROM games g
  JOIN game_players p ON p.game_id = g.id
  WHERE g.mode = 'duel' AND g.finished_at IS NOT NULL
    AND p.user_id IN (@user_id, @other_id)
  GROUP BY g.id
  HAVING count(*) FILTER (WHERE p.user_id IN (@user_id, @other_id)) = 2
)
SELECT COALESCE(count(*) FILTER (WHERE mine > theirs), 0)::int AS wins,
       COALESCE(count(*) FILTER (WHERE mine < theirs), 0)::int AS losses,
       COALESCE(count(*) FILTER (WHERE mine = theirs), 0)::int AS draws
FROM duels;

-- name: LastDuelAgainst :one
-- The duel these two played last: the score, the domain it ran on and when it ended.
SELECT g.id, g.scope, g.finished_at,
       COALESCE(max(p.score) FILTER (WHERE p.user_id = @user_id), 0)::int  AS mine,
       COALESCE(max(p.score) FILTER (WHERE p.user_id = @other_id), 0)::int AS theirs
FROM games g
JOIN game_players p ON p.game_id = g.id
WHERE g.mode = 'duel' AND g.finished_at IS NOT NULL
  AND p.user_id IN (@user_id, @other_id)
GROUP BY g.id
HAVING count(*) FILTER (WHERE p.user_id IN (@user_id, @other_id)) = 2
ORDER BY g.finished_at DESC
LIMIT 1;

-- name: MyDuelRatings :many
-- Endpoint 50: one line per domain the player has duelled in, best rating first.
SELECT r.domain_id, r.rating, r.wins, r.losses, r.draws,
       d.slug::text AS domain_slug, d.color, d.icon,
       COALESCE(t.name, te.name, d.slug)::text AS domain_name
FROM duel_ratings r
JOIN domains d ON d.id = r.domain_id
LEFT JOIN domain_translations t  ON t.domain_id = d.id AND t.lang = @lang::text
LEFT JOIN domain_translations te ON te.domain_id = d.id AND te.lang = 'en'
WHERE r.user_id = @user_id
ORDER BY r.rating DESC, d.position, d.id;

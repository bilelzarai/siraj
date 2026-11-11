-- Module 8 · Progress: XP, mastery, streaks, activity, achievements, leaderboards.
-- The first four statements run inside the game-finish transaction (games.FinishHook).

-- name: ApplyCategoryStats :exec
INSERT INTO user_category_stats (user_id, category_id, answered, correct, last_played_at)
SELECT @user_id::uuid, q.category_id, count(*)::int, count(*) FILTER (WHERE a.is_correct)::int, now()
FROM game_answers a
JOIN game_questions gq ON gq.game_id = a.game_id AND gq.position = a.position
JOIN questions q ON q.id = gq.question_id
WHERE a.game_id = @game_id AND a.player_id = @player_id
GROUP BY q.category_id
ON CONFLICT (user_id, category_id) DO UPDATE SET
  answered       = user_category_stats.answered + EXCLUDED.answered,
  correct        = user_category_stats.correct  + EXCLUDED.correct,
  last_played_at = now();

-- name: RecomputeCategoryMastery :exec
-- Mastery is plain accuracy over everything the player has answered in that category.
UPDATE user_category_stats
SET mastery = CASE WHEN answered = 0 THEN 0
                   ELSE round(100.0 * correct / answered)::smallint END
WHERE user_id = @user_id;

-- name: ApplyDomainStats :exec
INSERT INTO user_domain_stats (user_id, domain_id, games_played, answered, correct, last_played_at)
SELECT @user_id::uuid, c.domain_id, 1, count(*)::int, count(*) FILTER (WHERE a.is_correct)::int, now()
FROM game_answers a
JOIN game_questions gq ON gq.game_id = a.game_id AND gq.position = a.position
JOIN questions q  ON q.id = gq.question_id
JOIN categories c ON c.id = q.category_id
WHERE a.game_id = @game_id AND a.player_id = @player_id
GROUP BY c.domain_id
ON CONFLICT (user_id, domain_id) DO UPDATE SET
  games_played   = user_domain_stats.games_played + 1,
  answered       = user_domain_stats.answered + EXCLUDED.answered,
  correct        = user_domain_stats.correct  + EXCLUDED.correct,
  last_played_at = now();

-- name: RecomputeDomainMastery :exec
-- A domain's mastery is the average of its categories, so the two bars agree.
UPDATE user_domain_stats s
SET mastery = COALESCE((
  SELECT round(avg(cs.mastery))::smallint
  FROM user_category_stats cs
  JOIN categories c ON c.id = cs.category_id
  WHERE cs.user_id = s.user_id AND c.domain_id = s.domain_id
), 0)
WHERE s.user_id = @user_id;

-- name: BumpDailyActivity :exec
INSERT INTO daily_activity (user_id, day, games, points, correct)
VALUES (@user_id, current_date, 1, @points, @correct)
ON CONFLICT (user_id, day) DO UPDATE SET
  games   = daily_activity.games + 1,
  points  = daily_activity.points + EXCLUDED.points,
  correct = daily_activity.correct + EXCLUDED.correct;

-- name: AddXPEvent :exec
INSERT INTO xp_events (user_id, amount, reason, game_id)
VALUES (@user_id, @amount, @reason, sqlc.narg('game_id'));

-- name: AddUserXP :one
-- users.xp is the cached sum of the ledger; the level is 1 + floor(sqrt(xp / 50)).
UPDATE users
SET xp = xp + @amount,
    level = GREATEST(1, 1 + floor(sqrt(GREATEST(xp + @amount, 0) / 50.0))::int)
WHERE id = @id
RETURNING xp, level;

-- name: BumpLeaderboardWeekly :exec
INSERT INTO leaderboard_weekly (week_start, domain_id, user_id, points, games)
VALUES (date_trunc('week', current_date)::date, sqlc.narg('domain_id'), @user_id, @points, 1)
ON CONFLICT (week_start, user_id) WHERE domain_id IS NULL DO UPDATE SET
  points = leaderboard_weekly.points + EXCLUDED.points,
  games  = leaderboard_weekly.games + 1,
  updated_at = now();

-- name: BumpLeaderboardWeeklyDomain :exec
INSERT INTO leaderboard_weekly (week_start, domain_id, user_id, points, games)
VALUES (date_trunc('week', current_date)::date, @domain_id, @user_id, @points, 1)
ON CONFLICT (week_start, domain_id, user_id) WHERE domain_id IS NOT NULL DO UPDATE SET
  points = leaderboard_weekly.points + EXCLUDED.points,
  games  = leaderboard_weekly.games + 1,
  updated_at = now();

-- name: GamePointsByDomain :many
SELECT c.domain_id, sum(a.points)::int AS points
FROM game_answers a
JOIN game_questions gq ON gq.game_id = a.game_id AND gq.position = a.position
JOIN questions q  ON q.id = gq.question_id
JOIN categories c ON c.id = q.category_id
WHERE a.game_id = @game_id AND a.player_id = @player_id
GROUP BY c.domain_id;

-- ---------------------------------------------------------------- reads

-- name: MyProgress :one
-- Endpoint 90: XP, level, today, the daily goal and the current streak.
SELECT u.xp, u.level,
  COALESCE(s.daily_goal, 5)::int AS daily_goal,
  COALESCE((SELECT games  FROM daily_activity WHERE user_id = u.id AND day = current_date), 0)::int AS games_today,
  COALESCE((SELECT points FROM daily_activity WHERE user_id = u.id AND day = current_date), 0)::int AS points_today,
  COALESCE((SELECT count(*) FROM game_players gp JOIN games g ON g.id = gp.game_id
            WHERE gp.user_id = u.id AND g.status = 'finished'), 0)::int AS games_total,
  COALESCE((SELECT sum(points) FROM daily_activity WHERE user_id = u.id), 0)::int AS points_total,
  COALESCE((
    -- The streak is the run of consecutive days ending today or yesterday.
    SELECT count(*)::int FROM (
      SELECT day, day - (row_number() OVER (ORDER BY day DESC))::int AS grp
      FROM daily_activity
      WHERE user_id = u.id AND games > 0 AND day <= current_date
    ) d
    WHERE d.grp = (
      SELECT day - (row_number() OVER (ORDER BY day DESC))::int
      FROM daily_activity WHERE user_id = u.id AND games > 0 AND day <= current_date
      ORDER BY day DESC LIMIT 1)
      AND (SELECT max(day) FROM daily_activity WHERE user_id = u.id AND games > 0) >= current_date - 1
  ), 0)::int AS streak,
  COALESCE((
    -- The best streak ever: the longest run of days with at least one game (LB-02).
    SELECT max(n)::int FROM (
      SELECT count(*) AS n FROM (
        SELECT day, day - (row_number() OVER (ORDER BY day))::int AS grp
        FROM daily_activity WHERE user_id = u.id AND games > 0
      ) d GROUP BY d.grp
    ) runs
  ), 0)::int AS best_streak,
  COALESCE((SELECT count(*) FROM game_players gp JOIN games g ON g.id = gp.game_id
            WHERE gp.user_id = u.id AND g.status = 'finished'
              AND g.mode = 'duel' AND gp.rank = 1), 0)::int AS duels_won
FROM users u
LEFT JOIN user_settings s ON s.user_id = u.id
WHERE u.id = @id;

-- name: MyActivity :many
-- Endpoint 94: the 12-week grid.
SELECT day, games, points FROM daily_activity
WHERE user_id = @user_id AND day > current_date - @days::int
ORDER BY day;

-- name: MasteryByDomain :many
-- Endpoint 91: one row per domain the player owns, with its mastery and last played.
SELECT d.id, d.slug, d.family, d.color, d.icon, d.motif,
  COALESCE(dt.name, dtf.name) AS name,
  COALESCE(s.mastery, 0)::int      AS mastery,
  COALESCE(s.games_played, 0)::int AS games_played,
  COALESCE(s.answered, 0)::int     AS answered,
  COALESCE(s.correct, 0)::int      AS correct,
  s.last_played_at,
  (SELECT count(*) FROM categories c WHERE c.domain_id = d.id AND c.is_published)::int AS category_count
FROM user_domains ud
JOIN domains d ON d.id = ud.domain_id
LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
LEFT JOIN user_domain_stats s ON s.user_id = ud.user_id AND s.domain_id = d.id
WHERE ud.user_id = @user_id
  AND d.is_published
  AND (NOT @islamic_only::boolean OR d.family = 'islamic')
  AND (@q::text = '' OR COALESCE(dt.name, dtf.name) ILIKE '%' || @q || '%')
ORDER BY
  CASE WHEN @sort::text = 'mastery' THEN COALESCE(s.mastery, 0) END DESC,
  CASE WHEN @sort::text = 'weakest' THEN COALESCE(s.mastery, 0) END ASC,
  CASE WHEN @sort::text = 'played'  THEN COALESCE(s.games_played, 0) END DESC,
  ud.position, d.position
LIMIT @lim OFFSET @off;

-- name: CountMasteryDomains :one
SELECT count(*) FROM user_domains ud
JOIN domains d ON d.id = ud.domain_id
LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE ud.user_id = @user_id AND d.is_published
  AND (NOT @islamic_only::boolean OR d.family = 'islamic')
  AND (@q::text = '' OR COALESCE(dt.name, dtf.name) ILIKE '%' || @q || '%');

-- name: MasteryByCategory :many
-- Endpoint 92: the categories of one domain with the viewer's mastery.
SELECT c.id, c.slug, c.color, c.icon,
  COALESCE(ct.name, ctf.name) AS name,
  COALESCE(s.mastery, 0)::int  AS mastery,
  COALESCE(s.answered, 0)::int AS answered,
  COALESCE(s.correct, 0)::int  AS correct,
  c.question_count
FROM categories c
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN user_category_stats s ON s.category_id = c.id AND s.user_id = @user_id
WHERE c.domain_id = @domain_id AND c.is_published
ORDER BY c.position, c.id;

-- name: TopCategoriesForUser :many
-- Profile: strongest and weakest categories across the player's domains.
SELECT c.id, c.color, c.icon, COALESCE(ct.name, ctf.name) AS name,
  d.id AS domain_id, d.color AS domain_color,
  s.mastery::int AS mastery, s.answered::int AS answered
FROM user_category_stats s
JOIN categories c ON c.id = s.category_id
JOIN domains d    ON d.id = c.domain_id
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
WHERE s.user_id = @user_id AND s.answered > 0
  AND (NOT @islamic_only::boolean OR d.family = 'islamic')
ORDER BY CASE WHEN @weakest::boolean THEN s.mastery END ASC,
         CASE WHEN NOT @weakest::boolean THEN s.mastery END DESC,
         s.answered DESC
LIMIT @lim;

-- name: MyAchievements :many
-- Endpoint 93.
SELECT a.id, a.code, a.icon, a.color, a.target, a.kind,
  COALESCE(t.name, tf.name) AS name,
  COALESCE(t.description, tf.description) AS description,
  COALESCE(ua.progress, 0)::int AS progress,
  ua.unlocked_at
FROM achievements a
LEFT JOIN achievement_translations t  ON t.achievement_id = a.id AND t.lang = @lang
LEFT JOIN achievement_translations tf ON tf.achievement_id = a.id AND tf.lang = 'en'
LEFT JOIN user_achievements ua ON ua.achievement_id = a.id AND ua.user_id = @user_id
ORDER BY a.position, a.id;

-- name: UpsertAchievementProgress :exec
INSERT INTO user_achievements (user_id, achievement_id, progress, unlocked_at)
SELECT @user_id, a.id, @progress,
       CASE WHEN @progress::int >= a.target THEN now() END
FROM achievements a WHERE a.code = @code
ON CONFLICT (user_id, achievement_id) DO UPDATE SET
  progress    = GREATEST(user_achievements.progress, EXCLUDED.progress),
  unlocked_at = COALESCE(user_achievements.unlocked_at, EXCLUDED.unlocked_at);

-- name: Leaderboard :many
-- Endpoint 95. period: week | month | all · domain_id 0 = global · friends_only = my friends and me.
-- The rank is competition ranking, computed by the service from the ordered rows.
SELECT u.id, u.username, u.display_name, u.avatar_url, u.level, u.kind,
       l.points::int AS points, l.games::int AS games
FROM leaderboard_periods l
JOIN users u ON u.id = l.user_id
WHERE l.period = @period::text
  AND l.period_start = CASE @period::text
        WHEN 'week'  THEN date_trunc('week',  current_date)::date
        WHEN 'month' THEN date_trunc('month', current_date)::date
        ELSE DATE '1970-01-01' END
  AND ((@domain_id::bigint = 0 AND l.domain_id IS NULL) OR l.domain_id = @domain_id)
  AND (NOT @friends_only::boolean
       OR l.user_id = @viewer_id
       OR EXISTS (SELECT 1 FROM friendships f
                  WHERE (f.user_a = @viewer_id AND f.user_b = l.user_id)
                     OR (f.user_b = @viewer_id AND f.user_a = l.user_id)))
ORDER BY l.points DESC, u.display_name
LIMIT @lim OFFSET @off;

-- name: MyLeaderboardStanding :one
-- My points and my rank (1 + how many players are strictly ahead of me).
SELECT COALESCE(me.points, 0)::int AS points,
       (SELECT count(*) FROM leaderboard_periods o
         WHERE o.period = @period::text
           AND o.period_start = CASE @period::text
                 WHEN 'week'  THEN date_trunc('week',  current_date)::date
                 WHEN 'month' THEN date_trunc('month', current_date)::date
                 ELSE DATE '1970-01-01' END
           AND ((@domain_id::bigint = 0 AND o.domain_id IS NULL) OR o.domain_id = @domain_id)
           AND o.points > COALESCE(me.points, 0))::int + 1 AS rank
FROM (SELECT 1) one
LEFT JOIN leaderboard_periods me
  ON me.user_id = @viewer_id
 AND me.period = @period::text
 AND me.period_start = CASE @period::text
       WHEN 'week'  THEN date_trunc('week',  current_date)::date
       WHEN 'month' THEN date_trunc('month', current_date)::date
       ELSE DATE '1970-01-01' END
 AND ((@domain_id::bigint = 0 AND me.domain_id IS NULL) OR me.domain_id = @domain_id);

-- name: DomainPlayersThisWeek :one
-- DM-06: the "players this week" number on a domain page.
SELECT count(DISTINCT user_id)::int FROM leaderboard_weekly
WHERE domain_id = @domain_id AND week_start = date_trunc('week', current_date)::date;

-- name: RefreshLeaderboardViews :exec
REFRESH MATERIALIZED VIEW CONCURRENTLY leaderboard_monthly;

-- name: RefreshLeaderboardAlltime :exec
REFRESH MATERIALIZED VIEW CONCURRENTLY leaderboard_alltime;

-- name: MasteryAllCategories :many
-- The profile's mastery accordion: every category of every domain the player owns.
SELECT c.id, c.domain_id, c.slug, c.color, c.icon,
  COALESCE(ct.name, ctf.name) AS name,
  COALESCE(s.mastery, 0)::int  AS mastery,
  COALESCE(s.answered, 0)::int AS answered,
  COALESCE(s.correct, 0)::int  AS correct,
  c.question_count
FROM user_domains ud
JOIN domains d    ON d.id = ud.domain_id AND d.is_published
JOIN categories c ON c.domain_id = d.id AND c.is_published
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN user_category_stats s ON s.category_id = c.id AND s.user_id = ud.user_id
WHERE ud.user_id = @user_id
  AND (NOT @islamic_only::boolean OR d.family = 'islamic')
ORDER BY ud.position, d.position, c.position, c.id;

-- name: PlayersThisWeekByDomain :many
-- How many players touched each domain in the last seven days, for the domain cards
-- a guest sees instead of their own mastery (HM-05).
SELECT s.domain_id, count(DISTINCT s.user_id)::int AS players
FROM user_domain_stats s
WHERE s.last_played_at > now() - interval '7 days'
GROUP BY s.domain_id;

-- Admin back office (AD-01…AD-04). Every write also inserts audit_log, in the same
-- transaction as the change (the service does that, not these statements).

-- name: WriteAudit :exec
INSERT INTO audit_log (actor_id, action, entity, entity_id, diff, ip)
VALUES (sqlc.narg('actor_id'), @action, @entity, sqlc.narg('entity_id'), @diff, sqlc.narg('ip')::inet);

-- name: AuditLog :many
-- A11's list. @q searches what the screen says it searches — the action and the
-- target — and @since is the 24 h / 7 days / 30 days range above the table; both are
-- '' / NULL for "everything".
SELECT a.*, u.display_name AS actor_name, u.username AS actor_username
FROM audit_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE (@actor::text = '' OR u.username::text = @actor)
  AND (@action::text = '' OR a.action = @action)
  AND (@entity::text = '' OR a.entity = @entity)
  AND (@q::text = '' OR a.action ILIKE '%' || @q || '%' OR coalesce(a.entity_id, '') ILIKE '%' || @q || '%')
  AND (sqlc.narg('since')::timestamptz IS NULL OR a.created_at >= sqlc.narg('since'))
ORDER BY a.id DESC
LIMIT @lim OFFSET @off;

-- name: CountAuditLog :one
-- The same WHERE as AuditLog, so "248 entries" counts the rows the filters show.
SELECT count(*) FROM audit_log a
LEFT JOIN users u ON u.id = a.actor_id
WHERE (@actor::text = '' OR u.username::text = @actor)
  AND (@action::text = '' OR a.action = @action)
  AND (@entity::text = '' OR a.entity = @entity)
  AND (@q::text = '' OR a.action ILIKE '%' || @q || '%' OR coalesce(a.entity_id, '') ILIKE '%' || @q || '%')
  AND (sqlc.narg('since')::timestamptz IS NULL OR a.created_at >= sqlc.narg('since'));

-- name: AuditUserNames :many
-- A11's Target column names the row rather than printing its id. One query per page
-- per kind, not one per row.
SELECT id::text AS id, COALESCE(NULLIF(username, ''), display_name) AS name
FROM users WHERE id = ANY(@ids::uuid[]);

-- name: AuditCategoryNames :many
SELECT c.id::text AS id, COALESCE(t.name, tf.name, c.slug) AS name
FROM categories c
LEFT JOIN category_translations t  ON t.category_id = c.id AND t.lang = @lang
LEFT JOIN category_translations tf ON tf.category_id = c.id AND tf.lang = 'en'
WHERE c.id = ANY(@ids::bigint[]);

-- name: AuditDomainNames :many
SELECT d.id::text AS id, COALESCE(t.name, tf.name, d.slug) AS name
FROM domains d
LEFT JOIN domain_translations t  ON t.domain_id = d.id AND t.lang = @lang
LEFT JOIN domain_translations tf ON tf.domain_id = d.id AND tf.lang = 'en'
WHERE d.id = ANY(@ids::bigint[]);

-- name: AuditActions :many
SELECT DISTINCT action FROM audit_log ORDER BY action;

-- name: AuditAreas :many
-- The "All areas" menu: only areas that actually have entries.
SELECT DISTINCT entity FROM audit_log ORDER BY entity;

-- name: AuditActors :many
-- The "Anyone" menu: the staff who have written to the log, newest first.
SELECT u.username, u.display_name, count(*)::int AS entries
FROM audit_log a
JOIN users u ON u.id = a.actor_id
GROUP BY u.username, u.display_name
ORDER BY count(*) DESC;

-- ------------------------------------------------------------------ A01 dashboard

-- name: AdminKPIs :one
SELECT
  (SELECT count(DISTINCT user_id) FROM daily_activity WHERE day >= current_date - 6)::int AS players_week,
  (SELECT count(DISTINCT user_id) FROM daily_activity WHERE day >= current_date - 13 AND day < current_date - 6)::int AS players_week_before,
  (SELECT COALESCE(sum(games), 0) FROM daily_activity WHERE day = current_date)::int AS games_today,
  (SELECT COALESCE(sum(games), 0) FROM daily_activity WHERE day = current_date - 1)::int AS games_yesterday,
  (SELECT count(*) FROM questions WHERE status = 'published' AND deleted_at IS NULL)::int AS questions_live,
  (SELECT count(*) FROM questions WHERE status = 'review' AND deleted_at IS NULL)::int AS questions_review,
  (SELECT count(*) FROM support_tickets WHERE status IN ('open','waiting'))::int AS tickets_open,
  (SELECT count(*) FROM question_reports WHERE status = 'open')::int AS reports_open,
  (SELECT count(*) FROM comment_reports WHERE status = 'open')::int AS comment_reports_open,
  (SELECT count(*) FROM question_duplicates WHERE status = 'open')::int AS duplicates_open,
  (SELECT count(*) FROM users WHERE kind = 'member')::int AS members,
  (SELECT count(*) FROM users WHERE kind = 'guest' AND expires_at > now())::int AS guests_live;

-- name: GamesPerDay :many
SELECT day, COALESCE(sum(games), 0)::int AS games, COALESCE(sum(points), 0)::int AS points
FROM daily_activity
WHERE day > current_date - @days::int
GROUP BY day ORDER BY day;

-- name: AdminAccuracy :one
SELECT COALESCE(sum(correct), 0)::int AS correct, COALESCE(sum(games), 0)::int AS games
FROM daily_activity WHERE day > current_date - @days::int;

-- name: PlayersByDomain :many
SELECT d.id, d.color, COALESCE(dt.name, dtf.name) AS name, count(ud.user_id)::int AS players
FROM domains d
LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
LEFT JOIN user_domains ud ON ud.domain_id = d.id
GROUP BY d.id, dt.name, dtf.name ORDER BY players DESC;

-- name: PlayersByLanguage :many
SELECT lang, count(*)::int AS players FROM users GROUP BY lang ORDER BY players DESC;

-- name: CategoryPerformance :many
-- Which categories players find hardest (A01 and A10).
SELECT c.id, COALESCE(ct.name, ctf.name) AS name, c.color, c.icon, c.question_count,
       COALESCE(sum(s.answered), 0)::int AS answered,
       CASE WHEN COALESCE(sum(s.answered), 0) = 0 THEN 0
            ELSE round(100.0 * sum(s.correct) / sum(s.answered))::int END AS accuracy
FROM categories c
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN user_category_stats s ON s.category_id = c.id
GROUP BY c.id, ct.name, ctf.name
ORDER BY answered DESC, c.position
LIMIT @lim;

-- ------------------------------------------------------------------ A02 users

-- name: AdminUsers :many
-- A02's table: who the account is, what it may do, how much it has played and how
-- well. "status" is the one word the design's Status column prints — a live
-- sanction first, then an invitation nobody has accepted, then active.
SELECT u.id, u.kind, u.role, u.username, u.display_name, u.email, u.lang, u.level, u.xp,
       u.created_at, u.last_seen_at, u.expires_at,
       COALESCE(s.islamic_only, false) AS islamic_only,
       (SELECT count(*) FROM user_domains ud WHERE ud.user_id = u.id)::int AS domains,
       COALESCE(p.games, 0)::int AS games,
       CASE WHEN COALESCE(p.answered, 0) > 0
            THEN (p.correct * 100 / p.answered)::int ELSE -1 END AS accuracy,
       COALESCE(sa.kind::text, '')::text AS sanction,
       CASE
         WHEN sa.kind = 'ban' THEN 'banned'
         WHEN sa.kind = 'suspension' THEN 'suspended'
         WHEN u.kind = 'member' AND u.last_seen_at IS NULL THEN 'invited'
         ELSE 'active'
       END::text AS status
FROM users u
LEFT JOIN user_settings s ON s.user_id = u.id
LEFT JOIN LATERAL (
  SELECT (SELECT count(*) FROM game_players gp JOIN games g ON g.id = gp.game_id
          WHERE gp.user_id = u.id AND g.status = 'finished')::int AS games,
         (SELECT count(*) FILTER (WHERE ga.is_correct) FROM game_answers ga
          JOIN game_players gp ON gp.id = ga.player_id WHERE gp.user_id = u.id)::int AS correct,
         (SELECT count(*) FROM game_answers ga
          JOIN game_players gp ON gp.id = ga.player_id WHERE gp.user_id = u.id)::int AS answered
) p ON true
LEFT JOIN LATERAL (
  SELECT x.kind FROM user_sanctions x
  WHERE x.user_id = u.id AND x.lifted_at IS NULL AND (x.until IS NULL OR x.until > now())
  ORDER BY x.created_at DESC LIMIT 1
) sa ON true
WHERE (@kind::text = '' OR u.kind::text = @kind)
  AND (@role::text = '' OR (@role = 'staff') = (u.role <> 'player'))
  AND (@lang::text = '' OR u.lang = @lang)
  AND (@status::text = '' OR @status = CASE
         WHEN sa.kind = 'ban' THEN 'banned'
         WHEN sa.kind = 'suspension' THEN 'suspended'
         WHEN u.kind = 'member' AND u.last_seen_at IS NULL THEN 'invited'
         ELSE 'active' END)
  AND (@q::text = '' OR u.display_name ILIKE '%' || @q || '%' OR u.username ILIKE '%' || @q || '%' OR u.email ILIKE '%' || @q || '%')
-- One ORDER BY per column the design lets staff sort on; sqlc cannot take an
-- identifier as a parameter, so the choice is made with CASE rather than by
-- pasting a column name into the statement.
ORDER BY
  CASE WHEN @sort::text = 'user'      AND @ascending::bool THEN lower(u.display_name) END ASC,
  CASE WHEN @sort = 'user'      AND NOT @ascending THEN lower(u.display_name) END DESC,
  CASE WHEN @sort = 'role'      AND @ascending THEN u.role::text END ASC,
  CASE WHEN @sort = 'role'      AND NOT @ascending THEN u.role::text END DESC,
  CASE WHEN @sort = 'games'     AND @ascending THEN COALESCE(p.games, 0) END ASC,
  CASE WHEN @sort = 'games'     AND NOT @ascending THEN COALESCE(p.games, 0) END DESC,
  CASE WHEN @sort = 'accuracy'  AND @ascending THEN CASE WHEN COALESCE(p.answered,0) > 0 THEN p.correct * 100 / p.answered ELSE -1 END END ASC,
  CASE WHEN @sort = 'accuracy'  AND NOT @ascending THEN CASE WHEN COALESCE(p.answered,0) > 0 THEN p.correct * 100 / p.answered ELSE -1 END END DESC,
  CASE WHEN @sort = 'last_seen' AND @ascending THEN u.last_seen_at END ASC NULLS FIRST,
  CASE WHEN @sort = 'last_seen' AND NOT @ascending THEN u.last_seen_at END DESC NULLS LAST,
  u.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountAdminUsers :one
SELECT count(*) FROM users u
LEFT JOIN LATERAL (
  SELECT x.kind FROM user_sanctions x
  WHERE x.user_id = u.id AND x.lifted_at IS NULL AND (x.until IS NULL OR x.until > now())
  ORDER BY x.created_at DESC LIMIT 1
) sa ON true
WHERE (@kind::text = '' OR u.kind::text = @kind)
  AND (@role::text = '' OR (@role = 'staff') = (u.role <> 'player'))
  AND (@lang::text = '' OR u.lang = @lang)
  AND (@status::text = '' OR @status = CASE
         WHEN sa.kind = 'ban' THEN 'banned'
         WHEN sa.kind = 'suspension' THEN 'suspended'
         WHEN u.kind = 'member' AND u.last_seen_at IS NULL THEN 'invited'
         ELSE 'active' END)
  AND (@q::text = '' OR u.display_name ILIKE '%' || @q || '%' OR u.username ILIKE '%' || @q || '%' OR u.email ILIKE '%' || @q || '%');

-- name: CountAdminUsersByKind :one
-- The three numbers on the design's segmented control: Everyone, Players, Staff.
SELECT count(*)::int AS everyone,
       count(*) FILTER (WHERE u.role = 'player')::int AS players,
       count(*) FILTER (WHERE u.role <> 'player')::int AS staff
FROM users u;

-- name: SetUserRole :exec
UPDATE users SET role = @role WHERE id = @id;

-- name: AddSanction :one
INSERT INTO user_sanctions (user_id, kind, reason, until, by_id)
VALUES (@user_id, @kind, @reason, sqlc.narg('until'), sqlc.narg('by_id'))
RETURNING *;

-- name: LiftSanctions :exec
UPDATE user_sanctions SET lifted_at = now() WHERE user_id = @user_id AND lifted_at IS NULL;

-- name: UserSanctions :many
SELECT s.*, u.display_name AS by_name FROM user_sanctions s
LEFT JOIN users u ON u.id = s.by_id
WHERE s.user_id = @user_id ORDER BY s.created_at DESC;

-- ------------------------------------------------------------------ A04 domains

-- name: AdminDomains :many
SELECT d.*, COALESCE(dt.name, dtf.name) AS name, COALESCE(dt.description, '') AS description,
       (SELECT count(*) FROM categories c WHERE c.domain_id = d.id)::int AS categories,
       (SELECT count(*) FROM categories c JOIN questions q ON q.category_id = c.id
         WHERE c.domain_id = d.id AND q.status = 'published' AND q.deleted_at IS NULL)::int AS questions,
       (SELECT count(*) FROM user_domains ud WHERE ud.domain_id = d.id)::int AS players
FROM domains d
LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE (@family::text = '' OR d.family::text = @family)
  AND (@q::text = '' OR COALESCE(dt.name, dtf.name, '') ILIKE '%' || @q || '%' OR d.slug ILIKE '%' || @q || '%')
-- A04 sorts by its three numbers and by name; anything else keeps the order staff
-- arranged the domains in, which is the order players see them.
ORDER BY
  CASE WHEN @sort::text = 'name'       AND @ascending::bool THEN lower(COALESCE(dt.name, dtf.name)) END ASC,
  CASE WHEN @sort = 'name'       AND NOT @ascending THEN lower(COALESCE(dt.name, dtf.name)) END DESC,
  CASE WHEN @sort = 'categories' AND @ascending THEN (SELECT count(*) FROM categories c WHERE c.domain_id = d.id) END ASC,
  CASE WHEN @sort = 'categories' AND NOT @ascending THEN (SELECT count(*) FROM categories c WHERE c.domain_id = d.id) END DESC,
  CASE WHEN @sort = 'questions'  AND @ascending THEN (SELECT count(*) FROM categories c JOIN questions q ON q.category_id = c.id
         WHERE c.domain_id = d.id AND q.status = 'published' AND q.deleted_at IS NULL) END ASC,
  CASE WHEN @sort = 'questions'  AND NOT @ascending THEN (SELECT count(*) FROM categories c JOIN questions q ON q.category_id = c.id
         WHERE c.domain_id = d.id AND q.status = 'published' AND q.deleted_at IS NULL) END DESC,
  CASE WHEN @sort = 'players'    AND @ascending THEN (SELECT count(*) FROM user_domains ud WHERE ud.domain_id = d.id) END ASC,
  CASE WHEN @sort = 'players'    AND NOT @ascending THEN (SELECT count(*) FROM user_domains ud WHERE ud.domain_id = d.id) END DESC,
  d.position, d.id
LIMIT NULLIF(@lim::int, 0) OFFSET @off;

-- name: CountAdminDomains :one
-- A04's pager. The same WHERE as AdminDomains, so the count and the page agree.
SELECT count(*)::int FROM domains d
LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE (@family::text = '' OR d.family::text = @family)
  AND (@q::text = '' OR COALESCE(dt.name, dtf.name) ILIKE '%' || @q || '%' OR d.slug ILIKE '%' || @q || '%');

-- name: CountAdminCategories :one
-- A05's pager, counted inside the domain being looked at.
SELECT count(*)::int FROM categories c
JOIN domains d ON d.id = c.domain_id
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
WHERE (@domain_id::bigint = 0 OR c.domain_id = @domain_id)
  AND (@live::text = '' OR (@live = 'live') = c.is_published)
  AND (@q::text = '' OR COALESCE(ct.name, ctf.name) ILIKE '%' || @q || '%' OR c.slug ILIKE '%' || @q || '%');

-- name: CountAdminDomainsByFamily :one
-- The three numbers on A04's segmented control: All, the Islamic family, general.
SELECT count(*)::int AS all_domains,
       count(*) FILTER (WHERE d.family = 'islamic')::int AS islamic,
       count(*) FILTER (WHERE d.family = 'general')::int AS general
FROM domains d;

-- name: GetAdminDomain :one
SELECT * FROM domains WHERE id = @id;

-- name: DomainTexts :many
SELECT * FROM domain_translations WHERE domain_id = @domain_id;

-- name: CreateDomain :one
INSERT INTO domains (slug, family, icon, color, motif, position, is_featured, is_new_until, is_published)
VALUES (@slug, @family, @icon, @color, @motif, @position, @is_featured, sqlc.narg('is_new_until'), @is_published)
RETURNING *;

-- name: UpdateDomain :one
UPDATE domains SET
  slug = COALESCE(sqlc.narg('slug'), slug),
  family = COALESCE(sqlc.narg('family'), family),
  icon = COALESCE(sqlc.narg('icon'), icon),
  color = COALESCE(sqlc.narg('color'), color),
  motif = COALESCE(sqlc.narg('motif'), motif),
  position = COALESCE(sqlc.narg('position'), position),
  is_featured = COALESCE(sqlc.narg('is_featured'), is_featured),
  is_new_until = CASE WHEN @clear_new::boolean THEN NULL ELSE COALESCE(sqlc.narg('is_new_until'), is_new_until) END,
  is_published = COALESCE(sqlc.narg('is_published'), is_published)
WHERE id = @id
RETURNING *;

-- name: UpsertDomainText :exec
INSERT INTO domain_translations (domain_id, lang, name, description)
VALUES (@domain_id, @lang, @name, @description)
ON CONFLICT (domain_id, lang) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description;

-- name: UnfeatureDomains :exec
UPDATE domains SET is_featured = false WHERE id <> @id AND is_featured;

-- ------------------------------------------------------------------ A05 categories

-- name: AdminCategories :many
SELECT c.*, COALESCE(ct.name, ctf.name) AS name,
       COALESCE(dt.name, dtf.name) AS domain_name, d.color AS domain_color, d.family,
       (SELECT count(*) FROM questions q WHERE q.category_id = c.id AND q.deleted_at IS NULL)::int AS questions_total
FROM categories c
JOIN domains d ON d.id = c.domain_id
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf   ON dtf.domain_id = d.id AND dtf.lang = 'en'
WHERE (@domain_id::bigint = 0 OR c.domain_id = @domain_id)
  AND (@live::text = '' OR (@live = 'live') = c.is_published)
  AND (@q::text = '' OR COALESCE(ct.name, ctf.name, '') ILIKE '%' || @q || '%' OR c.slug ILIKE '%' || @q || '%')
-- A05 sorts by name, by the order staff arranged, and by how full a category is.
ORDER BY
  CASE WHEN @sort::text = 'name'      AND @ascending::bool THEN lower(COALESCE(ct.name, ctf.name)) END ASC,
  CASE WHEN @sort = 'name'      AND NOT @ascending THEN lower(COALESCE(ct.name, ctf.name)) END DESC,
  CASE WHEN @sort = 'order'     AND @ascending THEN c.position END ASC,
  CASE WHEN @sort = 'order'     AND NOT @ascending THEN c.position END DESC,
  CASE WHEN @sort = 'questions' AND @ascending THEN (SELECT count(*) FROM questions q WHERE q.category_id = c.id AND q.deleted_at IS NULL) END ASC,
  CASE WHEN @sort = 'questions' AND NOT @ascending THEN (SELECT count(*) FROM questions q WHERE q.category_id = c.id AND q.deleted_at IS NULL) END DESC,
  d.position, c.position, c.id
LIMIT NULLIF(@lim::int, 0) OFFSET @off;

-- name: CountAdminCategoriesByState :one
-- The numbers on A05's segmented control: every category, the live ones, the rest.
SELECT count(*)::int AS all_categories,
       count(*) FILTER (WHERE c.is_published)::int AS live,
       count(*) FILTER (WHERE NOT c.is_published)::int AS retired
FROM categories c
WHERE (@domain_id::bigint = 0 OR c.domain_id = @domain_id);

-- name: GetAdminCategory :one
SELECT * FROM categories WHERE id = @id;

-- name: CategoryTexts :many
SELECT * FROM category_translations WHERE category_id = @category_id;

-- name: CreateCategory :one
INSERT INTO categories (domain_id, slug, icon, color, position, is_published)
VALUES (@domain_id, @slug, @icon, @color, @position, @is_published)
RETURNING *;

-- name: UpdateCategory :one
UPDATE categories SET
  domain_id = COALESCE(sqlc.narg('domain_id'), domain_id),
  slug = COALESCE(sqlc.narg('slug'), slug),
  icon = COALESCE(sqlc.narg('icon'), icon),
  color = COALESCE(sqlc.narg('color'), color),
  position = COALESCE(sqlc.narg('position'), position),
  is_published = COALESCE(sqlc.narg('is_published'), is_published)
WHERE id = @id
RETURNING *;

-- name: UpsertCategoryText :exec
INSERT INTO category_translations (category_id, lang, name) VALUES (@category_id, @lang, @name)
ON CONFLICT (category_id, lang) DO UPDATE SET name = EXCLUDED.name;

-- ------------------------------------------------------------------ A06 questions

-- name: ReviewedToday :one
-- A07's ring: how many of the queue this reviewer has already decided today.
SELECT count(*)::int FROM question_reviews
WHERE reviewer_id = @reviewer_id
  AND created_at >= date_trunc('day', now());

-- name: QuestionAuthor :one
-- The "#104 · submitted by …" line, and the name a "Sent back to …" toast says.
SELECT COALESCE(u.display_name, u.username, '') AS author,
       split_part(COALESCE(u.display_name, u.username, ''), ' ', 1)::text AS first_name,
       q.created_at, q.updated_at, q.source, q.points, q.main_lang
FROM questions q
LEFT JOIN users u ON u.id = q.author_id
WHERE q.id = @id;

-- name: QuestionLooksDuplicate :one
-- The first automatic check. A pair the duplicates job has already found and nobody
-- has dismissed counts as a duplicate; anything else reads as clear.
SELECT EXISTS (
  SELECT 1 FROM question_duplicates
  WHERE (question_a = @id OR question_b = @id) AND status = 'open'
)::bool AS looks_duplicate;

-- name: AdminQuestions :many
SELECT q.id, q.category_id, q.difficulty, q.points, q.correct_index, q.status, q.source,
       q.main_lang, q.published_at, q.created_at,
       COALESCE(t.body, tm.body) AS body,
       COALESCE(ct.name, ctf.name) AS category_name, c.color AS category_color,
       c.icon AS category_icon,
       COALESCE(dt.name, dtf.name) AS domain_name, d.id AS domain_id,
       (SELECT count(*) FROM question_translations x WHERE x.question_id = q.id)::int AS languages,
       (SELECT COALESCE(array_agg(x.lang ORDER BY x.lang), '{}')::text[]
          FROM question_translations x WHERE x.question_id = q.id) AS lang_codes,
       (SELECT COALESCE(round(avg(stars) * 10), 0) FROM question_ratings r WHERE r.question_id = q.id)::int AS stars10,
       (SELECT count(*) FROM question_ratings r WHERE r.question_id = q.id)::int AS ratings,
       -- How often it has been served, and how often it was answered right: the
       -- two numbers A06 sorts on and the design prints as "Plays" and "Accuracy".
       COALESCE(a.plays, 0)::int AS plays,
       CASE WHEN COALESCE(a.plays, 0) > 0 THEN (a.correct * 100 / a.plays)::int ELSE -1 END AS accuracy
FROM questions q
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
LEFT JOIN question_translations t  ON t.question_id = q.id AND t.lang = @lang
LEFT JOIN question_translations tm ON tm.question_id = q.id AND tm.lang = q.main_lang
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang
LEFT JOIN domain_translations dtf   ON dtf.domain_id = d.id AND dtf.lang = 'en'
LEFT JOIN LATERAL (
  SELECT count(*)::int AS plays,
         count(*) FILTER (WHERE ga.is_correct)::int AS correct
  FROM game_answers ga
  JOIN game_questions gq ON gq.game_id = ga.game_id AND gq.position = ga.position
  WHERE gq.question_id = q.id
) a ON true
WHERE q.deleted_at IS NULL
  AND (@status::text = '' OR q.status::text = @status)
  AND (@domain_id::bigint = 0 OR d.id = @domain_id)
  AND (@category_id::bigint = 0 OR c.id = @category_id)
  AND (@difficulty::int = 0 OR q.difficulty = @difficulty)
  -- A06's "Translations" filter: all three, one missing, or none but the main one.
  AND (@langs::text = '' OR
       (@langs = 'all'     AND (SELECT count(*) FROM question_translations x WHERE x.question_id = q.id) = 3) OR
       (@langs = 'missing' AND (SELECT count(*) FROM question_translations x WHERE x.question_id = q.id) < 3))
  AND (@q::text = '' OR COALESCE(t.body, tm.body) ILIKE '%' || @q || '%' OR q.id::text = @q)
ORDER BY
  CASE WHEN @sort::text = 'body'       AND @ascending::bool THEN lower(COALESCE(t.body, tm.body)) END ASC,
  CASE WHEN @sort = 'body'       AND NOT @ascending THEN lower(COALESCE(t.body, tm.body)) END DESC,
  CASE WHEN @sort = 'difficulty' AND @ascending THEN q.difficulty END ASC,
  CASE WHEN @sort = 'difficulty' AND NOT @ascending THEN q.difficulty END DESC,
  CASE WHEN @sort = 'accuracy'   AND @ascending THEN CASE WHEN COALESCE(a.plays,0) > 0 THEN a.correct * 100 / a.plays ELSE -1 END END ASC,
  CASE WHEN @sort = 'accuracy'   AND NOT @ascending THEN CASE WHEN COALESCE(a.plays,0) > 0 THEN a.correct * 100 / a.plays ELSE -1 END END DESC,
  CASE WHEN @sort = 'plays'      AND @ascending THEN COALESCE(a.plays, 0) END ASC,
  CASE WHEN @sort = 'plays'      AND NOT @ascending THEN COALESCE(a.plays, 0) END DESC,
  q.id DESC
LIMIT @lim OFFSET @off;

-- name: CountAdminQuestions :one
SELECT count(*) FROM questions q
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
LEFT JOIN question_translations t  ON t.question_id = q.id AND t.lang = @lang
LEFT JOIN question_translations tm ON tm.question_id = q.id AND tm.lang = q.main_lang
WHERE q.deleted_at IS NULL
  AND (@status::text = '' OR q.status::text = @status)
  AND (@domain_id::bigint = 0 OR d.id = @domain_id)
  AND (@category_id::bigint = 0 OR c.id = @category_id)
  AND (@difficulty::int = 0 OR q.difficulty = @difficulty)
  AND (@langs::text = '' OR
       (@langs = 'all'     AND (SELECT count(*) FROM question_translations x WHERE x.question_id = q.id) = 3) OR
       (@langs = 'missing' AND (SELECT count(*) FROM question_translations x WHERE x.question_id = q.id) < 3))
  AND (@q::text = '' OR COALESCE(t.body, tm.body) ILIKE '%' || @q || '%' OR q.id::text = @q);

-- name: QuestionCountsByStatus :many
SELECT status, count(*)::int AS n FROM questions WHERE deleted_at IS NULL GROUP BY status;

-- name: GetAdminQuestion :one
SELECT * FROM questions WHERE id = @id;

-- name: QuestionTexts :many
SELECT * FROM question_translations WHERE question_id = @question_id;

-- name: CreateQuestion :one
INSERT INTO questions (category_id, difficulty, points, correct_index, status, source, main_lang, author_id)
VALUES (@category_id, @difficulty, @points, @correct_index, @status, @source, @main_lang, sqlc.narg('author_id'))
RETURNING *;

-- name: UpdateQuestion :one
UPDATE questions SET
  category_id   = COALESCE(sqlc.narg('category_id'), category_id),
  difficulty    = COALESCE(sqlc.narg('difficulty'), difficulty),
  points        = COALESCE(sqlc.narg('points'), points),
  correct_index = COALESCE(sqlc.narg('correct_index'), correct_index),
  source        = COALESCE(sqlc.narg('source'), source),
  main_lang     = COALESCE(sqlc.narg('main_lang'), main_lang)
WHERE id = @id
RETURNING *;

-- name: SetQuestionStatus :one
UPDATE questions
SET status = @status,
    published_at = CASE WHEN @status::question_status = 'published' THEN COALESCE(published_at, now()) ELSE published_at END
WHERE id = @id
RETURNING *;

-- name: BulkSetQuestionStatus :execrows
UPDATE questions SET status = @status WHERE id = ANY (@ids::bigint[]);

-- name: UpsertQuestionText :exec
INSERT INTO question_translations (question_id, lang, body, answers, explanation)
VALUES (@question_id, @lang, @body, @answers, @explanation)
ON CONFLICT (question_id, lang) DO UPDATE
SET body = EXCLUDED.body, answers = EXCLUDED.answers, explanation = EXCLUDED.explanation;

-- name: AddQuestionRevision :exec
INSERT INTO question_revisions (question_id, editor_id, snapshot, note)
VALUES (@question_id, sqlc.narg('editor_id'), @snapshot, @note);

-- name: AddQuestionReview :exec
INSERT INTO question_reviews (question_id, reviewer_id, decision, note)
VALUES (@question_id, sqlc.narg('reviewer_id'), @decision, @note);

-- name: RefreshCategoryCount :exec
UPDATE categories c SET question_count = (
  SELECT count(*) FROM questions q
  WHERE q.category_id = c.id AND q.status = 'published' AND q.deleted_at IS NULL)
WHERE c.id = @id;

-- ------------------------------------------------------------------ A08 comments

-- name: AdminComments :many
-- A08. The screen quotes the question each comment is on, so a moderator can judge it
-- without opening anything, and says which language it was written in.
--
-- @resolved_only is its own flag rather than a status: "resolved" means a comment that
-- was reported and whose reports have all been dealt with. Reading it as "every
-- visible comment" — which is what the absence of this flag used to mean — put the
-- whole bank in a tab labelled Resolved.
SELECT cm.id, cm.question_id, cm.body, cm.status, cm.created_at, cm.lang,
       u.id AS author_id, u.display_name, u.username,
       (SELECT count(*) FROM comment_reports r WHERE r.comment_id = cm.id AND r.status = 'open')::int AS reports,
       COALESCE(qt.body, qtf.body, '')::text AS question_body
FROM question_comments cm
LEFT JOIN users u ON u.id = cm.user_id
LEFT JOIN question_translations qt  ON qt.question_id = cm.question_id AND qt.lang = @lang
LEFT JOIN question_translations qtf ON qtf.question_id = cm.question_id AND qtf.lang = 'en'
WHERE (@status::text = '' OR cm.status = @status)
  AND (NOT @reported_only::boolean OR EXISTS (SELECT 1 FROM comment_reports r WHERE r.comment_id = cm.id AND r.status = 'open'))
  AND (NOT @resolved_only::boolean OR (
        EXISTS (SELECT 1 FROM comment_reports r WHERE r.comment_id = cm.id)
        AND NOT EXISTS (SELECT 1 FROM comment_reports r WHERE r.comment_id = cm.id AND r.status = 'open')))
  AND (@comment_lang::text = '' OR cm.lang = @comment_lang)
  AND (@q::text = '' OR cm.body ILIKE '%' || @q || '%' OR u.display_name ILIKE '%' || @q || '%')
ORDER BY reports DESC, cm.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountAdminCommentsByTab :one
-- A09's four tabs: what is waiting, what is only flagged, what has been dealt with
-- and what is out of sight.
SELECT count(*) FILTER (WHERE cm.status = 'visible' AND rep.open > 0)::int AS needs_action,
       count(*) FILTER (WHERE rep.flagged > 0)::int                       AS flagged,
       count(*) FILTER (WHERE cm.status = 'visible' AND rep.open = 0)::int AS resolved,
       count(*) FILTER (WHERE cm.status = 'hidden')::int                  AS hidden
FROM question_comments cm
LEFT JOIN LATERAL (
  SELECT count(*) FILTER (WHERE r.status = 'open')::int AS open,
         count(*)::int AS flagged
  FROM comment_reports r WHERE r.comment_id = cm.id
) rep ON true;

-- name: CountAdminComments :one
SELECT count(*) FROM question_comments cm
LEFT JOIN users u ON u.id = cm.user_id
WHERE (@status::text = '' OR cm.status = @status)
  AND (NOT @reported_only::boolean OR EXISTS (SELECT 1 FROM comment_reports r WHERE r.comment_id = cm.id AND r.status = 'open'))
  AND (NOT @resolved_only::boolean OR (
        EXISTS (SELECT 1 FROM comment_reports r WHERE r.comment_id = cm.id)
        AND NOT EXISTS (SELECT 1 FROM comment_reports r WHERE r.comment_id = cm.id AND r.status = 'open')))
  AND (@comment_lang::text = '' OR cm.lang = @comment_lang)
  AND (@q::text = '' OR cm.body ILIKE '%' || @q || '%' OR u.display_name ILIKE '%' || @q || '%');

-- name: SetCommentStatus :exec
UPDATE question_comments SET status = @status WHERE id = @id;

-- name: ResolveCommentReports :exec
UPDATE comment_reports SET status = @status WHERE comment_id = @comment_id AND status = 'open';

-- ------------------------------------------------------------------ A09 rated poorly

-- name: RatingSummary :one
-- A09's three cards, over one window. "Positive" is three stars or more: the player
-- screen asks for a thumb, and the bank stores it as stars, so the line is drawn
-- where the thumb was.
SELECT
  count(*)::int AS ratings,
  COALESCE(round(count(*) FILTER (WHERE r.stars >= 3) * 100.0 / NULLIF(count(*), 0)), 0)::int AS positive_percent,
  count(*) FILTER (WHERE r.reason <> '')::int AS reasons_given,
  COALESCE((SELECT r2.reason FROM question_ratings r2
             WHERE r2.reason <> '' AND r2.created_at >= now() - make_interval(days => @days::int)
             GROUP BY r2.reason ORDER BY count(*) DESC, r2.reason LIMIT 1), '')::text AS top_reason,
  COALESCE((SELECT count(*) FROM question_ratings r3
             WHERE r3.reason <> '' AND r3.created_at >= now() - make_interval(days => @days::int)
               AND r3.reason = (SELECT r4.reason FROM question_ratings r4
                                 WHERE r4.reason <> '' AND r4.created_at >= now() - make_interval(days => @days::int)
                                 GROUP BY r4.reason ORDER BY count(*) DESC, r4.reason LIMIT 1)), 0)::int AS top_reason_count
FROM question_ratings r
WHERE r.created_at >= now() - make_interval(days => @days::int);

-- name: RatedPoorlyDetailed :many
-- The list A09 draws: how a question's ratings split, and which reasons players gave,
-- worst share of negative first. The reason filter matches what the chips show.
SELECT q.id, COALESCE(t.body, tm.body) AS body,
       COALESCE(ct.name, ctf.name) AS category_name,
       c.color AS category_color, c.icon AS category_icon, q.status,
       count(*)::int AS ratings,
       count(*) FILTER (WHERE r.stars >= 3)::int AS up,
       count(*) FILTER (WHERE r.stars < 3)::int AS down,
       COALESCE(round(count(*) FILTER (WHERE r.stars < 3) * 100.0 / NULLIF(count(*), 0)), 0)::int AS down_percent,
       COALESCE(jsonb_object_agg(r.reason, 1) FILTER (WHERE r.reason <> ''), '{}')::jsonb AS reason_keys,
       COALESCE((SELECT jsonb_object_agg(x.reason, x.n)
                   FROM (SELECT r5.reason, count(*)::int AS n FROM question_ratings r5
                          WHERE r5.question_id = q.id AND r5.reason <> '' GROUP BY r5.reason) x), '{}')::jsonb AS reasons
FROM question_ratings r
JOIN questions q  ON q.id = r.question_id AND q.deleted_at IS NULL
JOIN categories c ON c.id = q.category_id
LEFT JOIN question_translations t  ON t.question_id = q.id AND t.lang = @lang
LEFT JOIN question_translations tm ON tm.question_id = q.id AND tm.lang = q.main_lang
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
WHERE (@reason::text = '' OR EXISTS (
        SELECT 1 FROM question_ratings r6 WHERE r6.question_id = q.id AND r6.reason = @reason))
GROUP BY q.id, t.body, tm.body, ct.name, ctf.name, c.color, c.icon, q.status
-- The threshold the screen names: more than a quarter of players unhappy.
HAVING count(*) FILTER (WHERE r.stars < 3) * 100.0 / NULLIF(count(*), 0) >= 25
ORDER BY count(*) FILTER (WHERE r.stars < 3) * 1.0 / NULLIF(count(*), 0) DESC, count(*) DESC
LIMIT @lim OFFSET @off;

-- name: RatingReasons :many
-- The segmented control, built from what players have actually picked rather than a
-- list in the code (§14).
SELECT reason, count(*)::int AS n FROM question_ratings
WHERE reason <> '' GROUP BY reason ORDER BY count(*) DESC;

-- name: CountRatedPoorly :one
-- The pager's total. It repeats RatedPoorly's FROM and HAVING on purpose: a count
-- taken over different rows than the page is a pager that lies.
SELECT count(*)::int FROM (
  SELECT q.id
  FROM question_ratings r
  JOIN questions q  ON q.id = r.question_id AND q.deleted_at IS NULL
  JOIN categories c ON c.id = q.category_id
  GROUP BY q.id
  HAVING avg(r.stars) <= 3 AND count(*) >= 1
) rated;

-- name: RatedPoorly :many
SELECT q.id, COALESCE(t.body, tm.body) AS body,
       COALESCE(ct.name, ctf.name) AS category_name, c.color AS category_color,
       q.status,
       round(avg(r.stars)::numeric, 1)::float8 AS stars,
       count(*)::int AS ratings,
       count(*) FILTER (WHERE r.stars <= 2)::int AS low,
       string_agg(DISTINCT NULLIF(r.reason, ''), ', ') AS reasons
FROM question_ratings r
JOIN questions q  ON q.id = r.question_id AND q.deleted_at IS NULL
JOIN categories c ON c.id = q.category_id
LEFT JOIN question_translations t  ON t.question_id = q.id AND t.lang = @lang
LEFT JOIN question_translations tm ON tm.question_id = q.id AND tm.lang = q.main_lang
LEFT JOIN category_translations ct  ON ct.category_id = c.id AND ct.lang = @lang
LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.lang = 'en'
GROUP BY q.id, t.body, tm.body, ct.name, ctf.name, c.color
HAVING avg(r.stars) <= 3 AND count(*) >= 1
ORDER BY avg(r.stars), count(*) DESC
LIMIT @lim OFFSET @off;

-- ------------------------------------------------------------------ A10 content health

-- name: TranslationCoverage :many
SELECT l.code,
       count(t.question_id)::int AS translated,
       (SELECT count(*) FROM questions q WHERE q.deleted_at IS NULL)::int AS total
FROM languages l
LEFT JOIN question_translations t ON t.lang = l.code
LEFT JOIN questions q ON q.id = t.question_id AND q.deleted_at IS NULL
GROUP BY l.code ORDER BY l.code;

-- name: CountOpenDuplicates :one
-- A10 showed the first 20 pairs and said nothing about the rest; this is what the
-- pager under the list counts.
SELECT count(*)::int FROM question_duplicates WHERE status = 'open';

-- name: CountOpenReports :one
SELECT count(*)::int FROM question_reports WHERE status = 'open';

-- name: OpenDuplicates :many
SELECT d.question_a, d.question_b, d.similarity,
       COALESCE(ta.body, tma.body) AS body_a,
       COALESCE(tb.body, tmb.body) AS body_b
FROM question_duplicates d
JOIN questions qa ON qa.id = d.question_a
JOIN questions qb ON qb.id = d.question_b
LEFT JOIN question_translations ta  ON ta.question_id = qa.id AND ta.lang = @lang
LEFT JOIN question_translations tma ON tma.question_id = qa.id AND tma.lang = qa.main_lang
LEFT JOIN question_translations tb  ON tb.question_id = qb.id AND tb.lang = @lang
LEFT JOIN question_translations tmb ON tmb.question_id = qb.id AND tmb.lang = qb.main_lang
WHERE d.status = 'open'
ORDER BY d.similarity DESC
LIMIT @lim OFFSET @off;

-- name: SetDuplicateStatus :exec
UPDATE question_duplicates SET status = @status WHERE question_a = @question_a AND question_b = @question_b;

-- name: FindDuplicates :exec
-- The worker job: questions whose body is very close to another's in the same language.
INSERT INTO question_duplicates (question_a, question_b, similarity)
SELECT LEAST(a.question_id, b.question_id), GREATEST(a.question_id, b.question_id),
       similarity(a.body, b.body)
FROM question_translations a
JOIN question_translations b
  ON b.lang = a.lang AND b.question_id > a.question_id AND similarity(a.body, b.body) > @threshold::real
ON CONFLICT DO NOTHING;

-- name: OpenReports :many
SELECT r.id, r.question_id, r.reason, r.note, r.created_at, r.lang,
       u.display_name, COALESCE(t.body, tm.body) AS body
FROM question_reports r
JOIN questions q ON q.id = r.question_id
LEFT JOIN users u ON u.id = r.user_id
LEFT JOIN question_translations t  ON t.question_id = q.id AND t.lang = @lang
LEFT JOIN question_translations tm ON tm.question_id = q.id AND tm.lang = q.main_lang
WHERE r.status = 'open'
ORDER BY r.created_at DESC
LIMIT @lim OFFSET @off;

-- name: SetReportStatus :exec
UPDATE question_reports SET status = @status, resolved_by = sqlc.narg('resolved_by') WHERE id = @id;

-- name: IslamicOnlyShare :one
-- A01: how many members keep "Islamic content only" on, as a percentage.
SELECT COALESCE(
  round(100.0 * count(*) FILTER (WHERE s.islamic_only) / NULLIF(count(*), 0)), 0
)::int
FROM users u JOIN user_settings s ON s.user_id = u.id
WHERE u.kind = 'member';

-- name: AdminUserDetail :one
-- A02b, the user drawer: the account, its settings and the numbers over them.
SELECT u.id, u.kind, u.role, u.username, u.display_name, u.email, u.avatar_url,
       u.lang, u.level, u.xp, u.created_at, u.last_seen_at, u.expires_at,
       COALESCE(s.theme, 'system')::text  AS theme,
       COALESCE(s.daily_goal, 5)::int     AS daily_goal,
       COALESCE(s.islamic_only, false)    AS islamic_only,
       COALESCE((SELECT string_agg(DISTINCT i.provider, ', ') FROM auth_identities i WHERE i.user_id = u.id), '')::text AS providers,
       COALESCE((SELECT count(*) FROM game_players gp JOIN games g ON g.id = gp.game_id
                 WHERE gp.user_id = u.id AND g.status = 'finished'), 0)::int AS games,
       -- Accuracy over everything they have played: right answers out of answers
       -- given, read from the games themselves rather than a running total.
       COALESCE((SELECT count(*) FILTER (WHERE ga.is_correct) FROM game_answers ga
                 JOIN game_players gp ON gp.id = ga.player_id
                 WHERE gp.user_id = u.id), 0)::int AS correct,
       COALESCE((SELECT count(*) FROM game_answers ga
                 JOIN game_players gp ON gp.id = ga.player_id
                 WHERE gp.user_id = u.id), 0)::int AS answered,
       COALESCE((SELECT count(*) FROM user_domains ud WHERE ud.user_id = u.id), 0)::int AS domains
FROM users u
LEFT JOIN user_settings s ON s.user_id = u.id
WHERE u.id = @id;

-- name: AdminUserDomains :many
-- The domains this player follows, in their own order, as the drawer's chips.
SELECT d.id, d.slug, d.color, d.icon, d.family, ud.source::text AS source,
       COALESCE(t.name, te.name, d.slug)::text AS name
FROM user_domains ud
JOIN domains d ON d.id = ud.domain_id
LEFT JOIN domain_translations t  ON t.domain_id = d.id AND t.lang = @lang::text
LEFT JOIN domain_translations te ON te.domain_id = d.id AND te.lang = 'en'
WHERE ud.user_id = @user_id
ORDER BY ud.position, d.id;

-- name: AdminSetUserSettings :exec
-- The staff side of a player's configuration (A02b). The row is created with the
-- account, so this only ever updates it.
UPDATE user_settings
SET theme = @theme, daily_goal = @daily_goal, islamic_only = @islamic_only, updated_at = now()
WHERE user_id = @user_id;

-- name: AdminSetUserLang :exec
UPDATE users SET lang = @lang WHERE id = @id;

-- name: AdminReplaceUserDomains :exec
DELETE FROM user_domains WHERE user_id = @user_id;

-- name: AdminAddUserDomain :exec
INSERT INTO user_domains (user_id, domain_id, position, source)
VALUES (@user_id, @domain_id, @position, @source)
ON CONFLICT (user_id, domain_id) DO NOTHING;

-- ------------------------------------------------------------------ CSV exports

-- name: ExportDomains :many
-- A04: every domain with its three names, for the CSV export.
SELECT d.id, d.slug, d.family::text AS family, d.icon, d.color, d.motif, d.position,
       d.is_featured, d.is_published,
       COALESCE(en.name, '')::text AS name_en, COALESCE(ar.name, '')::text AS name_ar,
       COALESCE(fr.name, '')::text AS name_fr,
       COALESCE(en.description, '')::text AS description_en,
       COALESCE(ar.description, '')::text AS description_ar,
       COALESCE(fr.description, '')::text AS description_fr
FROM domains d
LEFT JOIN domain_translations en ON en.domain_id = d.id AND en.lang = 'en'
LEFT JOIN domain_translations ar ON ar.domain_id = d.id AND ar.lang = 'ar'
LEFT JOIN domain_translations fr ON fr.domain_id = d.id AND fr.lang = 'fr'
ORDER BY d.position, d.id;

-- name: ExportCategories :many
SELECT c.id, c.slug, c.icon, c.color, c.position, c.is_published,
       d.slug::text AS domain_slug,
       COALESCE(en.name, '')::text AS name_en, COALESCE(ar.name, '')::text AS name_ar,
       COALESCE(fr.name, '')::text AS name_fr
FROM categories c
JOIN domains d ON d.id = c.domain_id
LEFT JOIN category_translations en ON en.category_id = c.id AND en.lang = 'en'
LEFT JOIN category_translations ar ON ar.category_id = c.id AND ar.lang = 'ar'
LEFT JOIN category_translations fr ON fr.category_id = c.id AND fr.lang = 'fr'
WHERE (@domain_id::bigint = 0 OR c.domain_id = @domain_id)
ORDER BY d.position, c.position, c.id;

-- name: ExportQuestions :many
-- A06: the bank as a file, in the same columns the import reads back.
SELECT q.id, d.slug::text AS domain_slug, c.slug::text AS category_slug,
       q.difficulty, q.points, q.correct_index, q.status::text AS status,
       q.main_lang, COALESCE(q.source, '')::text AS source,
       COALESCE(en.body, '')::text AS body_en, COALESCE(en.answers, '{}')::text[] AS answers_en,
       COALESCE(en.explanation, '')::text AS explanation_en,
       COALESCE(ar.body, '')::text AS body_ar, COALESCE(ar.answers, '{}')::text[] AS answers_ar,
       COALESCE(ar.explanation, '')::text AS explanation_ar,
       COALESCE(fr.body, '')::text AS body_fr, COALESCE(fr.answers, '{}')::text[] AS answers_fr,
       COALESCE(fr.explanation, '')::text AS explanation_fr
FROM questions q
JOIN categories c ON c.id = q.category_id
JOIN domains d    ON d.id = c.domain_id
LEFT JOIN question_translations en ON en.question_id = q.id AND en.lang = 'en'
LEFT JOIN question_translations ar ON ar.question_id = q.id AND ar.lang = 'ar'
LEFT JOIN question_translations fr ON fr.question_id = q.id AND fr.lang = 'fr'
WHERE (@category_id::bigint = 0 OR q.category_id = @category_id)
  AND (@status::text = '' OR q.status::text = @status)
ORDER BY q.id;

-- name: ExportUsers :many
-- A02: the account list as a file. No password, no token: only what the table shows.
SELECT u.id, u.kind::text AS kind, u.role::text AS role,
       COALESCE(u.username, '')::text AS username, u.display_name,
       COALESCE(u.email, '')::text AS email, u.lang, u.level, u.xp,
       u.created_at, u.last_seen_at,
       COALESCE(s.islamic_only, false) AS islamic_only,
       (SELECT count(*) FROM user_domains ud WHERE ud.user_id = u.id)::int AS domains
FROM users u
LEFT JOIN user_settings s ON s.user_id = u.id
ORDER BY u.created_at DESC;

-- name: CategoryIDBySlug :one
SELECT c.id FROM categories c JOIN domains d ON d.id = c.domain_id
WHERE c.slug = @slug AND d.slug = @domain_slug;

-- name: DomainIDBySlug :one
SELECT id FROM domains WHERE slug = @slug;

-- name: QuestionIDByBody :one
-- The import updates a question it has already written rather than doubling it: the
-- main-language body inside one category is the key a file can be re-imported on.
SELECT q.id FROM questions q
JOIN question_translations t ON t.question_id = q.id AND t.lang = q.main_lang
WHERE q.category_id = @category_id AND lower(t.body) = lower(@body::text)
LIMIT 1;

-- name: ReopenCommentReports :exec
-- A comment settled by mistake goes back to Needs action.
UPDATE comment_reports SET status = 'open' WHERE comment_id = @comment_id;

-- name: CommentAuthor :one
SELECT user_id FROM question_comments WHERE id = @id;

-- Catalog and Your domains.
-- Names fall back to English when a translation is missing.

-- name: ListDomains :many
-- scope=mine → only_mine = true (ordered by the player's own order); scope=all → every published domain.
SELECT d.id, d.slug, d.family, d.icon, d.color, d.motif, d.is_featured,
       (d.is_new_until IS NOT NULL AND d.is_new_until >= current_date)::bool AS is_new,
       COALESCE(t.name, te.name, d.slug)::text AS name,
       COALESCE(t.description, te.description, '')::text AS description,
       (SELECT count(*) FROM categories c WHERE c.domain_id = d.id AND c.is_published)::int AS category_count,
       (SELECT COALESCE(sum(c.question_count), 0) FROM categories c WHERE c.domain_id = d.id AND c.is_published)::int AS question_count,
       (ud.user_id IS NOT NULL)::bool AS is_mine
FROM domains d
LEFT JOIN domain_translations t  ON t.domain_id = d.id AND t.lang = @lang::text
LEFT JOIN domain_translations te ON te.domain_id = d.id AND te.lang = 'en'
LEFT JOIN user_domains ud ON ud.domain_id = d.id AND ud.user_id = sqlc.narg('user_id')::uuid
WHERE d.is_published
  AND (NOT @only_mine::bool OR ud.user_id IS NOT NULL)
  AND (NOT @islamic_only::bool OR d.family = 'islamic')
  AND (@q::text = '' OR unaccent(COALESCE(t.name, te.name)) ILIKE '%' || unaccent(@q::text) || '%')
-- Your domains keep the player's own order; the catalog is A–Z, as the Explore sheet
-- says. The Add sheet lifts the ones already on the player's home to the top, so what
-- they picked stays in front of them across every page of the list (O02).
ORDER BY CASE WHEN @only_mine::bool THEN ud.position END,
         CASE WHEN @mine_first::bool AND ud.user_id IS NOT NULL THEN 0 ELSE 1 END,
         CASE WHEN @alpha::bool THEN COALESCE(t.name, te.name) END,
         d.position, d.id
LIMIT @lim::int OFFSET @off::int;

-- name: CountDomains :one
SELECT count(*)::int
FROM domains d
LEFT JOIN domain_translations t  ON t.domain_id = d.id AND t.lang = @lang::text
LEFT JOIN domain_translations te ON te.domain_id = d.id AND te.lang = 'en'
LEFT JOIN user_domains ud ON ud.domain_id = d.id AND ud.user_id = sqlc.narg('user_id')::uuid
WHERE d.is_published
  AND (NOT @only_mine::bool OR ud.user_id IS NOT NULL)
  AND (NOT @islamic_only::bool OR d.family = 'islamic')
  AND (@q::text = '' OR unaccent(COALESCE(t.name, te.name)) ILIKE '%' || unaccent(@q::text) || '%');

-- name: GetDomain :one
SELECT d.id, d.slug, d.family, d.icon, d.color, d.motif, d.is_featured,
       (d.is_new_until IS NOT NULL AND d.is_new_until >= current_date)::bool AS is_new,
       COALESCE(t.name, te.name, d.slug)::text AS name,
       COALESCE(t.description, te.description, '')::text AS description,
       (SELECT count(*) FROM categories c WHERE c.domain_id = d.id AND c.is_published)::int AS category_count,
       (SELECT COALESCE(sum(c.question_count), 0) FROM categories c WHERE c.domain_id = d.id AND c.is_published)::int AS question_count,
       EXISTS (SELECT 1 FROM user_domains ud WHERE ud.domain_id = d.id AND ud.user_id = sqlc.narg('user_id')::uuid)::bool AS is_mine
FROM domains d
LEFT JOIN domain_translations t  ON t.domain_id = d.id AND t.lang = @lang::text
LEFT JOIN domain_translations te ON te.domain_id = d.id AND te.lang = 'en'
WHERE d.id = @id AND d.is_published;

-- name: ListCategories :many
SELECT c.id, c.domain_id, c.slug, c.icon, c.color, c.question_count,
       COALESCE(t.name, te.name, c.slug)::text AS name
FROM categories c
LEFT JOIN category_translations t  ON t.category_id = c.id AND t.lang = @lang::text
LEFT JOIN category_translations te ON te.category_id = c.id AND te.lang = 'en'
WHERE c.domain_id = @domain_id AND c.is_published
ORDER BY c.position, c.id;

-- name: CountPublishedDomains :one
-- How many of the given ids are real, published domains (validates PUT /me/domains).
SELECT count(*)::int FROM domains WHERE id = ANY(@ids::bigint[]) AND is_published;

-- name: DomainFamily :one
SELECT family FROM domains WHERE id = @id AND is_published;

-- name: DefaultDomainID :one
-- Starter domain for a new player who has not picked any yet: the flagship.
SELECT id FROM domains WHERE is_published ORDER BY is_featured DESC, position, id LIMIT 1;

-- name: CountUserDomains :one
SELECT count(*)::int FROM user_domains WHERE user_id = @user_id;

-- name: AddUserDomain :execrows
INSERT INTO user_domains (user_id, domain_id, position, source)
VALUES (
  @user_id, @domain_id,
  (SELECT COALESCE(max(position), 0) + 1 FROM user_domains WHERE user_id = @user_id),
  @source
)
ON CONFLICT (user_id, domain_id) DO NOTHING;

-- name: InsertUserDomain :exec
INSERT INTO user_domains (user_id, domain_id, position, source)
VALUES (@user_id, @domain_id, @position, @source);

-- name: DeleteUserDomain :execrows
DELETE FROM user_domains WHERE user_id = @user_id AND domain_id = @domain_id;

-- name: DeleteAllUserDomains :exec
DELETE FROM user_domains WHERE user_id = @user_id;

-- name: CountPublishedDomainsAll :one
-- Used by the welcome screen ("and N more domains to explore").
SELECT count(*) FROM domains WHERE is_published;

-- name: ListCategoriesForDomains :many
-- The category chips of every "Your domains" card on Home (HM-05), in one round trip.
SELECT c.id, c.domain_id, c.slug, c.icon, c.color, c.question_count,
       COALESCE(t.name, tf.name) AS name
FROM categories c
LEFT JOIN category_translations t  ON t.category_id = c.id AND t.lang = @lang
LEFT JOIN category_translations tf ON tf.category_id = c.id AND tf.lang = 'en'
WHERE c.is_published AND c.domain_id = ANY (@domain_ids::bigint[])
ORDER BY c.domain_id, c.position, c.id;

-- name: CountPublishedCategories :one
-- The "6 domains · 23 categories" line of the Explore sheet (O01).
SELECT count(*) FROM categories c
JOIN domains d ON d.id = c.domain_id
WHERE c.is_published AND d.is_published;

-- name: GetCategory :one
-- Endpoint 20: one category, with the domain that themes its screens.
SELECT c.id, c.domain_id, c.slug, c.icon, c.color, c.question_count,
       COALESCE(t.name, te.name, c.slug)::text AS name,
       d.slug::text AS domain_slug,
       d.family AS domain_family,
       COALESCE(dt.name, dte.name, d.slug)::text AS domain_name,
       EXISTS (SELECT 1 FROM user_domains ud WHERE ud.domain_id = d.id AND ud.user_id = sqlc.narg('user_id')::uuid)::bool AS is_mine
FROM categories c
JOIN domains d ON d.id = c.domain_id AND d.is_published
LEFT JOIN category_translations t   ON t.category_id = c.id AND t.lang = @lang::text
LEFT JOIN category_translations te  ON te.category_id = c.id AND te.lang = 'en'
LEFT JOIN domain_translations dt    ON dt.domain_id = d.id AND dt.lang = @lang::text
LEFT JOIN domain_translations dte   ON dte.domain_id = d.id AND dte.lang = 'en'
WHERE c.id = @id AND c.is_published;

-- name: SearchCategories :many
-- Endpoint 21, the categories half: the same scope and Islamic-only rules as the
-- domains half, so search never shows what the player cannot play.
SELECT c.id, c.domain_id, c.slug, c.icon, c.color, c.question_count,
       COALESCE(t.name, te.name, c.slug)::text AS name,
       COALESCE(dt.name, dte.name, d.slug)::text AS domain_name,
       (ud.user_id IS NOT NULL)::bool AS is_mine
FROM categories c
JOIN domains d ON d.id = c.domain_id AND d.is_published
LEFT JOIN category_translations t  ON t.category_id = c.id AND t.lang = @lang::text
LEFT JOIN category_translations te ON te.category_id = c.id AND te.lang = 'en'
LEFT JOIN domain_translations dt   ON dt.domain_id = d.id AND dt.lang = @lang::text
LEFT JOIN domain_translations dte  ON dte.domain_id = d.id AND dte.lang = 'en'
LEFT JOIN user_domains ud ON ud.domain_id = d.id AND ud.user_id = sqlc.narg('user_id')::uuid
WHERE c.is_published
  AND (NOT @only_mine::bool OR ud.user_id IS NOT NULL)
  AND (NOT @islamic_only::bool OR d.family = 'islamic')
  AND (@q::text = '' OR unaccent(COALESCE(t.name, te.name, c.slug)) ILIKE '%' || unaccent(@q::text) || '%')
ORDER BY COALESCE(t.name, te.name, c.slug), c.id
LIMIT @lim::int;

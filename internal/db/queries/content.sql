-- Content items (§8): the universal shape, hanging on a node at any depth.

-- name: ContentTypes :many
-- The registry. A new kind is a row here, not a migration (§14).
SELECT * FROM content_types WHERE is_enabled ORDER BY position, code;

-- name: ContentType :one
SELECT * FROM content_types WHERE code = @code AND is_enabled;

-- name: ContentOnNode :many
-- What hangs on one node, or on its whole branch when @subtree is true. The caller
-- has already been allowed to read the node; this is the listing, not the check.
SELECT i.*, COALESCE(t.fields, tf.fields, '{}')::jsonb AS fields,
       COALESCE(ct.icon, 'book') AS icon,
       COALESCE(ct.playable, false) AS playable
FROM content_items i
JOIN content_types ct ON ct.code = i.type_code
LEFT JOIN content_item_translations t  ON t.item_id = i.id AND t.lang = @lang
LEFT JOIN content_item_translations tf ON tf.item_id = i.id AND tf.lang = 'en'
WHERE i.deleted_at IS NULL
  AND (CASE WHEN @subtree::bool
            THEN i.node_id IN (SELECT n.id FROM nodes n
                                WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = @node_id)
                                  AND n.deleted_at IS NULL)
            ELSE i.node_id = @node_id END)
  AND (@type_code::text = '' OR i.type_code = @type_code)
  AND (@staff::bool
       OR i.status = 'published'
       OR i.owner_id = sqlc.narg('viewer')::uuid)
ORDER BY i.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountContentOnNode :one
-- The same WHERE, so the pager counts what the page shows.
SELECT count(*)::int FROM content_items i
WHERE i.deleted_at IS NULL
  AND (CASE WHEN @subtree::bool
            THEN i.node_id IN (SELECT n.id FROM nodes n
                                WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = @node_id)
                                  AND n.deleted_at IS NULL)
            ELSE i.node_id = @node_id END)
  AND (@type_code::text = '' OR i.type_code = @type_code)
  AND (@staff::bool
       OR i.status = 'published'
       OR i.owner_id = sqlc.narg('viewer')::uuid);

-- name: ContentItem :one
SELECT i.*, COALESCE(t.fields, tf.fields, '{}')::jsonb AS fields
FROM content_items i
LEFT JOIN content_item_translations t  ON t.item_id = i.id AND t.lang = @lang
LEFT JOIN content_item_translations tf ON tf.item_id = i.id AND tf.lang = 'en'
WHERE i.id = @id AND i.deleted_at IS NULL;

-- name: CreateContentItem :one
INSERT INTO content_items (node_id, type_code, owner_id, visibility, status,
                           difficulty, points, time_limit, tags, created_by)
VALUES (@node_id, @type_code, sqlc.narg('owner'), @visibility, @status,
        @difficulty, @points, @time_limit, @tags, sqlc.narg('actor'))
RETURNING *;

-- name: SetContentText :exec
INSERT INTO content_item_translations (item_id, lang, fields)
VALUES (@item_id, @lang, @fields)
ON CONFLICT (item_id, lang) DO UPDATE SET fields = EXCLUDED.fields;

-- name: UpdateContentItem :one
UPDATE content_items SET
  status     = COALESCE(sqlc.narg('status'), status),
  visibility = COALESCE(sqlc.narg('visibility'), visibility),
  difficulty = COALESCE(sqlc.narg('difficulty'), difficulty),
  points     = COALESCE(sqlc.narg('points'), points),
  node_id    = COALESCE(sqlc.narg('node'), node_id)
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: DeleteContentItem :exec
-- Soft, because a match that used it must keep making sense (§10.3).
UPDATE content_items SET deleted_at = now() WHERE id = @id;

-- name: ContentOwner :one
SELECT id, owner_id, visibility, status, node_id FROM content_items
WHERE id = @id AND deleted_at IS NULL;

-- name: ContentAnswerKey :one
-- What counts as right for one item, with the rule its kind is checked by. The
-- correct answer never leaves the server before a submission (§10.1): this is read
-- when an answer arrives, never when a question is dealt.
SELECT i.id, i.points, i.answer, ct.answer_kind
FROM content_items i
JOIN content_types ct ON ct.code = i.type_code
WHERE i.id = @id AND i.deleted_at IS NULL;

-- name: SetContentAnswer :exec
UPDATE content_items SET answer = @answer WHERE id = @id;

-- name: PickContent :many
-- The engine's selection over the universal model (CUT-1 + migration 26): playable
-- content from a branch, in a random order, excluding what this game has already
-- dealt. @node_scope is the §10.2 choice — direct | subtree | both.
--
-- It returns no answer: the correct one is read only when a submission arrives
-- (§10.1), by ContentAnswerKey.
SELECT i.id, i.node_id, i.difficulty, i.points, i.time_limit, i.type_code
FROM content_items i
JOIN content_types ct ON ct.code = i.type_code AND ct.playable AND ct.is_enabled
JOIN nodes n ON n.id = i.node_id AND n.deleted_at IS NULL
WHERE i.deleted_at IS NULL
  AND i.status = 'published'
  AND (CASE @node_scope::text
         WHEN 'direct'  THEN i.node_id = @node_id
         WHEN 'subtree' THEN n.path <@ (SELECT a.path FROM nodes a WHERE a.id = @node_id)
                              AND i.node_id <> @node_id
         ELSE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = @node_id)
       END)
  AND (@type_code::text = '' OR i.type_code = @type_code)
  AND (@difficulty::smallint = 0 OR i.difficulty = @difficulty)
  AND i.id <> ALL (@exclude::bigint[])
  -- Official published content, or the player's own, or a branch shared with them.
  AND (i.owner_id IS NULL
       OR i.owner_id = sqlc.narg('viewer')::uuid
       OR (sqlc.narg('viewer')::uuid IS NOT NULL
           AND node_shared_level(i.node_id, sqlc.narg('viewer')::uuid) IS NOT NULL)
       OR i.visibility = 'public')
ORDER BY random()
LIMIT @lim;

-- name: CountPlayableContent :one
-- The same filter without the limit: how much a branch can actually be played with.
SELECT count(*)::int FROM content_items i
JOIN content_types ct ON ct.code = i.type_code AND ct.playable AND ct.is_enabled
JOIN nodes n ON n.id = i.node_id AND n.deleted_at IS NULL
WHERE i.deleted_at IS NULL
  AND i.status = 'published'
  AND (CASE @node_scope::text
         WHEN 'direct'  THEN i.node_id = @node_id
         WHEN 'subtree' THEN n.path <@ (SELECT a.path FROM nodes a WHERE a.id = @node_id)
                              AND i.node_id <> @node_id
         ELSE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = @node_id)
       END)
  AND (@type_code::text = '' OR i.type_code = @type_code)
  AND (@difficulty::smallint = 0 OR i.difficulty = @difficulty)
  AND (i.owner_id IS NULL
       OR i.owner_id = sqlc.narg('viewer')::uuid
       OR (sqlc.narg('viewer')::uuid IS NOT NULL
           AND node_shared_level(i.node_id, sqlc.narg('viewer')::uuid) IS NOT NULL)
       OR i.visibility = 'public');

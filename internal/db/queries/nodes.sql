-- The universal hierarchy (migration 20). Every read here is scoped by what the
-- caller may see: @viewer is the player asking (uuid.Nil for a guest) and @staff is
-- true only for the admin console, which sees every branch.
--
-- The visibility rule, written once and repeated by each query rather than hidden in
-- a view, so it is impossible to read a node without stating who is reading:
--   official and published  → anyone
--   owned by the viewer     → the viewer
--   shared with the viewer  → NOT YET (PERM-2); the clause is ready for it
--   anything else           → nobody but staff

-- name: NodeByID :one
SELECT n.*, COALESCE(t.name, tf.name, n.slug) AS name,
       COALESCE(t.description, tf.description, '') AS description
FROM nodes n
LEFT JOIN node_translations t  ON t.node_id = n.id AND t.lang = @lang
LEFT JOIN node_translations tf ON tf.node_id = n.id AND tf.lang = 'en'
WHERE n.id = @id AND n.deleted_at IS NULL
  AND (@staff::bool
       OR (n.owner_id IS NULL AND n.status = 'published')
       OR n.owner_id = sqlc.narg('viewer')::uuid
       -- A share reaches the whole branch below the node it names (migration 22).
       OR (sqlc.narg('viewer')::uuid IS NOT NULL
           AND node_shared_level(n.id, sqlc.narg('viewer')::uuid) IS NOT NULL)
       -- Another player's public, published branch is readable by anyone.
       OR (n.owner_id IS NOT NULL AND n.visibility = 'public' AND n.status = 'published'));

-- name: NodeChildren :many
-- One level of the tree. The screen asks for a page of children as a branch is
-- opened, never the whole subtree.
SELECT n.*, COALESCE(t.name, tf.name, n.slug) AS name,
       (SELECT count(*) FROM nodes c WHERE c.parent_id = n.id AND c.deleted_at IS NULL)::int AS children,
       (SELECT count(*) FROM questions q WHERE q.node_id = n.id AND q.deleted_at IS NULL)::int AS questions
FROM nodes n
LEFT JOIN node_translations t  ON t.node_id = n.id AND t.lang = @lang
LEFT JOIN node_translations tf ON tf.node_id = n.id AND tf.lang = 'en'
WHERE n.deleted_at IS NULL
  AND (CASE WHEN sqlc.narg('parent')::bigint IS NULL
            THEN n.parent_id IS NULL
            ELSE n.parent_id = sqlc.narg('parent')::bigint END)
  AND (@staff::bool
       OR (n.owner_id IS NULL AND n.status = 'published')
       OR n.owner_id = sqlc.narg('viewer')::uuid
       -- A share reaches the whole branch below the node it names (migration 22).
       OR (sqlc.narg('viewer')::uuid IS NOT NULL
           AND node_shared_level(n.id, sqlc.narg('viewer')::uuid) IS NOT NULL)
       -- Another player's public, published branch is readable by anyone.
       OR (n.owner_id IS NOT NULL AND n.visibility = 'public' AND n.status = 'published'))
ORDER BY n.position, COALESCE(t.name, tf.name, n.slug), n.id
LIMIT @lim OFFSET @off;

-- name: CountNodeChildren :one
-- The same WHERE as NodeChildren, so the pager counts the rows the page shows.
SELECT count(*)::int FROM nodes n
WHERE n.deleted_at IS NULL
  AND (CASE WHEN sqlc.narg('parent')::bigint IS NULL
            THEN n.parent_id IS NULL
            ELSE n.parent_id = sqlc.narg('parent')::bigint END)
  AND (@staff::bool
       OR (n.owner_id IS NULL AND n.status = 'published')
       OR n.owner_id = sqlc.narg('viewer')::uuid
       -- A share reaches the whole branch below the node it names (migration 22).
       OR (sqlc.narg('viewer')::uuid IS NOT NULL
           AND node_shared_level(n.id, sqlc.narg('viewer')::uuid) IS NOT NULL)
       -- Another player's public, published branch is readable by anyone.
       OR (n.owner_id IS NOT NULL AND n.visibility = 'public' AND n.status = 'published'));

-- name: NodeBreadcrumb :many
-- The trail from the root down to this node, read straight off the materialized
-- path: one indexed query whatever the depth.
SELECT n.id, n.depth, n.owner_id, COALESCE(t.name, tf.name, n.slug) AS name
FROM nodes n
LEFT JOIN node_translations t  ON t.node_id = n.id AND t.lang = @lang
LEFT JOIN node_translations tf ON tf.node_id = n.id AND tf.lang = 'en'
WHERE n.path @> (SELECT a.path FROM nodes a WHERE a.id = @id) AND n.deleted_at IS NULL
ORDER BY n.depth;

-- name: NodeSubtreeStats :one
-- What a branch holds, for "direct only / descendants / both" (§10.2) and for the
-- counts the tree screen prints. `path <@` reads the GiST index.
SELECT
  (SELECT count(*) FROM nodes d
     WHERE d.path <@ (SELECT a.path FROM nodes a WHERE a.id = @id)
       AND d.id <> @id AND d.deleted_at IS NULL)::int AS descendants,
  (SELECT count(*) FROM questions q
     WHERE q.node_id = @id AND q.deleted_at IS NULL AND q.status = 'published')::int AS direct_questions,
  (SELECT count(*) FROM questions q
     JOIN nodes d ON d.id = q.node_id
     WHERE d.path <@ (SELECT a.path FROM nodes a WHERE a.id = @id)
       AND q.deleted_at IS NULL AND q.status = 'published')::int AS subtree_questions;

-- name: SearchNodes :many
-- Search across the tree, or inside one branch when @within is given.
SELECT n.id, n.depth, n.owner_id, n.icon, n.color,
       COALESCE(t.name, tf.name, n.slug) AS name,
       (SELECT COALESCE(string_agg(COALESCE(bt.name, btf.name, b.slug), ' / ' ORDER BY b.depth), '')::text
          FROM nodes b
          LEFT JOIN node_translations bt  ON bt.node_id = b.id AND bt.lang = @lang
          LEFT JOIN node_translations btf ON btf.node_id = b.id AND btf.lang = 'en'
         WHERE b.path @> n.path AND b.id <> n.id) AS trail
FROM nodes n
LEFT JOIN node_translations t  ON t.node_id = n.id AND t.lang = @lang
LEFT JOIN node_translations tf ON tf.node_id = n.id AND tf.lang = 'en'
WHERE n.deleted_at IS NULL
  AND COALESCE(t.name, tf.name, n.slug) ILIKE '%' || @q::text || '%'
  AND (sqlc.narg('within')::bigint IS NULL
       OR n.path <@ (SELECT a.path FROM nodes a WHERE a.id = sqlc.narg('within')::bigint))
  AND (@staff::bool
       OR (n.owner_id IS NULL AND n.status = 'published')
       OR n.owner_id = sqlc.narg('viewer')::uuid
       -- A share reaches the whole branch below the node it names (migration 22).
       OR (sqlc.narg('viewer')::uuid IS NOT NULL
           AND node_shared_level(n.id, sqlc.narg('viewer')::uuid) IS NOT NULL)
       -- Another player's public, published branch is readable by anyone.
       OR (n.owner_id IS NOT NULL AND n.visibility = 'public' AND n.status = 'published'))
ORDER BY n.depth, COALESCE(t.name, tf.name, n.slug)
LIMIT @lim;

-- name: CreateNode :one
-- The trigger places it: path, depth and root_id are never written by hand.
INSERT INTO nodes (parent_id, owner_id, extends_id, node_type, visibility, status,
                   slug, icon, color, position, created_by, updated_by)
VALUES (sqlc.narg('parent'), sqlc.narg('owner'), sqlc.narg('extends'), @node_type,
        @visibility, @status, @slug, @icon, @color, @position,
        sqlc.narg('actor'), sqlc.narg('actor'))
RETURNING *;

-- name: SetNodeText :exec
INSERT INTO node_translations (node_id, lang, name, description)
VALUES (@node_id, @lang, @name, @description)
ON CONFLICT (node_id, lang) DO UPDATE
  SET name = EXCLUDED.name, description = EXCLUDED.description;

-- name: UpdateNode :one
UPDATE nodes SET
  node_type  = COALESCE(sqlc.narg('node_type'), node_type),
  visibility = COALESCE(sqlc.narg('visibility'), visibility),
  status     = COALESCE(sqlc.narg('status'), status),
  slug       = COALESCE(sqlc.narg('slug'), slug),
  icon       = COALESCE(sqlc.narg('icon'), icon),
  color      = COALESCE(sqlc.narg('color'), color),
  position   = COALESCE(sqlc.narg('position'), position),
  updated_by = sqlc.narg('actor')
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: MoveNode :one
-- Only parent_id is written; the trigger rewrites the subtree and refuses a loop.
UPDATE nodes SET parent_id = sqlc.narg('parent'), updated_by = sqlc.narg('actor')
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: ArchiveNode :exec
-- Archiving a branch archives what is under it: a child left published under an
-- archived parent would still be reachable by search.
UPDATE nodes SET status = 'archived', updated_by = sqlc.narg('actor')
WHERE path <@ (SELECT a.path FROM nodes a WHERE a.id = @id) AND deleted_at IS NULL;

-- name: RestoreNode :exec
UPDATE nodes SET status = 'draft', updated_by = sqlc.narg('actor')
WHERE path <@ (SELECT a.path FROM nodes a WHERE a.id = @id) AND deleted_at IS NULL;

-- name: NodeOwner :one
-- What a permission check needs, without reading the whole row.
SELECT id, owner_id, visibility, status, parent_id, root_id, extends_id FROM nodes
WHERE id = @id AND deleted_at IS NULL;

-- name: LibraryRoots :many
-- What "my library" opens on: the player's own roots, plus the branches they have
-- hung under official nodes. An extension lives deep in the official tree, so without
-- this it would only be reachable by navigating there — the player would have no way
-- back to their own work.
SELECT n.id, n.slug, n.icon, n.color, n.status, n.extends_id,
       COALESCE(t.name, tf.name, n.slug) AS name,
       (SELECT count(*) FROM nodes c WHERE c.parent_id = n.id AND c.deleted_at IS NULL)::int AS children,
       (SELECT count(*) FROM questions q WHERE q.node_id = n.id AND q.deleted_at IS NULL)::int AS questions,
       COALESCE((SELECT COALESCE(pt.name, ptf.name, p.slug) FROM nodes p
                   LEFT JOIN node_translations pt  ON pt.node_id = p.id AND pt.lang = @lang
                   LEFT JOIN node_translations ptf ON ptf.node_id = p.id AND ptf.lang = 'en'
                  WHERE p.id = n.extends_id), '')::text AS under
FROM nodes n
LEFT JOIN node_translations t  ON t.node_id = n.id AND t.lang = @lang
LEFT JOIN node_translations tf ON tf.node_id = n.id AND tf.lang = 'en'
WHERE n.deleted_at IS NULL
  AND n.owner_id = @owner
  AND n.status <> 'archived'
  AND (n.parent_id IS NULL OR n.extends_id IS NOT NULL)
ORDER BY n.extends_id NULLS FIRST, n.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountLibraryRoots :one
SELECT count(*)::int FROM nodes n
WHERE n.deleted_at IS NULL AND n.owner_id = @owner AND n.status <> 'archived'
  AND (n.parent_id IS NULL OR n.extends_id IS NOT NULL);

-- name: NodeSharedLevel :one
-- What a share grants this person on this node, walking up the branch (migration 22).
-- Returned as text: no share is the common answer, and an empty string says so more
-- simply than a nullable enum.
SELECT COALESCE(node_shared_level(@target::bigint, @who::uuid)::text, '')::text AS share_level;

-- name: ShareNode :exec
INSERT INTO node_shares (node_id, user_id, level, invited_by)
VALUES (@node_id, @user_id, @level, sqlc.narg('invited_by'))
ON CONFLICT (node_id, user_id) DO UPDATE SET level = EXCLUDED.level;

-- name: UnshareNode :exec
DELETE FROM node_shares WHERE node_id = @node_id AND user_id = @user_id;

-- name: NodeShares :many
SELECT s.user_id, s.level, u.display_name
FROM node_shares s JOIN users u ON u.id = s.user_id
WHERE s.node_id = @node_id
ORDER BY s.created_at DESC;

-- name: SharedWithMe :many
-- The branches somebody else has opened to this player: their own entry points into
-- another player's tree.
SELECT n.id, n.slug, n.icon, n.color, s.level,
       COALESCE(t.name, tf.name, n.slug) AS name,
       COALESCE(u.display_name, '') AS owner_name
FROM node_shares s
JOIN nodes n ON n.id = s.node_id AND n.deleted_at IS NULL
LEFT JOIN users u ON u.id = n.owner_id
LEFT JOIN node_translations t  ON t.node_id = n.id AND t.lang = @lang
LEFT JOIN node_translations tf ON tf.node_id = n.id AND tf.lang = 'en'
WHERE s.user_id = @user_id
ORDER BY s.created_at DESC
LIMIT @lim OFFSET @off;

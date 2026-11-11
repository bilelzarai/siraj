-- Sharing (§7). A node is owned by the platform or by one player; a share lets
-- somebody else in at a stated level without changing who owns it.
--
-- The levels are ordered, not a set of flags: anyone who may edit may also view and
-- play. Keeping them ordered means a check is one comparison rather than a matrix,
-- and a new level in the middle is one ALTER TYPE rather than a migration of rows.
CREATE TYPE share_level AS ENUM ('view', 'play', 'edit', 'manage');

CREATE TABLE node_shares (
  node_id    bigint NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
  user_id    uuid   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  level      share_level NOT NULL DEFAULT 'view',
  -- Who opened the door, so revoking can say where a share came from.
  invited_by uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (node_id, user_id)
);

-- "What is shared with me" is a screen; "who may see this branch" is a check. Both
-- are indexed, because both run on every page that lists a tree.
CREATE INDEX node_shares_user ON node_shares (user_id, created_at DESC);

-- A share reaches the whole branch below the node it names: sharing a subject and
-- then having to share each lesson under it separately is how access goes wrong.
-- The check walks the path, which the GiST index on nodes already serves.
CREATE FUNCTION node_shared_level(target bigint, who uuid)
RETURNS share_level AS $$
  SELECT s.level
  FROM node_shares s
  JOIN nodes shared ON shared.id = s.node_id
  JOIN nodes t      ON t.id = target
  WHERE s.user_id = who
    AND t.path <@ shared.path
    AND shared.deleted_at IS NULL
  -- The most generous share wins when a branch is shared twice at different levels.
  ORDER BY s.level DESC
  LIMIT 1;
$$ LANGUAGE sql STABLE;

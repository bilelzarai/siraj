-- The universal hierarchy: one node table, unlimited depth, official and personal
-- branches side by side.
--
-- Strategy: adjacency list + materialized ltree path.
--   * parent_id is the source of truth for structure — one column to write, a real
--     foreign key, and a trigger that refuses a cycle.
--   * path is derived from it and kept by the same trigger. Subtree reads are the
--     dominant query on this table (navigating a tree, scoping a challenge to a node
--     and its descendants, counting content under a branch), and `path <@ ancestor`
--     answers them from a GiST index instead of a recursive CTE per request.
-- ltree ships with PostgreSQL, so this costs no application complexity. There is no
-- depth limit: ltree carries 65 535 labels.
CREATE EXTENSION IF NOT EXISTS ltree;

CREATE TYPE node_visibility AS ENUM ('private', 'shared', 'public');
CREATE TYPE node_status     AS ENUM ('draft', 'review', 'published', 'archived');

CREATE TABLE nodes (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  -- NULL parent = a root, which is what the product calls a domain.
  parent_id   bigint REFERENCES nodes (id) ON DELETE RESTRICT,
  -- The root this node belongs to, carried so "everything in this domain" is one
  -- indexed comparison rather than a walk.
  root_id     bigint REFERENCES nodes (id) ON DELETE RESTRICT,
  path        ltree NOT NULL,
  depth       integer NOT NULL DEFAULT 0,

  -- NULL owner = the platform. A node is owned by the platform or by exactly one
  -- player; sharing and publishing never change this column (§7).
  owner_id    uuid REFERENCES users (id) ON DELETE CASCADE,
  -- A player's own branch hanging under an official node. The official record is
  -- untouched: the link lives here, on the extension (§6.2).
  extends_id  bigint REFERENCES nodes (id) ON DELETE CASCADE,

  -- Free-form so a new kind of node needs no migration (§14).
  node_type   text NOT NULL DEFAULT 'node',
  visibility  node_visibility NOT NULL DEFAULT 'private',
  status      node_status NOT NULL DEFAULT 'draft',

  slug        text NOT NULL,
  icon        text NOT NULL DEFAULT '',
  color       text NOT NULL DEFAULT '',
  motif       text NOT NULL DEFAULT '',
  position    integer NOT NULL DEFAULT 0,
  metadata    jsonb NOT NULL DEFAULT '{}',

  created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  updated_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz,

  -- A root is its own root; everything else names one.
  CONSTRAINT nodes_root_present CHECK (parent_id IS NULL OR root_id IS NOT NULL),
  -- An extension is somebody's: the platform does not extend itself.
  CONSTRAINT nodes_extension_owned CHECK (extends_id IS NULL OR owner_id IS NOT NULL),
  CONSTRAINT nodes_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id)
);

-- A slug is unique among its siblings, not globally: two domains may both have a
-- "basics" child. NULLS NOT DISTINCT makes the rule hold for roots too.
CREATE UNIQUE INDEX nodes_sibling_slug ON nodes (parent_id, slug) NULLS NOT DISTINCT
  WHERE deleted_at IS NULL;
CREATE INDEX nodes_parent    ON nodes (parent_id, position, id) WHERE deleted_at IS NULL;
CREATE INDEX nodes_root      ON nodes (root_id) WHERE deleted_at IS NULL;
CREATE INDEX nodes_owner     ON nodes (owner_id) WHERE owner_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX nodes_extends   ON nodes (extends_id) WHERE extends_id IS NOT NULL;
CREATE INDEX nodes_browse    ON nodes (visibility, status) WHERE deleted_at IS NULL;
-- The subtree index: `path <@ @ancestor` and `path ~ 'a.b.*{1}'` both use it.
CREATE INDEX nodes_path_gist ON nodes USING gist (path);

-- Every text a player reads, per language (§16).
CREATE TABLE node_translations (
  node_id     bigint NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
  lang        text   NOT NULL REFERENCES languages (code),
  name        text   NOT NULL,
  description text   NOT NULL DEFAULT '',
  PRIMARY KEY (node_id, lang)
);
CREATE INDEX node_translations_name ON node_translations USING gin (name gin_trgm_ops);

-- nodes_place keeps path, depth and root_id true, and refuses a parent that would
-- close a loop. Doing it in the database means no caller can write a broken tree,
-- whatever layer they come from.
CREATE FUNCTION nodes_place() RETURNS trigger AS $$
DECLARE
  parent nodes%ROWTYPE;
BEGIN
  IF NEW.parent_id IS NULL THEN
    NEW.path  := text2ltree(NEW.id::text);
    NEW.depth := 0;
    NEW.root_id := NEW.id;
    RETURN NEW;
  END IF;

  SELECT * INTO parent FROM nodes WHERE id = NEW.parent_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'node %: parent % does not exist', NEW.id, NEW.parent_id;
  END IF;
  -- A node cannot be moved under its own descendant.
  IF TG_OP = 'UPDATE' AND parent.path <@ OLD.path THEN
    RAISE EXCEPTION 'node %: parent % is inside its own subtree', NEW.id, NEW.parent_id;
  END IF;

  NEW.path    := parent.path || text2ltree(NEW.id::text);
  NEW.depth   := parent.depth + 1;
  NEW.root_id := parent.root_id;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Moving a node carries its whole subtree with it.
--
-- The trigger is AFTER UPDATE with no column list on purpose: `UPDATE OF path` names
-- the columns the *statement* sets, and the statement sets parent_id — the path is
-- rewritten by the BEFORE trigger above, which such a clause would never see.
-- pg_trigger_depth() stops the cascade re-entering: one statement fixes every
-- descendant, instead of each descendant trying to move its own subtree in turn.
CREATE FUNCTION nodes_move_subtree() RETURNS trigger AS $$
BEGIN
  IF pg_trigger_depth() > 1 THEN
    RETURN NULL;
  END IF;
  IF NEW.path IS DISTINCT FROM OLD.path THEN
    UPDATE nodes
       SET path    = NEW.path || subpath(path, nlevel(OLD.path)),
           depth   = NEW.depth + (nlevel(path) - nlevel(OLD.path)),
           root_id = NEW.root_id
     WHERE path <@ OLD.path AND id <> NEW.id;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- PostgreSQL fills an identity column before BEFORE INSERT runs, so a root knows
-- its own id here and places itself in one pass.
CREATE TRIGGER nodes_place_before BEFORE INSERT OR UPDATE OF parent_id ON nodes
  FOR EACH ROW EXECUTE FUNCTION nodes_place();
CREATE TRIGGER nodes_subtree AFTER UPDATE ON nodes
  FOR EACH ROW EXECUTE FUNCTION nodes_move_subtree();
CREATE TRIGGER nodes_updated_at BEFORE UPDATE ON nodes
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------- v1 data
-- The two fixed levels become the first two levels of the universal tree. The old
-- tables keep working and gain a pointer at their node, so the cut-over can happen
-- query by query instead of in one unreviewable change, and so this migration can be
-- rolled back without losing anything.
ALTER TABLE domains    ADD COLUMN node_id bigint REFERENCES nodes (id) ON DELETE SET NULL;
ALTER TABLE categories ADD COLUMN node_id bigint REFERENCES nodes (id) ON DELETE SET NULL;

INSERT INTO nodes (slug, node_type, visibility, status, icon, color, motif, position, metadata, created_at)
SELECT d.slug, 'domain',
       CASE WHEN d.is_published THEN 'public' ELSE 'private' END::node_visibility,
       CASE WHEN d.is_published THEN 'published' ELSE 'draft' END::node_status,
       d.icon, d.color, d.motif, d.position,
       jsonb_build_object('family', d.family, 'is_featured', d.is_featured, 'legacy_domain_id', d.id),
       d.created_at
FROM domains d;

UPDATE domains d SET node_id = n.id
  FROM nodes n WHERE (n.metadata->>'legacy_domain_id')::bigint = d.id;

INSERT INTO nodes (parent_id, slug, node_type, visibility, status, icon, color, position, metadata, created_at)
SELECT d.node_id, c.slug, 'category',
       CASE WHEN c.is_published THEN 'public' ELSE 'private' END::node_visibility,
       CASE WHEN c.is_published THEN 'published' ELSE 'draft' END::node_status,
       c.icon, c.color, c.position,
       jsonb_build_object('legacy_category_id', c.id),
       c.created_at
FROM categories c JOIN domains d ON d.id = c.domain_id;

UPDATE categories c SET node_id = n.id
  FROM nodes n WHERE (n.metadata->>'legacy_category_id')::bigint = c.id;

INSERT INTO node_translations (node_id, lang, name, description)
SELECT d.node_id, t.lang, t.name, COALESCE(t.description, '')
FROM domain_translations t JOIN domains d ON d.id = t.domain_id
WHERE d.node_id IS NOT NULL;

INSERT INTO node_translations (node_id, lang, name)
SELECT c.node_id, t.lang, t.name
FROM category_translations t JOIN categories c ON c.id = t.category_id
WHERE c.node_id IS NOT NULL;

-- Content hangs off a node from here on. category_id stays until every query has
-- moved, and the two are kept in step by the backfill below.
ALTER TABLE questions ADD COLUMN node_id bigint REFERENCES nodes (id) ON DELETE RESTRICT;
UPDATE questions q SET node_id = c.node_id FROM categories c WHERE c.id = q.category_id;
CREATE INDEX questions_node ON questions (node_id) WHERE deleted_at IS NULL;

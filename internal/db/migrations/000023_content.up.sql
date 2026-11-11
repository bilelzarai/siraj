-- Content (§8). One table for everything a player can be given to learn from, and a
-- registry that says what kinds exist.
--
-- The v1 bank stays where it is: `questions` is the specialisation the game engine
-- reads, and it is not being rewritten here (AD-2). What this adds is the universal
-- shape every *other* kind needs — flashcards today, exercises and quizzes next —
-- so a new kind is a row in content_types, not a migration and a new table.
--
-- Type-specific fields live in jsonb rather than columns, because the alternative is
-- a column per kind on a shared table, which is how a schema stops being universal.
-- What every kind has in common — where it hangs, who owns it, what it is worth — is
-- a real column, indexed.

CREATE TABLE content_types (
  code        text PRIMARY KEY,
  icon        text NOT NULL DEFAULT 'book',
  -- The fields this kind carries, as a hint for the editor: ["front","back"].
  -- Validation stays in the service; this is what a form is built from.
  fields      text[] NOT NULL DEFAULT '{}',
  -- Whether a game can draw this kind. A flashcard is studied, not answered.
  playable    boolean NOT NULL DEFAULT false,
  position    integer NOT NULL DEFAULT 0,
  is_enabled  boolean NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- The kinds §8 names as the starting set. These are configuration, not content: the
-- app cannot run without knowing that a question is a kind of thing. Adding a fifth
-- is an INSERT, from the admin console, with no code change (§14).
INSERT INTO content_types (code, icon, fields, playable, position) VALUES
  ('question',  'question', ARRAY['body','answers','explanation'], true,  1),
  ('flashcard', 'layers',   ARRAY['front','back','explanation'],   false, 2),
  ('exercise',  'edit',     ARRAY['instructions','body'],          true,  3),
  ('quiz',      'check',    ARRAY['instructions'],                 true,  4);

CREATE TABLE content_items (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  -- Content hangs on a node at any depth — a root domain, a tenth level, a player's
  -- extension. There is no intermediate category to invent (§8).
  node_id     bigint NOT NULL REFERENCES nodes (id) ON DELETE RESTRICT,
  type_code   text   NOT NULL REFERENCES content_types (code) ON DELETE RESTRICT,
  -- NULL owner = the platform, the same rule as nodes.
  owner_id    uuid REFERENCES users (id) ON DELETE CASCADE,
  visibility  node_visibility NOT NULL DEFAULT 'private',
  status      node_status NOT NULL DEFAULT 'draft',
  difficulty  smallint NOT NULL DEFAULT 1 CHECK (difficulty BETWEEN 1 AND 3),
  points      integer NOT NULL DEFAULT 10,
  -- Seconds a player gets, 0 for untimed. Kept here rather than in the game rules so
  -- a single hard card can be given more room than its neighbours.
  time_limit  integer NOT NULL DEFAULT 0,
  tags        text[] NOT NULL DEFAULT '{}',
  metadata    jsonb  NOT NULL DEFAULT '{}',
  -- The v1 question this row stands for, while both exist. NULL for anything born
  -- in the new model. It is what lets the bank be migrated later without guessing.
  question_id bigint REFERENCES questions (id) ON DELETE CASCADE,
  created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz
);

CREATE INDEX content_items_node   ON content_items (node_id, status) WHERE deleted_at IS NULL;
CREATE INDEX content_items_type   ON content_items (type_code) WHERE deleted_at IS NULL;
CREATE INDEX content_items_owner  ON content_items (owner_id) WHERE owner_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX content_items_tags   ON content_items USING gin (tags);
CREATE UNIQUE INDEX content_items_question ON content_items (question_id) WHERE question_id IS NOT NULL;

CREATE TRIGGER content_items_updated_at BEFORE UPDATE ON content_items
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Every text a player reads, per language (§16). `fields` holds what the kind needs:
-- {"front": "...", "back": "..."} for a flashcard, {"body": "...", "answers": [...]}
-- for a question. One shape, so a new kind needs no new table.
CREATE TABLE content_item_translations (
  item_id bigint NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
  lang    text   NOT NULL REFERENCES languages (code),
  fields  jsonb  NOT NULL DEFAULT '{}',
  PRIMARY KEY (item_id, lang)
);

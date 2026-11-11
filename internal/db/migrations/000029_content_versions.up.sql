-- Snapshots (§10.3). A match must not change under the people playing it.
--
-- Today an edit to a question rewrites what everyone sees, including a challenge
-- already in progress and a result already recorded. That makes history a lie: the
-- score says a player got a question right that no longer exists in that form.
--
-- The design is version-on-edit, freeze-on-deal:
--   * content_items.version counts edits. A trigger bumps it when the wording, the
--     answer or the worth of an item changes.
--   * A row is written to content_item_versions the first time a version is dealt,
--     not on every edit — items nobody plays cost nothing, and an item played a
--     thousand times is stored once per version.
--   * game_questions.item_version records exactly what was dealt, so a round can be
--     read back as it was however the source has moved on since.
--
-- Deleting source content is already soft (content_items.deleted_at), so a frozen
-- round survives it. Revoking access does not reach backwards either: the snapshot
-- belongs to the match, not to the branch it came from.

ALTER TABLE content_items ADD COLUMN version integer NOT NULL DEFAULT 1;

CREATE TABLE content_item_versions (
  item_id    bigint  NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
  version    integer NOT NULL,
  -- Every language at once: a match may be played in any of them, and a snapshot
  -- that only froze one would still shift under a player who switched.
  texts      jsonb   NOT NULL DEFAULT '{}',
  answer     jsonb   NOT NULL DEFAULT '{}',
  points     integer NOT NULL,
  difficulty smallint NOT NULL,
  frozen_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (item_id, version)
);

ALTER TABLE game_questions ADD COLUMN item_version integer;

-- An edit bumps the version. Only the things a player sees or is scored on count:
-- moving an item to another branch is not a different question.
CREATE FUNCTION content_items_bump_version() RETURNS trigger AS $$
BEGIN
  IF NEW.answer IS DISTINCT FROM OLD.answer
     OR NEW.points IS DISTINCT FROM OLD.points
     OR NEW.difficulty IS DISTINCT FROM OLD.difficulty THEN
    NEW.version := OLD.version + 1;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER content_items_version BEFORE UPDATE ON content_items
  FOR EACH ROW EXECUTE FUNCTION content_items_bump_version();

-- The wording lives in another table, so a change there bumps the item too.
CREATE FUNCTION content_text_bump_version() RETURNS trigger AS $$
BEGIN
  UPDATE content_items SET version = version + 1
  WHERE id = COALESCE(NEW.item_id, OLD.item_id);
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER content_item_translations_version
  AFTER INSERT OR UPDATE OR DELETE ON content_item_translations
  FOR EACH ROW EXECUTE FUNCTION content_text_bump_version();

-- freeze_content_item stores the item as it is now, unless that version is already
-- stored. It is what the engine calls as it deals.
CREATE FUNCTION freeze_content_item(target bigint) RETURNS integer AS $$
DECLARE v integer;
BEGIN
  SELECT version INTO v FROM content_items WHERE id = target;
  IF v IS NULL THEN
    RETURN NULL;
  END IF;
  INSERT INTO content_item_versions (item_id, version, texts, answer, points, difficulty)
  SELECT i.id, i.version,
         COALESCE((SELECT jsonb_object_agg(t.lang, t.fields)
                     FROM content_item_translations t WHERE t.item_id = i.id), '{}'::jsonb),
         i.answer, i.points, i.difficulty
  FROM content_items i WHERE i.id = target
  ON CONFLICT (item_id, version) DO NOTHING;
  RETURN v;
END;
$$ LANGUAGE plpgsql;

-- A round freezes what it deals, and records which version it got.
CREATE FUNCTION game_questions_freeze() RETURNS trigger AS $$
BEGIN
  IF NEW.item_id IS NOT NULL AND NEW.item_version IS NULL THEN
    NEW.item_version := freeze_content_item(NEW.item_id);
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- After the pairing trigger from migration 28, so item_id is filled by the time this
-- runs for a round written with only a question id.
CREATE TRIGGER game_questions_snapshot BEFORE INSERT ON game_questions
  FOR EACH ROW EXECUTE FUNCTION game_questions_freeze();

-- Rounds already dealt are frozen as the content stands now: the closest thing to
-- the truth that is still available, and better than nothing to read back.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT DISTINCT item_id FROM game_questions WHERE item_id IS NOT NULL LOOP
    PERFORM freeze_content_item(r.item_id);
  END LOOP;
  UPDATE game_questions gq SET item_version = i.version
  FROM content_items i WHERE i.id = gq.item_id AND gq.item_version IS NULL;
END $$;

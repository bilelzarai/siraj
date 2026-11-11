-- CUT-2 and CUT-3: mastery and a player's sets move onto the universal tree.
--
-- The same shape as migration 21 used for questions: add the column, backfill it,
-- and keep it true with a trigger, so whichever code path writes a row the two
-- columns cannot drift while both exist. category_id goes with the v1 tables in
-- CUT-4; until then nothing has to be cut over in one unreviewable change.

-- Mastery, per branch rather than per category. A player who has learned a tenth
-- level has mastery of that node, which the old shape could not record at all.
ALTER TABLE user_category_stats ADD COLUMN node_id bigint REFERENCES nodes (id) ON DELETE CASCADE;
UPDATE user_category_stats m SET node_id = c.node_id FROM categories c WHERE c.id = m.category_id;
CREATE INDEX user_category_stats_node ON user_category_stats (node_id);

CREATE FUNCTION stats_follow_category() RETURNS trigger AS $$
BEGIN
  IF NEW.category_id IS NOT NULL
     AND (NEW.node_id IS NULL OR NEW.category_id IS DISTINCT FROM OLD.category_id) THEN
    SELECT c.node_id INTO NEW.node_id FROM categories c WHERE c.id = NEW.category_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER user_category_stats_node BEFORE INSERT OR UPDATE OF category_id, node_id ON user_category_stats
  FOR EACH ROW EXECUTE FUNCTION stats_follow_category();

-- A player's set can hang on a branch of the tree. There is nothing to backfill:
-- question_sets never had a category, so this is a new place for a set to live
-- rather than a column being moved.
ALTER TABLE question_sets ADD COLUMN node_id bigint REFERENCES nodes (id) ON DELETE SET NULL;
CREATE INDEX question_sets_node ON question_sets (node_id) WHERE node_id IS NOT NULL;

-- A question inside a set does carry a category, and follows it onto the tree the
-- same way the bank's questions did in migration 21.
ALTER TABLE set_questions ADD COLUMN node_id bigint REFERENCES nodes (id) ON DELETE SET NULL;
UPDATE set_questions q SET node_id = c.node_id FROM categories c WHERE c.id = q.category_id;
CREATE INDEX set_questions_node ON set_questions (node_id) WHERE node_id IS NOT NULL;

CREATE FUNCTION set_questions_follow_category() RETURNS trigger AS $$
BEGIN
  IF NEW.category_id IS NOT NULL
     AND (NEW.node_id IS NULL OR NEW.category_id IS DISTINCT FROM OLD.category_id) THEN
    SELECT c.node_id INTO NEW.node_id FROM categories c WHERE c.id = NEW.category_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_questions_node BEFORE INSERT OR UPDATE OF category_id, node_id ON set_questions
  FOR EACH ROW EXECUTE FUNCTION set_questions_follow_category();

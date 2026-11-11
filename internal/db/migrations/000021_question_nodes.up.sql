-- questions.node_id must never drift from category_id while both exist.
--
-- Migration 20 added node_id and backfilled it, but every write path still sets only
-- category_id — so the next question created would have had no node, and the game
-- engine is about to select on exactly that column. A trigger is the only place that
-- cannot be bypassed: the admin console, the CSV import, a player's set, a future
-- caller and a hand-written statement all pass through it.
--
-- It disappears with the v1 tables (checklist CUT-4). Until then the database, not
-- each caller, is what keeps the two columns telling the same story.
CREATE FUNCTION questions_follow_category() RETURNS trigger AS $$
BEGIN
  IF NEW.category_id IS NOT NULL
     AND (NEW.node_id IS NULL OR NEW.category_id IS DISTINCT FROM OLD.category_id) THEN
    SELECT c.node_id INTO NEW.node_id FROM categories c WHERE c.id = NEW.category_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER questions_node BEFORE INSERT OR UPDATE OF category_id, node_id ON questions
  FOR EACH ROW EXECUTE FUNCTION questions_follow_category();

-- Anything written between migration 20 and this one.
UPDATE questions q SET node_id = c.node_id
FROM categories c WHERE c.id = q.category_id AND q.node_id IS NULL;

-- A category that is given a node later carries its questions with it.
CREATE FUNCTION categories_push_node() RETURNS trigger AS $$
BEGIN
  IF NEW.node_id IS DISTINCT FROM OLD.node_id THEN
    UPDATE questions SET node_id = NEW.node_id WHERE category_id = NEW.id;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER categories_node AFTER UPDATE OF node_id ON categories
  FOR EACH ROW EXECUTE FUNCTION categories_push_node();

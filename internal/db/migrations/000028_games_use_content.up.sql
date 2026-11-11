-- CUT-4, first step: a game's questions point at content items.
--
-- The engine cannot be moved onto the universal model while the row recording what
-- was dealt can only name a v1 question. This adds the reference, backfills it from
-- the mirror (migration 26) and keeps it true, so the deal path can be switched
-- later with no data migration still pending — and so a game already in progress
-- keeps working throughout.
--
-- question_id stays until the engine reads item_id everywhere. Both are nullable:
-- a round built from a player's own set uses set_question_id and neither.

ALTER TABLE game_questions ADD COLUMN item_id bigint REFERENCES content_items (id) ON DELETE RESTRICT;

UPDATE game_questions gq SET item_id = i.id
FROM content_items i
WHERE i.question_id = gq.question_id AND gq.question_id IS NOT NULL;

CREATE INDEX game_questions_item ON game_questions (item_id) WHERE item_id IS NOT NULL;

-- Whichever column a caller writes, the other follows. One place, so the engine can
-- be switched a query at a time without a half-written game going dark.
CREATE FUNCTION game_questions_pair() RETURNS trigger AS $$
BEGIN
  IF NEW.question_id IS NOT NULL AND NEW.item_id IS NULL THEN
    SELECT i.id INTO NEW.item_id FROM content_items i WHERE i.question_id = NEW.question_id;
  ELSIF NEW.item_id IS NOT NULL AND NEW.question_id IS NULL THEN
    -- A mirrored item names the question it stands for; an item born in the new
    -- model has none, and the column simply stays empty.
    SELECT i.question_id INTO NEW.question_id FROM content_items i WHERE i.id = NEW.item_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER game_questions_pair_ids BEFORE INSERT OR UPDATE OF question_id, item_id ON game_questions
  FOR EACH ROW EXECUTE FUNCTION game_questions_pair();

-- The check loosens: a round may now name an item instead of a v1 question.
ALTER TABLE game_questions DROP CONSTRAINT IF EXISTS game_questions_check;
ALTER TABLE game_questions ADD CONSTRAINT game_questions_has_something
  CHECK (question_id IS NOT NULL OR set_question_id IS NOT NULL OR item_id IS NOT NULL);

ALTER TABLE game_questions DROP CONSTRAINT IF EXISTS game_questions_has_something;
ALTER TABLE game_questions ADD CONSTRAINT game_questions_check
  CHECK (question_id IS NOT NULL OR set_question_id IS NOT NULL);
DROP TRIGGER IF EXISTS game_questions_pair_ids ON game_questions;
DROP FUNCTION IF EXISTS game_questions_pair;
DROP INDEX IF EXISTS game_questions_item;
ALTER TABLE game_questions DROP COLUMN IF EXISTS item_id;

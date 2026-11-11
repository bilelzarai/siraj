ALTER TABLE challenges          DROP CONSTRAINT IF EXISTS challenges_set_fk;
ALTER TABLE challenge_questions DROP CONSTRAINT IF EXISTS challenge_questions_set_question_fk;
ALTER TABLE game_questions      DROP CONSTRAINT IF EXISTS game_questions_set_question_fk;
DROP TABLE IF EXISTS bank_suggestions;
DROP TABLE IF EXISTS set_question_translations;
DROP TABLE IF EXISTS set_questions;
DROP TABLE IF EXISTS question_sets;

ALTER TABLE games DROP CONSTRAINT IF EXISTS games_challenge_fk;
DROP TABLE IF EXISTS challenge_links;
DROP TABLE IF EXISTS challenge_participants;
DROP TABLE IF EXISTS challenge_questions;
DROP TABLE IF EXISTS challenges;
DROP TYPE IF EXISTS challenge_result;
DROP TYPE IF EXISTS participant_status;
DROP TYPE IF EXISTS challenge_status;

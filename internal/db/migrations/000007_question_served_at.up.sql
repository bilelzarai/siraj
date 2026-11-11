-- The speed bonus must not depend on a number the client sends (NF-05): the server
-- stamps when it handed a question to the player and measures the answer itself.
ALTER TABLE game_questions ADD COLUMN served_at timestamptz;

-- Player ratings on a question, 1 to 5 stars.
--
-- The note a player can leave after answering says what is wrong in words, and
-- someone has to read it. A rating says the same thing in a number, which is
-- what lets a bad question surface on its own instead of waiting for a
-- moderator to work through a queue.
--
-- One rating per person per question, the same rule the notes follow: a second
-- submission replaces the first, so a question cannot be voted down twice by
-- the same player.

CREATE TABLE IF NOT EXISTS question_ratings (
    question_id bigint NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    user_id     uuid   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stars       smallint NOT NULL CHECK (stars BETWEEN 1 AND 5),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (question_id, user_id)
);

-- The admin alert reads every question's average in one pass, so it is worth
-- the index even though the table is small to begin with.
CREATE INDEX IF NOT EXISTS question_ratings_question_idx
    ON question_ratings (question_id);

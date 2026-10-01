-- Deciding what happens to a possible duplicate.
--
-- Content health listed pairs and offered nothing to do about them: the same
-- sixty pairs on every visit, including the ones a moderator had already read
-- and judged fine. A list that cannot be worked through is a list that stops
-- being read — and the two genuinely useful verdicts were already expressible
-- in the data, just not recordable.
--
-- "Not a duplicate" is the one that needs a row of its own. Retiring the weaker
-- of the two is `is_active = false`, which the bank already understands, and
-- deleting is deliberately not offered here: it cascades into every player's
-- history, and the question editor already refuses it once for exactly that.
CREATE TABLE IF NOT EXISTS question_duplicate_verdicts (
    -- Ordered by the caller so a pair has one row whichever way round it is
    -- raised: the sweep can surface (a,b) in Arabic and (b,a) in French.
    left_id    bigint NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    right_id   bigint NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    verdict    text   NOT NULL CHECK (verdict IN ('distinct')),
    decided_by uuid   REFERENCES users(id) ON DELETE SET NULL,
    decided_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (left_id, right_id),
    CHECK (left_id < right_id)
);

CREATE INDEX IF NOT EXISTS question_duplicate_verdicts_right_idx
    ON question_duplicate_verdicts (right_id);

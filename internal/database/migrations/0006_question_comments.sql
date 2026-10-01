-- Player comments on a question.
--
-- Written straight after answering, while the reason for the remark is fresh:
-- a wrong-looking answer, a translation that reads oddly, or a point worth
-- adding. They are player-authored text, so they are moderatable and are
-- never mixed into the question bank itself.

CREATE TABLE IF NOT EXISTS question_comments (
    id          bigserial PRIMARY KEY,
    question_id bigint NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    user_id     uuid   REFERENCES users(id) ON DELETE SET NULL,
    -- The language the comment was written in, so a reader is not shown a
    -- remark about the Arabic wording while playing in French.
    locale      text   NOT NULL,
    body        text   NOT NULL CHECK (length(btrim(body)) BETWEEN 2 AND 2000),
    -- hidden keeps a moderated comment for the audit trail without showing it.
    hidden      boolean NOT NULL DEFAULT false,
    hidden_by   uuid   REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- One remark per person per question: the second submission edits the first,
-- so the thread cannot be used to flood a question.
CREATE UNIQUE INDEX IF NOT EXISTS question_comments_one_per_user
    ON question_comments (question_id, user_id)
    WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS question_comments_question_idx
    ON question_comments (question_id, created_at DESC)
    WHERE NOT hidden;

-- The moderation queue reads this.
CREATE INDEX IF NOT EXISTS question_comments_recent_idx
    ON question_comments (created_at DESC);

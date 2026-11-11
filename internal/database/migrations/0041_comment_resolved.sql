-- =============================================================================
-- 0041_comment_resolved.sql — a remark that has been dealt with
--
-- A player's remark under a question is usually a report of a fault: a wrong
-- answer, a translation that reads oddly, a choice that repeats another. The
-- moderation screen had two states for them — visible and hidden — and hiding
-- is the wrong tool for a remark that was right: it was useful, it was acted
-- on, and taking it off the screen pretends it never arrived.
--
-- So the queue could only ever grow. Every remark anybody had already fixed
-- stayed in it, and the only way to clear one was to hide a player's correct
-- observation.
--
-- Resolved is the third state: the remark stands, and the queue stops asking.
-- =============================================================================

ALTER TABLE question_comments
    ADD COLUMN IF NOT EXISTS resolved_at timestamptz,
    ADD COLUMN IF NOT EXISTS resolved_by uuid REFERENCES users(id) ON DELETE SET NULL;

-- The queue reads "everything still waiting", which is a shrinking slice of a
-- growing table.
CREATE INDEX IF NOT EXISTS question_comments_open_idx
    ON question_comments (created_at DESC)
    WHERE resolved_at IS NULL AND NOT hidden;

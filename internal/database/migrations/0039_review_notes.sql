-- =============================================================================
-- 0039_review_notes.sql — the answer between approve and reject
--
-- Review had two verdicts: approve, which publishes the text, and reject,
-- which deletes it. A reviewer who can see what is wrong with a translation
-- but not fix it themselves had neither — rejecting throws away the draft and
-- the problem with it, and approving ships text the reviewer does not stand
-- behind. So the realistic case, "this is close, here is what to change", was
-- the one answer the queue could not record.
--
-- Changes are requested per translation, not per question: a French rendering
-- can be wrong while the English one is fine, and a note on the question would
-- not say which.
-- =============================================================================

ALTER TABLE question_translations
    ADD COLUMN IF NOT EXISTS review_note text NOT NULL DEFAULT '',
    -- Who asked and when. The author needs to know whose note this is, and the
    -- queue needs to sort the longest-waiting request to the top.
    ADD COLUMN IF NOT EXISTS review_note_by uuid REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS review_note_at timestamptz;

-- A note without needs_review is a note on published text, which the queue
-- would never show again — the request would be invisible the moment it was
-- made. Changes requested therefore implies still awaiting review.
ALTER TABLE question_translations
    DROP CONSTRAINT IF EXISTS question_translations_note_is_pending;
ALTER TABLE question_translations
    ADD CONSTRAINT question_translations_note_is_pending
    CHECK (review_note = '' OR needs_review);

-- The queue reads "every translation with changes requested" across the whole
-- bank, which is a small slice of a large table.
CREATE INDEX IF NOT EXISTS question_translations_noted_idx
    ON question_translations (review_note_at DESC) WHERE review_note <> '';

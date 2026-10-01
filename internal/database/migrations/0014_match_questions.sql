-- Questions a player writes for their own match.
--
-- The rule this has to respect: unvetted text never reaches players who did not
-- ask for it. A question written by a stranger and drawn into somebody's solo
-- round breaks that; a question written by your friend, in a match they invited
-- you to and you accepted, does not — you know who wrote it, and you chose to
-- play it.
--
-- So containment rather than review: the question is bound to one match and is
-- never drawn by anything else. Three properties make that true, and all three
-- are enforced here rather than remembered at each call site.
--
--   1. challenge_id is set, which marks it as belonging to a match.
--   2. is_active is false, and every public draw already filters on is_active,
--      so it cannot appear in a solo round, the daily, or the landing count
--      without a query being changed on purpose.
--   3. author_id records who wrote it, so a report has somebody to name.
--
-- It lives in `questions` rather than a table of its own so a match plays on
-- exactly the same machinery — the same translations, the same answers, the
-- same review screen afterwards. A parallel table would have meant a second
-- implementation of the round, which is where the bugs would be.
ALTER TABLE questions
    ADD COLUMN IF NOT EXISTS challenge_id uuid REFERENCES challenges(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS author_id    uuid REFERENCES users(id) ON DELETE SET NULL;

-- A match question is never active. Stated as a constraint so it cannot be
-- switched on by an admin screen that does not know what it is looking at.
ALTER TABLE questions
    DROP CONSTRAINT IF EXISTS questions_match_never_active;
ALTER TABLE questions
    ADD CONSTRAINT questions_match_never_active
    CHECK (challenge_id IS NULL OR NOT is_active);

CREATE INDEX IF NOT EXISTS questions_challenge_idx
    ON questions (challenge_id) WHERE challenge_id IS NOT NULL;

-- The bank is everything without a match behind it. Every list, count and sweep
-- that means "the question bank" filters on this.
CREATE INDEX IF NOT EXISTS questions_bank_idx
    ON questions (id) WHERE challenge_id IS NULL;

-- Where the text came from. 'player' joins the list a translation may declare:
-- a question somebody wrote for their own match has a provenance of its own,
-- and calling it 'human' would put it in the same bucket as text a moderator
-- wrote for the bank.
--
-- `questions.source` is deliberately left unconstrained, as it has been since
-- the beginning — it defaults to '' and several rows rely on that.
ALTER TABLE question_translations
    DROP CONSTRAINT IF EXISTS question_translations_source_check;
ALTER TABLE question_translations
    ADD CONSTRAINT question_translations_source_check
    CHECK (source IN ('seed', 'human', 'machine', 'import', 'player'));

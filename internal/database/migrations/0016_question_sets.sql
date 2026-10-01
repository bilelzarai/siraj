-- A player's own questions, kept rather than thrown away.
--
-- Yesterday's shape bound a written question to one match: you wrote it, you
-- played it, it was dead. Somebody who plays the same friends every week wrote
-- the same questions every week.
--
-- So the thing a question belongs to is its author, not a match, and a set is a
-- named collection of them — "Ramadan quiz", "Seerah for the kids". A match
-- references a set; the set outlives the match.
--
-- The containment argument is unchanged and the enforcement moves with it:
-- author_id is now the discriminator, is_active is still false for every one of
-- them, and a constraint still makes that pair unsettable. The bank is
-- everything nobody wrote for themselves.
CREATE TABLE IF NOT EXISTS question_sets (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       text NOT NULL,
    locale     text NOT NULL DEFAULT 'en',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (btrim(name) <> '')
);

CREATE INDEX IF NOT EXISTS question_sets_owner_idx
    ON question_sets (owner_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS question_set_items (
    set_id      uuid   NOT NULL REFERENCES question_sets(id) ON DELETE CASCADE,
    question_id bigint NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    position    integer NOT NULL DEFAULT 0,
    PRIMARY KEY (set_id, question_id)
);

CREATE INDEX IF NOT EXISTS question_set_items_question_idx
    ON question_set_items (question_id);

-- The discriminator moves from "which match" to "who wrote it".
ALTER TABLE questions
    DROP CONSTRAINT IF EXISTS questions_match_never_active;
ALTER TABLE questions
    ADD CONSTRAINT questions_authored_never_active
    CHECK (author_id IS NULL OR NOT is_active);

-- The bank is everything nobody wrote for themselves.
DROP INDEX IF EXISTS questions_bank_idx;
CREATE INDEX IF NOT EXISTS questions_bank_idx
    ON questions (id) WHERE author_id IS NULL;
CREATE INDEX IF NOT EXISTS questions_author_idx
    ON questions (author_id) WHERE author_id IS NOT NULL;

-- Yesterday's match-bound questions become their authors' own, in a set named
-- after where they came from, so nothing written is lost.
INSERT INTO question_sets (owner_id, name, locale)
SELECT DISTINCT q.author_id, 'Saved from a match', COALESCE(t.locale, 'en')
  FROM questions q
  LEFT JOIN LATERAL (
        SELECT locale FROM question_translations
         WHERE question_id = q.id ORDER BY locale LIMIT 1
  ) t ON true
 WHERE q.author_id IS NOT NULL AND q.challenge_id IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM question_sets s
                    WHERE s.owner_id = q.author_id AND s.name = 'Saved from a match');

INSERT INTO question_set_items (set_id, question_id)
SELECT s.id, q.id
  FROM questions q
  JOIN question_sets s ON s.owner_id = q.author_id AND s.name = 'Saved from a match'
 WHERE q.author_id IS NOT NULL AND q.challenge_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Tell seeded translations apart from edited ones.
--
-- SEED_ON_START defaults to true and the seeder upserted every locale of every
-- bundled question on every boot. So a correction made through import, or a
-- machine translation a reviewer had approved, was silently reverted by the
-- JSON file the next time the process started — no warning, no audit entry.
--
-- With a provenance of its own, the seeder can refuse to overwrite a row that
-- something else has touched, and still keep the documented behaviour that
-- editing questions.json and restarting updates the bundled bank in place.

ALTER TABLE question_translations
    DROP CONSTRAINT IF EXISTS question_translations_source_check;

ALTER TABLE question_translations
    ADD CONSTRAINT question_translations_source_check
    CHECK (source IN ('seed', 'human', 'machine', 'import'));

-- Everything currently marked 'human' was written by the seeder: it was the
-- only writer that did not set a source, so it took the column default. The
-- admin paths that do set one ('import', 'machine') are left alone.
UPDATE question_translations SET source = 'seed' WHERE source = 'human';

-- New rows are seeder rows unless a writer says otherwise, which matches where
-- the bulk of this table comes from.
ALTER TABLE question_translations ALTER COLUMN source SET DEFAULT 'seed';

-- Same treatment for categories, whose translations are seeded the same way.
ALTER TABLE category_translations
    DROP CONSTRAINT IF EXISTS category_translations_source_check;

ALTER TABLE category_translations
    ADD CONSTRAINT category_translations_source_check
    CHECK (source IN ('seed', 'human', 'machine', 'import'));

UPDATE category_translations SET source = 'seed' WHERE source = 'human';
ALTER TABLE category_translations ALTER COLUMN source SET DEFAULT 'seed';

-- =============================================================================
-- 0034_domains.sql — the level above a category
--
-- All eight categories are Islamic, and there is nowhere to say that Football
-- and Qur'an are different kinds of subject. This adds that level: an admin
-- creates a domain, files categories under exactly one each, and nothing in Go
-- knows any of them by name — Islamic is a row this migration inserts, not a
-- constant, which is what lets a second subject area arrive without a deploy.
-- =============================================================================

CREATE TABLE domains (
    id         serial PRIMARY KEY,
    slug       text NOT NULL UNIQUE,
    icon       text NOT NULL DEFAULT '🗂️',
    color      text NOT NULL DEFAULT '#0ea5a4',
    sort_order integer NOT NULL DEFAULT 0,
    is_active  boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The same provenance discipline category_translations carries since 0008: an
-- unreviewed machine name must not reach a player.
CREATE TABLE domain_translations (
    domain_id    integer NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    locale       text NOT NULL,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    source       text NOT NULL DEFAULT 'seed'
                 CHECK (source IN ('seed', 'human', 'machine', 'import')),
    needs_review boolean NOT NULL DEFAULT false,
    PRIMARY KEY (domain_id, locale)
);

CREATE INDEX domain_translations_review_idx
    ON domain_translations (locale) WHERE needs_review;

-- The eight categories are all Islamic, so they move under one domain rather
-- than being re-entered. A backfill of what exists, not new content.
INSERT INTO domains (id, slug, icon, color, sort_order)
VALUES (1, 'islamic', '🕌', '#0ea5a4', 1)
ON CONFLICT (id) DO NOTHING;
SELECT setval('domains_id_seq', (SELECT max(id) FROM domains));

INSERT INTO domain_translations (domain_id, locale, name, description, source) VALUES
  (1, 'ar', 'العلوم الإسلامية', 'القرآن والسنة والفقه والسيرة والتاريخ', 'seed'),
  (1, 'en', 'Islamic Knowledge',  'Qur''an, Sunnah, fiqh, sīrah and history', 'seed'),
  (1, 'fr', 'Savoir Islamique',   'Coran, Sunna, fiqh, sîra et histoire',    'seed')
ON CONFLICT (domain_id, locale) DO NOTHING;

-- Nullable, backfilled, then required — expand-then-contract inside one
-- migration, because the table is small and the window is the deploy itself.
--
-- The default is what keeps every existing writer working between this
-- migration and the screens that name a domain: a category created today has
-- no domain to offer, and a NOT NULL column with no default would turn every
-- such write into a 500. Dropped once the category form carries the field,
-- which is the point at which "no domain" becomes a mistake rather than the
-- only possibility.
ALTER TABLE categories
    ADD COLUMN domain_id integer DEFAULT 1 REFERENCES domains(id) ON DELETE RESTRICT;
UPDATE categories SET domain_id = 1 WHERE domain_id IS NULL;
ALTER TABLE categories ALTER COLUMN domain_id SET NOT NULL;
CREATE INDEX categories_domain_idx ON categories (domain_id, sort_order);

-- questions.category_id was ON DELETE CASCADE, safe only while nothing but a
-- migration could delete a category. The admin screens changed that: a Delete
-- button over a cascade destroys every question and, through game_answers, the
-- answers players gave. The handler has refused a non-empty delete since those
-- screens shipped, so nothing violating can exist — this makes the schema say
-- the same thing. The count runs first so it fails loudly, not halfway.
DO $$
DECLARE orphans integer;
BEGIN
    SELECT count(*) INTO orphans
      FROM questions q
      LEFT JOIN categories c ON c.id = q.category_id
     WHERE c.id IS NULL;
    IF orphans > 0 THEN
        RAISE EXCEPTION 'questions reference % missing categories', orphans;
    END IF;
END $$;

ALTER TABLE questions DROP CONSTRAINT questions_category_id_fkey;
ALTER TABLE questions ADD CONSTRAINT questions_category_id_fkey
    FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT;

-- A round may be "any category in Sport". Without somewhere to record it the
-- only choices are one category or the whole bank, and a Sport player is asked
-- about the Qur'an. Nullable: a round drawn from one category records that
-- category and no domain, exactly as it does today. (D13)
ALTER TABLE game_sessions ADD COLUMN domain_id integer
    REFERENCES domains(id) ON DELETE SET NULL;
ALTER TABLE challenges ADD COLUMN domain_id integer
    REFERENCES domains(id) ON DELETE SET NULL;
CREATE INDEX game_sessions_domain_idx ON game_sessions (domain_id)
    WHERE domain_id IS NOT NULL;

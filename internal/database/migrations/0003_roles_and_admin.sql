-- =============================================================================
-- 0003_roles_and_admin.sql — real roles, the admin area, and bulk import
-- =============================================================================

-- ----------------------------------------------------------------- roles --

-- is_admin was a single boolean with no room for a moderator tier, so it is
-- replaced by a role. Existing admins keep their rights.
CREATE TYPE user_role AS ENUM ('player', 'moderator', 'admin');

ALTER TABLE users ADD COLUMN role user_role NOT NULL DEFAULT 'player';
UPDATE users SET role = 'admin' WHERE is_admin;
ALTER TABLE users DROP COLUMN is_admin;

-- Suspension is reversible; deletion is not. Admins need the reversible one.
ALTER TABLE users ADD COLUMN status text NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'suspended'));
ALTER TABLE users ADD COLUMN suspended_reason text NOT NULL DEFAULT '';

CREATE INDEX users_role_idx    ON users (role) WHERE role <> 'player';
CREATE INDEX users_status_idx  ON users (status) WHERE status <> 'active';
CREATE INDEX users_country_idx ON users (country) WHERE country <> '';
CREATE INDEX users_seen_idx    ON users (last_seen_at DESC);

-- ------------------------------------------------------------- audit log --

-- Every privileged action is recorded. actor_username is denormalised so the
-- trail survives the actor's account being deleted.
CREATE TABLE admin_audit (
    id             bigserial PRIMARY KEY,
    actor_id       uuid REFERENCES users(id) ON DELETE SET NULL,
    actor_username text NOT NULL,
    action         text NOT NULL,
    target_kind    text NOT NULL DEFAULT '',
    target_id      text NOT NULL DEFAULT '',
    detail         jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip             text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_audit_recent_idx ON admin_audit (created_at DESC);
CREATE INDEX admin_audit_actor_idx  ON admin_audit (actor_id, created_at DESC);
CREATE INDEX admin_audit_target_idx ON admin_audit (target_kind, target_id);

-- -------------------------------------------------- translation provenance --

-- A machine translation of a Qur'anic or hadith text must never reach a player
-- unreviewed, so every translation records where it came from and whether a
-- human has signed it off. Read queries filter on needs_review.
ALTER TABLE question_translations
    ADD COLUMN source text NOT NULL DEFAULT 'human'
        CHECK (source IN ('human', 'machine', 'import')),
    ADD COLUMN needs_review boolean NOT NULL DEFAULT false,
    ADD COLUMN reviewed_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN reviewed_at  timestamptz;

CREATE INDEX question_translations_review_idx
    ON question_translations (locale) WHERE needs_review;

ALTER TABLE category_translations
    ADD COLUMN source text NOT NULL DEFAULT 'human'
        CHECK (source IN ('human', 'machine', 'import')),
    ADD COLUMN needs_review boolean NOT NULL DEFAULT false;

-- Near-duplicate detection over prompts. Without the GIN index a similarity
-- sweep is O(n²) and unusable past a few thousand rows.
CREATE INDEX question_translations_prompt_trgm
    ON question_translations USING gin (prompt gin_trgm_ops);

-- -------------------------------------------------------------- authoring --

-- Who added a question, and whether it is a draft awaiting review.
ALTER TABLE questions
    ADD COLUMN created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- ---------------------------------------------------------- bulk imports --

CREATE TABLE question_imports (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    filename   text NOT NULL,
    format     text NOT NULL,
    committed  boolean NOT NULL DEFAULT false,
    total      integer NOT NULL DEFAULT 0,
    added      integer NOT NULL DEFAULT 0,
    updated    integer NOT NULL DEFAULT 0,
    skipped    integer NOT NULL DEFAULT 0,
    rejected   integer NOT NULL DEFAULT 0,
    report     jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX question_imports_recent_idx ON question_imports (created_at DESC);

-- Module 2 · Catalog and Your domains

CREATE TYPE domain_family AS ENUM ('islamic', 'general');
CREATE TYPE user_domain_source AS ENUM ('onboarding', 'added', 'played');

CREATE TABLE domains (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug         text NOT NULL UNIQUE,
  family       domain_family NOT NULL DEFAULT 'general', -- islamic = geometric style, kept in Islamic-only mode
  icon         text NOT NULL,
  color        text NOT NULL,                            -- accent that themes every screen of the domain
  motif        text NOT NULL,                            -- large background icon on cards and the hero
  position     integer NOT NULL DEFAULT 0,
  is_featured  boolean NOT NULL DEFAULT false,           -- the flagship card on Home
  is_new_until date,                                     -- "New" badge until this day
  is_published boolean NOT NULL DEFAULT true,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER domains_updated_at BEFORE UPDATE ON domains FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE domain_translations (
  domain_id   bigint NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
  lang        text NOT NULL REFERENCES languages (code),
  name        text NOT NULL,
  description text NOT NULL DEFAULT '',
  PRIMARY KEY (domain_id, lang)
);
CREATE INDEX domain_translations_name_trgm ON domain_translations USING gin (name gin_trgm_ops);

CREATE TABLE categories (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  domain_id      bigint NOT NULL REFERENCES domains (id) ON DELETE RESTRICT,
  slug           text NOT NULL,
  icon           text NOT NULL,
  color          text NOT NULL,
  position       integer NOT NULL DEFAULT 0,
  is_published   boolean NOT NULL DEFAULT true,
  question_count integer NOT NULL DEFAULT 0, -- cached, refreshed when questions are published
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (domain_id, slug)
);
CREATE TRIGGER categories_updated_at BEFORE UPDATE ON categories FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE category_translations (
  category_id bigint NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
  lang        text NOT NULL REFERENCES languages (code),
  name        text NOT NULL,
  PRIMARY KEY (category_id, lang)
);
CREATE INDEX category_translations_name_trgm ON category_translations USING gin (name gin_trgm_ops);

-- Your domains: the whole player app works inside these rows.
-- Every player (guests too) keeps at least one; the service refuses to delete the last.
CREATE TABLE user_domains (
  user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  domain_id bigint NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
  position  integer NOT NULL DEFAULT 0,
  source    user_domain_source NOT NULL DEFAULT 'added',
  added_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, domain_id)
);
CREATE INDEX user_domains_order ON user_domains (user_id, position);

-- Module 1 · Identity and access
-- Requires PostgreSQL 18 (uuidv7() is built in).

CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

-- One shared trigger keeps updated_at current on every table that has it.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE languages (
  code text PRIMARY KEY,
  name text NOT NULL,
  dir  text NOT NULL CHECK (dir IN ('ltr', 'rtl'))
);

INSERT INTO languages (code, name, dir) VALUES
  ('ar', 'العربية', 'rtl'),
  ('en', 'English', 'ltr'),
  ('fr', 'Français', 'ltr');

CREATE TYPE user_kind AS ENUM ('guest', 'member');
CREATE TYPE user_role AS ENUM ('player', 'translator', 'moderator', 'support', 'admin');

CREATE TABLE users (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  kind         user_kind NOT NULL DEFAULT 'guest',
  role         user_role NOT NULL DEFAULT 'player',
  username     citext UNIQUE,
  display_name text NOT NULL,
  email        citext UNIQUE,
  avatar_url   text,
  lang         text NOT NULL DEFAULT 'en' REFERENCES languages (code),
  level        integer NOT NULL DEFAULT 1,
  xp           integer NOT NULL DEFAULT 0,
  expires_at   timestamptz,                 -- guests only
  last_seen_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT guests_expire CHECK (kind = 'member' OR expires_at IS NOT NULL),
  CONSTRAINT members_have_email CHECK (kind = 'guest' OR email IS NOT NULL)
);
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX users_guest_expiry ON users (expires_at) WHERE kind = 'guest';

CREATE TABLE auth_identities (
  user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  provider      text NOT NULL CHECK (provider IN ('password', 'google')),
  provider_uid  text NOT NULL,
  password_hash text,                       -- argon2id, password provider only
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, provider),
  UNIQUE (provider, provider_uid)
);

CREATE TABLE sessions (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,         -- sha256 of the token; the token itself is never stored
  client     text NOT NULL DEFAULT 'web' CHECK (client IN ('web', 'ios', 'android')),
  user_agent text,
  ip         inet,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE auth_tokens (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose    text NOT NULL CHECK (purpose IN ('verify_email', 'reset_password')),
  token_hash bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_settings (
  user_id       uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  theme         text NOT NULL DEFAULT 'system' CHECK (theme IN ('system', 'light', 'dark')),
  daily_goal    integer NOT NULL DEFAULT 5,
  reminder_time time,
  islamic_only  boolean NOT NULL DEFAULT false, -- hides every general domain (v1 look)
  notify        jsonb NOT NULL DEFAULT '{}',
  privacy       jsonb NOT NULL DEFAULT '{}',
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER user_settings_updated_at BEFORE UPDATE ON user_settings FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE devices (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  platform   text NOT NULL CHECK (platform IN ('ios', 'android', 'web')),
  push_token text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Admin Users: warnings, suspensions and bans (moderation history).
-- An active suspension or ban blocks sign-in; the session lookup ignores such users.
CREATE TYPE sanction_kind AS ENUM ('warning', 'suspension', 'ban');

CREATE TABLE user_sanctions (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  kind       sanction_kind NOT NULL,
  reason     text NOT NULL,
  until      timestamptz,                   -- NULL = permanent (bans) or not applicable (warnings)
  by_id      uuid REFERENCES users (id) ON DELETE SET NULL,
  lifted_at  timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_sanctions_active ON user_sanctions (user_id) WHERE lifted_at IS NULL AND kind <> 'warning';

-- V5 sign-in flow: a phone number is an identity of its own, and a six-digit code is
-- the ordinary way in — for a phone or an email, to sign in or to create an account.
--
-- A password stays available for anyone who prefers one (AU-02); it is no longer the
-- way an account is made.

-- The number in E.164 ("+21655444333"): one account per number.
ALTER TABLE users ADD COLUMN phone text UNIQUE;
ALTER TABLE users ADD CONSTRAINT users_phone_e164 CHECK (phone IS NULL OR phone ~ '^\+[1-9][0-9]{6,14}$');

-- A member needs an email or a phone, not an email specifically, now that a number
-- alone creates an account.
ALTER TABLE users DROP CONSTRAINT members_have_email;
ALTER TABLE users ADD CONSTRAINT members_are_reachable
  CHECK (kind = 'guest' OR email IS NOT NULL OR phone IS NOT NULL);

-- "phone" joins password and google as a way to prove who you are. Apple is listed
-- so the button in the design has somewhere to land once it is wired.
ALTER TABLE auth_identities DROP CONSTRAINT auth_identities_provider_check;
ALTER TABLE auth_identities ADD CONSTRAINT auth_identities_provider_check
  CHECK (provider IN ('password', 'google', 'apple', 'phone', 'email'));

-- One row per code asked for. The code itself is never stored, only its hash, the
-- same rule the session tokens follow.
CREATE TABLE auth_codes (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  channel      text NOT NULL CHECK (channel IN ('phone', 'email')),
  -- The number in E.164 or the lower-cased address: what the code was sent to.
  destination  text NOT NULL,
  code_hash    bytea NOT NULL,
  -- The guest who asked, so their points follow them into the account (AU-05).
  guest_id     uuid REFERENCES users (id) ON DELETE SET NULL,
  lang         text NOT NULL DEFAULT 'en' REFERENCES languages (code),
  attempts     smallint NOT NULL DEFAULT 0,
  -- Whether the code was read out by telephone instead of texted ("Call me instead").
  by_call      boolean NOT NULL DEFAULT false,
  sent_at      timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  consumed_at  timestamptz,
  ip           inet
);
-- The newest live code for a destination is the one a verification checks.
CREATE INDEX auth_codes_live ON auth_codes (destination, sent_at DESC) WHERE consumed_at IS NULL;
CREATE INDEX auth_codes_expiry ON auth_codes (expires_at);

-- "Keep me signed in" decides how long a session lasts; the short one is a browser
-- the player did not tick it on.
ALTER TABLE sessions ADD COLUMN remember boolean NOT NULL DEFAULT true;

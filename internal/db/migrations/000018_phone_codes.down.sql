ALTER TABLE sessions DROP COLUMN IF EXISTS remember;
DROP TABLE IF EXISTS auth_codes;
ALTER TABLE auth_identities DROP CONSTRAINT IF EXISTS auth_identities_provider_check;
ALTER TABLE auth_identities ADD CONSTRAINT auth_identities_provider_check
  CHECK (provider IN ('password', 'google'));
ALTER TABLE users DROP CONSTRAINT IF EXISTS members_are_reachable;
ALTER TABLE users ADD CONSTRAINT members_have_email CHECK (kind = 'guest' OR email IS NOT NULL);
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_phone_e164;
ALTER TABLE users DROP COLUMN IF EXISTS phone;

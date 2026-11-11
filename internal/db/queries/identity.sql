-- name: CreateGuest :one
INSERT INTO users (kind, display_name, lang, expires_at)
VALUES ('guest', @display_name, @lang, @expires_at)
RETURNING *;

-- name: CreateUserSettings :exec
INSERT INTO user_settings (user_id) VALUES (@user_id)
ON CONFLICT (user_id) DO NOTHING;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: CreateSession :exec
-- remember is "Keep me signed in": it decides how long the session was given.
INSERT INTO sessions (user_id, token_hash, client, user_agent, ip, expires_at, remember)
VALUES (@user_id, @token_hash, @client, @user_agent, @ip, @expires_at, @remember);

-- name: GetSessionUser :one
-- The user behind a token, with the settings every request needs.
SELECT u.id, u.kind, u.role, u.display_name, u.lang, u.expires_at,
       COALESCE(st.islamic_only, false)::bool AS islamic_only
FROM sessions s
JOIN users u ON u.id = s.user_id
LEFT JOIN user_settings st ON st.user_id = u.id
WHERE s.token_hash = @token_hash
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND (u.expires_at IS NULL OR u.expires_at > now())
  AND NOT EXISTS (                                  -- suspended or banned players are signed out
    SELECT 1 FROM user_sanctions x
    WHERE x.user_id = u.id AND x.kind <> 'warning' AND x.lifted_at IS NULL
      AND (x.until IS NULL OR x.until > now())
  );

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = @token_hash AND revoked_at IS NULL;

-- name: TouchUser :exec
-- Presence (FR-02): every request a player makes keeps them "online now". The write
-- only happens once a minute, so a screen that polls itself every few seconds does
-- not write a row every time.
UPDATE users SET last_seen_at = now()
WHERE id = @id
  AND (last_seen_at IS NULL OR last_seen_at < now() - interval '1 minute');

-- name: LockUser :exec
-- Serialises changes to one player's rows (e.g. Your domains) inside a transaction.
SELECT id FROM users WHERE id = @id FOR UPDATE;

-- name: DeleteExpiredGuests :execrows
DELETE FROM users WHERE kind = 'guest' AND expires_at < now();

-- name: GetUserSettings :one
SELECT * FROM user_settings WHERE user_id = @user_id;

-- name: SetTheme :exec
UPDATE user_settings SET theme = @theme WHERE user_id = @user_id;

-- name: SetUserLang :exec
UPDATE users SET lang = @lang WHERE id = @id;

-- name: SetIslamicOnly :exec
-- DM-09: general domains are hidden everywhere while this is on; the rows in
-- user_domains stay (decision D-05), so they come back when it is turned off.
UPDATE user_settings SET islamic_only = @islamic_only WHERE user_id = @user_id;

-- name: UpdateSettings :exec
UPDATE user_settings
SET theme         = COALESCE(sqlc.narg('theme'), theme),
    daily_goal    = COALESCE(sqlc.narg('daily_goal'), daily_goal),
    reminder_time = CASE WHEN @clear_reminder::bool THEN NULL
                         ELSE COALESCE(sqlc.narg('reminder_time'), reminder_time) END,
    islamic_only  = COALESCE(sqlc.narg('islamic_only'), islamic_only),
    notify        = COALESCE(sqlc.narg('notify'), notify),
    privacy       = COALESCE(sqlc.narg('privacy'), privacy)
WHERE user_id = @user_id;

-- name: CreateMember :one
-- A member is reachable by email or by phone: both are nullable here, and the
-- table's own check refuses a row that has neither.
INSERT INTO users (kind, display_name, username, email, phone, lang)
VALUES ('member', @display_name, @username, @email, sqlc.narg('phone')::text, @lang)
RETURNING *;

-- name: UpgradeGuest :one
-- RL-04: the row keeps its id, so points, games and history carry over.
UPDATE users
SET kind = 'member', display_name = @display_name, username = @username,
    email = @email, expires_at = NULL
WHERE id = @id AND kind = 'guest'
RETURNING *;

-- name: UsernameTaken :one
SELECT EXISTS (SELECT 1 FROM users WHERE username = @username) AS taken;

-- name: EmailTaken :one
SELECT EXISTS (SELECT 1 FROM users WHERE email = @email) AS taken;

-- name: GetUserByLogin :one
-- Sign in with the email or the username, both case-insensitive (citext).
SELECT u.*, i.password_hash
FROM users u
LEFT JOIN auth_identities i ON i.user_id = u.id AND i.provider = 'password'
WHERE u.kind = 'member' AND (u.email = @login OR u.username = @login);

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = @email;

-- name: GetUserByProvider :one
SELECT u.* FROM users u
JOIN auth_identities i ON i.user_id = u.id
WHERE i.provider = @provider AND i.provider_uid = @provider_uid;

-- name: UpsertIdentity :exec
INSERT INTO auth_identities (user_id, provider, provider_uid, password_hash)
VALUES (@user_id, @provider, @provider_uid, @password_hash)
ON CONFLICT (user_id, provider)
DO UPDATE SET provider_uid = EXCLUDED.provider_uid,
              password_hash = COALESCE(EXCLUDED.password_hash, auth_identities.password_hash);

-- name: SetPasswordHash :exec
UPDATE auth_identities SET password_hash = @password_hash
WHERE user_id = @user_id AND provider = 'password';

-- name: UpdateProfile :one
UPDATE users
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    username     = COALESCE(sqlc.narg('username')::citext, username),
    avatar_url   = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    lang         = COALESCE(sqlc.narg('lang'), lang)
WHERE id = @id
RETURNING *;

-- name: UserProviders :many
-- Which sign-in methods the account has (Settings › Account).
SELECT provider FROM auth_identities WHERE user_id = @user_id;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = @id;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: RenewSession :one
-- Endpoint 6: slide the window of a session that is still valid.
UPDATE sessions SET expires_at = @expires_at
WHERE token_hash = @token_hash AND revoked_at IS NULL AND expires_at > now()
RETURNING user_id;

-- name: ActiveSanction :one
-- RL / sanctions: an active suspension or ban blocks sign-in.
SELECT kind, until FROM user_sanctions
WHERE user_id = @user_id AND kind <> 'warning' AND lifted_at IS NULL
  AND (until IS NULL OR until > now())
ORDER BY (until IS NULL) DESC, until DESC
LIMIT 1;

-- name: CreateAuthToken :exec
INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at)
VALUES (@user_id, @purpose, @token_hash, @expires_at);

-- name: UseAuthToken :one
-- One-time links: the UPDATE itself is the guard against a second use.
UPDATE auth_tokens SET used_at = now()
WHERE token_hash = @token_hash AND purpose = @purpose AND used_at IS NULL AND expires_at > now()
RETURNING user_id;

-- name: DeleteUserTokens :exec
DELETE FROM auth_tokens WHERE user_id = @user_id AND purpose = @purpose;

-- name: RegisterDevice :exec
-- Endpoint 15: push notifications.
INSERT INTO devices (user_id, platform, push_token)
VALUES (@user_id, @platform, @push_token)
ON CONFLICT (push_token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform;

-- ---------------------------------------------------------------- V5 sign-in

-- name: CreateAuthCode :one
-- A six-digit code, stored as its hash, for a phone or an email. When the code is
-- the second step of "Create account", the name and the hashed password wait here
-- until the digits prove the destination.
INSERT INTO auth_codes (channel, destination, code_hash, guest_id, lang, by_call,
                        expires_at, ip, display_name, password_hash)
VALUES (@channel, @destination, @code_hash, sqlc.narg('guest_id')::uuid, @lang, @by_call,
        @expires_at, @ip, sqlc.narg('display_name')::text, sqlc.narg('password_hash')::text)
RETURNING *;

-- name: LiveAuthCode :one
-- The code a verification checks: the newest one for that destination that has not
-- been used and has not run out.
SELECT * FROM auth_codes
WHERE destination = @destination AND consumed_at IS NULL AND expires_at > now()
ORDER BY sent_at DESC LIMIT 1;

-- name: LastAuthCodeFor :one
-- The last code asked for at this destination, used or not: "Send it again" on the
-- create flow reads the name and the password back off it rather than asking twice.
SELECT * FROM auth_codes
WHERE destination = @destination ORDER BY sent_at DESC LIMIT 1;

-- name: LastAuthCodeSentAt :one
-- When the last code went out, so the resend timer is the server's and not the page's.
SELECT sent_at FROM auth_codes
WHERE destination = @destination ORDER BY sent_at DESC LIMIT 1;

-- name: BumpAuthCodeAttempts :one
UPDATE auth_codes SET attempts = attempts + 1 WHERE id = @id RETURNING attempts;

-- name: ConsumeAuthCode :exec
UPDATE auth_codes SET consumed_at = now() WHERE id = @id;

-- name: ExpireAuthCodesFor :exec
-- Asking for a new code puts the old ones out of use at once.
UPDATE auth_codes SET consumed_at = now()
WHERE destination = @destination AND consumed_at IS NULL;

-- name: DeleteOldAuthCodes :execrows
-- The worker's sweep: nothing is kept after it has expired.
DELETE FROM auth_codes WHERE expires_at < now() - interval '1 day';

-- name: GetUserByPhone :one
SELECT * FROM users WHERE phone = @phone AND kind = 'member';

-- name: SetUserPhone :exec
UPDATE users SET phone = @phone WHERE id = @id;

-- name: CountCodesSince :one
-- How many codes this destination has asked for lately: the brake on a number being
-- hammered (NF-03).
SELECT count(*)::int AS codes FROM auth_codes
WHERE destination = @destination AND sent_at > now() - make_interval(mins => @minutes::int);

-- name: PromoteGuest :one
-- A guest becomes the account they just proved: same id, so every point, game and
-- friendship they made as a guest is already theirs (AU-05).
UPDATE users
SET kind = 'member', display_name = @display_name,
    phone = sqlc.narg('phone')::text,
    email = sqlc.narg('email')::citext,
    username = sqlc.narg('username')::citext,
    lang = @lang, expires_at = NULL
WHERE id = @id
RETURNING *;

-- name: MoveGuestPoints :one
-- A guest who signs into an account they already had: the points earned in this
-- browser are added to that account, and the guest row is closed behind them.
WITH moved AS (
  SELECT COALESCE(g.xp, 0) AS xp FROM users g WHERE g.id = @guest_id AND g.kind = 'guest'
)
UPDATE users SET xp = users.xp + (SELECT moved.xp FROM moved)
WHERE users.id = @user_id
RETURNING (SELECT moved.xp FROM moved)::int AS moved_xp;

-- name: RevokeAllSessions :execrows
UPDATE sessions SET revoked_at = now()
WHERE user_id = @user_id AND revoked_at IS NULL AND expires_at > now();

-- name: ListUserSessions :many
-- "Linked devices": where this account is signed in.
SELECT sessions.id, client, user_agent, ip, created_at, expires_at,
       (token_hash = @current::bytea) AS is_current
FROM sessions
WHERE user_id = @user_id AND revoked_at IS NULL AND expires_at > now()
ORDER BY created_at DESC;

-- name: RevokeSessionByID :execrows
-- One linked device, signed out from the account menu.
UPDATE sessions SET revoked_at = now() WHERE sessions.id = @id AND user_id = @user_id;

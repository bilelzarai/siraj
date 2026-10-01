-- Password reset by emailed link.
--
-- Before this, a locked-out account could only be recovered from the command
-- line, which is not a recovery path for anyone but the operator.

CREATE TABLE IF NOT EXISTS password_resets (
    -- The token is stored as a SHA-256 hash, never in the clear. A leaked
    -- database backup then yields nothing usable: the raw token exists only
    -- in the email and in the URL the person clicks.
    token_hash  text PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    requested_ip text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS password_resets_user_idx
    ON password_resets (user_id, created_at DESC);

-- Sweeping expired rows is cheap with this index and keeps the table small.
CREATE INDEX IF NOT EXISTS password_resets_expires_idx
    ON password_resets (expires_at);

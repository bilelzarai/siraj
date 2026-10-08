-- =============================================================================
-- 0036_api_keys.sql — the one credential that is not a browser session
--
-- An external consumer cannot present a session cookie, and every mutating
-- route pairs that cookie with a token it has no way to obtain. The public
-- endpoint therefore needs a credential of its own: minted from the CLI,
-- carried as a bearer header, and revocable without touching anything else.
-- =============================================================================

CREATE TABLE api_keys (
    id         serial PRIMARY KEY,
    label      text NOT NULL,
    -- Only the hash. Shown once at mint, never recoverable — the rule the
    -- password column already follows, for the same reason: a credential a
    -- database read can hand back is a credential a database leak hands out.
    key_hash   bytea NOT NULL UNIQUE,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);

-- Every request looks a key up by its hash and only live keys can answer, so
-- the index carries the condition rather than the query filtering after it.
CREATE INDEX api_keys_live_idx ON api_keys (key_hash) WHERE revoked_at IS NULL;

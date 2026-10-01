-- =============================================================================
-- 0025 — playing without an account, sharing a device, and the rules that make
--        "one room, one match" true of the database rather than of the screens
-- =============================================================================
--
-- Four things the schema could not say before this:
--
--   1. A player is not always an account. Somebody playing without signing up,
--      and somebody handed the phone for one round, are both players — they
--      answer questions, they take a place in a match, they get a score — and
--      neither of them has an email address.
--
--   2. A player is in one room at a time, and inside one match at a time. Both
--      were rules the handlers tried to keep. A rule kept by a handler is kept
--      until the second tab, the double submit, or the next call site.
--
--   3. A match can be teams rather than everybody for themselves.
--
--   4. A question in a challenge can run out of time and be lost. That is a
--      different thing from not having been reached yet, and the answer row is
--      where the difference has to be recorded.

-- ------------------------------------------------------- temporary players --

-- A temporary player is a row in `users` and not a table of its own, which is
-- the whole point: every foreign key that means "a player" — a round, a place
-- in a match, a membership — already points here. A parallel table would have
-- meant every one of them becoming two nullable columns and a check, at every
-- join, forever.
--
-- What separates the two is what they carry rather than where they live:
--
--   host_user_id  the account that made this player and is responsible for it.
--                 Set for a guest handed the phone on a shared device.
--   guest_key     the browser session that made it, for somebody playing with
--                 no account at all. It is the anonymous equivalent of a host.
--   expires_at    when it stops existing. Anonymous progress is temporary by
--                 design; this is the column that makes that literally true.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_temporary boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS host_user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS guest_key    text,
    ADD COLUMN IF NOT EXISTS expires_at   timestamptz;

-- An account is an email address; a temporary player is not. The column has to
-- stop being NOT NULL for that, so uniqueness moves to a partial index — which
-- keeps "two accounts cannot share an address" exactly as strict as it was.
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_key ON users (email) WHERE email IS NOT NULL;

-- The three things that must stay true of the split, stated where they cannot
-- be forgotten:
--
--   an account always has an address to recover it with;
--   a temporary player is always a plain player and always has an end;
--   a temporary player always belongs to somebody, so it can be swept.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_account_has_email;
ALTER TABLE users ADD CONSTRAINT users_account_has_email
    CHECK (is_temporary OR email IS NOT NULL);

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_temporary_is_plain;
ALTER TABLE users ADD CONSTRAINT users_temporary_is_plain
    CHECK (NOT is_temporary OR (role = 'player' AND expires_at IS NOT NULL));

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_temporary_has_owner;
ALTER TABLE users ADD CONSTRAINT users_temporary_has_owner
    CHECK (NOT is_temporary OR host_user_id IS NOT NULL OR guest_key IS NOT NULL);

-- One browser session, one anonymous player. Without this a lost cookie or a
-- racing pair of first requests leaves two roots for the same visitor and the
-- guests created under them belong to whichever one won.
CREATE UNIQUE INDEX IF NOT EXISTS users_guest_key_idx
    ON users (guest_key) WHERE guest_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS users_temporary_expiry_idx
    ON users (expires_at) WHERE is_temporary;

CREATE INDEX IF NOT EXISTS users_host_idx
    ON users (host_user_id) WHERE host_user_id IS NOT NULL;

-- Every list of people in the application is a list of accounts: the
-- leaderboard, the player count, user search. A partial index over the same
-- predicate those queries now carry keeps them off a sequential scan.
CREATE INDEX IF NOT EXISTS users_accounts_xp_idx
    ON users (xp DESC) WHERE NOT is_temporary;

-- ----------------------------------------------------------- one room each --

-- A room is a place, and a person is in one place. The rule was previously
-- nowhere: `conversation_members` holds a row per membership and nothing said
-- how many of them could be rooms.
--
-- A partial unique index is the natural way to say it, and it needs the kind of
-- the conversation on the membership row to do so — an index cannot reach into
-- another table. The flag is filled by a trigger rather than by whoever writes
-- the row, so it cannot disagree with the conversation it describes.
ALTER TABLE conversation_members
    ADD COLUMN IF NOT EXISTS is_room boolean NOT NULL DEFAULT false;

UPDATE conversation_members m
   SET is_room = true
  FROM conversations c
 WHERE c.id = m.conversation_id AND c.kind = 'room' AND NOT m.is_room;

CREATE OR REPLACE FUNCTION conversation_member_room_flag() RETURNS trigger AS $$
BEGIN
    SELECT (c.kind = 'room') INTO NEW.is_room
      FROM conversations c WHERE c.id = NEW.conversation_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS conversation_members_room_flag ON conversation_members;
CREATE TRIGGER conversation_members_room_flag
    BEFORE INSERT OR UPDATE OF conversation_id ON conversation_members
    FOR EACH ROW EXECUTE FUNCTION conversation_member_room_flag();

-- Anyone already in several rooms keeps the one they joined most recently,
-- because that is the one they are actually in.
DELETE FROM conversation_members m
 WHERE m.is_room
   AND EXISTS (
       SELECT 1 FROM conversation_members q
        WHERE q.user_id = m.user_id AND q.is_room
          AND (q.joined_at, q.conversation_id) > (m.joined_at, m.conversation_id));

CREATE UNIQUE INDEX IF NOT EXISTS conversation_members_one_room_idx
    ON conversation_members (user_id) WHERE is_room;

-- ------------------------------------------------- one match, and teams in it --

-- Which side somebody is on. 0 is nobody's side, which is what every player in
-- a free-for-all is on; a team match numbers them from 1.
ALTER TABLE challenge_players
    ADD COLUMN IF NOT EXISTS team smallint NOT NULL DEFAULT 0;

ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS format text NOT NULL DEFAULT 'duel';
ALTER TABLE challenges DROP CONSTRAINT IF EXISTS challenges_format_check;
ALTER TABLE challenges ADD CONSTRAINT challenges_format_check
    CHECK (format IN ('duel', 'team'));

-- The winning team, for a match that has sides. Left null by a free-for-all,
-- where winner_id already answers it.
ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS winner_team smallint;

-- A match that is over, or has run out of time, is holding nobody. Said here
-- because the index below is about to make that difference load-bearing: a
-- player left sitting in `joined` on a dead match could never enter another.
UPDATE challenge_players p
   SET state = 'eliminated'
  FROM challenges c
 WHERE c.id = p.challenge_id
   AND p.state IN ('invited', 'joined')
   AND (c.status IN ('completed', 'declined', 'expired') OR c.expires_at <= now());

-- And anybody who is somehow in two live ones keeps the newer.
UPDATE challenge_players p
   SET state = 'eliminated'
 WHERE p.state = 'joined'
   AND EXISTS (
       SELECT 1 FROM challenge_players q
        WHERE q.user_id = p.user_id AND q.state = 'joined'
          AND (COALESCE(q.joined_at, q.invited_at), q.challenge_id)
            > (COALESCE(p.joined_at, p.invited_at), p.challenge_id));

-- Inside one match at a time. `joined` is precisely "accepted and the round is
-- not finished", which is what being inside a match means: an invitation you
-- have not answered does not hold you, and a score you have already posted does
-- not either.
CREATE UNIQUE INDEX IF NOT EXISTS challenge_players_one_active_idx
    ON challenge_players (user_id) WHERE state = 'joined';

-- ------------------------------------------- a challenge question can be lost --

-- When the question on screen stops being answerable. A challenge round does
-- not wait for anybody: leave it, or let the clock run out, and that question
-- is gone and the round moves on. Solo has no deadline and resumes where it was
-- left, so this stays null there.
ALTER TABLE game_sessions
    ADD COLUMN IF NOT EXISTS deadline_at timestamptz;

-- A question that ran out is recorded as answered with -1, which is not a
-- choice anybody can make: the four are 0 to 3. Stated as a constraint so the
-- value keeps meaning that.
ALTER TABLE game_answers DROP CONSTRAINT IF EXISTS game_answers_selected_check;
ALTER TABLE game_answers ADD CONSTRAINT game_answers_selected_check
    CHECK (selected_index BETWEEN -1 AND 3);

-- ---------------------------------------------------------- idempotent posts --

-- One press, one thing created.
--
-- A disabled button covers the double-click and nothing else: the second tab,
-- the retried request, the back button and the resubmitted form all arrive as
-- honest, separate POSTs. The form carries a key it generated, the first
-- request to claim it wins, and every later one is handed the result of the
-- first instead of making a second group, a second match, a second player.
CREATE TABLE IF NOT EXISTS request_keys (
    key        text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope      text NOT NULL,
    result     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS request_keys_age_idx ON request_keys (created_at);

-- =============================================================================
-- 0027 — a match can be called off, and calling it off lets everybody go
-- =============================================================================
--
-- There was no way to end a match. "Declined" was one player's answer to their
-- own invitation, and it was being used as the status of the whole thing when
-- too few people were left — which meant a host who said no stayed listed as a
-- player in a match that carried on without them, and the people still in it
-- were shown an Accept button for a game that could never start.
--
-- Cancelled is a different fact from declined and needs its own name:
--
--   declined   one person's answer. The match may well carry on.
--   cancelled  the match is over before it began. Nobody plays it, nobody can
--              join it, and everybody in it is free immediately.
--
-- The last clause is the one with teeth. A player is "inside" a match while
-- their row says 'joined', and that is what the one-match-at-a-time index is
-- written against. A match that ends without releasing its players leaves them
-- locked out of every other match until the janitor happens to run — fifteen
-- minutes of being told they are busy with a game nobody is playing.

-- ADD VALUE inside a transaction is allowed from PostgreSQL 12 as long as the
-- new value is not used in the same transaction. Nothing below uses it: the
-- column defaults and the backfill are all about older rows.
ALTER TYPE challenge_status ADD VALUE IF NOT EXISTS 'cancelled';

-- Who ended it and when. A match that simply stopped existing, with nobody
-- named, is a thing people ask about and nobody can answer.
ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS cancelled_at timestamptz,
    ADD COLUMN IF NOT EXISTS cancelled_by uuid REFERENCES users(id) ON DELETE SET NULL;

-- The sweep reads exactly this: open matches whose time is up. It was a
-- sequential scan over every match ever played.
CREATE INDEX IF NOT EXISTS challenges_open_idx
    ON challenges (expires_at)
    WHERE status IN ('pending', 'accepted');

-- Reading a person's matches goes through the player rows, so this is the index
-- that listing is actually served by.
CREATE INDEX IF NOT EXISTS challenge_players_user_state_idx
    ON challenge_players (user_id, state, challenge_id);

-- A match has one host. It is true of every row today and nothing enforces it,
-- which is the condition under which it stops being true.
CREATE UNIQUE INDEX IF NOT EXISTS challenge_players_one_host_idx
    ON challenge_players (challenge_id) WHERE is_host;

-- Anybody left holding a dead match is let go now rather than at the next
-- sweep. These are the rows the old close path stranded: a match marked
-- declined or expired whose other players were never moved out of 'joined'.
UPDATE challenge_players p
   SET state = 'eliminated'
  FROM challenges c
 WHERE c.id = p.challenge_id
   AND p.state IN ('invited', 'joined')
   AND (c.status IN ('declined', 'expired', 'completed') OR c.expires_at <= now());

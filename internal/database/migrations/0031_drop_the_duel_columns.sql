-- =============================================================================
-- 0031 — the two-player columns go
-- =============================================================================
--
-- `challenges` was built for a duel: challenger_id and opponent_id, one score
-- and one session column each. Migration 0013 moved membership to a row per
-- player and left these in place, carrying the history they already held, on
-- the understanding that dropping them was a later migration once nothing read
-- them. Nothing does; this is that migration.
--
-- They are worth the trouble of removing because of what they cost while they
-- stayed. Three separate bugs this month came from code reading them as if they
-- still meant "everyone in this match":
--
--   the home screen listed a match for the first two players and nobody else;
--   accept and decline answered "Not allowed" to the third player onward, and
--     to every guest at a shared device;
--   "waiting for X" named the host-or-first-invitee, so in a match where the
--     seat belonged to somebody not in it, both sides were told to wait for
--     the other.
--
-- Each was found and fixed separately. The columns were the thing they had in
-- common: two names that look authoritative, are easy to reach for, and have
-- been wrong since a match could hold three people.
--
-- Nothing is lost. Every fact they held is in challenge_players — who was in
-- it, what they scored, which round was theirs — for every player rather than
-- for the first two.

-- The checks and foreign keys that only exist for these columns.
ALTER TABLE challenges DROP CONSTRAINT IF EXISTS challenges_check;
ALTER TABLE challenges DROP CONSTRAINT IF EXISTS challenges_challenger_id_fkey;
ALTER TABLE challenges DROP CONSTRAINT IF EXISTS challenges_opponent_id_fkey;

DROP INDEX IF EXISTS challenges_opponent_idx;
DROP INDEX IF EXISTS challenges_challenger_idx;

ALTER TABLE challenges
    DROP COLUMN IF EXISTS challenger_id,
    DROP COLUMN IF EXISTS opponent_id,
    DROP COLUMN IF EXISTS challenger_score,
    DROP COLUMN IF EXISTS opponent_score,
    DROP COLUMN IF EXISTS challenger_session_id,
    DROP COLUMN IF EXISTS opponent_session_id;

-- host_id carried a default of NULL while the old columns were the real
-- membership. It is the only owner now, so it says so.
UPDATE challenges c
   SET host_id = p.user_id
  FROM challenge_players p
 WHERE p.challenge_id = c.id AND p.is_host AND c.host_id IS NULL;

DELETE FROM challenges WHERE host_id IS NULL;
ALTER TABLE challenges ALTER COLUMN host_id SET NOT NULL;

-- The listing index the two dropped ones were standing in for: a person's
-- matches are found through their player row.
CREATE INDEX IF NOT EXISTS challenges_host_idx ON challenges (host_id, created_at DESC);

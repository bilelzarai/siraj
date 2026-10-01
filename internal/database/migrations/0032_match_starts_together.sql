-- =============================================================================
-- 0032 — a match starts for everybody at the same moment
-- =============================================================================
--
-- Until now a remote match was asynchronous: everybody got the same questions
-- and played whenever they next opened the page, any time in the following
-- week. Two people who arranged to play each other had no way to actually play
-- each other — one of them answered on Tuesday and the other on Thursday, and
-- the scoreboard compared two solitary rounds.
--
-- Accepting now means "I am here", and nothing more. The host presses start,
-- and that one press opens a round for every player who has accepted, at the
-- same instant, with the same questions. Each still has their own
-- twenty-five-second clock per question — a shared countdown would mean one
-- person who walks away stalls the table for the full timeout on every
-- question — but nobody can begin before the others are in.
--
-- started_at is what makes that a fact rather than a sequence of events that
-- happened to be close together: it is set once, by the host, and every round
-- in the match is created under it.
ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS started_at timestamptz;

-- Matches already under way count as started, so the lobby does not reopen
-- underneath people who are halfway through.
UPDATE challenges c
   SET started_at = COALESCE(c.started_at, c.created_at)
 WHERE c.started_at IS NULL
   AND EXISTS (SELECT 1 FROM challenge_players p
                WHERE p.challenge_id = c.id AND p.session_id IS NOT NULL);

-- The lobby listing: matches waiting on their host to begin.
CREATE INDEX IF NOT EXISTS challenges_waiting_idx
    ON challenges (host_id)
    WHERE started_at IS NULL AND status IN ('pending', 'accepted');

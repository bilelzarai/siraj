-- A player who never turned up.
--
-- A match settled only when everybody had played, so one person who was invited
-- and never opened it held the result hostage for everyone else — four people
-- finished, and the scoreboard said "waiting" until the whole match expired a
-- week later.
--
-- Eliminated is not declined. Declining is something you did; being eliminated
-- is something that happened because you did not. Keeping them apart means the
-- scoreboard can say which, and nobody is recorded as having refused a match
-- they simply missed.
ALTER TABLE challenge_players
    DROP CONSTRAINT IF EXISTS challenge_players_state_check;
ALTER TABLE challenge_players
    ADD CONSTRAINT challenge_players_state_check
    CHECK (state IN ('invited', 'joined', 'played', 'declined', 'eliminated'));

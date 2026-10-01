-- Server-side timing for the speed bonus.
--
-- Points were computed from a duration the client reported and the server only
-- capped at the top end, so a client posting timeMs: 0 on every answer
-- collected the maximum speed bonus every time and nothing could notice. The
-- README's claim that the client is never trusted with scoring held for the
-- answer index and the cursor position, but not for the clock.
--
-- served_at is stamped the first time a position is shown and cleared when the
-- round advances, so it measures one question. Stamping it on every render
-- would let a player reset their own clock by refreshing the page.

ALTER TABLE game_sessions ADD COLUMN IF NOT EXISTS served_at timestamptz;

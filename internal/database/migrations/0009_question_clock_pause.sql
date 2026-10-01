-- Let a question's clock be put down and picked back up.
--
-- served_at alone makes the clock pure wall time: it starts when the question
-- is first shown and never stops. That is right for a refresh — reloading must
-- not rewind the timer — but wrong for the thing the dashboard explicitly
-- invites, "you have a round in progress, resume". Leaving for an hour and
-- coming back handed the player a question whose time was already spent, so
-- resuming meant losing the question on the spot.
--
-- elapsed_ms is the time banked on the current question from earlier visits.
-- The page banks its elapsed time when it is closed, which clears served_at;
-- the next render stamps served_at afresh, and the real total is the banked
-- time plus whatever the current visit adds. RecordAnswer resets both as the
-- round advances, so each position still gets its own clock.
--
-- It is the server that computes what to bank, from its own served_at, so a
-- client cannot bank a favourable number — the property the speed bonus rests
-- on is unchanged.

ALTER TABLE game_sessions
	ADD COLUMN IF NOT EXISTS elapsed_ms integer NOT NULL DEFAULT 0;

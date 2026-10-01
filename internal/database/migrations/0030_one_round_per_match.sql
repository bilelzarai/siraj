-- =============================================================================
-- 0030 — one round per player per match, and the database says so
-- =============================================================================
--
-- Accepting spends your one attempt at a match's questions. Pressing it twice
-- spent it twice: Start abandons whatever round is open and makes a new one —
-- which is right for "play again" and wrong here — so four taps in quick
-- succession produced four rounds, three of them abandoned, and whatever had
-- been answered in them gone.
--
-- A double tap is the gentle version. The same thing arrives as a retried
-- request on a slow connection, a second tab, or the back button, and none of
-- those are things a player did on purpose.
--
-- The handler now hands back the round that already exists instead of starting
-- another. This is the floor under that: one row per player per match, so no
-- path can make a second one whatever it believes about the first.

-- Anything already doubled up keeps the round that got furthest — the one with
-- answers in it is the one somebody actually played.
DELETE FROM game_sessions g
 WHERE g.challenge_id IS NOT NULL
   AND EXISTS (
       SELECT 1 FROM game_sessions keep
        WHERE keep.challenge_id = g.challenge_id
          AND keep.user_id = g.user_id
          AND keep.id <> g.id
          AND (keep.cursor, keep.started_at, keep.id) > (g.cursor, g.started_at, g.id));

CREATE UNIQUE INDEX IF NOT EXISTS game_sessions_one_per_match_idx
    ON game_sessions (user_id, challenge_id)
    WHERE challenge_id IS NOT NULL;

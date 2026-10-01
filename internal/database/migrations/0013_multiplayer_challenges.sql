-- A duel becomes a match: two players or ten.
--
-- The original shape put both players in the challenge row — challenger_id and
-- opponent_id, one score column each — which makes "one against one" a fact of
-- the schema rather than a choice. Everything a third player needs is already
-- per-player: an invitation to accept, a round of their own, a score, a moment
-- they finished. So those move to a table with a row per person, and the
-- challenge keeps only what the whole match shares: the questions, the
-- settings, and whether it is still open.
--
-- The existing duels are carried across rather than dropped. A finished duel is
-- somebody's history, and the profile counts it.

CREATE TABLE IF NOT EXISTS challenge_players (
    challenge_id uuid NOT NULL REFERENCES challenges(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The host is the person who opened the match. They are a player like any
    -- other; the distinction is only about who may change it.
    is_host      boolean NOT NULL DEFAULT false,

    -- invited → the match is waiting on them
    -- joined   → they are in and have not played yet
    -- played   → their round is finished and their score is in
    -- declined → they said no; the match goes ahead without them
    state        text NOT NULL DEFAULT 'invited'
                 CHECK (state IN ('invited', 'joined', 'played', 'declined')),

    session_id   uuid REFERENCES game_sessions(id) ON DELETE SET NULL,
    score        integer,
    joined_at    timestamptz,
    played_at    timestamptz,
    invited_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (challenge_id, user_id)
);

CREATE INDEX IF NOT EXISTS challenge_players_user_idx
    ON challenge_players (user_id, state);
CREATE INDEX IF NOT EXISTS challenge_players_challenge_idx
    ON challenge_players (challenge_id, state);

-- Carry every existing duel over: the challenger as host, the opponent as the
-- guest, each with whatever they had already done.
INSERT INTO challenge_players
    (challenge_id, user_id, is_host, state, session_id, score, joined_at, played_at, invited_at)
SELECT c.id, c.challenger_id, true,
       CASE WHEN c.challenger_score IS NOT NULL THEN 'played'
            WHEN c.challenger_session_id IS NOT NULL THEN 'joined'
            ELSE 'joined' END,
       c.challenger_session_id, c.challenger_score,
       c.created_at,
       CASE WHEN c.challenger_score IS NOT NULL THEN COALESCE(c.completed_at, c.created_at) END,
       c.created_at
  FROM challenges c
 WHERE NOT EXISTS (SELECT 1 FROM challenge_players p
                    WHERE p.challenge_id = c.id AND p.user_id = c.challenger_id);

INSERT INTO challenge_players
    (challenge_id, user_id, is_host, state, session_id, score, joined_at, played_at, invited_at)
SELECT c.id, c.opponent_id, false,
       CASE WHEN c.opponent_score IS NOT NULL THEN 'played'
            WHEN c.status = 'declined' THEN 'declined'
            WHEN c.opponent_session_id IS NOT NULL THEN 'joined'
            ELSE 'invited' END,
       c.opponent_session_id, c.opponent_score,
       CASE WHEN c.opponent_session_id IS NOT NULL THEN c.created_at END,
       CASE WHEN c.opponent_score IS NOT NULL THEN COALESCE(c.completed_at, c.created_at) END,
       c.created_at
  FROM challenges c
 WHERE NOT EXISTS (SELECT 1 FROM challenge_players p
                    WHERE p.challenge_id = c.id AND p.user_id = c.opponent_id);

-- A match needs a host, and the old columns no longer carry that.
ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS host_id uuid REFERENCES users(id) ON DELETE CASCADE;
UPDATE challenges SET host_id = challenger_id WHERE host_id IS NULL;

-- A rematch points back at what it replays, so a series can be followed.
ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS rematch_of uuid REFERENCES challenges(id) ON DELETE SET NULL;

-- The two-player columns stay for now, carrying the history they already hold.
-- Reading moves to challenge_players first; dropping them is a later migration,
-- once nothing reads them at all.

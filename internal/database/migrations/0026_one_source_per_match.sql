-- =============================================================================
-- 0026 — a match is played with one group of people, not a mixture of three
-- =============================================================================
--
-- The picker offers three groups because there are three ways somebody is
-- reachable: a friend, a guest at this device, a person standing in your room.
-- They are not interchangeable, and a match that mixes them is three different
-- games at once — a friend answering on their own phone in their own time, a
-- guest waiting for the phone to be handed to them, and a stranger in a room
-- who may walk out of it mid-match and stop being reachable at all.
--
-- So a match declares which group it is with, and every player in it comes from
-- that group. The host is exempt: they are the one doing the inviting.
--
-- Keeping that true is the point of the columns below, and it is kept by the
-- database rather than by the three call sites that build a match. The shape is
-- the standard one for "these rows must agree with their parent":
--
--   challenges       gains player_source, and a unique key on (id, source)
--   challenge_players gains origin, and a copy of the parent's source
--   a foreign key on (challenge_id, challenge_source) → (id, player_source)
--     makes the copy provably the parent's
--   a check on the row then compares origin against that copy
--
-- The copy is filled by a trigger, so no caller can set it wrong, and the
-- foreign key cascades on update, so the parent's source cannot be changed out
-- from under players who no longer match it — the check refuses it.

-- ------------------------------------------------- which group a match is with --

ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS player_source text NOT NULL DEFAULT 'friends';

ALTER TABLE challenges DROP CONSTRAINT IF EXISTS challenges_player_source_check;
ALTER TABLE challenges ADD CONSTRAINT challenges_player_source_check
    CHECK (player_source IN ('friends', 'device', 'room'));

-- Existing matches are labelled from what they actually hold. A match with a
-- temporary player in it was played round one device, whatever else is true of
-- it; everything else keeps the default.
UPDATE challenges c
   SET player_source = 'device'
 WHERE EXISTS (
     SELECT 1 FROM challenge_players p
       JOIN users u ON u.id = p.user_id
      WHERE p.challenge_id = c.id AND u.is_temporary);

-- The target of the composite key below.
ALTER TABLE challenges DROP CONSTRAINT IF EXISTS challenges_id_source_key;
ALTER TABLE challenges ADD CONSTRAINT challenges_id_source_key
    UNIQUE (id, player_source);

-- -------------------------------------------------- where each player came from --

ALTER TABLE challenge_players
    ADD COLUMN IF NOT EXISTS origin           text NOT NULL DEFAULT 'host',
    ADD COLUMN IF NOT EXISTS challenge_source text NOT NULL DEFAULT 'friends';

-- Carry the existing rows across before anything starts refusing them.
UPDATE challenge_players p
   SET challenge_source = c.player_source,
       origin = CASE WHEN p.is_host THEN 'host' ELSE c.player_source END
  FROM challenges c
 WHERE c.id = p.challenge_id;

ALTER TABLE challenge_players DROP CONSTRAINT IF EXISTS challenge_players_origin_check;
ALTER TABLE challenge_players ADD CONSTRAINT challenge_players_origin_check
    CHECK (origin IN ('host', 'friends', 'device', 'room'));

-- The rule, stated on the row: you are the host, or you came from the group
-- this match is with. There is no third possibility and no way to write one.
ALTER TABLE challenge_players DROP CONSTRAINT IF EXISTS challenge_players_one_source;
ALTER TABLE challenge_players ADD CONSTRAINT challenge_players_one_source
    CHECK (origin = 'host' OR origin = challenge_source);

-- And what makes `challenge_source` trustworthy: it is not a copy anybody may
-- edit, it is a foreign key into the parent's own value.
ALTER TABLE challenge_players DROP CONSTRAINT IF EXISTS challenge_players_source_fk;
ALTER TABLE challenge_players ADD CONSTRAINT challenge_players_source_fk
    FOREIGN KEY (challenge_id, challenge_source)
    REFERENCES challenges (id, player_source)
    ON UPDATE CASCADE ON DELETE CASCADE;

-- Filled from the parent on the way in, so a caller cannot get it wrong by
-- forgetting it — the same shape as the room flag on conversation_members.
CREATE OR REPLACE FUNCTION challenge_player_source() RETURNS trigger AS $$
BEGIN
    SELECT c.player_source INTO NEW.challenge_source
      FROM challenges c WHERE c.id = NEW.challenge_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS challenge_players_source ON challenge_players;
CREATE TRIGGER challenge_players_source
    BEFORE INSERT OR UPDATE OF challenge_id ON challenge_players
    FOR EACH ROW EXECUTE FUNCTION challenge_player_source();

CREATE INDEX IF NOT EXISTS challenge_players_origin_idx
    ON challenge_players (challenge_id, origin);

-- Rooms for people without an account.
--
-- Anonymous play is a complete game: a temporary player can play, can have
-- others at their device, and can be in a match. What they could not do is
-- find anybody to play with, because the one place in this application where
-- strangers meet is a room, and rooms were behind an account.
--
-- So a room can now be temporary, the way a player can. The rule is that the
-- two kinds never mix: a temporary room holds temporary players and nothing
-- else, and a permanent one holds accounts and nothing else. That is not
-- squeamishness about who talks to whom. A guest is swept when their time is
-- up, taking everything they wrote with them, and a room of accounts should
-- not be built partly out of people who will be deleted on a timer. Read the
-- other way: somebody playing anonymously should be able to open a room
-- without that room outliving them in a directory of real ones.

ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS is_temporary boolean NOT NULL DEFAULT false;

-- Filled from the owner rather than from whoever writes the row, for the same
-- reason conversation_members.is_room is: a flag that describes another row is
-- a flag that can disagree with it, and a trigger is the only place that
-- cannot be skipped by a caller who forgot.
CREATE OR REPLACE FUNCTION conversation_temporary_flag() RETURNS trigger AS $$
BEGIN
    -- A direct thread has no owner and is never temporary: a guest has no
    -- friends and no private threads, so there is nobody for one to be with.
    NEW.is_temporary := COALESCE(
        (SELECT u.is_temporary FROM users u WHERE u.id = NEW.owner_id), false);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS conversations_temporary_flag ON conversations;
CREATE TRIGGER conversations_temporary_flag
    BEFORE INSERT ON conversations
    FOR EACH ROW EXECUTE FUNCTION conversation_temporary_flag();

CREATE INDEX IF NOT EXISTS conversations_room_kind_idx
    ON conversations (is_temporary, last_message_at DESC) WHERE kind = 'room';

-- A room and the people in it agree about being temporary.
--
-- Enforced in the trigger that already fills is_room rather than as a CHECK
-- over two denormalised columns, which is how challenge_players does the
-- equivalent job. The CHECK pattern earns its extra columns when the facts it
-- compares can change under it; these cannot. A conversation's kind is set
-- once at insert, and users.is_temporary never flips — there is no path that
-- turns a guest into an account, by design, because the guest is the thing
-- that gets deleted.
CREATE OR REPLACE FUNCTION conversation_member_room_flag() RETURNS trigger AS $$
DECLARE
    room_temporary boolean;
    person_temporary boolean;
BEGIN
    SELECT (c.kind = 'room'), c.is_temporary INTO NEW.is_room, room_temporary
      FROM conversations c WHERE c.id = NEW.conversation_id;

    IF NEW.is_room THEN
        SELECT u.is_temporary INTO person_temporary
          FROM users u WHERE u.id = NEW.user_id;
        IF person_temporary IS DISTINCT FROM room_temporary THEN
            RAISE EXCEPTION 'a % room cannot hold a % player',
                CASE WHEN room_temporary THEN 'temporary' ELSE 'permanent' END,
                CASE WHEN person_temporary THEN 'temporary' ELSE 'permanent' END
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- The trigger it replaces fired on INSERT OR UPDATE OF conversation_id, and
-- that is still the set of writes that can change either answer.
DROP TRIGGER IF EXISTS conversation_members_room_flag ON conversation_members;
CREATE TRIGGER conversation_members_room_flag
    BEFORE INSERT OR UPDATE OF conversation_id ON conversation_members
    FOR EACH ROW EXECUTE FUNCTION conversation_member_room_flag();

-- Nothing to backfill: every conversation that exists today was opened by an
-- account, so is_temporary is false for all of them and the default is right.

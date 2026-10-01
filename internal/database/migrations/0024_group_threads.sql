-- Conversations that are not between exactly two people.
--
-- A thread used to be a pair: user_a, user_b, one row, unique on the pair. That
-- shape is why there was no way to talk to three people at once, and no way to
-- have a thread that is about a subject rather than about a person.
--
-- Three kinds now share the table:
--   direct  two people, a private thread, exactly what existed before
--   group   people someone chose, everyone sees everyone
--   room    an open space for one topic, which anyone may join
--
-- The pair columns stay for direct threads — every query that reads them still
-- works, and the pair is still what makes "the conversation with this person"
-- a findable thing — but they are nullable now, and the uniqueness that kept
-- one pair to one thread is scoped to direct so two groups can exist.
ALTER TABLE conversations ADD COLUMN kind text NOT NULL DEFAULT 'direct'
    CHECK (kind IN ('direct', 'group', 'room'));
ALTER TABLE conversations ADD COLUMN title text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN topic text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN owner_id uuid REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE conversations ALTER COLUMN user_a DROP NOT NULL;
ALTER TABLE conversations ALTER COLUMN user_b DROP NOT NULL;
ALTER TABLE conversations DROP CONSTRAINT conversations_user_a_user_b_key;
CREATE UNIQUE INDEX conversations_pair_idx
    ON conversations (user_a, user_b) WHERE kind = 'direct';

-- A direct thread is its pair and has no name; anything else is the reverse.
ALTER TABLE conversations ADD CONSTRAINT conversations_direct_is_a_pair
    CHECK ((kind = 'direct') = (user_a IS NOT NULL AND user_b IS NOT NULL));
ALTER TABLE conversations ADD CONSTRAINT conversations_named
    CHECK (kind = 'direct' OR title <> '');

-- Who is in a thread, and how far each of them has read.
--
-- read_at on the message itself answers "has it been read" for two people and
-- nothing at all for five: one column cannot hold five answers. A member's own
-- high-water mark can, and it is what the unread count is counted against for
-- every thread that is not a pair.
CREATE TABLE conversation_members (
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            text NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member')),
    last_read_id    bigint NOT NULL DEFAULT 0,
    joined_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX conversation_members_user_idx ON conversation_members (user_id);

-- Everyone already in a thread is a member of it. Without this the pair
-- threads that exist today would belong to nobody the moment membership
-- becomes the thing that decides who can read them.
INSERT INTO conversation_members (conversation_id, user_id, role)
SELECT id, user_a, 'member' FROM conversations WHERE user_a IS NOT NULL
UNION ALL
SELECT id, user_b, 'member' FROM conversations WHERE user_b IS NOT NULL;

-- A room is findable by name, because it is a place rather than a person.
CREATE INDEX conversations_room_idx ON conversations (kind, last_message_at DESC)
    WHERE kind = 'room';

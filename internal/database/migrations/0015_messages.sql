-- Messages, as a conversation rather than a log.
--
-- Three things were missing that a person expects from any thread they have
-- used before: knowing whether what they sent was read, being able to take back
-- something they should not have sent, and seeing where they left off.
--
-- Deleting is a tombstone, not a removal. The row stays so the other person's
-- thread does not silently lose a message they have already read and replied
-- to — what they see is that it was withdrawn, which is the truth, rather than
-- a gap where a sentence used to be.
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

-- Reading a thread is the common query and it reads from the end. This is the
-- index for "the newest N in this conversation", which is every open thread.
CREATE INDEX IF NOT EXISTS messages_thread_live_idx
    ON messages (conversation_id, id DESC) WHERE deleted_at IS NULL;

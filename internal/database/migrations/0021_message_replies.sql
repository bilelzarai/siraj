-- Which message a message answers.
--
-- ON DELETE SET NULL rather than CASCADE: a reply is still a sentence somebody
-- wrote, and losing the thing it answered is not a reason to lose it too. The
-- quote simply stops being shown.
ALTER TABLE messages ADD COLUMN reply_to_id bigint REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX messages_reply_to_idx ON messages (reply_to_id) WHERE reply_to_id IS NOT NULL;

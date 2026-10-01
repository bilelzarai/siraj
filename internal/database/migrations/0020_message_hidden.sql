-- One message, hidden from one person.
--
-- Taking a message back (`messages.deleted_at`) removes it from the
-- conversation for both sides, and only its sender may do it. This is the other
-- half: anybody may drop a line from their own copy, and the other person's
-- thread is untouched. Same reasoning as conversation_state.cleared_at, one
-- message at a time.
CREATE TABLE message_hidden (
    message_id bigint NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    uuid   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hidden_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, user_id)
);

CREATE INDEX message_hidden_user_idx ON message_hidden (user_id);

-- Per-participant conversation state.
--
-- Deleting a conversation and muting it are both one person's business: the
-- other side keeps their copy of everything that was said. So neither can live
-- on `conversations`, which is shared.
--
-- cleared_at is a floor rather than a delete. The messages stay — the other
-- person is still replying to them — they are simply no longer yours to see.
CREATE TABLE conversation_state (
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cleared_at      timestamptz,
    muted_at        timestamptz,
    PRIMARY KEY (conversation_id, user_id)
);

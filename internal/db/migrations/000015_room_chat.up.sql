-- Room chat (RM-05): what players say to each other while they wait in the lobby and
-- between questions. Messages belong to the room and go with it.
CREATE TABLE room_messages (
  id         bigserial PRIMARY KEY,
  room_id    uuid NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  body       text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 200),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX room_messages_room_idx ON room_messages (room_id, created_at);

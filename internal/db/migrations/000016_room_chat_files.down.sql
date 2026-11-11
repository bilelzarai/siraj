DELETE FROM room_messages WHERE body IS NULL;
ALTER TABLE room_messages DROP CONSTRAINT room_messages_said_something;
ALTER TABLE room_messages ALTER COLUMN body SET NOT NULL;
ALTER TABLE room_messages
  ADD CONSTRAINT room_messages_body_check
  CHECK (char_length(btrim(body)) BETWEEN 1 AND 200);
ALTER TABLE room_messages DROP COLUMN kind, DROP COLUMN upload_id;

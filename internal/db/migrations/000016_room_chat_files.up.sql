-- Room chat carries what a player attached, not only what they typed (MS-02, RM-05).
-- The upload itself already has a home; the line only needs to point at it.
ALTER TABLE room_messages
  ADD COLUMN upload_id uuid REFERENCES uploads (id) ON DELETE SET NULL,
  ADD COLUMN kind      text NOT NULL DEFAULT 'text'
             CHECK (kind IN ('text', 'image', 'file', 'voice'));

-- A line is either words or a file; it is never empty.
ALTER TABLE room_messages DROP CONSTRAINT room_messages_body_check;
ALTER TABLE room_messages ALTER COLUMN body DROP NOT NULL;
ALTER TABLE room_messages
  ADD CONSTRAINT room_messages_said_something
  CHECK (upload_id IS NOT NULL OR char_length(btrim(body)) BETWEEN 1 AND 200);

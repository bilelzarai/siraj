-- Bytes somebody sent: a photo, a voice note, a file, a profile picture.
--
-- The row describes the upload; the bytes live on disk under UPLOAD_DIR, named
-- by this id. Keeping them out of the database keeps a thread of photographs
-- from turning every backup into a media library.
CREATE TABLE attachments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('image', 'audio', 'file')),
    mime       text NOT NULL,
    name       text NOT NULL,
    bytes      bigint NOT NULL CHECK (bytes > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX attachments_owner_idx ON attachments (owner_id);

-- What a message carries besides its text. ON DELETE SET NULL for the same
-- reason as reply_to_id: losing the file is not a reason to lose the sentence.
ALTER TABLE messages ADD COLUMN attachment_id uuid REFERENCES attachments(id) ON DELETE SET NULL;

-- A message may now be an attachment with nothing said about it.
ALTER TABLE messages ADD CONSTRAINT messages_have_something
    CHECK (body <> '' OR attachment_id IS NOT NULL OR deleted_at IS NOT NULL);

-- Module 7 · Messages (MS-01…MS-04). Cursor paging on uuid v7, unread by last_read_message_id.

CREATE TYPE conversation_kind AS ENUM ('direct', 'group', 'room', 'system');
CREATE TYPE message_kind      AS ENUM ('text', 'file', 'image', 'voice', 'challenge', 'system');

CREATE TABLE uploads (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  owner_id    uuid REFERENCES users (id) ON DELETE CASCADE,
  kind        text NOT NULL CHECK (kind IN ('image', 'file', 'voice')),
  mime        text NOT NULL,
  bytes       integer NOT NULL,
  duration_ms integer,
  storage_key text NOT NULL,
  file_name   text,
  width       integer,
  height      integer,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX uploads_owner ON uploads (owner_id, created_at DESC);

CREATE TABLE conversations (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  kind            conversation_kind NOT NULL DEFAULT 'direct',
  title           text,
  room_id         uuid REFERENCES rooms (id) ON DELETE SET NULL,
  direct_key      text UNIQUE,                 -- sorted pair of user ids: one thread per two people
  last_message_at timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER conversations_updated_at BEFORE UPDATE ON conversations FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE conversation_members (
  conversation_id      uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
  user_id              uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role                 text NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'owner')),
  pinned               boolean NOT NULL DEFAULT false,
  muted                boolean NOT NULL DEFAULT false,
  last_read_message_id uuid,
  hidden_at            timestamptz,
  joined_at            timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (conversation_id, user_id)
);
CREATE INDEX conversation_members_user ON conversation_members (user_id);

CREATE TABLE messages (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),   -- time-ordered: the cursor
  conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
  sender_id       uuid REFERENCES users (id) ON DELETE SET NULL,
  kind            message_kind NOT NULL DEFAULT 'text',
  body            text,
  reply_to_id     uuid REFERENCES messages (id) ON DELETE SET NULL,
  upload_id       uuid REFERENCES uploads (id) ON DELETE SET NULL,
  payload         jsonb NOT NULL DEFAULT '{}',
  deleted_at      timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_thread ON messages (conversation_id, id DESC);

CREATE TABLE message_hidden (                           -- delete for me
  message_id uuid NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (message_id, user_id)
);

CREATE TABLE message_reports (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  message_id  uuid NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
  reporter_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  reason      text NOT NULL,
  note        text,
  status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'actioned')),
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX message_reports_open ON message_reports (status, created_at);

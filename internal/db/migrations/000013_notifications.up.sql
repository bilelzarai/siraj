-- Module 10 · Notifications, support and the admin audit log (OT-01, OT-03, AD-03).

CREATE TYPE notification_kind AS ENUM (
  'friend_request', 'friend_accepted',
  'challenge_received', 'challenge_participant_finished', 'challenge_results_ready', 'challenge_removed',
  'message', 'room_invite', 'duel_result',
  'question_approved', 'question_rejected', 'question_sent_back',
  'support_reply', 'achievement', 'streak_reminder', 'system'
);

CREATE TABLE notifications (
  id      uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  kind    notification_kind NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}',
  link    text,
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_inbox ON notifications (user_id, read_at, id DESC);

CREATE TYPE ticket_topic  AS ENUM ('question', 'account', 'report_user', 'bug', 'suggestion', 'other');
CREATE TYPE ticket_status AS ENUM ('open', 'waiting', 'resolved', 'closed');

CREATE TABLE support_tickets (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  number      text NOT NULL UNIQUE,              -- T-2092
  user_id     uuid REFERENCES users (id) ON DELETE SET NULL,
  email       citext,
  topic       ticket_topic  NOT NULL,
  subject     text NOT NULL,
  status      ticket_status NOT NULL DEFAULT 'open',
  priority    text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high')),
  assignee_id uuid REFERENCES users (id) ON DELETE SET NULL,
  context     jsonb NOT NULL DEFAULT '{}',       -- device, category, question, reported user
  resolved_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
  -- A ticket needs a way to reply (an account or an email); the service checks that.
  -- No constraint here on purpose: a ticket outlives the account that opened it.
);
CREATE TRIGGER support_tickets_updated_at BEFORE UPDATE ON support_tickets FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX support_tickets_inbox ON support_tickets (status, topic, created_at DESC);
CREATE INDEX support_tickets_user  ON support_tickets (user_id, created_at DESC);

-- A running counter so ticket numbers read like T-2092 instead of a uuid.
CREATE SEQUENCE support_ticket_number START 2001;

CREATE TABLE ticket_messages (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  ticket_id  uuid NOT NULL REFERENCES support_tickets (id) ON DELETE CASCADE,
  author_id  uuid REFERENCES users (id) ON DELETE SET NULL,
  body       text NOT NULL,
  is_staff   boolean NOT NULL DEFAULT false,
  is_note    boolean NOT NULL DEFAULT false,     -- internal note, never shown to the player
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ticket_messages_thread ON ticket_messages (ticket_id, id);

CREATE TABLE ticket_attachments (
  ticket_id uuid NOT NULL REFERENCES support_tickets (id) ON DELETE CASCADE,
  upload_id uuid NOT NULL REFERENCES uploads (id) ON DELETE CASCADE,
  PRIMARY KEY (ticket_id, upload_id)
);

-- Every staff write, in the same transaction as the change (AD-03).
CREATE TABLE audit_log (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
  action     text NOT NULL,                      -- question.publish, user.ban, domain.update …
  entity     text NOT NULL,                      -- question | user | domain | category | ticket | comment
  entity_id  text,
  diff       jsonb NOT NULL DEFAULT '{}',
  ip         inet,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_recent ON audit_log (created_at DESC);
CREATE INDEX audit_log_actor  ON audit_log (actor_id, created_at DESC);
CREATE INDEX audit_log_entity ON audit_log (entity, entity_id);

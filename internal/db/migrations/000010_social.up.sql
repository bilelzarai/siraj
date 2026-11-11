-- Module 6 · Social: friends, requests, blocks and invite links (FR-01).

-- One row per pair, always stored with user_a < user_b.
CREATE TABLE friendships (
  user_a     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  user_b     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_a, user_b),
  CHECK (user_a < user_b)
);
CREATE INDEX friendships_b ON friendships (user_b);

CREATE TABLE friend_requests (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  from_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  to_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled')),
  responded_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CHECK (from_id <> to_id)
);
CREATE UNIQUE INDEX friend_requests_pending ON friend_requests (from_id, to_id) WHERE status = 'pending';
CREATE INDEX friend_requests_inbox ON friend_requests (to_id, status, created_at DESC);

-- Checked before messages, challenges and invites.
CREATE TABLE blocks (
  blocker_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  blocked_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (blocker_id, blocked_id),
  CHECK (blocker_id <> blocked_id)
);
CREATE INDEX blocks_blocked ON blocks (blocked_id);

CREATE TABLE invites (
  code       text PRIMARY KEY,
  inviter_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  uses       integer NOT NULL DEFAULT 0,
  max_uses   integer NOT NULL DEFAULT 0,          -- 0 = unlimited
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invites_inviter ON invites (inviter_id);

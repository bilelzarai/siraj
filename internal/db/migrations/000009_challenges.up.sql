-- Module 5 · Challenges: one model for 2+ players (CH-02…CH-12, 03-BACKEND-CHANGES §1).
-- The creator is a participant too; a 1 v 1 is simply two participants.

CREATE TYPE challenge_status   AS ENUM ('open', 'closed', 'cancelled');
CREATE TYPE participant_status AS ENUM ('pending', 'playing', 'played', 'declined', 'removed');
CREATE TYPE challenge_result   AS ENUM ('won', 'lost', 'draw');

CREATE TABLE challenges (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  creator_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  source         text NOT NULL DEFAULT 'bank' CHECK (source IN ('bank', 'set', 'written')),
  scope          jsonb NOT NULL DEFAULT '{}',
  set_id         uuid,                                  -- module 9, FK added with the sets migration
  difficulty     smallint,
  question_count smallint NOT NULL,
  seconds_per_question smallint NOT NULL DEFAULT 20,
  message        text,
  deadline_at    timestamptz,
  is_guest       boolean NOT NULL DEFAULT false,
  status         challenge_status NOT NULL DEFAULT 'open',
  closed_at      timestamptz,
  closed_by      uuid REFERENCES users (id) ON DELETE SET NULL,  -- NULL = closed by the system
  close_reason   text CHECK (close_reason IN ('all_played', 'deadline', 'host')),
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER challenges_updated_at BEFORE UPDATE ON challenges FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX challenges_creator ON challenges (creator_id, created_at DESC);
CREATE INDEX challenges_open_deadline ON challenges (status, deadline_at) WHERE status = 'open';

-- Everyone gets the same questions, frozen when the challenge is created.
CREATE TABLE challenge_questions (
  challenge_id    uuid NOT NULL REFERENCES challenges (id) ON DELETE CASCADE,
  position        smallint NOT NULL,
  question_id     bigint REFERENCES questions (id) ON DELETE RESTRICT,
  set_question_id bigint,
  PRIMARY KEY (challenge_id, position),
  CHECK (question_id IS NOT NULL OR set_question_id IS NOT NULL)
);

CREATE TABLE challenge_participants (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  challenge_id uuid NOT NULL REFERENCES challenges (id) ON DELETE CASCADE,
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  status       participant_status NOT NULL DEFAULT 'pending',
  game_id      uuid REFERENCES games (id) ON DELETE SET NULL,
  started_at   timestamptz,
  finished_at  timestamptz,
  score        integer,
  correct      smallint,
  rank         smallint,                -- filled at close
  result       challenge_result,        -- filled at close
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (challenge_id, user_id)
);
CREATE TRIGGER challenge_participants_updated_at BEFORE UPDATE ON challenge_participants
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX challenge_participants_user ON challenge_participants (user_id, status);

-- Guests share a link instead of picking friends.
CREATE TABLE challenge_links (
  code         text PRIMARY KEY,
  challenge_id uuid NOT NULL REFERENCES challenges (id) ON DELETE CASCADE,
  expires_at   timestamptz NOT NULL,
  uses         integer NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE games ADD CONSTRAINT games_challenge_fk
  FOREIGN KEY (challenge_id) REFERENCES challenges (id) ON DELETE SET NULL;

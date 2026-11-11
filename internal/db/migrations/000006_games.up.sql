-- Module 4 · Games (every mode), rooms and duel ratings
-- One model for solo, live room, duel, challenge and this-device rounds.

CREATE TYPE game_mode AS ENUM ('solo', 'live', 'duel', 'challenge', 'device');
CREATE TYPE game_status AS ENUM ('lobby', 'playing', 'finished', 'abandoned');

CREATE TABLE games (
  id                  uuid PRIMARY KEY DEFAULT uuidv7(),
  mode                game_mode NOT NULL,
  status              game_status NOT NULL DEFAULT 'playing',
  owner_id            uuid REFERENCES users (id) ON DELETE CASCADE, -- who started it
  scope               jsonb NOT NULL DEFAULT '{}',   -- {domain_id, category_ids, set_id, kind}
  difficulty          smallint,                      -- NULL = mixed
  question_count      smallint NOT NULL,
  seconds_per_question smallint NOT NULL DEFAULT 20, -- 0 = no timer
  room_id             uuid,
  challenge_id        uuid,
  started_at          timestamptz NOT NULL DEFAULT now(),
  finished_at         timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER games_updated_at BEFORE UPDATE ON games FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX games_owner ON games (owner_id, created_at DESC);
CREATE INDEX games_room ON games (room_id) WHERE room_id IS NOT NULL;
CREATE INDEX games_challenge ON games (challenge_id) WHERE challenge_id IS NOT NULL;

-- The questions of a game, in a fixed order, the same for every player.
CREATE TABLE game_questions (
  game_id     uuid NOT NULL REFERENCES games (id) ON DELETE CASCADE,
  position    smallint NOT NULL,
  question_id bigint REFERENCES questions (id) ON DELETE RESTRICT,
  set_question_id bigint,                     -- player-made questions (module 9)
  PRIMARY KEY (game_id, position),
  CHECK (question_id IS NOT NULL OR set_question_id IS NOT NULL)
);

CREATE TABLE game_players (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  game_id      uuid NOT NULL REFERENCES games (id) ON DELETE CASCADE,
  user_id      uuid REFERENCES users (id) ON DELETE CASCADE,
  local_name   text,                          -- pass-the-device players (DV-01)
  team         smallint CHECK (team IN (0, 1)),
  score        integer NOT NULL DEFAULT 0,
  correct      smallint NOT NULL DEFAULT 0,
  rank         smallint,
  xp_gained    integer NOT NULL DEFAULT 0,
  rating_delta integer NOT NULL DEFAULT 0,
  joined_at    timestamptz NOT NULL DEFAULT now(),
  finished_at  timestamptz,
  CHECK (user_id IS NOT NULL OR local_name IS NOT NULL)
);
CREATE UNIQUE INDEX game_players_user ON game_players (game_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX game_players_of_user ON game_players (user_id, game_id);

-- The biggest table: partitioned by month from day one.
CREATE TABLE game_answers (
  game_id    uuid NOT NULL,
  player_id  uuid NOT NULL,
  position   smallint NOT NULL,
  choice     smallint CHECK (choice BETWEEN 0 AND 3), -- NULL = no answer (time's up or skip)
  is_correct boolean NOT NULL DEFAULT false,
  time_ms    integer NOT NULL DEFAULT 0,
  points     integer NOT NULL DEFAULT 0,
  lifeline   text CHECK (lifeline IN ('fifty', 'skip')),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (game_id, player_id, position, created_at)
) PARTITION BY RANGE (created_at);

-- A partition for this month, the next two, and a catch-all so an insert never fails.
DO $$
DECLARE
  start_month date := date_trunc('month', current_date)::date;
  i int;
  from_day date;
  to_day date;
BEGIN
  FOR i IN 0..2 LOOP
    from_day := (start_month + (i || ' month')::interval)::date;
    to_day   := (start_month + ((i + 1) || ' month')::interval)::date;
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS game_answers_%s PARTITION OF game_answers FOR VALUES FROM (%L) TO (%L)',
      to_char(from_day, 'YYYY_MM'), from_day, to_day);
  END LOOP;
END $$;
CREATE TABLE IF NOT EXISTS game_answers_default PARTITION OF game_answers DEFAULT;

-- Live rooms (RM-01…RM-08). The lobby and the live state live in the hub; results land here.
CREATE TABLE rooms (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  code          char(6) NOT NULL,                 -- letters + digits, no look-alikes
  host_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  is_guest_room boolean NOT NULL DEFAULT false,   -- guests can only join guest rooms
  max_players   smallint NOT NULL DEFAULT 8 CHECK (max_players BETWEEN 2 AND 12),
  chat_enabled  boolean NOT NULL DEFAULT true,
  settings      jsonb NOT NULL DEFAULT '{}',      -- {mode: ffa|teams, teams: [[..],[..]], scope, difficulty, count, seconds}
  status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'playing', 'closed')),
  closes_at     timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER rooms_updated_at BEFORE UPDATE ON rooms FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE UNIQUE INDEX rooms_open_code ON rooms (code) WHERE status <> 'closed';

CREATE TABLE room_members (
  room_id   uuid NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
  user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  is_ready  boolean NOT NULL DEFAULT false,
  kicked    boolean NOT NULL DEFAULT false,
  joined_at timestamptz NOT NULL DEFAULT now(),
  left_at   timestamptz,
  PRIMARY KEY (room_id, user_id)
);

ALTER TABLE games ADD CONSTRAINT games_room_fk FOREIGN KEY (room_id) REFERENCES rooms (id) ON DELETE SET NULL;

-- Elo per domain; guests never get a row (DU-02).
CREATE TABLE duel_ratings (
  user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  domain_id bigint NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
  rating    integer NOT NULL DEFAULT 1200,
  wins      integer NOT NULL DEFAULT 0,
  losses    integer NOT NULL DEFAULT 0,
  draws     integer NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, domain_id)
);
CREATE TRIGGER duel_ratings_updated_at BEFORE UPDATE ON duel_ratings FOR EACH ROW EXECUTE FUNCTION set_updated_at();

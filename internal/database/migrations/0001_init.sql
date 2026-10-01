-- =============================================================================
-- 0001_init.sql — core schema
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ---------------------------------------------------------------- identity --

CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username        citext NOT NULL UNIQUE,
    email           citext NOT NULL UNIQUE,
    password_hash   text   NOT NULL,
    display_name    text   NOT NULL,
    bio             text   NOT NULL DEFAULT '',
    country         text   NOT NULL DEFAULT '',
    avatar_seed     text   NOT NULL DEFAULT '',
    locale          text   NOT NULL DEFAULT 'ar',
    theme           text   NOT NULL DEFAULT 'system',
    xp              integer NOT NULL DEFAULT 0,
    coins           integer NOT NULL DEFAULT 0,
    games_played    integer NOT NULL DEFAULT 0,
    games_won       integer NOT NULL DEFAULT 0,
    best_streak     integer NOT NULL DEFAULT 0,
    is_admin        boolean NOT NULL DEFAULT false,
    last_seen_at    timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX users_xp_idx       ON users (xp DESC);
CREATE INDEX users_username_trgm ON users USING gin (username gin_trgm_ops);
CREATE INDEX users_display_trgm  ON users USING gin (display_name gin_trgm_ops);

CREATE TABLE sessions (
    id          text PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_agent  text NOT NULL DEFAULT '',
    ip          text NOT NULL DEFAULT '',
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx    ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- ------------------------------------------------------------ game content --

CREATE TABLE categories (
    id          serial PRIMARY KEY,
    slug        text NOT NULL UNIQUE,
    icon        text NOT NULL DEFAULT '📖',
    color       text NOT NULL DEFAULT '#0ea5a4',
    sort_order  integer NOT NULL DEFAULT 0,
    is_active   boolean NOT NULL DEFAULT true
);

CREATE TABLE category_translations (
    category_id integer NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    locale      text    NOT NULL,
    name        text    NOT NULL,
    description text    NOT NULL DEFAULT '',
    PRIMARY KEY (category_id, locale)
);

CREATE TABLE questions (
    id            serial PRIMARY KEY,
    category_id   integer NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    difficulty    smallint NOT NULL DEFAULT 1 CHECK (difficulty BETWEEN 1 AND 3),
    points        integer  NOT NULL DEFAULT 10,
    correct_index smallint NOT NULL CHECK (correct_index BETWEEN 0 AND 3),
    source        text     NOT NULL DEFAULT '',
    is_active     boolean  NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX questions_pick_idx ON questions (category_id, difficulty) WHERE is_active;

CREATE TABLE question_translations (
    question_id integer NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    locale      text    NOT NULL,
    prompt      text    NOT NULL,
    choices     text[]  NOT NULL CHECK (array_length(choices, 1) = 4),
    explanation text    NOT NULL DEFAULT '',
    PRIMARY KEY (question_id, locale)
);

-- --------------------------------------------------------------- gameplay --

CREATE TYPE game_mode   AS ENUM ('solo', 'challenge', 'daily');
CREATE TYPE game_status AS ENUM ('active', 'finished', 'abandoned');

CREATE TABLE game_sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mode            game_mode   NOT NULL DEFAULT 'solo',
    status          game_status NOT NULL DEFAULT 'active',
    category_id     integer REFERENCES categories(id) ON DELETE SET NULL,
    difficulty      smallint NOT NULL DEFAULT 0,
    locale          text     NOT NULL DEFAULT 'ar',
    question_ids    integer[] NOT NULL DEFAULT '{}',
    cursor          smallint NOT NULL DEFAULT 0,
    total_questions smallint NOT NULL DEFAULT 10,
    correct_count   smallint NOT NULL DEFAULT 0,
    best_streak     smallint NOT NULL DEFAULT 0,
    score           integer  NOT NULL DEFAULT 0,
    xp_earned       integer  NOT NULL DEFAULT 0,
    duration_ms     integer  NOT NULL DEFAULT 0,
    challenge_id    uuid,
    started_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz
);

CREATE INDEX game_sessions_user_idx      ON game_sessions (user_id, started_at DESC);
CREATE INDEX game_sessions_challenge_idx ON game_sessions (challenge_id);
CREATE UNIQUE INDEX game_sessions_one_active_idx
    ON game_sessions (user_id) WHERE status = 'active';

CREATE TABLE game_answers (
    id             bigserial PRIMARY KEY,
    session_id     uuid    NOT NULL REFERENCES game_sessions(id) ON DELETE CASCADE,
    question_id    integer NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    position       smallint NOT NULL,
    selected_index smallint NOT NULL,
    is_correct     boolean  NOT NULL,
    time_ms        integer  NOT NULL DEFAULT 0,
    points_awarded integer  NOT NULL DEFAULT 0,
    answered_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_id, position)
);

CREATE INDEX game_answers_session_idx ON game_answers (session_id, position);

CREATE TYPE challenge_status AS ENUM
    ('pending', 'accepted', 'declined', 'completed', 'expired');

CREATE TABLE challenges (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    challenger_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    opponent_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id          integer REFERENCES categories(id) ON DELETE SET NULL,
    difficulty           smallint NOT NULL DEFAULT 0,
    question_ids         integer[] NOT NULL,
    status               challenge_status NOT NULL DEFAULT 'pending',
    challenger_session_id uuid REFERENCES game_sessions(id) ON DELETE SET NULL,
    opponent_session_id   uuid REFERENCES game_sessions(id) ON DELETE SET NULL,
    challenger_score     integer,
    opponent_score       integer,
    winner_id            uuid REFERENCES users(id) ON DELETE SET NULL,
    message              text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    expires_at           timestamptz NOT NULL DEFAULT now() + interval '7 days',
    completed_at         timestamptz,
    CHECK (challenger_id <> opponent_id)
);

CREATE INDEX challenges_opponent_idx   ON challenges (opponent_id, status, created_at DESC);
CREATE INDEX challenges_challenger_idx ON challenges (challenger_id, status, created_at DESC);

ALTER TABLE game_sessions
    ADD CONSTRAINT game_sessions_challenge_fk
    FOREIGN KEY (challenge_id) REFERENCES challenges(id) ON DELETE SET NULL;

-- ----------------------------------------------------------------- social --

CREATE TYPE friendship_status AS ENUM ('pending', 'accepted', 'blocked');

CREATE TABLE friendships (
    id           bigserial PRIMARY KEY,
    requester_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    addressee_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       friendship_status NOT NULL DEFAULT 'pending',
    created_at   timestamptz NOT NULL DEFAULT now(),
    responded_at timestamptz,
    UNIQUE (requester_id, addressee_id),
    CHECK (requester_id <> addressee_id)
);

CREATE INDEX friendships_addressee_idx ON friendships (addressee_id, status);
CREATE INDEX friendships_requester_idx ON friendships (requester_id, status);

-- Pair is stored normalised: user_a < user_b, so a conversation is unique.
CREATE TABLE conversations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_message_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_a, user_b),
    CHECK (user_a < user_b)
);

CREATE INDEX conversations_a_idx ON conversations (user_a, last_message_at DESC);
CREATE INDEX conversations_b_idx ON conversations (user_b, last_message_at DESC);

CREATE TABLE messages (
    id              bigserial PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body            text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    read_at         timestamptz
);

CREATE INDEX messages_conv_idx   ON messages (conversation_id, id DESC);
CREATE INDEX messages_unread_idx ON messages (conversation_id, sender_id) WHERE read_at IS NULL;

CREATE TABLE notifications (
    id         bigserial PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text NOT NULL,
    payload    jsonb NOT NULL DEFAULT '{}'::jsonb,
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);

-- --------------------------------------------------------------- rewards --

CREATE TABLE badges (
    id         serial PRIMARY KEY,
    slug       text NOT NULL UNIQUE,
    icon       text NOT NULL DEFAULT '🏅',
    threshold  integer NOT NULL DEFAULT 0,
    kind       text NOT NULL DEFAULT 'xp'
);

CREATE TABLE badge_translations (
    badge_id    integer NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
    locale      text    NOT NULL,
    name        text    NOT NULL,
    description text    NOT NULL DEFAULT '',
    PRIMARY KEY (badge_id, locale)
);

CREATE TABLE user_badges (
    user_id   uuid    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    badge_id  integer NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
    earned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, badge_id)
);

-- Module 3 · Questions and content health
-- The bank of questions, their three languages, and everything the admin screens need
-- to keep it healthy: reviews, ratings, comments, reports and possible duplicates.

CREATE TYPE question_status AS ENUM ('draft', 'review', 'published', 'archived');
CREATE TYPE review_decision AS ENUM ('approved', 'rejected', 'sent_back');

CREATE TABLE questions (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, -- the #604 players see
  category_id   bigint NOT NULL REFERENCES categories (id) ON DELETE RESTRICT,
  difficulty    smallint NOT NULL CHECK (difficulty BETWEEN 1 AND 3),
  points        integer NOT NULL,                      -- 10 / 20 / 30 (games/scoring.go)
  correct_index smallint NOT NULL CHECK (correct_index BETWEEN 0 AND 3),
  status        question_status NOT NULL DEFAULT 'draft',
  source        text NOT NULL DEFAULT '',              -- every answer cites its source
  main_lang     text NOT NULL DEFAULT 'en' REFERENCES languages (code),
  author_id     uuid REFERENCES users (id) ON DELETE SET NULL,
  published_at  timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  deleted_at    timestamptz
);
CREATE TRIGGER questions_updated_at BEFORE UPDATE ON questions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- Hot path: picking questions by category and difficulty among the published ones.
CREATE INDEX questions_pick ON questions (category_id, difficulty) WHERE status = 'published' AND deleted_at IS NULL;
CREATE INDEX questions_status ON questions (status, created_at DESC);

CREATE TABLE question_translations (
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  lang        text NOT NULL REFERENCES languages (code),
  body        text NOT NULL,
  answers     text[] NOT NULL CHECK (cardinality(answers) = 4), -- same order in every language
  explanation text NOT NULL DEFAULT '',
  PRIMARY KEY (question_id, lang)
);
CREATE INDEX question_translations_body_trgm ON question_translations USING gin (body gin_trgm_ops);

CREATE TABLE question_revisions (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  editor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
  snapshot    jsonb NOT NULL,
  note        text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX question_revisions_question ON question_revisions (question_id, created_at DESC);

CREATE TABLE question_reviews (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  reviewer_id uuid REFERENCES users (id) ON DELETE SET NULL,
  decision    review_decision NOT NULL,
  note        text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX question_reviews_question ON question_reviews (question_id, created_at DESC);

-- Players rate a question right after answering it (GP-05); 1–2 stars ask for a reason.
CREATE TABLE question_ratings (
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  stars       smallint NOT NULL CHECK (stars BETWEEN 1 AND 5),
  reason      text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (question_id, user_id)
);
CREATE TRIGGER question_ratings_updated_at BEFORE UPDATE ON question_ratings FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX question_ratings_low ON question_ratings (question_id) WHERE stars <= 2;

CREATE TABLE question_comments (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  user_id     uuid REFERENCES users (id) ON DELETE SET NULL,
  body        text NOT NULL,
  status      text NOT NULL DEFAULT 'visible' CHECK (status IN ('visible', 'hidden')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER question_comments_updated_at BEFORE UPDATE ON question_comments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX question_comments_question ON question_comments (question_id, created_at DESC);

CREATE TABLE comment_reports (
  comment_id  uuid NOT NULL REFERENCES question_comments (id) ON DELETE CASCADE,
  reporter_id uuid REFERENCES users (id) ON DELETE SET NULL,
  reason      text NOT NULL,
  status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'actioned')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (comment_id, reporter_id)
);

-- "Report a problem" from the feedback panel (RS-04).
CREATE TABLE question_reports (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  user_id     uuid REFERENCES users (id) ON DELETE SET NULL,
  reason      text NOT NULL CHECK (reason IN ('answer_wrong', 'unclear', 'translation', 'two_correct', 'typo', 'other')),
  note        text NOT NULL DEFAULT '',
  lang        text REFERENCES languages (code),
  status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
  resolved_by uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX question_reports_open ON question_reports (status, created_at DESC);

-- Filled by the worker with similarity(); the admin merges or marks them different.
CREATE TABLE question_duplicates (
  question_a bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  question_b bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  similarity real NOT NULL,
  status     text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'merged', 'dismissed')),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (question_a, question_b),
  CHECK (question_a < question_b)
);

-- One question of the day per domain per day; the player gets today's from one of
-- their own domains, rotating by day (HM-02, endpoints 22 and 23).
CREATE TABLE daily_questions (
  day         date NOT NULL,
  domain_id   bigint NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (day, domain_id)
);

-- One try a day: the answer of a player to their question of the day.
CREATE TABLE daily_answers (
  day         date NOT NULL,
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  question_id bigint NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
  choice      smallint NOT NULL CHECK (choice BETWEEN 0 AND 3),
  is_correct  boolean NOT NULL,
  points      integer NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (day, user_id)
);
CREATE INDEX daily_answers_question ON daily_answers (question_id);

-- categories.question_count is a cache; this keeps it true when questions are
-- published, retired or deleted.
CREATE OR REPLACE FUNCTION refresh_category_question_count() RETURNS trigger AS $$
DECLARE
  touched bigint[];
BEGIN
  touched := ARRAY(SELECT DISTINCT unnest(ARRAY[COALESCE(NEW.category_id, 0), COALESCE(OLD.category_id, 0)]));
  UPDATE categories c
  SET question_count = (
    SELECT count(*) FROM questions q
    WHERE q.category_id = c.id AND q.status = 'published' AND q.deleted_at IS NULL
  )
  WHERE c.id = ANY (touched);
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER questions_count_after
AFTER INSERT OR UPDATE OF status, category_id, deleted_at OR DELETE ON questions
FOR EACH ROW EXECUTE FUNCTION refresh_category_question_count();

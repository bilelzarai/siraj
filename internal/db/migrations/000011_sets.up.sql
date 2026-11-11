-- Module 9 · Player-made questions (MQ-01…MQ-03). A set is playable at 5+ questions.

CREATE TABLE question_sets (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  owner_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name       text NOT NULL,
  main_lang  text NOT NULL DEFAULT 'en' REFERENCES languages (code),
  plays      integer NOT NULL DEFAULT 0,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER question_sets_updated_at BEFORE UPDATE ON question_sets FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX question_sets_owner ON question_sets (owner_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE set_questions (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  set_id        uuid NOT NULL REFERENCES question_sets (id) ON DELETE CASCADE,
  position      smallint NOT NULL,
  category_id   bigint REFERENCES categories (id) ON DELETE SET NULL,
  difficulty    smallint NOT NULL DEFAULT 1 CHECK (difficulty BETWEEN 1 AND 3),
  points        smallint NOT NULL DEFAULT 10,
  correct_index smallint NOT NULL CHECK (correct_index BETWEEN 0 AND 3),
  main_lang     text NOT NULL DEFAULT 'en' REFERENCES languages (code),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER set_questions_updated_at BEFORE UPDATE ON set_questions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX set_questions_set ON set_questions (set_id, position);

CREATE TABLE set_question_translations (
  set_question_id bigint NOT NULL REFERENCES set_questions (id) ON DELETE CASCADE,
  lang            text   NOT NULL REFERENCES languages (code),
  body            text   NOT NULL,
  answers         text[] NOT NULL CHECK (cardinality(answers) = 4),
  explanation     text,
  PRIMARY KEY (set_question_id, lang)
);

CREATE TABLE bank_suggestions (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  set_question_id bigint NOT NULL REFERENCES set_questions (id) ON DELETE CASCADE,
  user_id         uuid   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  status          text   NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected')),
  question_id     bigint REFERENCES questions (id) ON DELETE SET NULL,
  note            text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER bank_suggestions_updated_at BEFORE UPDATE ON bank_suggestions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX bank_suggestions_status ON bank_suggestions (status, created_at);

-- The game and challenge tables kept set_question_id free of a key until the sets existed.
ALTER TABLE game_questions      ADD CONSTRAINT game_questions_set_question_fk
  FOREIGN KEY (set_question_id) REFERENCES set_questions (id) ON DELETE RESTRICT;
ALTER TABLE challenge_questions ADD CONSTRAINT challenge_questions_set_question_fk
  FOREIGN KEY (set_question_id) REFERENCES set_questions (id) ON DELETE RESTRICT;
ALTER TABLE challenges          ADD CONSTRAINT challenges_set_fk
  FOREIGN KEY (set_id) REFERENCES question_sets (id) ON DELETE SET NULL;

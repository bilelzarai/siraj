-- Question types (§8). A question is not one shape: single choice, several right
-- answers, true/false, a typed word, a number.
--
-- `answer_kind` on the content type is what decides how an answer is checked, so a
-- new shape is a row plus one case in the checker — never a new table, and never a
-- change to the engine, which only ever asks "was this right?".
ALTER TABLE content_types ADD COLUMN answer_kind text NOT NULL DEFAULT 'none';

-- The kinds that already exist keep behaving as they did.
UPDATE content_types SET answer_kind = 'single' WHERE code = 'question';

INSERT INTO content_types (code, icon, fields, playable, answer_kind, position) VALUES
  ('multi_choice', 'check',    ARRAY['body','answers','explanation'], true, 'multi',   5),
  ('true_false',   'check',    ARRAY['body','explanation'],           true, 'boolean', 6),
  ('text_answer',  'text',     ARRAY['body','answer','explanation'],  true, 'text',    7),
  ('number',       'question', ARRAY['body','answer','explanation'],  true, 'number',  8)
ON CONFLICT (code) DO NOTHING;

-- What counts as right, per item. Kept out of the translations because the answer to
-- "how many pillars" is the same number in every language; only the wording differs.
ALTER TABLE content_items ADD COLUMN answer jsonb NOT NULL DEFAULT '{}';

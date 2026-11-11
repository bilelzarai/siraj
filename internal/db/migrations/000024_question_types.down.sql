ALTER TABLE content_items DROP COLUMN IF EXISTS answer;
DELETE FROM content_types WHERE code IN ('multi_choice','true_false','text_answer','number');
ALTER TABLE content_types DROP COLUMN IF EXISTS answer_kind;

DROP TRIGGER IF EXISTS question_translations_to_content ON question_translations;
DROP TRIGGER IF EXISTS questions_to_content ON questions;
DROP FUNCTION IF EXISTS question_text_mirror_content;
DROP FUNCTION IF EXISTS questions_mirror_content;
DELETE FROM content_item_translations WHERE item_id IN (SELECT id FROM content_items WHERE question_id IS NOT NULL);
DELETE FROM content_items WHERE question_id IS NOT NULL;

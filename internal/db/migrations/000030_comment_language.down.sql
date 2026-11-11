DROP INDEX IF EXISTS question_comments_lang;
DROP TRIGGER IF EXISTS question_comments_lang ON question_comments;
DROP FUNCTION IF EXISTS comments_take_author_lang;
ALTER TABLE question_comments DROP COLUMN IF EXISTS lang;

-- A comment declares the language it was written in (§16).
--
-- The moderation screen shows it beside the author, and a moderator filtering by
-- language needs it; without the column the only way to guess is to read the text.
-- It defaults to the writer's own language, which is what they were reading the
-- question in when they wrote.
ALTER TABLE question_comments ADD COLUMN lang text REFERENCES languages (code);

UPDATE question_comments c SET lang = u.lang FROM users u WHERE u.id = c.user_id AND c.lang IS NULL;
UPDATE question_comments SET lang = 'en' WHERE lang IS NULL;
ALTER TABLE question_comments ALTER COLUMN lang SET DEFAULT 'en';
ALTER TABLE question_comments ALTER COLUMN lang SET NOT NULL;

CREATE FUNCTION comments_take_author_lang() RETURNS trigger AS $$
BEGIN
  IF NEW.lang IS NULL OR NEW.lang = 'en' THEN
    SELECT COALESCE(u.lang, NEW.lang, 'en') INTO NEW.lang FROM users u WHERE u.id = NEW.user_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER question_comments_lang BEFORE INSERT ON question_comments
  FOR EACH ROW EXECUTE FUNCTION comments_take_author_lang();

CREATE INDEX question_comments_lang ON question_comments (lang);

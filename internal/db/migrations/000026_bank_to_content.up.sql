-- The v1 bank becomes content. Every question is copied into content_items as an
-- item of kind 'question', keeping its node, its points, its difficulty and what
-- counts as right.
--
-- This is the last join between the two models (AD-2). After it, one table holds
-- everything a player can be given — questions, flashcards, and whatever is
-- registered next — and the engine can read that one table instead of two.
--
-- `questions` is not dropped here. It still has the readers the engine and the admin
-- console use, and CUT-4 removes it once they are moved. Until then a trigger keeps
-- the copy true, so the two cannot tell different stories about the same question.

INSERT INTO content_items
  (node_id, type_code, owner_id, visibility, status, difficulty, points, tags,
   answer, question_id, created_at)
SELECT q.node_id, 'question', NULL,
       CASE WHEN q.status = 'published' THEN 'public' ELSE 'private' END::node_visibility,
       CASE q.status
         WHEN 'published' THEN 'published'
         WHEN 'review'    THEN 'review'
         WHEN 'archived'  THEN 'archived'
         ELSE 'draft'
       END::node_status,
       q.difficulty, q.points, '{}',
       jsonb_build_object('index', q.correct_index),
       q.id, q.created_at
FROM questions q
WHERE q.node_id IS NOT NULL AND q.deleted_at IS NULL;

-- The wording, per language, in the shape every kind uses.
INSERT INTO content_item_translations (item_id, lang, fields)
SELECT i.id, t.lang,
       jsonb_build_object(
         'body', t.body,
         'answers', to_jsonb(t.answers),
         'explanation', COALESCE(t.explanation, ''))
FROM content_items i
JOIN question_translations t ON t.question_id = i.question_id
WHERE i.question_id IS NOT NULL;

-- A question written or edited from here on keeps its copy in step. One place, so no
-- caller — console, CSV import, a hand-written statement — can leave them disagreeing.
CREATE FUNCTION questions_mirror_content() RETURNS trigger AS $$
DECLARE
  item_status node_status;
  item_vis    node_visibility;
BEGIN
  item_status := CASE NEW.status
                   WHEN 'published' THEN 'published'
                   WHEN 'review'    THEN 'review'
                   WHEN 'archived'  THEN 'archived'
                   ELSE 'draft' END::node_status;
  item_vis := CASE WHEN NEW.status = 'published' THEN 'public' ELSE 'private' END::node_visibility;

  IF TG_OP = 'INSERT' THEN
    INSERT INTO content_items
      (node_id, type_code, visibility, status, difficulty, points, tags, answer, question_id)
    VALUES (NEW.node_id, 'question', item_vis, item_status, NEW.difficulty, NEW.points, '{}',
            jsonb_build_object('index', NEW.correct_index), NEW.id);
  ELSE
    UPDATE content_items SET
      node_id    = NEW.node_id,
      visibility = item_vis,
      status     = item_status,
      difficulty = NEW.difficulty,
      points     = NEW.points,
      answer     = jsonb_build_object('index', NEW.correct_index),
      deleted_at = NEW.deleted_at
    WHERE question_id = NEW.id;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER questions_to_content AFTER INSERT OR UPDATE ON questions
  FOR EACH ROW EXECUTE FUNCTION questions_mirror_content();

-- The same for the wording.
CREATE FUNCTION question_text_mirror_content() RETURNS trigger AS $$
BEGIN
  INSERT INTO content_item_translations (item_id, lang, fields)
  SELECT i.id, NEW.lang,
         jsonb_build_object('body', NEW.body, 'answers', to_jsonb(NEW.answers),
                            'explanation', COALESCE(NEW.explanation, ''))
  FROM content_items i WHERE i.question_id = NEW.question_id
  ON CONFLICT (item_id, lang) DO UPDATE SET fields = EXCLUDED.fields;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER question_translations_to_content
  AFTER INSERT OR UPDATE ON question_translations
  FOR EACH ROW EXECUTE FUNCTION question_text_mirror_content();

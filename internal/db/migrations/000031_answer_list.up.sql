-- Answers are frozen in two shapes, and a reader has to cope with both.
--
-- The mirrored bank stores them as a jsonb array (migration 26 copied `text[]`
-- straight across with to_jsonb); content written in the new model stores them as a
-- pipe-separated string, which is what a form produces. Both are legitimate and both
-- are already in frozen history, so neither can be rewritten.
--
-- One function reads either, and both branches of the game query call it — rather
-- than each guessing, which is how a round came back with one answer instead of four.
CREATE FUNCTION content_answer_list(texts jsonb, want text) RETURNS text[] AS $$
DECLARE field jsonb;
BEGIN
  field := COALESCE(texts -> want -> 'answers', texts -> 'en' -> 'answers');
  IF field IS NULL THEN
    RETURN NULL;
  END IF;
  CASE jsonb_typeof(field)
    WHEN 'array'  THEN RETURN ARRAY(SELECT jsonb_array_elements_text(field));
    WHEN 'string' THEN RETURN string_to_array(field #>> '{}', '|');
    ELSE RETURN NULL;
  END CASE;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

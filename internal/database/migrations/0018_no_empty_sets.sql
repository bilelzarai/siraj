-- A set with nothing in it is not a set.
--
-- Creating one asked only for a name, so the screen filled with named nothings:
-- a person tries the feature, writes a name, gets distracted, and is left with
-- a shelf of empty labels they now have to tidy up. A collection is created
-- with its first questions instead.
--
-- The empty ones already made are removed here. Nothing is lost: they held
-- nothing, which is the whole complaint.
DELETE FROM question_sets s
 WHERE NOT EXISTS (
       SELECT 1 FROM question_set_items i WHERE i.set_id = s.id);

-- Empty the database of content and player data, keeping the schema, the language
-- list and the achievement definitions that the migrations install.
--
-- Used by `make db-clear` to remove sample content from a database that was created
-- before the seeds were dropped. Everything it deletes — domains, categories,
-- questions, users, games, rooms, challenges, messages — is re-entered for real in
-- the admin console, or created by players.
DO $$
DECLARE
  keep text[] := ARRAY['schema_migrations', 'languages', 'achievements', 'achievement_translations'];
  names text;
BEGIN
  SELECT string_agg(format('%I.%I', schemaname, tablename), ', ')
    INTO names
    FROM pg_tables
   WHERE schemaname = 'public'
     AND NOT (tablename = ANY (keep))
     -- game_answers is partitioned: truncating the parent empties every partition.
     AND tablename !~ '^game_answers_';
  IF names IS NOT NULL THEN
    EXECUTE 'TRUNCATE TABLE ' || names || ' RESTART IDENTITY CASCADE';
  END IF;
END $$;

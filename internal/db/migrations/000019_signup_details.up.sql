-- Creating an account asks for the name and the password before the code goes out
-- (the design's "1 · Your details → 2 · Verify"). The answers wait on the code row
-- rather than in the page, so a plain password never travels back through a form
-- and a reloaded tab loses nothing.
ALTER TABLE auth_codes ADD COLUMN display_name text;
ALTER TABLE auth_codes ADD COLUMN password_hash text;

-- Marking a poorly-rated question as read.
--
-- The alert listed questions players had marked down and offered one action:
-- edit. There was no way to say "I have read this and it is fine", so a
-- question three people disliked and a moderator had judged sound stayed on
-- the list and kept the navigation badge red. A badge that is always lit stops
-- being read at all, which costs the alert its whole purpose.
--
-- A timestamp rather than a flag, so the question comes back if players keep
-- rating it: dismissing settles the ratings that exist now, not every rating
-- the question will ever receive.
ALTER TABLE questions
    ADD COLUMN IF NOT EXISTS ratings_reviewed_at timestamptz;

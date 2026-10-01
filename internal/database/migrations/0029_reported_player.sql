-- =============================================================================
-- 0029 — a report about a player says which player
-- =============================================================================
--
-- "Report a player" is one of the six kinds of support message, and the form
-- had nowhere to name the player. The only place their name could go was the
-- body, in prose, which means staff reading a report have to find the person
-- from a description and hope they have found the right one — and that two
-- reports about the same person look like two unrelated messages.
--
-- The same form asked everybody for a question category, which is a field that
-- means something for "about a question" and nothing for the other five. A
-- report of abuse asking which category of question it belongs to is a form
-- that has not understood what it is being told.

ALTER TABLE support_tickets
    ADD COLUMN IF NOT EXISTS reported_user_id uuid REFERENCES users(id) ON DELETE SET NULL;

-- Everything staff needs to see about one person at once: how many reports,
-- and whether any are still open.
CREATE INDEX IF NOT EXISTS support_tickets_reported_idx
    ON support_tickets (reported_user_id, created_at DESC)
    WHERE reported_user_id IS NOT NULL;

-- A category belongs to a message about a question. It was being collected
-- from every kind because the field was on every form; the ones it could not
-- mean anything for are cleared, so the admin screens stop showing a category
-- against a report of abuse.
UPDATE support_tickets
   SET category_id = NULL
 WHERE category_id IS NOT NULL
   AND kind NOT IN ('question', 'suggestion');

-- =============================================================================
-- 0040_ticket_device.sql — what the reporter was looking at
--
-- A bug report carries page_path already, so staff know which screen broke.
-- They do not know what it broke on, and "it does not work on my phone" is the
-- most common thing a ticket says. The browser string is already arriving on
-- the request that opens the ticket and was simply being dropped.
--
-- Copied onto the ticket rather than read back from sessions: a session is
-- deleted at sign-out and purged when it expires, while the ticket outlives
-- both, and a temporary player has no session row to join to at all.
-- =============================================================================

ALTER TABLE support_tickets
    ADD COLUMN IF NOT EXISTS user_agent text NOT NULL DEFAULT '';

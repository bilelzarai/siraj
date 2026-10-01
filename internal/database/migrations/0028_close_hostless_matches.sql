-- =============================================================================
-- 0028 — close the matches the old decline path left standing
-- =============================================================================
--
-- Before cancelling existed, a host who declined was treated as an ordinary
-- player: their row went to 'declined' and the match carried on without them.
-- What that left on screen was a lobby inviting two people to accept a game
-- whose host had walked out of it, under the words "waiting for at least 2
-- players before this can start" — which it never would.
--
-- Those rows are still in the table. The new rule is applied to them once here;
-- the janitor applies it continuously from now on, so this is a one-off tidy
-- rather than the mechanism.

-- A match whose host is out is over, and it is cancelled rather than declined:
-- declined is a person's answer, cancelled is what happened to the match.
UPDATE challenges c
   SET status = 'cancelled',
       cancelled_at = COALESCE(c.cancelled_at, now()),
       cancelled_by = COALESCE(c.cancelled_by, c.host_id)
 WHERE c.status IN ('pending', 'accepted')
   AND EXISTS (
       SELECT 1 FROM challenge_players p
        WHERE p.challenge_id = c.id AND p.is_host
          AND p.state IN ('declined', 'eliminated'));

-- And a match nobody accepted, whose invitations have all been refused.
UPDATE challenges c
   SET status = 'cancelled',
       cancelled_at = COALESCE(c.cancelled_at, now()),
       cancelled_by = COALESCE(c.cancelled_by, c.host_id)
 WHERE c.status IN ('pending', 'accepted')
   AND NOT EXISTS (
       SELECT 1 FROM challenge_players p
        WHERE p.challenge_id = c.id AND NOT p.is_host
          AND p.state IN ('joined', 'played'))
   AND NOT EXISTS (
       SELECT 1 FROM challenge_players p
        WHERE p.challenge_id = c.id AND NOT p.is_host
          AND p.state = 'invited');

-- Cancelling frees everybody, including here.
UPDATE challenge_players p
   SET state = 'eliminated'
  FROM challenges c
 WHERE c.id = p.challenge_id
   AND c.status = 'cancelled'
   AND p.state IN ('invited', 'joined');

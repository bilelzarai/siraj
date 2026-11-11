-- Module 10 · Support tickets (OT-03, endpoints 106–110) and the admin inbox.

-- name: CreateTicket :one
INSERT INTO support_tickets (number, user_id, email, topic, subject, context)
VALUES ('T-' || nextval('support_ticket_number'), sqlc.narg('user_id'), sqlc.narg('email'),
        @topic, @subject, @context)
RETURNING *;

-- name: AddTicketMessage :one
INSERT INTO ticket_messages (ticket_id, author_id, body, is_staff, is_note)
VALUES (@ticket_id, sqlc.narg('author_id'), @body, @is_staff, @is_note)
RETURNING *;

-- name: MyTickets :many
SELECT * FROM support_tickets WHERE user_id = @user_id ORDER BY created_at DESC LIMIT @lim OFFSET @off;

-- name: CountMyTickets :one
SELECT count(*) FROM support_tickets WHERE user_id = @user_id;

-- name: GetTicket :one
SELECT * FROM support_tickets WHERE id = @id;

-- name: GetTicketByNumber :one
SELECT * FROM support_tickets WHERE number = @number;

-- name: TicketThread :many
-- include_notes is false for the player, true for staff.
SELECT m.*, u.display_name, u.username
FROM ticket_messages m
LEFT JOIN users u ON u.id = m.author_id
WHERE m.ticket_id = @ticket_id AND (@include_notes::boolean OR NOT m.is_note)
ORDER BY m.id;

-- name: SetTicketStatus :exec
UPDATE support_tickets
SET status = @status, resolved_at = CASE WHEN @status::ticket_status IN ('resolved','closed') THEN now() END
WHERE id = @id;

-- name: AssignTicket :exec
UPDATE support_tickets SET assignee_id = sqlc.narg('assignee_id') WHERE id = @id;

-- name: AddTicketAttachment :exec
INSERT INTO ticket_attachments (ticket_id, upload_id) VALUES (@ticket_id, @upload_id) ON CONFLICT DO NOTHING;

-- name: AdminTickets :many
-- The admin inbox (A03): filter by status and topic, newest first.
SELECT t.*, u.display_name AS user_name, u.username AS user_username,
       a.display_name AS assignee_name,
       (SELECT count(*) FROM ticket_messages m WHERE m.ticket_id = t.id)::int AS message_count
FROM support_tickets t
LEFT JOIN users u ON u.id = t.user_id
LEFT JOIN users a ON a.id = t.assignee_id
WHERE (@status::text = '' OR t.status::text = @status)
  AND (@topic::text  = '' OR t.topic::text  = @topic)
  -- The design also filters on how urgent a ticket is and on who holds it:
  -- "mine" is the signed-in member of staff, "unassigned" is nobody at all.
  AND (@priority::text = '' OR t.priority = @priority)
  AND (@assignee::text = ''
       OR (@assignee = 'unassigned' AND t.assignee_id IS NULL)
       OR (@assignee = 'mine' AND t.assignee_id = sqlc.narg('me')::uuid))
  AND (@q::text = '' OR t.subject ILIKE '%' || @q || '%' OR t.number ILIKE '%' || @q || '%')
ORDER BY t.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountAdminTickets :one
SELECT count(*) FROM support_tickets t
WHERE (@status::text = '' OR t.status::text = @status)
  AND (@topic::text  = '' OR t.topic::text  = @topic)
  AND (@priority::text = '' OR t.priority = @priority)
  AND (@assignee::text = ''
       OR (@assignee = 'unassigned' AND t.assignee_id IS NULL)
       OR (@assignee = 'mine' AND t.assignee_id = sqlc.narg('me')::uuid))
  AND (@q::text = '' OR t.subject ILIKE '%' || @q || '%' OR t.number ILIKE '%' || @q || '%');

-- name: TicketCountsByStatus :many
SELECT status, count(*)::int AS n FROM support_tickets GROUP BY status;

-- name: SetTicketPriority :exec
UPDATE support_tickets SET priority = @priority WHERE id = @id;

-- name: SupportKPIs :one
-- The four tiles above the A03 inbox.
SELECT
  count(*) FILTER (WHERE status = 'open')::int AS open,
  count(*) FILTER (WHERE status = 'waiting')::int AS waiting,
  count(*) FILTER (WHERE priority = 'high' AND status <> 'resolved')::int AS high,
  count(*) FILTER (WHERE assignee_id = sqlc.narg('me')::uuid AND status <> 'resolved')::int AS mine
FROM support_tickets;

-- name: TicketTopicCounts :many
SELECT topic, count(*)::int AS n FROM support_tickets
WHERE status <> 'resolved' GROUP BY topic ORDER BY topic;

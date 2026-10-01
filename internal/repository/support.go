package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

const ticketColumns = `
	t.id, t.user_id, t.kind::text, t.status::text, t.priority::text, t.subject,
	t.locale, t.category_id, t.question_id, t.page_path, t.reported_user_id, t.assigned_to,
	t.last_message_at, t.last_sender, t.user_unread, t.staff_unread,
	t.message_count, t.created_at, t.updated_at, t.resolved_at,
	u.username, u.display_name, u.avatar_seed,
	COALESCE(a.username, ''), COALESCE(cat.slug, ''),
	COALESCE(rep.username, ''), COALESCE(rep.display_name, '')`

const ticketJoins = `
	  JOIN users u ON u.id = t.user_id
	  LEFT JOIN users a ON a.id = t.assigned_to
	  LEFT JOIN categories cat ON cat.id = t.category_id
	  LEFT JOIN users rep ON rep.id = t.reported_user_id`

func scanTicket(row pgx.Row) (*models.Ticket, error) {
	var t models.Ticket
	err := row.Scan(&t.ID, &t.UserID, &t.Kind, &t.Status, &t.Priority, &t.Subject,
		&t.Locale, &t.CategoryID, &t.QuestionID, &t.PagePath, &t.ReportedUserID, &t.AssignedTo,
		&t.LastMessageAt, &t.LastSender, &t.UserUnread, &t.StaffUnread,
		&t.MessageCount, &t.CreatedAt, &t.UpdatedAt, &t.ResolvedAt,
		&t.Username, &t.DisplayName, &t.AvatarSeed,
		&t.AssigneeUsername, &t.CategorySlug,
		&t.ReportedUsername, &t.ReportedDisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTicket opens a conversation with its first message, in one
// transaction so a ticket can never exist with nothing in it.
func (r *Repo) CreateTicket(ctx context.Context, t *models.Ticket, body string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO support_tickets
				(user_id, kind, status, priority, subject, locale,
				 category_id, question_id, page_path, reported_user_id, message_count)
			VALUES ($1, $2::ticket_kind, 'open', $3::ticket_priority, $4, $5, $6, $7, $8, $9, 1)
			RETURNING id, created_at, updated_at, last_message_at`,
			t.UserID, t.Kind, t.Priority, t.Subject, t.Locale,
			t.CategoryID, t.QuestionID, t.PagePath, t.ReportedUserID,
		).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.LastMessageAt)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO support_messages (ticket_id, sender_id, sender_role, body)
			VALUES ($1, $2, 'user', $3)`, t.ID, t.UserID, body)
		return err
	})
}

// TicketFilter is the triage filter set the staff inbox works from.
type TicketFilter struct {
	Query    string
	Kind     string
	Status   string
	Priority string
	Assignee string // a user id, "me", "unassigned", or ""
	ViewerID uuid.UUID
	Waiting  bool // only tickets whose last message came from the player
	Limit    int
	Offset   int
}

// Tickets lists the staff inbox. Ordering puts urgent work first, then the
// longest-waiting conversation.
func (r *Repo) Tickets(ctx context.Context, f TicketFilter) ([]*models.Ticket, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 40
	}

	where := []string{"true"}
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	if q := strings.TrimSpace(f.Query); q != "" {
		add("(t.subject ILIKE '%%' || $%d || '%%' OR u.username ILIKE '%%' || $%[1]d || '%%')", q)
	}
	if f.Kind != "" {
		add("t.kind = $%d::ticket_kind", f.Kind)
	}
	if f.Status != "" {
		add("t.status = $%d::ticket_status", f.Status)
	} else {
		// The default view is the work queue, not the archive.
		where = append(where, "t.status <> 'closed'")
	}
	if f.Priority != "" {
		add("t.priority = $%d::ticket_priority", f.Priority)
	}
	switch f.Assignee {
	case "":
	case "unassigned":
		where = append(where, "t.assigned_to IS NULL")
	case "me":
		add("t.assigned_to = $%d", f.ViewerID)
	default:
		if id, err := uuid.Parse(f.Assignee); err == nil {
			add("t.assigned_to = $%d", id)
		}
	}
	if f.Waiting {
		where = append(where, "t.last_sender = 'user'")
	}

	clause := strings.Join(where, " AND ")

	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM support_tickets t`+ticketJoins+` WHERE `+clause, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT `+ticketColumns+`
		  FROM support_tickets t`+ticketJoins+`
		 WHERE `+clause+`
		 ORDER BY
		   CASE t.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1
		                   WHEN 'normal' THEN 2 ELSE 3 END,
		   t.last_message_at DESC
		 LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// MyTickets lists a player's own conversations.
func (r *Repo) MyTickets(ctx context.Context, userID uuid.UUID) ([]*models.Ticket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+ticketColumns+`
		  FROM support_tickets t`+ticketJoins+`
		 WHERE t.user_id = $1
		 ORDER BY t.last_message_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Ticket loads one conversation. A player may only load their own; staff may
// load any, which is expressed by passing staff=true.
func (r *Repo) Ticket(ctx context.Context, id uuid.UUID, viewer uuid.UUID, staff bool) (*models.Ticket, error) {
	if staff {
		return scanTicket(r.pool.QueryRow(ctx,
			`SELECT `+ticketColumns+` FROM support_tickets t`+ticketJoins+` WHERE t.id = $1`, id))
	}
	return scanTicket(r.pool.QueryRow(ctx,
		`SELECT `+ticketColumns+` FROM support_tickets t`+ticketJoins+`
		  WHERE t.id = $1 AND t.user_id = $2`, id, viewer))
}

// TicketMessages returns the thread. Internal staff notes are withheld from
// the player by the caller passing includeInternal=false.
func (r *Repo) TicketMessages(ctx context.Context, ticketID uuid.UUID, includeInternal bool) ([]*models.TicketMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.ticket_id, m.sender_id, m.sender_role, m.body,
		       m.internal, m.created_at, COALESCE(u.display_name, ''),
		       COALESCE(u.username, ''), COALESCE(u.avatar_seed, '')
		  FROM support_messages m
		  LEFT JOIN users u ON u.id = m.sender_id
		 WHERE m.ticket_id = $1 AND ($2 OR NOT m.internal)
		 ORDER BY m.id`, ticketID, includeInternal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.TicketMessage
	for rows.Next() {
		var m models.TicketMessage
		if err := rows.Scan(&m.ID, &m.TicketID, &m.SenderID, &m.SenderRole,
			&m.Body, &m.Internal, &m.CreatedAt, &m.SenderName,
			&m.SenderUsername, &m.SenderAvatar); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// ReplyToTicket appends a message and moves the ticket's state to match who
// is now waiting on whom.
func (r *Repo) ReplyToTicket(ctx context.Context, ticketID uuid.UUID, senderID uuid.UUID, role, body string, internal bool) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO support_messages (ticket_id, sender_id, sender_role, body, internal)
			VALUES ($1, $2, $3, $4, $5)`, ticketID, senderID, role, body, internal)
		if err != nil {
			return err
		}

		// An internal note is bookkeeping: it must not change whose turn it
		// is, nor mark anything unread for the player.
		if internal {
			_, err = tx.Exec(ctx,
				`UPDATE support_tickets SET updated_at = now() WHERE id = $1`, ticketID)
			return err
		}

		if role == "staff" {
			_, err = tx.Exec(ctx, `
				UPDATE support_tickets
				   SET last_message_at = now(), last_sender = 'staff',
				       user_unread = user_unread + 1, staff_unread = 0,
				       message_count = message_count + 1,
				       status = CASE WHEN status IN ('open', 'in_progress')
				                     THEN 'waiting_user'::ticket_status ELSE status END,
				       updated_at = now()
				 WHERE id = $1`, ticketID)
			return err
		}

		// A player reply reopens a resolved ticket: they are not satisfied.
		_, err = tx.Exec(ctx, `
			UPDATE support_tickets
			   SET last_message_at = now(), last_sender = 'user',
			       staff_unread = staff_unread + 1, user_unread = 0,
			       message_count = message_count + 1,
			       status = CASE WHEN status IN ('resolved', 'closed', 'waiting_user')
			                     THEN 'open'::ticket_status ELSE status END,
			       resolved_at = NULL,
			       updated_at = now()
			 WHERE id = $1`, ticketID)
		return err
	})
}

// UpdateTicket applies a staff triage change. Empty fields are left alone.
// ReopenTicket puts a settled ticket back in the queue, for its owner.
func (r *Repo) ReopenTicket(ctx context.Context, id, userID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE support_tickets
		   SET status = 'open', resolved_at = NULL, updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status IN ('resolved', 'closed')`,
		id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CloseTicketAsOwner lets the person who opened a ticket close it themselves.
//
// Status was staff-only, so someone who had solved their own problem could only
// reply "never mind" — which left the ticket in the queue and the badge lit for
// a moderator to clear by hand. Scoped to the owner and to closing: triage,
// priority and assignment stay with the staff who do them.
func (r *Repo) CloseTicketAsOwner(ctx context.Context, id, userID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE support_tickets
		   SET status = 'closed', resolved_at = now(), updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status <> 'closed'`, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) UpdateTicket(ctx context.Context, id uuid.UUID, status, priority, kind string, assignee *uuid.UUID, clearAssignee bool) error {
	sets := []string{"updated_at = now()"}
	args := []any{id}
	add := func(clause string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}

	if status != "" {
		add("status = $%d::ticket_status", status)
		if status == models.TicketResolved || status == models.TicketClosed {
			sets = append(sets, "resolved_at = now()")
		} else {
			sets = append(sets, "resolved_at = NULL")
		}
	}
	if priority != "" {
		add("priority = $%d::ticket_priority", priority)
	}
	if kind != "" {
		add("kind = $%d::ticket_kind", kind)
	}
	if clearAssignee {
		sets = append(sets, "assigned_to = NULL")
	} else if assignee != nil {
		add("assigned_to = $%d", *assignee)
	}

	if len(sets) == 1 {
		return nil // nothing to change
	}

	ct, err := r.pool.Exec(ctx,
		`UPDATE support_tickets SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkTicketRead clears the unread counter for whichever side is looking.
func (r *Repo) MarkTicketRead(ctx context.Context, id uuid.UUID, staff bool) error {
	column := "user_unread"
	if staff {
		column = "staff_unread"
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE support_tickets SET `+column+` = 0 WHERE id = $1`, id)
	return err
}

// TicketCounts drives the badges on both the player and the staff side.
func (r *Repo) TicketCounts(ctx context.Context, userID uuid.UUID, staff bool) (models.TicketCounts, error) {
	var c models.TicketCounts

	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(sum(user_unread), 0) FROM support_tickets WHERE user_id = $1`,
		userID).Scan(&c.MyUnread)
	if err != nil {
		return c, err
	}
	if !staff {
		return c, nil
	}

	err = r.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status <> 'closed'),
			count(*) FILTER (WHERE status IN ('open', 'in_progress') AND last_sender = 'user'),
			count(*) FILTER (WHERE priority IN ('high', 'urgent') AND status <> 'closed'),
			count(*) FILTER (WHERE assigned_to = $1 AND status <> 'closed')
		  FROM support_tickets`, userID,
	).Scan(&c.OpenTotal, &c.AwaitingReply, &c.HighPriority, &c.AssignedToMe)
	return c, err
}

// TicketKindBreakdown powers the filter chips with live counts, so staff can
// see at a glance where the volume actually is.
func (r *Repo) TicketKindBreakdown(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT kind::text, count(*) FROM support_tickets
		 WHERE status <> 'closed' GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		out[kind] = n
	}
	return out, rows.Err()
}

// Contact is who to write to, and in which language.
type Contact struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Locale      string
}

// StaffContacts is everyone who should hear that a ticket has arrived.
//
// StaffMembers returns cards for the assignment menu and carries no address —
// a card is what a screen needs, not what an envelope needs.
func (r *Repo) StaffContacts(ctx context.Context) ([]Contact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, email, display_name, locale
		  FROM users
		 WHERE role IN ('admin', 'moderator') AND status = 'active'
		   AND email IS NOT NULL
		 ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Email, &c.DisplayName, &c.Locale); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TicketOwnerContact is the person who opened a ticket, for writing back to.
func (r *Repo) TicketOwnerContact(ctx context.Context, ticketID uuid.UUID) (Contact, error) {
	var c Contact
	err := r.pool.QueryRow(ctx, `
		SELECT u.id, COALESCE(u.email::text, ''), u.display_name, u.locale
		  FROM support_tickets t
		  JOIN users u ON u.id = t.user_id
		 WHERE t.id = $1`, ticketID,
	).Scan(&c.ID, &c.Email, &c.DisplayName, &c.Locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// StaffMembers lists who a ticket can be assigned to.
func (r *Repo) StaffMembers(ctx context.Context) ([]*models.UserCard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, username, display_name, avatar_seed, country, xp, last_seen_at
		  FROM users WHERE role IN ('admin', 'moderator') ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserCards(rows)
}

// CannedReplies returns the saved replies for a locale, falling back to
// English so staff always have something.
func (r *Repo) CannedReplies(ctx context.Context, locale string) ([]*models.CannedReply, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, locale, title, body FROM support_canned_replies
		 WHERE locale = $1 ORDER BY sort_order`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.CannedReply
	for rows.Next() {
		var c models.CannedReply
		if err := rows.Scan(&c.ID, &c.Locale, &c.Title, &c.Body); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

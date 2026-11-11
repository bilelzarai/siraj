// Package support is "Contact us" (OT-03, endpoints 106–110) and the rows the admin
// inbox (A03) reads. Six topics, each with the fields it needs; a reply also reaches
// the player in their messages.
package support

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// MinMessage is the "at least 20 characters" rule the screen shows.
const MinMessage = 20

// Topics are the six topics of the picker, in the order the design shows them.
var Topics = []sqlc.TicketTopic{
	sqlc.TicketTopicBug,
	sqlc.TicketTopicQuestion,
	sqlc.TicketTopicAccount,
	sqlc.TicketTopicReportUser,
	sqlc.TicketTopicSuggestion,
	sqlc.TicketTopicOther,
}

// TopicIcon is the circle in the topic picker.
var TopicIcon = map[sqlc.TicketTopic]string{
	sqlc.TicketTopicBug:        "alert",
	sqlc.TicketTopicQuestion:   "info",
	sqlc.TicketTopicAccount:    "user",
	sqlc.TicketTopicReportUser: "flag",
	sqlc.TicketTopicSuggestion: "star",
	sqlc.TicketTopicOther:      "chat",
}

func ValidTopic(v string) bool {
	for _, t := range Topics {
		if string(t) == v {
			return true
		}
	}
	return false
}

type Service struct{ db *db.DB }

func NewService(d *db.DB) *Service { return &Service{db: d} }

// Ticket is one support thread as the player sees it.
type Ticket struct {
	ID      uuid.UUID `json:"id"`
	Number  string    `json:"number"`
	Topic   string    `json:"topic"`
	Subject string    `json:"subject"`
	Status  string    `json:"status"`
	Created string    `json:"created_at"`
	// The staff inbox (A03) also needs who it is from and how it is triaged.
	Priority     string `json:"priority,omitempty"`
	Who          string `json:"who,omitempty"`
	AssignedToMe bool   `json:"assigned_to_me,omitempty"`
}

// KPI is the four tiles above the staff inbox (A03).
type KPI struct{ Open, Waiting, High, Mine int }

func (s *Service) KPIs(ctx context.Context, me uuid.UUID) (KPI, error) {
	row, err := s.db.Q.SupportKPIs(ctx, uuid.NullUUID{UUID: me, Valid: true})
	if err != nil {
		return KPI{}, err
	}
	return KPI{Open: int(row.Open), Waiting: int(row.Waiting), High: int(row.High), Mine: int(row.Mine)}, nil
}

// TopicCount is one of the topic chips under the inbox search.
type TopicCount struct {
	Topic string
	Count int
}

func (s *Service) TopicCounts(ctx context.Context) []TopicCount {
	rows, err := s.db.Q.TicketTopicCounts(ctx)
	if err != nil {
		return nil
	}
	out := make([]TopicCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, TopicCount{Topic: string(r.Topic), Count: int(r.N)})
	}
	return out
}

// SetPriority and Assign are the staff triage controls of A03.
func (s *Service) SetPriority(ctx context.Context, id uuid.UUID, priority string) error {
	if priority != "high" && priority != "normal" {
		return apperr.Invalid(map[string]string{"priority": "normal or high"})
	}
	return s.db.Q.SetTicketPriority(ctx, sqlc.SetTicketPriorityParams{ID: id, Priority: priority})
}

func (s *Service) Assign(ctx context.Context, id uuid.UUID, assignee uuid.NullUUID) error {
	return s.db.Q.AssignTicket(ctx, sqlc.AssignTicketParams{ID: id, AssigneeID: assignee})
}

// Message is one post in the thread.
type Message struct {
	ID      uuid.UUID `json:"id"`
	Body    string    `json:"body"`
	IsStaff bool      `json:"is_staff"`
	IsNote  bool      `json:"is_note,omitempty"`
	Author  string    `json:"author,omitempty"`
	Created string    `json:"created_at"`
}

// Input is the Contact us form; the extra fields depend on the topic.
type Input struct {
	Who     *authctx.Principal // nil = a visitor, who must give an email
	Email   string
	Topic   string
	Subject string
	Body    string
	Context map[string]any
}

func (s *Service) Create(ctx context.Context, in Input) (Ticket, error) {
	fields := map[string]string{}
	if !ValidTopic(in.Topic) {
		fields["topic"] = "pick a topic"
	}
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimSpace(in.Body)
	if len([]rune(in.Subject)) < 3 || len([]rune(in.Subject)) > 80 {
		fields["subject"] = "3 to 80 characters"
	}
	if len([]rune(in.Body)) < MinMessage {
		fields["body"] = "at least 20 characters"
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Who == nil && !strings.Contains(in.Email, "@") {
		fields["email"] = "we need an email to reply"
	}
	switch in.Topic {
	case string(sqlc.TicketTopicReportUser):
		if v, _ := in.Context["username"].(string); strings.TrimSpace(v) == "" {
			fields["username"] = "who are you reporting?"
		}
	case string(sqlc.TicketTopicAccount):
		if v, _ := in.Context["account"].(string); strings.TrimSpace(v) == "" && in.Who == nil {
			fields["account"] = "which account?"
		}
	}
	if len(fields) > 0 {
		return Ticket{}, apperr.Invalid(fields)
	}
	if in.Context == nil {
		in.Context = map[string]any{}
	}
	raw, err := json.Marshal(in.Context)
	if err != nil {
		return Ticket{}, err
	}
	var out Ticket
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		p := sqlc.CreateTicketParams{
			Topic: sqlc.TicketTopic(in.Topic), Subject: in.Subject, Context: raw,
		}
		if in.Who != nil {
			p.UserID = uuid.NullUUID{UUID: in.Who.ID, Valid: true}
		}
		if in.Email != "" {
			p.Email = pgtype.Text{String: in.Email, Valid: true}
		}
		row, err := q.CreateTicket(ctx, p)
		if err != nil {
			return err
		}
		var author uuid.NullUUID
		if in.Who != nil {
			author = uuid.NullUUID{UUID: in.Who.ID, Valid: true}
		}
		if _, err := q.AddTicketMessage(ctx, sqlc.AddTicketMessageParams{
			TicketID: row.ID, AuthorID: author, Body: in.Body,
		}); err != nil {
			return err
		}
		out = ticket(row)
		return nil
	})
	return out, err
}

func ticket(r sqlc.SupportTicket) Ticket {
	t := Ticket{
		ID: r.ID, Number: r.Number, Topic: string(r.Topic), Subject: r.Subject,
		Status: string(r.Status), Priority: r.Priority,
	}
	if r.CreatedAt.Valid {
		t.Created = r.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	return t
}

func (s *Service) Mine(ctx context.Context, who *authctx.Principal, page, perPage int) ([]Ticket, int, error) {
	rows, err := s.db.Q.MyTickets(ctx, sqlc.MyTicketsParams{
		UserID: uuid.NullUUID{UUID: who.ID, Valid: true},
		Lim:    int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountMyTickets(ctx, uuid.NullUUID{UUID: who.ID, Valid: true})
	if err != nil {
		return nil, 0, err
	}
	out := make([]Ticket, 0, len(rows))
	for _, r := range rows {
		out = append(out, ticket(r))
	}
	return out, int(total), nil
}

// Thread is endpoint 109. Staff see the internal notes; the player never does.
func (s *Service) Thread(ctx context.Context, who *authctx.Principal, id uuid.UUID, staff bool) (Ticket, []Message, error) {
	row, err := s.db.Q.GetTicket(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Ticket{}, nil, apperr.NotFound("ticket")
		}
		return Ticket{}, nil, err
	}
	if !staff && (!row.UserID.Valid || row.UserID.UUID != who.ID) {
		return Ticket{}, nil, apperr.NotFound("ticket")
	}
	msgs, err := s.db.Q.TicketThread(ctx, sqlc.TicketThreadParams{TicketID: id, IncludeNotes: staff})
	if err != nil {
		return Ticket{}, nil, err
	}
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		msg := Message{ID: m.ID, Body: m.Body, IsStaff: m.IsStaff, IsNote: m.IsNote}
		if m.DisplayName.Valid {
			msg.Author = m.DisplayName.String
		}
		if m.CreatedAt.Valid {
			msg.Created = m.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, msg)
	}
	t := ticket(row)
	t.AssignedToMe = row.AssigneeID.Valid && who != nil && row.AssigneeID.UUID == who.ID
	if row.UserID.Valid {
		if u, err := s.db.Q.GetUser(ctx, row.UserID.UUID); err == nil {
			t.Who = u.DisplayName
			if u.Username.Valid {
				t.Who += " · @" + u.Username.String
			}
		}
	} else if row.Email.Valid {
		t.Who = row.Email.String
	}
	return t, out, nil
}

// Reply is endpoint 110 for the player, and the admin inbox's reply box for staff.
func (s *Service) Reply(ctx context.Context, who *authctx.Principal, id uuid.UUID, body string, staff, note bool) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return apperr.Invalid(map[string]string{"body": "write something"})
	}
	if _, _, err := s.Thread(ctx, who, id, staff); err != nil {
		return err
	}
	_, err := s.db.Q.AddTicketMessage(ctx, sqlc.AddTicketMessageParams{
		TicketID: id, AuthorID: uuid.NullUUID{UUID: who.ID, Valid: true},
		Body: body, IsStaff: staff, IsNote: note,
	})
	if err != nil {
		return err
	}
	status := sqlc.TicketStatusWaiting
	if staff {
		status = sqlc.TicketStatusWaiting
	} else {
		status = sqlc.TicketStatusOpen
	}
	return s.db.Q.SetTicketStatus(ctx, sqlc.SetTicketStatusParams{Status: status, ID: id})
}

// SetStatus is the admin's Resolve / Close.
func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, status string) error {
	switch status {
	case "open", "waiting", "resolved", "closed":
	default:
		return apperr.Invalid(map[string]string{"status": "open, waiting, resolved or closed"})
	}
	return s.db.Q.SetTicketStatus(ctx, sqlc.SetTicketStatusParams{Status: sqlc.TicketStatus(status), ID: id})
}

// Inbox is the admin screen A03.
// InboxFilter is A03's toolbar: the status tab, the topic chip, how urgent, who
// holds it, and the search.
type InboxFilter struct {
	Status, Topic, Priority, Assignee, Q string
	// Me is the member of staff looking, which is what "mine" means.
	Me            uuid.UUID
	Page, PerPage int
}

var ticketPriorities = map[string]bool{"high": true, "normal": true, "low": true}

func (f InboxFilter) priority() string {
	if ticketPriorities[f.Priority] {
		return f.Priority
	}
	return ""
}

// assignee resolves to the pair the query takes: the word, and whose id "mine"
// means. An unknown word filters nothing rather than filtering everything out.
func (f InboxFilter) assignee() (string, uuid.NullUUID) {
	switch f.Assignee {
	case "unassigned":
		return "unassigned", uuid.NullUUID{}
	case "mine":
		if f.Me == uuid.Nil {
			return "", uuid.NullUUID{}
		}
		return "mine", uuid.NullUUID{UUID: f.Me, Valid: true}
	}
	return "", uuid.NullUUID{}
}

func (s *Service) Inbox(ctx context.Context, f InboxFilter) ([]sqlc.AdminTicketsRow, int, error) {
	assignee, me := f.assignee()
	rows, err := s.db.Q.AdminTickets(ctx, sqlc.AdminTicketsParams{
		Status: f.Status, Topic: f.Topic, Priority: f.priority(),
		Assignee: assignee, Me: me, Q: f.Q,
		Lim: int32(f.PerPage), Off: int32((f.Page - 1) * f.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountAdminTickets(ctx, sqlc.CountAdminTicketsParams{
		Status: f.Status, Topic: f.Topic, Priority: f.priority(),
		Assignee: assignee, Me: me, Q: f.Q,
	})
	return rows, int(total), err
}

// CountsByStatus fills the admin sidebar badge and the inbox tabs.
func (s *Service) CountsByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.Q.TicketCountsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, r := range rows {
		out[string(r.Status)] = int(r.N)
	}
	return out, nil
}

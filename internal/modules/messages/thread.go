package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

// Open reads one thread: the header over it and every bubble in it, and marks it read
// (MS-02). A room code opens the room's chat instead of a conversation.
func (s *Service) Open(ctx context.Context, who *authctx.Principal, key string) (Head, []Bubble, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return s.openRoom(ctx, who, key)
	}
	if err := s.must(ctx, who, id); err != nil {
		return Head{}, nil, err
	}
	conversation, err := s.db.Q.GetConversation(ctx, id)
	if err != nil {
		return Head{}, nil, err
	}
	head := Head{
		ID:       id.String(),
		Group:    conversation.Kind == sqlc.ConversationKindGroup,
		System:   conversation.Kind == sqlc.ConversationKindSystem,
		CanReply: conversation.Kind != sqlc.ConversationKindSystem,
	}
	if conversation.Title.Valid {
		head.Name = conversation.Title.String
	}
	people, err := s.db.Q.ConversationPeople(ctx, id)
	if err != nil {
		return Head{}, nil, err
	}
	names := make([]string, 0, len(people))
	for _, p := range people {
		if p.ID == who.ID {
			continue
		}
		names = append(names, p.DisplayName)
		if !head.Group && !head.System {
			head.Name, head.UserID, head.Online = p.DisplayName, p.ID, p.Online
			head.Guest = p.Kind == sqlc.UserKindGuest
		}
	}
	head.People = strings.Join(names, ", ")
	// A deleted guest, or a thread of one: the screen still needs a name.
	if head.Name == "" {
		head.Name = "—"
	}

	rows, err := s.db.Q.ConversationMessages(ctx, sqlc.ConversationMessagesParams{
		ConversationID: id, UserID: who.ID, Lim: 300,
	})
	if err != nil {
		return Head{}, nil, err
	}
	// How far the others have read: a sent message carries one tick until they have.
	readTo := ""
	if markers, err := s.db.Q.ReadMarkers(ctx, sqlc.ReadMarkersParams{
		ConversationID: id, UserID: who.ID,
	}); err == nil {
		for _, m := range markers {
			if m.LastReadMessageID.Valid {
				if mark := m.LastReadMessageID.UUID.String(); mark > readTo {
					readTo = mark
				}
			}
		}
	}

	out := make([]Bubble, 0, len(rows))
	for _, r := range rows {
		b := Bubble{
			ID: r.ID.String(), Kind: string(r.Kind), At: r.CreatedAt.Time,
			Deleted: r.DeletedAt.Valid, System: r.Kind == sqlc.MessageKindSystem,
		}
		if r.Body.Valid {
			b.Body = r.Body.String
		}
		if r.SenderID.Valid {
			b.Mine = r.SenderID.UUID == who.ID
		}
		if r.SenderName.Valid {
			b.From = r.SenderName.String
		}
		if b.Mine && readTo != "" && b.ID <= readTo {
			b.Read = true
		}
		if r.QuoteBody.Valid || r.QuoteName.Valid {
			b.Quote, b.QuoteWho = quoteOf(r), nameOf(r.QuoteName)
		}
		if r.FileName.Valid || r.FileKey.Valid {
			b.FileName = nameOf(r.FileName)
			b.FileSize = int64(r.FileBytes.Int32)
			b.Duration = int(r.FileDuration.Int32)
			if r.FileKey.Valid {
				b.FileURL = "/uploads/" + r.FileKey.String
			}
		}
		if r.Kind == sqlc.MessageKindChallenge {
			var card struct {
				Title       string `json:"title"`
				Questions   int    `json:"questions"`
				ScoreToBeat int    `json:"score_to_beat"`
				ChallengeID string `json:"challenge_id"`
			}
			if json.Unmarshal(r.Payload, &card) == nil {
				b.Title, b.ChallengeID = card.Title, card.ChallengeID
				b.Line = fmt.Sprintf("%d|%d", card.Questions, card.ScoreToBeat)
			}
		}
		out = append(out, b)
	}
	return head, out, s.db.Q.MarkConversationRead(ctx, sqlc.MarkConversationReadParams{
		ConversationID: id, UserID: who.ID,
	})
}

// openRoom reads a live room's chat into the same shapes, so the thread draws a room
// exactly as it draws a person (MS-01).
func (s *Service) openRoom(ctx context.Context, who *authctx.Principal, code string) (Head, []Bubble, error) {
	if s.rooms == nil {
		return Head{}, nil, apperr.NotFound("conversation")
	}
	row, lines, err := s.rooms.RoomLines(ctx, who, code)
	if err != nil {
		return Head{}, nil, err
	}
	head := Head{
		ID: row.Code, Name: row.Title, People: row.People,
		Room: true, RoomCode: row.Code, CanReply: true,
	}
	out := make([]Bubble, 0, len(lines))
	for i, l := range lines {
		kind := l.Kind
		if kind == "" {
			kind = "text"
		}
		out = append(out, Bubble{
			ID: fmt.Sprintf("%s-%d", row.Code, i), Kind: kind, Body: l.Body,
			At: l.At, From: l.Author, Mine: l.Mine, System: l.System,
			FileName: l.FileName, FileSize: l.FileSize, FileURL: l.FileURL,
			Duration: l.Duration,
		})
	}
	return head, out, nil
}

func quoteOf(r sqlc.ConversationMessagesRow) string {
	if r.QuoteBody.Valid && r.QuoteBody.String != "" {
		return r.QuoteBody.String
	}
	if !r.QuoteKind.Valid {
		return ""
	}
	switch r.QuoteKind.MessageKind {
	case sqlc.MessageKindVoice:
		return "{voice}"
	case sqlc.MessageKindImage:
		return "{image}"
	case sqlc.MessageKindFile:
		return "{file}"
	}
	return ""
}

func nameOf(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

// Draft is everything the composer can send at once: a line of text, a reply to
// something, and up to one upload (MS-02).
type Draft struct {
	Body     string
	ReplyTo  uuid.UUID
	UploadID uuid.UUID
	Kind     string
}

// Send writes one message (MS-03). The thread refuses an empty draft, so a stray
// submit never leaves a blank bubble behind.
func (s *Service) Send(ctx context.Context, who *authctx.Principal, key string, in Draft) error {
	if id, err := uuid.Parse(key); err == nil {
		return s.sendToConversation(ctx, who, id, in)
	}
	if s.rooms == nil {
		return apperr.NotFound("conversation")
	}
	return s.rooms.RoomSay(ctx, who, key, in.Body, in.UploadID, in.Kind)
}

func (s *Service) sendToConversation(ctx context.Context, who *authctx.Principal, id uuid.UUID, in Draft) error {
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	conversation, err := s.db.Q.GetConversation(ctx, id)
	if err != nil {
		return err
	}
	if conversation.Kind == sqlc.ConversationKindSystem {
		return apperr.Forbidden("This thread does not take replies")
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" && in.UploadID == uuid.Nil {
		return apperr.Invalid(map[string]string{"body": "write something first"})
	}
	if len([]rune(in.Body)) > MaxBody {
		return apperr.Invalid(map[string]string{"body": "too long"})
	}
	kind := sqlc.MessageKindText
	switch in.Kind {
	case "voice":
		kind = sqlc.MessageKindVoice
	case "image":
		kind = sqlc.MessageKindImage
	case "file":
		kind = sqlc.MessageKindFile
	}
	if err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.SendMessage(ctx, sqlc.SendMessageParams{
			ConversationID: id,
			SenderID:       uuid.NullUUID{UUID: who.ID, Valid: true},
			Kind:           kind,
			Body:           pgtype.Text{String: in.Body, Valid: in.Body != ""},
			ReplyToID:      uuid.NullUUID{UUID: in.ReplyTo, Valid: in.ReplyTo != uuid.Nil},
			UploadID:       uuid.NullUUID{UUID: in.UploadID, Valid: in.UploadID != uuid.Nil},
			Payload:        []byte("{}"),
		}); err != nil {
			return err
		}
		return q.TouchConversation(ctx, id)
	}); err != nil {
		return err
	}
	// Everyone in the thread hears it, the writer included: their other tabs are
	// screens too.
	if ids, err := s.db.Q.ConversationMemberIDs(ctx, id); err == nil {
		s.live.SendAll(ids, live.Messages)
	}
	return nil
}

// DeleteForEveryone takes a message back. Only the writer may, and what is left is
// the tombstone the design draws (MS-04).
func (s *Service) DeleteForEveryone(ctx context.Context, who *authctx.Principal, messageID uuid.UUID) error {
	message, err := s.message(ctx, who, messageID)
	if err != nil {
		return err
	}
	if !message.SenderID.Valid || message.SenderID.UUID != who.ID {
		return apperr.Forbidden("You can only take back your own messages")
	}
	if err := s.db.Q.DeleteMessageForEveryone(ctx, sqlc.DeleteMessageForEveryoneParams{
		ID: messageID, SenderID: uuid.NullUUID{UUID: who.ID, Valid: true},
	}); err != nil {
		return err
	}
	if ids, err := s.db.Q.ConversationMemberIDs(ctx, message.ConversationID); err == nil {
		s.live.SendAll(ids, live.Messages)
	}
	return nil
}

// DeleteForMe hides one message from this player's copy of the thread (MS-04).
func (s *Service) DeleteForMe(ctx context.Context, who *authctx.Principal, messageID uuid.UUID) error {
	if _, err := s.message(ctx, who, messageID); err != nil {
		return err
	}
	return s.db.Q.HideMessageForMe(ctx, sqlc.HideMessageForMeParams{
		MessageID: messageID, UserID: who.ID,
	})
}

// Report sends a message to the moderation queue (MS-04, AD-06).
func (s *Service) Report(ctx context.Context, who *authctx.Principal, messageID uuid.UUID, reason, note string) error {
	if _, err := s.message(ctx, who, messageID); err != nil {
		return err
	}
	if reason == "" {
		reason = "other"
	}
	return s.db.Q.ReportMessage(ctx, sqlc.ReportMessageParams{
		MessageID: messageID, ReporterID: who.ID, Reason: reason,
		Note: pgtype.Text{String: note, Valid: note != ""},
	})
}

// message reads one message and checks the player is in its thread, so no id typed
// into the address bar reaches someone else's conversation.
func (s *Service) message(ctx context.Context, who *authctx.Principal, id uuid.UUID) (sqlc.Message, error) {
	if who == nil {
		return sqlc.Message{}, apperr.Unauthorized("Sign in first")
	}
	message, err := s.db.Q.GetMessage(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Message{}, apperr.NotFound("message")
	}
	if err != nil {
		return sqlc.Message{}, err
	}
	if err := s.must(ctx, who, message.ConversationID); err != nil {
		return sqlc.Message{}, err
	}
	return message, nil
}

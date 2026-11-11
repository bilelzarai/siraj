// Package messages is the direct thread between two players (MS-01…MS-06). A thread
// is created the first time someone writes, and only the two members can read it.
package messages

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

// Blocks is what messages needs from the social module: a blocked player cannot be
// written to (FR-07).
type Blocks interface {
	Blocked(ctx context.Context, a, b uuid.UUID) (bool, error)
}

type Service struct {
	db         *db.DB
	social     Blocks
	live       *live.Hub
	rooms      Rooms
	challenges Challenges
	people     People
	uploads    string
}

// text is a nullable string the way sqlc wants it.
func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// SetLive gives the service the event stream, so a message lands on the other screen
// as it is written rather than on its next poll (MS-01, NF-02).
func (s *Service) SetLive(hub *live.Hub) { s.live = hub }

func NewService(d *db.DB, social Blocks) *Service { return &Service{db: d, social: social} }

// MaxBody is the longest message the UI accepts.
const MaxBody = 2000

// directKey is the same for both sides of a thread, whichever one writes first.
func directKey(a, b uuid.UUID) string {
	x, y := a.String(), b.String()
	if x > y {
		x, y = y, x
	}
	return x + ":" + y
}

// WithPerson finds the thread with another player, creating it on first use.
func (s *Service) WithPerson(ctx context.Context, who *authctx.Principal, other uuid.UUID) (sqlc.Conversation, error) {
	if who == nil {
		return sqlc.Conversation{}, apperr.Unauthorized("Sign in first")
	}
	if other == who.ID {
		return sqlc.Conversation{}, apperr.Invalid(map[string]string{"user_id": "pick someone else"})
	}
	if s.social != nil {
		if blocked, err := s.social.Blocked(ctx, who.ID, other); err != nil {
			return sqlc.Conversation{}, err
		} else if blocked {
			return sqlc.Conversation{}, apperr.Forbidden("You cannot message this player")
		}
	}
	key := directKey(who.ID, other)
	conversation, err := s.db.Q.FindDirect(ctx, pgtype.Text{String: key, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
			conversation, err = q.CreateDirect(ctx, pgtype.Text{String: key, Valid: true})
			if err != nil {
				return err
			}
			for _, id := range []uuid.UUID{who.ID, other} {
				if err := q.AddConversationMember(ctx, sqlc.AddConversationMemberParams{
					ConversationID: conversation.ID, UserID: id,
				}); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return conversation, err
}

// Unread feeds the navigation badge (MS-06).
func (s *Service) Unread(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.db.Q.UnreadConversations(ctx, userID)
	return int(n), err
}

func (s *Service) must(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	if who == nil {
		return apperr.Unauthorized("Sign in first")
	}
	ok, err := s.db.Q.IsConversationMember(ctx, sqlc.IsConversationMemberParams{
		ConversationID: id, UserID: who.ID,
	})
	if err != nil {
		return err
	}
	if !ok {
		return apperr.NotFound("conversation")
	}
	return nil
}

// IsMember answers whether this id is one of the player's own threads, so the screen
// can tell a conversation from a person without guessing (MS-02).
func (s *Service) IsMember(ctx context.Context, who *authctx.Principal, id uuid.UUID) (bool, error) {
	if who == nil {
		return false, apperr.Unauthorized("Sign in first")
	}
	return s.db.Q.IsConversationMember(ctx, sqlc.IsConversationMemberParams{
		ConversationID: id, UserID: who.ID,
	})
}

// MaxGroupMembers is how many people one group thread holds, the creator included.
const MaxGroupMembers = 20

// StartGroup is the other half of endpoint 76: a group thread with the people the
// player picked. Blocks are honoured here as they are for a direct thread (MS-05).
func (s *Service) StartGroup(ctx context.Context, who *authctx.Principal, title string, members []uuid.UUID) (sqlc.Conversation, error) {
	if who == nil {
		return sqlc.Conversation{}, apperr.Unauthorized("Sign in first")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return sqlc.Conversation{}, apperr.Invalid(map[string]string{"title": "name the group"})
	}
	if len([]rune(title)) > 60 {
		return sqlc.Conversation{}, apperr.Invalid(map[string]string{"title": "60 characters at most"})
	}
	// Unique, without the creator: they are added once, below.
	seen := map[uuid.UUID]bool{who.ID: true}
	others := make([]uuid.UUID, 0, len(members))
	for _, id := range members {
		if seen[id] {
			continue
		}
		seen[id] = true
		others = append(others, id)
	}
	if len(others) == 0 {
		return sqlc.Conversation{}, apperr.Invalid(map[string]string{"members": "pick at least one person"})
	}
	if len(others)+1 > MaxGroupMembers {
		return sqlc.Conversation{}, apperr.Invalid(map[string]string{"members": "too many people for one group"})
	}
	if s.social != nil {
		for _, id := range others {
			blocked, err := s.social.Blocked(ctx, who.ID, id)
			if err != nil {
				return sqlc.Conversation{}, err
			}
			if blocked {
				return sqlc.Conversation{}, apperr.Forbidden("You cannot message one of these players")
			}
		}
	}

	var conversation sqlc.Conversation
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		var err error
		conversation, err = q.CreateGroup(ctx, pgtype.Text{String: title, Valid: true})
		if err != nil {
			return err
		}
		for _, id := range append([]uuid.UUID{who.ID}, others...) {
			if err := q.AddConversationMember(ctx, sqlc.AddConversationMemberParams{
				ConversationID: conversation.ID, UserID: id,
			}); err != nil {
				return err
			}
		}
		// Whoever starts a group owns it: they are the one who may rename it and
		// remove somebody (MS-01).
		return q.SetConversationOwner(ctx, sqlc.SetConversationOwnerParams{
			ConversationID: conversation.ID, UserID: who.ID,
		})
	})
	return conversation, err
}

// Rename is part of endpoint 78: a group's name, changed by one of its members.
func (s *Service) Rename(ctx context.Context, who *authctx.Principal, id uuid.UUID, title string) error {
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 60 {
		return apperr.Invalid(map[string]string{"title": "between 1 and 60 characters"})
	}
	return s.db.Q.SetConversationTitle(ctx, sqlc.SetConversationTitleParams{
		ID: id, Title: pgtype.Text{String: title, Valid: true},
	})
}

// MarkRead is endpoint 79: everything in the thread is read up to now (MS-02).
func (s *Service) MarkRead(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	return s.db.Q.MarkConversationRead(ctx, sqlc.MarkConversationReadParams{
		ConversationID: id, UserID: who.ID,
	})
}

// Package notifications is the player's bell (OT-01, endpoints 87–89) and the one way
// other modules tell a player something happened. Writes can join a caller's
// transaction, so a challenge and its notification are never out of step.
package notifications

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

type Service struct {
	db   *db.DB
	live *live.Hub
}

func NewService(d *db.DB) *Service { return &Service{db: d} }

// Kind mirrors the notification_kind enum; other modules use these constants.
const (
	KindFriendRequest     = sqlc.NotificationKindFriendRequest
	KindFriendAccepted    = sqlc.NotificationKindFriendAccepted
	KindChallengeReceived = sqlc.NotificationKindChallengeReceived
	KindChallengeFinished = sqlc.NotificationKindChallengeParticipantFinished
	KindChallengeResults  = sqlc.NotificationKindChallengeResultsReady
	KindChallengeRemoved  = sqlc.NotificationKindChallengeRemoved
	KindMessage           = sqlc.NotificationKindMessage
	KindRoomInvite        = sqlc.NotificationKindRoomInvite
	KindDuelResult        = sqlc.NotificationKindDuelResult
	KindQuestionApproved  = sqlc.NotificationKindQuestionApproved
	KindQuestionRejected  = sqlc.NotificationKindQuestionRejected
	KindQuestionSentBack  = sqlc.NotificationKindQuestionSentBack
	KindSupportReply      = sqlc.NotificationKindSupportReply
	KindAchievement       = sqlc.NotificationKindAchievement
	KindStreakReminder    = sqlc.NotificationKindStreakReminder
	KindSystem            = sqlc.NotificationKindSystem
)

// Notification is what the bell and the API show. The text is built in the page from
// kind + payload, so every language reads it in its own words (NF-06).
type Notification struct {
	ID      uuid.UUID      `json:"id"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
	Link    string         `json:"link,omitempty"`
	Unread  bool           `json:"unread"`
	Created string         `json:"created_at"`
}

// Push writes one notification. Pass the transaction's queries when the caller is in a
// transaction, or nil to use the pool.
// SetLive gives the service the event stream. Every notification is announced on it,
// so the badge and the list update the moment they are written (NF-02).
func (s *Service) SetLive(hub *live.Hub) { s.live = hub }

// topicOf is which part of the app a notification belongs to, so a screen can listen
// for its own kind without waking for every other.
func topicOf(kind sqlc.NotificationKind) live.Topic {
	switch kind {
	case sqlc.NotificationKindFriendRequest, sqlc.NotificationKindFriendAccepted:
		return live.Friends
	case sqlc.NotificationKindMessage:
		return live.Messages
	case sqlc.NotificationKindChallengeReceived, sqlc.NotificationKindChallengeResultsReady,
		sqlc.NotificationKindChallengeParticipantFinished, sqlc.NotificationKindChallengeRemoved:
		return live.Challenges
	case sqlc.NotificationKindRoomInvite:
		return live.Rooms
	}
	return live.Notifications
}

func (s *Service) Push(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, kind sqlc.NotificationKind, link string, payload map[string]any) error {
	if q == nil {
		q = s.db.Q
	}
	payload = s.withActorName(ctx, q, payload)
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if payload == nil {
		raw = []byte("{}")
	}
	_, err = q.CreateNotification(ctx, sqlc.CreateNotificationParams{
		UserID: userID, Kind: kind, Payload: raw,
		Link: pgtype.Text{String: link, Valid: link != ""},
	})
	if err == nil {
		s.live.Send(userID, live.Notifications, topicOf(kind))
	}
	return err
}

// withActorName fills in the name the sentence needs. Every "{name} did X" line is
// written once, when the thing happens, so the reader sees a name and not the raw
// placeholder — and a later rename does not rewrite history.
func (s *Service) withActorName(ctx context.Context, q *sqlc.Queries, payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	if _, named := payload["name"]; named {
		return payload
	}
	raw, ok := payload["actor_id"]
	if !ok {
		return payload
	}
	var id uuid.UUID
	switch value := raw.(type) {
	case uuid.UUID:
		id = value
	case string:
		parsed, err := uuid.Parse(value)
		if err != nil {
			return payload
		}
		id = parsed
	default:
		return payload
	}
	user, err := q.GetUser(ctx, id)
	if err != nil || user.DisplayName == "" {
		return payload
	}
	payload["name"] = user.DisplayName
	return payload
}

func (s *Service) List(ctx context.Context, who *authctx.Principal, unreadOnly bool, page, perPage int) ([]Notification, int, error) {
	rows, err := s.db.Q.ListNotifications(ctx, sqlc.ListNotificationsParams{
		UserID: who.ID, UnreadOnly: unreadOnly,
		Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountNotifications(ctx, sqlc.CountNotificationsParams{UserID: who.ID, UnreadOnly: unreadOnly})
	if err != nil {
		return nil, 0, err
	}
	out := make([]Notification, 0, len(rows))
	for _, r := range rows {
		n := Notification{ID: r.ID, Kind: string(r.Kind), Unread: !r.ReadAt.Valid}
		if r.Link.Valid {
			n.Link = r.Link.String
		}
		if r.CreatedAt.Valid {
			n.Created = r.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
		}
		_ = json.Unmarshal(r.Payload, &n.Payload)
		out = append(out, n)
	}
	return out, int(total), nil
}

func (s *Service) Unread(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.db.Q.UnreadNotifications(ctx, userID)
	return int(n), err
}

// MarkRead marks the given ids, or every unread one when ids is empty (endpoint 88).
func (s *Service) MarkRead(ctx context.Context, who *authctx.Principal, ids []uuid.UUID) error {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return s.db.Q.MarkNotificationsRead(ctx, sqlc.MarkNotificationsReadParams{UserID: who.ID, Ids: ids})
}

// Open marks one notification read and returns where it points.
func (s *Service) Open(ctx context.Context, who *authctx.Principal, id uuid.UUID) (string, error) {
	row, err := s.db.Q.GetNotification(ctx, sqlc.GetNotificationParams{ID: id, UserID: who.ID})
	if err != nil {
		return "", err
	}
	if err := s.db.Q.MarkNotificationRead(ctx, sqlc.MarkNotificationReadParams{ID: id, UserID: who.ID}); err != nil {
		return "", err
	}
	if row.Link.Valid && row.Link.String != "" {
		return row.Link.String, nil
	}
	return "/notifications", nil
}

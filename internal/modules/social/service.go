// Package social is friends, friend requests, blocks, people search and invite links
// (FR-01, endpoints 63–74). Blocks are checked here, before messages, challenges and
// room invites ever reach their own module.
package social

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/modules/notifications"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

var (
	ErrBlocked     = apperr.Forbidden("You cannot do that with this player")
	ErrGuestSocial = apperr.Forbidden("Create a free account to add friends")
	ErrSelf        = apperr.BadRequest("self", "That is you")
)

type Service struct {
	db     *db.DB
	notify *notifications.Service
}

func NewService(d *db.DB, n *notifications.Service) *Service { return &Service{db: d, notify: n} }

// pair orders two ids the way friendships stores them (user_a < user_b).
func pair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

// Person is another player as every social list shows them.
type Person struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Name      string    `json:"display_name"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Level     int32     `json:"level"`
	IsGuest   bool      `json:"is_guest"`
	Online    bool      `json:"online"`
	IsFriend  bool      `json:"is_friend"`
	Requested bool      `json:"requested"`
	Wins      int32     `json:"wins,omitempty"`
	Losses    int32     `json:"losses,omitempty"`
}

// Friends is endpoint 63: online first, optionally filtered by a search box.
func (s *Service) Friends(ctx context.Context, who *authctx.Principal, q string) ([]Person, error) {
	rows, err := s.db.Q.ListFriends(ctx, sqlc.ListFriendsParams{UserID: who.ID, Q: q})
	if err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(rows))
	for _, r := range rows {
		p := Person{
			ID: r.ID, Name: r.DisplayName, Level: r.Level, Online: r.Online,
			IsGuest: r.Kind == sqlc.UserKindGuest, IsFriend: true,
			Wins: r.WinsVs, Losses: r.LossesVs,
		}
		if r.Username.Valid {
			p.Username = r.Username.String
		}
		if r.AvatarUrl.Valid {
			p.AvatarURL = r.AvatarUrl.String
		}
		out = append(out, p)
	}
	return out, nil
}

// Request is one pending friend request, as the side column lists them.
type Request struct {
	ID     uuid.UUID `json:"id"`
	Person Person    `json:"person"`
}

func (s *Service) Requests(ctx context.Context, who *authctx.Principal, incoming bool) ([]Request, error) {
	rows, err := s.db.Q.ListFriendRequests(ctx, sqlc.ListFriendRequestsParams{Incoming: incoming, UserID: who.ID})
	if err != nil {
		return nil, err
	}
	out := make([]Request, 0, len(rows))
	for _, r := range rows {
		p := Person{ID: r.FromID, Name: r.DisplayName, Level: r.Level}
		if !incoming {
			p.ID = r.ToID
		}
		if r.Username.Valid {
			p.Username = r.Username.String
		}
		if r.AvatarUrl.Valid {
			p.AvatarURL = r.AvatarUrl.String
		}
		out = append(out, Request{ID: r.ID, Person: p})
	}
	return out, nil
}

func (s *Service) PendingCount(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.db.Q.CountFriendRequests(ctx, userID)
	return int(n), err
}

// SendRequest is endpoint 66. A pending request the other way round is accepted
// instead, so two people who ask at the same time simply become friends.
func (s *Service) SendRequest(ctx context.Context, who *authctx.Principal, toID uuid.UUID) error {
	if who.IsGuest() {
		return ErrGuestSocial
	}
	if toID == who.ID {
		return ErrSelf
	}
	// A guest is a real user but not an account: there is nobody to stay friends
	// with once their day is up, so they are never the other end of a request
	// (RL-03). The screens hide the button; this is the rule behind it.
	if other, err := s.db.Q.GetUser(ctx, toID); err == nil && other.Kind == sqlc.UserKindGuest {
		return ErrGuestSocial
	}
	blocked, err := s.db.Q.IsBlocked(ctx, sqlc.IsBlockedParams{A: who.ID, B: toID})
	if err != nil {
		return err
	}
	if blocked {
		return ErrBlocked
	}
	a, b := pair(who.ID, toID)
	already, err := s.db.Q.IsFriend(ctx, sqlc.IsFriendParams{A: a, B: b})
	if err != nil {
		return err
	}
	if already {
		return nil
	}
	if existing, err := s.db.Q.PendingRequestBetween(ctx, sqlc.PendingRequestBetweenParams{A: who.ID, B: toID}); err == nil {
		if existing.ToID == who.ID {
			return s.Accept(ctx, who, existing.ID)
		}
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	req, err := s.db.Q.CreateFriendRequest(ctx, sqlc.CreateFriendRequestParams{FromID: who.ID, ToID: toID})
	if err != nil {
		return err
	}
	return s.notify.Push(ctx, nil, toID, notifications.KindFriendRequest, "/friends",
		map[string]any{"request_id": req.ID, "from_id": who.ID, "actor_id": who.ID})
}

// Accept is endpoint 67: the request becomes a friendship, in one transaction.
func (s *Service) Accept(ctx context.Context, who *authctx.Principal, requestID uuid.UUID) error {
	var other uuid.UUID
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		req, err := q.GetFriendRequest(ctx, requestID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound("request")
			}
			return err
		}
		if req.ToID != who.ID || req.Status != "pending" {
			return apperr.NotFound("request")
		}
		if err := q.AnswerFriendRequest(ctx, sqlc.AnswerFriendRequestParams{Status: "accepted", ID: requestID}); err != nil {
			return err
		}
		a, b := pair(req.FromID, req.ToID)
		other = req.FromID
		return q.AddFriendship(ctx, sqlc.AddFriendshipParams{A: a, B: b})
	})
	if err != nil {
		return err
	}
	return s.notify.Push(ctx, nil, other, notifications.KindFriendAccepted, "/friends",
		map[string]any{"from_id": who.ID, "actor_id": who.ID})
}

// Decline is endpoint 68.
func (s *Service) Decline(ctx context.Context, who *authctx.Principal, requestID uuid.UUID) error {
	req, err := s.db.Q.GetFriendRequest(ctx, requestID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("request")
		}
		return err
	}
	if req.ToID != who.ID && req.FromID != who.ID {
		return apperr.NotFound("request")
	}
	status := "declined"
	if req.FromID == who.ID {
		status = "cancelled"
	}
	return s.db.Q.AnswerFriendRequest(ctx, sqlc.AnswerFriendRequestParams{Status: status, ID: requestID})
}

// Remove is endpoint 64.
func (s *Service) Remove(ctx context.Context, who *authctx.Principal, otherID uuid.UUID) error {
	a, b := pair(who.ID, otherID)
	n, err := s.db.Q.RemoveFriendship(ctx, sqlc.RemoveFriendshipParams{A: a, B: b})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("friend")
	}
	return nil
}

// Search is endpoint 69.
func (s *Service) Search(ctx context.Context, who *authctx.Principal, q string, limit int32) ([]Person, error) {
	q = strings.TrimPrefix(strings.TrimSpace(q), "@")
	if q == "" {
		return []Person{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.db.Q.SearchPeople(ctx, sqlc.SearchPeopleParams{ViewerID: who.ID, Q: pgtype.Text{String: q, Valid: true}, Lim: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(rows))
	for _, r := range rows {
		p := Person{
			ID: r.ID, Name: r.DisplayName, Level: r.Level, Online: r.Online,
			IsGuest: r.Kind == sqlc.UserKindGuest, IsFriend: r.IsFriend, Requested: r.Requested,
		}
		if r.Username.Valid {
			p.Username = r.Username.String
		}
		if r.AvatarUrl.Valid {
			p.AvatarURL = r.AvatarUrl.String
		}
		out = append(out, p)
	}
	return out, nil
}

// Suggestions is endpoint 71: people met in a game, room or challenge. For a guest
// these are the guests they met (§4 of 03-BACKEND-CHANGES).
func (s *Service) Suggestions(ctx context.Context, who *authctx.Principal, limit int32) ([]Person, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Q.PlayedWith(ctx, sqlc.PlayedWithParams{
		ViewerID: who.ID, GuestsOnly: who.IsGuest(), Lim: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(rows))
	for _, r := range rows {
		p := Person{
			ID: r.ID, Name: r.DisplayName, Level: r.Level, Online: r.Online,
			IsGuest: r.Kind == sqlc.UserKindGuest, Requested: r.Requested,
		}
		if r.Username.Valid {
			p.Username = r.Username.String
		}
		out = append(out, p)
	}
	return out, nil
}

// Block is endpoint 72; it also drops any friendship and pending request.
func (s *Service) Block(ctx context.Context, who *authctx.Principal, otherID uuid.UUID) error {
	if otherID == who.ID {
		return ErrSelf
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.AddBlock(ctx, sqlc.AddBlockParams{BlockerID: who.ID, BlockedID: otherID}); err != nil {
			return err
		}
		a, b := pair(who.ID, otherID)
		_, err := q.RemoveFriendship(ctx, sqlc.RemoveFriendshipParams{A: a, B: b})
		return err
	})
}

// Unblock is endpoint 73.
func (s *Service) Unblock(ctx context.Context, who *authctx.Principal, otherID uuid.UUID) error {
	_, err := s.db.Q.RemoveBlock(ctx, sqlc.RemoveBlockParams{BlockerID: who.ID, BlockedID: otherID})
	return err
}

// Blocked is the guard every other module calls before it lets two players interact.
func (s *Service) Blocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	return s.db.Q.IsBlocked(ctx, sqlc.IsBlockedParams{A: a, B: b})
}

// AreFriends answers the privacy rules ("friends only") in messages and challenges.
func (s *Service) AreFriends(ctx context.Context, a, b uuid.UUID) (bool, error) {
	x, y := pair(a, b)
	return s.db.Q.IsFriend(ctx, sqlc.IsFriendParams{A: x, B: y})
}

// HaveMet is the guest rule (MS-04): guests may only talk to guests they played with.
func (s *Service) HaveMet(ctx context.Context, a, b uuid.UUID) (bool, error) {
	return s.db.Q.SharedHistory(ctx, sqlc.SharedHistoryParams{A: a, B: b})
}

// InviteLink is endpoint 74: one live link per player, re-used while it lasts.
func (s *Service) InviteLink(ctx context.Context, who *authctx.Principal) (string, error) {
	if row, err := s.db.Q.MyInvite(ctx, who.ID); err == nil {
		return row.Code, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	code, err := NewCode(8)
	if err != nil {
		return "", err
	}
	row, err := s.db.Q.CreateInvite(ctx, sqlc.CreateInviteParams{
		Code: code, InviterID: who.ID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(30 * 24 * time.Hour), Valid: true},
	})
	if err != nil {
		return "", err
	}
	return row.Code, nil
}

// NewCode makes a short, unambiguous code for invites, rooms and challenge links.
func NewCode(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	enc := base32.NewEncoding("ABCDEFGHJKLMNPQRSTUVWXYZ23456789").WithPadding(base32.NoPadding)
	return enc.EncodeToString(raw)[:n], nil
}

// Presence is what a friend is doing now, for the Friends card on Home (HM-07).
type Presence struct {
	Playing    string // the domain of a game they are in, if any
	LastSeenAt *time.Time
}

func (s *Service) Presence(ctx context.Context, ids []uuid.UUID, lang string) (map[uuid.UUID]Presence, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Q.FriendPresence(ctx, sqlc.FriendPresenceParams{Ids: ids, Lang: lang})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]Presence, len(rows))
	for _, r := range rows {
		p := Presence{}
		p.Playing = r.PlayingDomain
		if r.LastSeenAt.Valid {
			when := r.LastSeenAt.Time
			p.LastSeenAt = &when
		}
		out[r.ID] = p
	}
	return out, nil
}

// FriendIDs is the plain list of who this player is friends with. The rooms module
// reads it to show their open rooms on P13, without importing this package.
func (s *Service) FriendIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Q.ListFriends(ctx, sqlc.ListFriendsParams{UserID: userID})
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out, nil
}

// MessagePerson is one line of the "New message" dialog of P22.
type MessagePerson struct {
	ID     uuid.UUID
	Name   string
	Note   string
	Online bool
	Guest  bool
}

// MessagePeople is who this player may start a thread with. A member picks a friend;
// a guest picks one of the guests they actually played beside, and the dialog says
// where they met (RL-03, §4 of 03-BACKEND-CHANGES).
func (s *Service) MessagePeople(ctx context.Context, who *authctx.Principal) ([]MessagePerson, error) {
	if who == nil {
		return nil, nil
	}
	if who.IsGuest() {
		return s.guestsMet(ctx, who)
	}
	friends, err := s.Friends(ctx, who, "")
	if err != nil {
		return nil, err
	}
	out := make([]MessagePerson, 0, len(friends))
	for _, p := range friends {
		out = append(out, MessagePerson{ID: p.ID, Name: p.Name, Online: p.Online})
	}
	return out, nil
}

// guestsMet is the other guests this guest has shared a room, a duel or a challenge
// with: the only people a guest may write to.
func (s *Service) guestsMet(ctx context.Context, who *authctx.Principal) ([]MessagePerson, error) {
	rows, err := s.db.Q.GuestsMet(ctx, who.ID)
	if err != nil {
		return nil, err
	}
	out := make([]MessagePerson, 0, len(rows))
	for _, r := range rows {
		out = append(out, MessagePerson{
			ID: r.ID, Name: r.DisplayName, Online: r.Online, Guest: true, Note: r.Note,
		})
	}
	return out, nil
}

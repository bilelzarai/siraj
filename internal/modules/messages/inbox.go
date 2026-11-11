package messages

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// Rooms is what the Rooms tab needs from live rooms: the rooms this player is in, the
// chat of one of them, and a way to say something in it. The app wires a rooms
// service behind it, so this module never imports that one (NF-01).
type Rooms interface {
	RoomInbox(ctx context.Context, userID uuid.UUID) ([]RoomRow, error)
	RoomLines(ctx context.Context, who *authctx.Principal, code string) (RoomRow, []RoomLine, error)
	// RoomSay posts one line into a room's chat, with whatever was attached to it.
	RoomSay(ctx context.Context, who *authctx.Principal, code, body string, upload uuid.UUID, kind string) error
}

// SetRooms wires the live rooms in. Without it the Rooms tab is simply empty.
func (s *Service) SetRooms(r Rooms) { s.rooms = r }

// Inbox is one tab of MS-01: the threads of that kind, pinned first, filtered by what
// the player typed in the search box.
// Inbox is one page of a player's conversations, newest first with pinned on top.
// The screens that show all of it pass page 1 and a generous size; the API pages.
func (s *Service) Inbox(ctx context.Context, who *authctx.Principal, tab Tab, query string) ([]Row, error) {
	rows, _, err := s.InboxPage(ctx, who, tab, query, 1, 100)
	return rows, err
}

// InboxPage is Inbox with the page the caller asked for, and the total so a client
// knows whether to ask for another.
func (s *Service) InboxPage(ctx context.Context, who *authctx.Principal, tab Tab, query string, page, perPage int) ([]Row, int, error) {
	if who == nil {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 25
	}
	if tab == TabRooms {
		// Rooms are a different source and short by nature: the whole list is the
		// page, and saying so is more honest than pretending to offset it.
		rows, err := s.roomRows(ctx, who, query)
		return rows, len(rows), err
	}
	rows, err := s.db.Q.MyConversations(ctx, sqlc.MyConversationsParams{
		UserID: who.ID, Kinds: tab.kinds(),
		Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountInbox(ctx, sqlc.CountInboxParams{UserID: who.ID, Kinds: tab.kinds()})
	if err != nil {
		return nil, 0, err
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		row := Row{
			ID: r.ID.String(), UserID: r.OtherID, Name: r.OtherName,
			Unread: int(r.Unread), Online: r.OtherOnline, Pinned: r.Pinned,
			Muted: r.Muted, Members: int(r.Members),
			Guest:  r.OtherKind == string(sqlc.UserKindGuest),
			Group:  r.Kind == sqlc.ConversationKindGroup,
			System: r.Kind == sqlc.ConversationKindSystem,
		}
		if r.Title.Valid && r.Title.String != "" {
			row.Name = r.Title.String
		}
		if r.LastMessageAt.Valid {
			row.At = r.LastMessageAt.Time
		}
		row.Preview = s.preview(r, who.ID)
		if !matches(row, query) {
			continue
		}
		out = append(out, row)
	}
	return out, int(total), nil
}

// preview is the one line under a name in the list: who said it and what it was. A
// message that is not words says what it is instead (MS-01).
func (s *Service) preview(r sqlc.MyConversationsRow, me uuid.UUID) string {
	body := ""
	if r.LastBody.Valid {
		body = r.LastBody.String
	}
	switch r.LastKind {
	case "voice":
		body = "{voice}"
	case "image":
		body = "{image}"
	case "file":
		body = "{file}"
	case "challenge":
		body = "{challenge}"
	}
	if body == "" {
		return ""
	}
	switch {
	case r.LastSender.Valid && r.LastSender.UUID == me:
		return "{you}" + body
	case r.Kind == sqlc.ConversationKindGroup && r.LastSenderName != "":
		return r.LastSenderName + ": " + body
	}
	return body
}

// roomRows is the Rooms tab: the live rooms this player joined, newest chat first.
func (s *Service) roomRows(ctx context.Context, who *authctx.Principal, query string) ([]Row, error) {
	if s.rooms == nil {
		return nil, nil
	}
	rooms, err := s.rooms.RoomInbox(ctx, who.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(rooms))
	for _, r := range rooms {
		row := Row{
			ID: r.Code, Name: r.Title, Preview: r.Preview, At: r.At,
			Room: true, RoomCode: r.Code, Members: r.Members,
		}
		if matches(row, query) {
			out = append(out, row)
		}
	}
	return out, nil
}

// matches is the search box: a name or a line of the conversation (MS-01).
func matches(r Row, query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(r.Name), query) ||
		strings.Contains(strings.ToLower(r.Preview), query)
}

// TabCounts is the number on each tab (MS-01).
func (s *Service) TabCounts(ctx context.Context, who *authctx.Principal) (map[Tab]int, error) {
	out := map[Tab]int{}
	if who == nil {
		return out, nil
	}
	rows, err := s.db.Q.UnreadByKind(ctx, who.ID)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		switch r.Kind {
		case "group":
			out[TabGroups] += int(r.Threads)
		case "room":
			out[TabRooms] += int(r.Threads)
		default:
			out[TabPeople] += int(r.Threads)
		}
	}
	return out, nil
}

// Pin, Mute, Unread and Hide are the conversation menu of the design (MS-01).
func (s *Service) Pin(ctx context.Context, who *authctx.Principal, id uuid.UUID, on bool) error {
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	return s.db.Q.SetConversationPinned(ctx, sqlc.SetConversationPinnedParams{
		ConversationID: id, UserID: who.ID, Pinned: on,
	})
}

func (s *Service) Mute(ctx context.Context, who *authctx.Principal, id uuid.UUID, on bool) error {
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	return s.db.Q.SetConversationMuted(ctx, sqlc.SetConversationMutedParams{
		ConversationID: id, UserID: who.ID, Muted: on,
	})
}

func (s *Service) MarkUnread(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	return s.db.Q.MarkConversationUnread(ctx, sqlc.MarkConversationUnreadParams{
		ConversationID: id, UserID: who.ID,
	})
}

func (s *Service) Hide(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	return s.db.Q.HideConversation(ctx, sqlc.HideConversationParams{
		ConversationID: id, UserID: who.ID,
	})
}

// Candidates is who the "New message" dialog offers. A member picks a friend; a guest
// picks one of the guests they played with, and is told why they can (RL-03, §4).
func (s *Service) Candidates(ctx context.Context, who *authctx.Principal) ([]Candidate, error) {
	if who == nil {
		return nil, apperr.Unauthorized("Sign in first")
	}
	if s.people == nil {
		return nil, nil
	}
	return s.people.MessageCandidates(ctx, who)
}

// People is the social graph as the New message dialog needs it.
type People interface {
	MessageCandidates(ctx context.Context, who *authctx.Principal) ([]Candidate, error)
}

// SetPeople wires the list of people a player may write to.
func (s *Service) SetPeople(p People) { s.people = p }

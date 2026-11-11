package rooms

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// Friends is the one thing the rooms module needs from the social graph: whose open
// rooms to show on P13. It is an interface so rooms never imports social (NF-01).
type Friends interface {
	FriendIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

// SetFriends wires the social graph in. Without it, "open rooms from friends" is
// simply empty — the screen still works.
func (s *Service) SetFriends(f Friends) { s.friends = f }

// SettingsInput is the Room settings panel of P14: the host changing the round while
// everyone waits (RM-01, RM-02).
type SettingsInput struct {
	Count       int32
	Seconds     int32
	AllowGuests bool
}

// allowedCounts and allowedSeconds are what the picker offers, so a hand-written post
// cannot ask for a round the rest of the app does not know how to play (GP-02).
var (
	allowedCounts  = map[int32]bool{5: true, 10: true, 15: true, 20: true}
	allowedSeconds = map[int32]bool{10: true, 20: true, 30: true}
)

// UpdateSettings saves the panel. Only the host, and only while the room is waiting:
// the questions are dealt at start, so changing them mid-game would mean nothing.
func (s *Service) UpdateSettings(ctx context.Context, who *authctx.Principal, room sqlc.Room, in SettingsInput) error {
	if err := s.hostOnly(who, room); err != nil {
		return err
	}
	if room.Status != "open" {
		return apperr.Conflict("room_started", "The game has already started")
	}
	if !allowedCounts[in.Count] {
		return apperr.Invalid(map[string]string{"count": "5, 10, 15 or 20"})
	}
	if !allowedSeconds[in.Seconds] {
		return apperr.Invalid(map[string]string{"seconds": "10, 20 or 30"})
	}
	set := s.Settings(room)
	set.Count, set.Seconds, set.GuestsOff = in.Count, in.Seconds, !in.AllowGuests
	if err := s.saveSettings(ctx, room, set); err != nil {
		return err
	}
	s.announce(ctx, room)
	return nil
}

// OpenRoom is one line of P13's "Open rooms from friends".
type OpenRoom struct {
	Code     string
	HostID   uuid.UUID
	HostName string
	Players  int
	Max      int
	Teams    bool
	DomainID int64
}

// MaxOpenRooms is how many of a player's friends' rooms the join screen lists.
const MaxOpenRooms = 5

// OpenRooms is the rooms this player's friends are waiting in. A guest has no friend
// list, so the list is empty rather than a window into other people's rooms (RL-03).
func (s *Service) OpenRooms(ctx context.Context, who *authctx.Principal) ([]OpenRoom, error) {
	if who == nil || who.IsGuest() || s.friends == nil {
		return nil, nil
	}
	ids, err := s.friends.FriendIDs(ctx, who.ID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	rows, err := s.db.Q.OpenRoomsOfFriends(ctx, sqlc.OpenRoomsOfFriendsParams{
		FriendIds: ids, GuestRooms: false, Lim: MaxOpenRooms,
	})
	if err != nil {
		return nil, err
	}
	out := make([]OpenRoom, 0, len(rows))
	for _, r := range rows {
		set := s.Settings(sqlc.Room{Settings: r.Settings})
		out = append(out, OpenRoom{
			Code: strings.TrimSpace(r.Code), HostID: r.HostID, HostName: r.HostName,
			Players: int(r.Players), Max: int(r.MaxPlayers),
			Teams: set.Mode == ModeTeams, DomainID: set.Scope.DomainID,
		})
	}
	return out, nil
}

// Rename is "Your name in the room" on P13. Only a guest may use it: a member's name
// is their profile, which Settings owns (RL-02, AU-05).
func (s *Service) Rename(ctx context.Context, who *authctx.Principal, name string) error {
	if who == nil || !who.IsGuest() {
		return apperr.Forbidden("Only a guest can change their room name")
	}
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 2 || n > 20 {
		return apperr.Invalid(map[string]string{"name": "between 2 and 20 characters"})
	}
	_, err := s.db.Q.UpdateProfile(ctx, sqlc.UpdateProfileParams{
		ID: who.ID, DisplayName: pgtype.Text{String: name, Valid: true},
	})
	return err
}

// PlayerRoom is a live room as the Messages inbox lists it (MS-01).
type PlayerRoom struct {
	Code    string
	Title   string
	People  string
	Preview string
	At      time.Time
	Members int
	Open    bool
}

// PlayerRooms is the rooms this player is in, with the last line said in each.
func (s *Service) PlayerRooms(ctx context.Context, userID uuid.UUID) ([]PlayerRoom, error) {
	rows, err := s.db.Q.MyRooms(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]PlayerRoom, 0, len(rows))
	for _, r := range rows {
		room := PlayerRoom{
			Code: strings.TrimSpace(r.Code), Members: int(r.Players),
			Open: r.Status == "open",
		}
		room.Title = "Room " + room.Code
		room.Preview = r.LastBody
		if r.LastAt.Valid {
			room.At = r.LastAt.Time
		}
		out = append(out, room)
	}
	return out, nil
}

// ChatByCode is one room's conversation, read for the Messages screen: the room, who
// is in it, and every line said — ending with how the last game went (MS-01, RM-05).
func (s *Service) ChatByCode(ctx context.Context, who *authctx.Principal, code string) (PlayerRoom, []ChatLine, error) {
	room, err := s.ByCode(ctx, code)
	if err != nil {
		return PlayerRoom{}, nil, err
	}
	members, err := s.Members(ctx, room)
	if err != nil {
		return PlayerRoom{}, nil, err
	}
	inRoom := false
	names := make([]string, 0, len(members))
	for _, m := range members {
		if who != nil && m.UserID == who.ID {
			inRoom = true
			continue
		}
		names = append(names, m.DisplayName)
	}
	if !inRoom {
		return PlayerRoom{}, nil, apperr.Forbidden("You are not in this room")
	}
	lines, err := s.Chat(ctx, room)
	if err != nil {
		return PlayerRoom{}, nil, err
	}
	// A line is mine when its author is the name I wear in the room.
	myName := ""
	for _, m := range members {
		if who != nil && m.UserID == who.ID {
			myName = m.DisplayName
		}
	}
	for i := range lines {
		lines[i].Mine = myName != "" && lines[i].Author == myName
	}
	out := PlayerRoom{
		Code: strings.TrimSpace(room.Code), Members: len(members),
		Open: room.Status == "open", People: strings.Join(names, ", "),
	}
	out.Title = "Room " + out.Code
	return out, lines, nil
}

// SayByCode posts one line into a room's chat from the Messages screen, with
// whatever was attached to it (MS-02).
func (s *Service) SayByCode(ctx context.Context, who *authctx.Principal, code, body string, upload uuid.UUID, kind string) error {
	room, err := s.ByCode(ctx, code)
	if err != nil {
		return err
	}
	return s.SayWith(ctx, who, room, body, upload, kind)
}

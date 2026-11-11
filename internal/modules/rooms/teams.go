package rooms

import (
	"context"
	"crypto/rand"
	"math/big"

	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// The two ways a room plays: everyone for themselves, or Team A against Team B
// (RM-04, RM-05). The mode lives in rooms.settings, with the two sides beside it.
const (
	ModeFFA   = "ffa"
	ModeTeams = "teams"
)

// MinTeamPlayers is the smallest room that can take sides: two against two (RM-05).
const MinTeamPlayers = 4

// Sides is Team A and Team B, in that order.
type Sides [2][]uuid.UUID

// Len is how many players are on a side.
func (s Sides) Len(k int) int { return len(s[k]) }

// Of is the side a player is on, or -1 when the room plays for itself.
func (s Sides) Of(id uuid.UUID) int {
	for k := range s {
		for _, m := range s[k] {
			if m == id {
				return k
			}
		}
	}
	return -1
}

// Mode is how the room will play when it starts. A room set to teams that has lost
// players falls back to everyone for themselves instead of starting two against one
// (RM-05, §2 of 03-BACKEND-CHANGES).
func (s *Service) Mode(set Settings, members int) string {
	if set.Mode == ModeTeams && members >= MinTeamPlayers {
		return ModeTeams
	}
	return ModeFFA
}

// Sides reads the two teams as they stand: the host's arrangement, minus whoever has
// left, plus whoever arrived since — a newcomer joins the shorter side, so the room
// stays even without the host touching anything (RM-05).
func Split(stored [][]string, members []uuid.UUID) Sides {
	in := make(map[uuid.UUID]bool, len(members))
	for _, id := range members {
		in[id] = true
	}
	var out Sides
	placed := map[uuid.UUID]bool{}
	for k := 0; k < 2 && k < len(stored); k++ {
		for _, raw := range stored[k] {
			id, err := uuid.Parse(raw)
			if err != nil || !in[id] || placed[id] {
				continue
			}
			placed[id] = true
			out[k] = append(out[k], id)
		}
	}
	for _, id := range members {
		if placed[id] {
			continue
		}
		k := 0
		if len(out[0]) > len(out[1]) {
			k = 1
		}
		out[k] = append(out[k], id)
		placed[id] = true
	}
	return out
}

// store writes the two sides back the way rooms.settings holds them.
func (s Sides) store() [][]string {
	out := make([][]string, 2)
	for k := range s {
		out[k] = make([]string, 0, len(s[k]))
		for _, id := range s[k] {
			out[k] = append(out[k], id.String())
		}
	}
	return out
}

// Teams is the room's two sides right now, read from its settings and its members.
func (s *Service) Teams(ctx context.Context, room sqlc.Room) (Sides, error) {
	members, err := s.Members(ctx, room)
	if err != nil {
		return Sides{}, err
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	return Split(s.Settings(room).Teams, ids), nil
}

// SetMode switches the room between the two ways of playing. Only the host may, and
// only while the room is still waiting (RM-02, RM-05).
func (s *Service) SetMode(ctx context.Context, who *authctx.Principal, room sqlc.Room, mode string) error {
	if err := s.hostOnly(who, room); err != nil {
		return err
	}
	if mode != ModeFFA && mode != ModeTeams {
		return apperr.Invalid(map[string]string{"mode": "everyone for themselves, or teams"})
	}
	set := s.Settings(room)
	set.Mode = mode
	if mode == ModeTeams {
		sides, err := s.Teams(ctx, room)
		if err != nil {
			return err
		}
		set.Teams = sides.store()
	}
	return s.saveSettings(ctx, room, set)
}

// Shuffle deals the room into two even sides at random — endpoint 119, the host's
// "shuffle teams" (RM-05).
func (s *Service) Shuffle(ctx context.Context, who *authctx.Principal, room sqlc.Room) error {
	if err := s.hostOnly(who, room); err != nil {
		return err
	}
	members, err := s.Members(ctx, room)
	if err != nil {
		return err
	}
	if len(members) < MinTeamPlayers {
		return apperr.Conflict("too_few_for_teams", "Teams need at least four players")
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	shuffle(ids)
	var sides Sides
	for i, id := range ids {
		sides[i%2] = append(sides[i%2], id)
	}
	set := s.Settings(room)
	set.Mode, set.Teams = ModeTeams, sides.store()
	if err := s.saveSettings(ctx, room, set); err != nil {
		return err
	}
	s.announce(ctx, room)
	return nil
}

// Move puts the player who asked on the side they picked. Anyone in the lobby can
// move themselves; a side is never emptied by the last player walking out of it.
func (s *Service) Move(ctx context.Context, who *authctx.Principal, room sqlc.Room, side int) error {
	if who == nil {
		return apperr.Unauthorized("Sign in or play as a guest first")
	}
	if side != 0 && side != 1 {
		return apperr.Invalid(map[string]string{"team": "Team A or Team B"})
	}
	if room.Status != "open" {
		return apperr.Conflict("room_started", "The game has already started")
	}
	members, err := s.Members(ctx, room)
	if err != nil {
		return err
	}
	inRoom := false
	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
		inRoom = inRoom || m.UserID == who.ID
	}
	if !inRoom {
		return apperr.Forbidden("Join the room first")
	}
	// The same floor as Shuffle: a room that cannot play in teams must not answer a
	// move as though it had taken one, or the lobby shows sides the game will not
	// honour — Mode() falls back to everyone-for-themselves below four (RM-05).
	if len(members) < MinTeamPlayers {
		return apperr.Conflict("too_few_for_teams", "Teams need at least four players")
	}
	sides := Split(s.Settings(room).Teams, ids)
	from := sides.Of(who.ID)
	if from == side {
		return nil
	}
	if from >= 0 && len(sides[from]) <= 1 {
		return apperr.Conflict("team_empty", "A team cannot be left empty")
	}
	var next Sides
	for k := range sides {
		for _, id := range sides[k] {
			if id == who.ID {
				continue
			}
			next[k] = append(next[k], id)
		}
	}
	next[side] = append(next[side], who.ID)
	set := s.Settings(room)
	set.Mode, set.Teams = ModeTeams, next.store()
	if err := s.saveSettings(ctx, room, set); err != nil {
		return err
	}
	s.announce(ctx, room)
	return nil
}

func (s *Service) hostOnly(who *authctx.Principal, room sqlc.Room) error {
	if who == nil || room.HostID != who.ID {
		return apperr.Forbidden("Only the host can change the room")
	}
	return nil
}

// shuffle is a Fisher-Yates over crypto/rand: teams a player cannot predict.
func shuffle(ids []uuid.UUID) {
	for i := len(ids) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return
		}
		j := int(n.Int64())
		ids[i], ids[j] = ids[j], ids[i]
	}
}

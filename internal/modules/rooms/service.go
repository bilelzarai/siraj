// Package rooms is live play: a host opens a room with a six-character code, players
// join with the code or the link, everyone answers the same questions at the same
// time, and the podium closes it (RM-01…RM-05).
//
// Liveness is server-rendered: the lobby and the live game re-fetch themselves every
// couple of seconds through htmx, so a room works without a socket and without
// JavaScript beyond htmx itself.
package rooms

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

type Service struct {
	live    *live.Hub
	db      *db.DB
	games   *games.Service
	friends Friends
}

func NewService(d *db.DB, g *games.Service) *Service { return &Service{db: d, games: g} }

// CodeAlphabet leaves out the letters and digits that read alike (RM-01).
const CodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// MaxPlayers is the biggest room a member can host; guests host guests only (RM-01).
const MaxPlayers = 12

// NewCode is a six-character room code.
func NewCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 6)
	for i, v := range b {
		out[i] = CodeAlphabet[int(v)%len(CodeAlphabet)]
	}
	return string(out), nil
}

// HostInput is what P12 collects.
type HostInput struct {
	Who        *authctx.Principal
	Scope      games.Scope
	Difficulty int32
	Count      int32
	Seconds    int32
	MaxPlayers int32
	Chat       bool
	Mode       string
	Lang       string
}

// Host opens a room and puts the host in it. The questions are not chosen yet: the
// host can still change the settings until the room starts (RM-02).
func (s *Service) Host(ctx context.Context, in HostInput) (sqlc.Room, error) {
	if in.Who == nil {
		return sqlc.Room{}, apperr.Unauthorized("Sign in or play as a guest first")
	}
	if in.MaxPlayers < 2 || in.MaxPlayers > MaxPlayers {
		in.MaxPlayers = 8
	}
	if in.Who.IsGuest() && in.MaxPlayers > 8 {
		in.MaxPlayers = 8 // a guest room is a guest room (RL-03)
	}
	if in.Seconds == 0 {
		in.Seconds = 20 // a live room always has a timer, so everyone moves together
	}
	mode := ModeFFA
	if in.Mode == ModeTeams {
		mode = ModeTeams
	}
	settings, err := json.Marshal(Settings{
		Scope: in.Scope, Difficulty: in.Difficulty,
		Count: in.Count, Seconds: in.Seconds, Mode: mode,
	})
	if err != nil {
		return sqlc.Room{}, err
	}
	var room sqlc.Room
	for attempt := 0; attempt < 5; attempt++ {
		code, err := NewCode()
		if err != nil {
			return sqlc.Room{}, err
		}
		room, err = s.db.Q.CreateRoom(ctx, sqlc.CreateRoomParams{
			Code: code, HostID: in.Who.ID, IsGuestRoom: in.Who.IsGuest(),
			MaxPlayers: int16(in.MaxPlayers), ChatEnabled: in.Chat, Settings: settings,
		})
		if err == nil {
			break
		}
		if attempt == 4 {
			return sqlc.Room{}, err
		}
	}
	return room, s.db.Q.JoinRoom(ctx, sqlc.JoinRoomParams{RoomID: room.ID, UserID: in.Who.ID})
}

// Settings is what the host picked, read back out of the room. Mode and Teams are
// the ★ fields of §2 of 03-BACKEND-CHANGES: how the room plays, and who is on which
// side (RM-05).
type Settings struct {
	Scope      games.Scope `json:"scope"`
	Difficulty int32       `json:"difficulty"`
	Count      int32       `json:"count"`
	Seconds    int32       `json:"seconds"`
	Mode       string      `json:"mode,omitempty"`
	Teams      [][]string  `json:"teams,omitempty"`
	// GuestsOff is the "Guests allowed" switch of P14, stored the other way round so
	// every room written before the switch existed keeps letting guests in (RM-01).
	GuestsOff bool `json:"guests_off,omitempty"`
}

func (s *Service) Settings(room sqlc.Room) Settings {
	var out Settings
	_ = json.Unmarshal(room.Settings, &out)
	if out.Count == 0 {
		out.Count = 10
	}
	if out.Seconds == 0 {
		out.Seconds = 20
	}
	if out.Mode == "" {
		out.Mode = ModeFFA
	}
	return out
}

// saveSettings writes the host's choice back, leaving everything else in the room
// untouched.
func (s *Service) saveSettings(ctx context.Context, room sqlc.Room, set Settings) error {
	raw, err := json.Marshal(set)
	if err != nil {
		return err
	}
	return s.db.Q.UpdateRoomSettings(ctx, sqlc.UpdateRoomSettingsParams{ID: room.ID, Settings: raw})
}

// ByCode finds an open room. Codes are typed by hand, so case and spaces are forgiven.
func (s *Service) ByCode(ctx context.Context, code string) (sqlc.Room, error) {
	code = strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(code, " ", "")))
	if len(code) != 6 {
		return sqlc.Room{}, apperr.Invalid(map[string]string{"code": "six characters"})
	}
	room, err := s.db.Q.GetRoomByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Room{}, apperr.NotFound("room")
	}
	return room, err
}

// Join puts a player in a room, within its limits (RM-02, RL-03).
func (s *Service) Join(ctx context.Context, who *authctx.Principal, room sqlc.Room) error {
	if who == nil {
		return apperr.Unauthorized("Sign in or play as a guest first")
	}
	if room.Status == "closed" {
		return apperr.Conflict("room_closed", "That room has closed")
	}
	if room.IsGuestRoom && !who.IsGuest() {
		return apperr.Conflict("guest_room", "That room is for guests only")
	}
	if who.IsGuest() && !room.IsGuestRoom && s.Settings(room).GuestsOff {
		return apperr.Conflict("guests_off", "This room is not taking guests")
	}
	n, err := s.db.Q.CountRoomMembers(ctx, room.ID)
	if err != nil {
		return err
	}
	if int(n) >= int(room.MaxPlayers) {
		return apperr.Conflict("room_full", "That room is full")
	}
	err = s.db.Q.JoinRoom(ctx, sqlc.JoinRoomParams{RoomID: room.ID, UserID: who.ID})
	if err == nil {
		s.announce(ctx, room)
	}
	return err
}

// SetLive gives the service the event stream: the lobby updates when someone joins,
// readies or leaves, instead of waiting for the next poll (RM-02, NF-02).
func (s *Service) SetLive(hub *live.Hub) { s.live = hub }

// announce tells everyone in the room that it changed.
func (s *Service) announce(ctx context.Context, room sqlc.Room) {
	members, err := s.Members(ctx, room)
	if err != nil {
		return
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	s.live.SendAll(ids, live.Rooms)
}

func (s *Service) Leave(ctx context.Context, who *authctx.Principal, room sqlc.Room) error {
	err := s.db.Q.LeaveRoom(ctx, sqlc.LeaveRoomParams{RoomID: room.ID, UserID: who.ID})
	if err == nil {
		s.announce(ctx, room)
	}
	return err
}

// Kick removes another player from the lobby. Only the host may, and never themselves
// (RM-02).
func (s *Service) Kick(ctx context.Context, who *authctx.Principal, room sqlc.Room, userID uuid.UUID) error {
	if room.HostID != who.ID {
		return apperr.Forbidden("Only the host can remove a player")
	}
	if userID == who.ID {
		return apperr.Invalid(map[string]string{"player": "the host stays in the room"})
	}
	err := s.db.Q.LeaveRoom(ctx, sqlc.LeaveRoomParams{RoomID: room.ID, UserID: userID})
	if err == nil {
		s.announce(ctx, room)
	}
	return err
}

func (s *Service) Ready(ctx context.Context, who *authctx.Principal, room sqlc.Room, ready bool) error {
	err := s.db.Q.SetReady(ctx, sqlc.SetReadyParams{RoomID: room.ID, UserID: who.ID, IsReady: ready})
	if err == nil {
		s.announce(ctx, room)
	}
	return err
}

func (s *Service) Members(ctx context.Context, room sqlc.Room) ([]sqlc.RoomMembersRow, error) {
	return s.db.Q.RoomMembers(ctx, room.ID)
}

// Start deals one set of questions to the whole room: one game, one player row per
// member, so everyone answers the same list (RM-03).
func (s *Service) Start(ctx context.Context, who *authctx.Principal, room sqlc.Room) (sqlc.Game, error) {
	if room.HostID != who.ID {
		return sqlc.Game{}, apperr.Forbidden("Only the host can start the game")
	}
	if game, err := s.db.Q.RoomGame(ctx, uuid.NullUUID{UUID: room.ID, Valid: true}); err == nil {
		s.announce(ctx, room)
		return game, nil // already started; everyone joins the same game
	}
	members, err := s.Members(ctx, room)
	if err != nil {
		return sqlc.Game{}, err
	}
	settings := s.Settings(room)
	ids, err := s.games.PickIDs(ctx, games.StartInput{
		Who: who, Scope: settings.Scope, Difficulty: settings.Difficulty,
		Count: settings.Count, Seconds: settings.Seconds,
	})
	if err != nil {
		return sqlc.Game{}, err
	}
	game, err := s.games.StartFixed(ctx, games.FixedInput{
		Who: who, Mode: sqlc.GameModeLive, Scope: settings.Scope,
		Difficulty: settings.Difficulty, Seconds: settings.Seconds,
		QuestionIDs: ids, RoomID: room.ID,
	})
	if err != nil {
		return sqlc.Game{}, err
	}
	// Everyone else in the room gets a player row on the same game.
	for _, m := range members {
		if m.UserID == who.ID {
			continue
		}
		if _, err := s.db.Q.AddGamePlayer(ctx, sqlc.AddGamePlayerParams{
			GameID: game.Row.ID, UserID: uuid.NullUUID{UUID: m.UserID, Valid: true},
		}); err != nil {
			return sqlc.Game{}, err
		}
	}
	// Two sides, when the room is big enough for them: the arrangement the lobby
	// showed is written onto the player rows, so the game itself carries the teams
	// and "play again" can read them back (RM-05).
	if s.Mode(settings, len(members)) == ModeTeams {
		ids := make([]uuid.UUID, 0, len(members))
		for _, m := range members {
			ids = append(ids, m.UserID)
		}
		sides := Split(settings.Teams, ids)
		for k := range sides {
			for _, id := range sides[k] {
				if err := s.db.Q.SetGamePlayerTeam(ctx, sqlc.SetGamePlayerTeamParams{
					GameID: game.Row.ID, UserID: uuid.NullUUID{UUID: id, Valid: true},
					Team: pgtype.Int2{Int16: int16(k), Valid: true},
				}); err != nil {
					return sqlc.Game{}, err
				}
			}
		}
		// Play again keeps the sides the room ended on (§2 of 03-BACKEND-CHANGES).
		settings.Teams = sides.store()
		if err := s.saveSettings(ctx, room, settings); err != nil {
			return sqlc.Game{}, err
		}
	}
	if err := s.db.Q.SetRoomStatus(ctx, sqlc.SetRoomStatusParams{ID: room.ID, Status: "playing"}); err != nil {
		return sqlc.Game{}, err
	}
	// The loudest change of all: every lobby screen walks into the game at once.
	s.announce(ctx, room)
	return game.Row, nil
}

// Game is the room's running game, if any.
func (s *Service) Game(ctx context.Context, room sqlc.Room) (sqlc.Game, bool) {
	game, err := s.db.Q.RoomGame(ctx, uuid.NullUUID{UUID: room.ID, Valid: true})
	return game, err == nil
}

// Standing is one line of the live list, of the teams bar and of the podium: one
// player, how far they have got and what they just scored (RM-04, RM-05, RM-07).
type Standing struct {
	PlayerID uuid.UUID
	UserID   uuid.UUID
	Name     string
	Score    int32
	Correct  int32
	Answered int32
	Done     bool
	IsMe     bool
	Guest    bool
	// Team is 0 for Team A, 1 for Team B, -1 when the room plays for itself.
	Team int32
	// AvgMs and BestMs are the speed awards of the podium; Streak is the longest run
	// of right answers (RM-07).
	AvgMs  int32
	BestMs int32
	Streak int32
	XP     int32
	// The question on screen: whether this player has answered it yet, and what it
	// gave them once the room has seen the answer (RM-04, RM-06).
	AnsweredThis bool
	CorrectThis  bool
	Gain         int32
}

// ChatLine is one line of the room's chat: something a player said, or the room
// itself saying who arrived (RM-05).
type ChatLine struct {
	Author string
	Body   string
	// What came with the line, when it was a file rather than words (MS-02).
	Kind     string
	FileName string
	FileSize int64
	FileURL  string
	Duration int
	System   bool
	// Mine is set when the Messages screen reads the chat, so a player's own lines
	// sit on their side of the thread (MS-02).
	Mine bool
	When time.Time
}

// MaxChatLines is how far back the panel reads.
const MaxChatLines = 40

// Chat is the room's conversation, oldest first, with the arrivals woven in at the
// moment they happened so the panel reads like the design's.
func (s *Service) Chat(ctx context.Context, room sqlc.Room) ([]ChatLine, error) {
	rows, err := s.db.Q.ListRoomMessages(ctx, sqlc.ListRoomMessagesParams{RoomID: room.ID, Lim: MaxChatLines})
	if err != nil {
		return nil, err
	}
	out := make([]ChatLine, 0, len(rows)+8)
	for _, r := range rows {
		line := ChatLine{Author: r.DisplayName, Kind: r.LineKind}
		if r.Body.Valid {
			line.Body = r.Body.String
		}
		if r.FileKey.Valid {
			line.FileURL = "/uploads/" + r.FileKey.String
			line.FileSize = int64(r.FileBytes.Int32)
			line.Duration = int(r.FileDuration.Int32)
			if r.FileName.Valid {
				line.FileName = r.FileName.String
			}
		}
		if r.CreatedAt.Valid {
			line.When = r.CreatedAt.Time
		}
		out = append(out, line)
	}
	members, err := s.Members(ctx, room)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.IsHost || !m.JoinedAt.Valid {
			continue
		}
		out = append(out, ChatLine{Author: m.DisplayName, System: true, When: m.JoinedAt.Time})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].When.Before(out[j].When) })
	if len(out) > MaxChatLines {
		out = out[len(out)-MaxChatLines:]
	}
	return out, nil
}

// Say adds one line to the room's chat. Only a member may, and only while the room is
// open (RM-05).
func (s *Service) Say(ctx context.Context, who *authctx.Principal, room sqlc.Room, body string) error {
	return s.SayWith(ctx, who, room, body, uuid.Nil, "text")
}

// SayWith adds one line, with whatever the player attached to it (MS-02, RM-05).
func (s *Service) SayWith(ctx context.Context, who *authctx.Principal, room sqlc.Room, body string, upload uuid.UUID, kind string) error {
	body = strings.TrimSpace(body)
	if body == "" && upload == uuid.Nil {
		return apperr.Invalid(map[string]string{"body": "write something first"})
	}
	if len([]rune(body)) > 200 {
		body = string([]rune(body)[:200])
	}
	if !room.ChatEnabled {
		return apperr.Conflict("chat_off", "Chat is off in this room")
	}
	members, err := s.Members(ctx, room)
	if err != nil {
		return err
	}
	for _, m := range members {
		if m.UserID == who.ID {
			if kind == "" {
				kind = "text"
			}
			_, err := s.db.Q.PostRoomMessage(ctx, sqlc.PostRoomMessageParams{
				RoomID: room.ID, UserID: who.ID, Kind: kind,
				Body:     pgtype.Text{String: body, Valid: body != ""},
				UploadID: uuid.NullUUID{UUID: upload, Valid: upload != uuid.Nil},
			})
			if err == nil {
				s.announce(ctx, room)
			}
			return err
		}
	}
	return apperr.Forbidden("Join the room first")
}

// Answered is how far the room has got on one question: how many players have
// answered it, out of how many are playing (RM-03).
func (s *Service) Answered(ctx context.Context, gameID uuid.UUID, position int32) (int, int, error) {
	done, err := s.db.Q.CountAnswersAt(ctx, sqlc.CountAnswersAtParams{GameID: gameID, Position: int16(position)})
	if err != nil {
		return 0, 0, err
	}
	total, err := s.db.Q.CountGamePlayers(ctx, gameID)
	if err != nil {
		return 0, 0, err
	}
	return int(done), int(total), nil
}

// Standings is everyone on a shared game, best first. Position is the question on
// screen: pass -1 (or a position nobody has reached) and the "this question" columns
// stay empty.
func (s *Service) Standings(ctx context.Context, who *authctx.Principal, gameID uuid.UUID, position int32) ([]Standing, error) {
	rows, err := s.db.Q.PlayerProgress(ctx, gameID)
	if err != nil {
		return nil, err
	}
	gains := map[uuid.UUID]sqlc.AnswersAtPositionRow{}
	if position >= 0 {
		answers, err := s.db.Q.AnswersAtPosition(ctx, sqlc.AnswersAtPositionParams{
			GameID: gameID, Position: int16(position),
		})
		if err != nil {
			return nil, err
		}
		for _, a := range answers {
			gains[a.PlayerID] = a
		}
	}
	streaks := map[uuid.UUID]int32{}
	if runs, err := s.db.Q.GameStreaks(ctx, gameID); err == nil {
		for _, r := range runs {
			streaks[r.PlayerID] = r.Streak
		}
	}
	out := make([]Standing, 0, len(rows))
	for _, p := range rows {
		row := Standing{
			PlayerID: p.ID, Score: p.Score, Correct: int32(p.Correct),
			Answered: p.Answered, Done: p.FinishedAt.Valid, Team: -1,
			AvgMs: p.AvgMs, BestMs: p.BestMs, Streak: streaks[p.ID], XP: p.XpGained,
			IsMe: p.UserID.Valid && who != nil && p.UserID.UUID == who.ID,
		}
		if p.Team.Valid {
			row.Team = int32(p.Team.Int16)
		}
		if p.UserID.Valid {
			row.UserID = p.UserID.UUID
			if u, err := s.db.Q.GetUser(ctx, p.UserID.UUID); err == nil {
				row.Name, row.Guest = u.DisplayName, u.Kind == sqlc.UserKindGuest
			}
		} else if p.LocalName.Valid {
			row.Name = p.LocalName.String
		}
		if a, ok := gains[p.ID]; ok {
			row.AnsweredThis, row.CorrectThis, row.Gain = true, a.IsCorrect, a.Points
		}
		out = append(out, row)
	}
	return out, nil
}

// TeamTotals adds a side's players up. Team totals are computed, never stored (§2 of
// 03-BACKEND-CHANGES).
func TeamTotals(rows []Standing) (totals [2]int32, gains [2]int32, counts [2]int) {
	for _, r := range rows {
		if r.Team != 0 && r.Team != 1 {
			continue
		}
		totals[r.Team] += r.Score
		gains[r.Team] += r.Gain
		counts[r.Team]++
	}
	return
}

// HasTeams says whether a finished or running game was played in two sides.
func HasTeams(rows []Standing) bool {
	var seen [2]bool
	for _, r := range rows {
		if r.Team == 0 || r.Team == 1 {
			seen[r.Team] = true
		}
	}
	return seen[0] && seen[1]
}

// Reopen puts a finished room back in the lobby so the same players can go again.
func (s *Service) Reopen(ctx context.Context, who *authctx.Principal, room sqlc.Room) error {
	if room.HostID != who.ID {
		return apperr.Forbidden("Only the host can reopen the room")
	}
	return s.db.Q.SetRoomStatus(ctx, sqlc.SetRoomStatusParams{ID: room.ID, Status: "open"})
}

// Room reads one room by id, for the screens that only hold the game's room_id.
func (s *Service) Room(ctx context.Context, id uuid.UUID) (sqlc.Room, error) {
	return s.db.Q.GetRoom(ctx, id)
}

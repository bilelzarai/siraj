package challenges

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db/sqlc"
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// A round on one device runs question by question, not player by player: everyone
// answers question one in turn, the picks stay hidden until the last of them has
// answered, and only then does the screen reveal what the answer was (DV-02…DV-04).

// DevicePlayer is one of the people sharing the phone, as the pass screen and the
// reveal screen need them.
type DevicePlayer struct {
	ID      uuid.UUID
	Name    string
	Score   int32
	Correct int32
	Team    int32
	// At this question.
	Answered bool
	Choice   *int32
	Right    bool
	Gain     int32
	TimeMS   int32
	// Now is true for the person whose turn it is.
	Now bool
}

// DeviceState is where a round has got to: which question, who still has to answer
// it, and whether the table has turned it over yet.
type DeviceState struct {
	GameID    uuid.UUID
	Position  int32
	Questions int
	Players   []DevicePlayer
	// Turn is the player holding the phone, or nil when the question is done.
	Turn *DevicePlayer
	// Revealed is true once everyone has answered this question (DV-04).
	Revealed bool
	// Done is true after the last question has been revealed (DV-05).
	Done bool
	// Fastest is the quickest right answer of this question, named on the reveal.
	Fastest string
	Teams   bool
	Totals  [2]int32
	Gains   [2]int32
}

// DeviceState reads the round. Everything comes from the answers already written, so
// a reload never loses a turn and nothing is kept in the browser (NF-05).
func (s *Service) DeviceState(ctx context.Context, gameID uuid.UUID) (DeviceState, error) {
	game, err := s.db.Q.GetGame(ctx, gameID)
	if err != nil {
		return DeviceState{}, apperr.NotFound("game")
	}
	if game.Mode != sqlc.GameModeDevice {
		return DeviceState{}, apperr.NotFound("game")
	}
	rows, err := s.db.Q.GamePlayersInOrder(ctx, gameID)
	if err != nil {
		return DeviceState{}, err
	}
	if len(rows) == 0 {
		return DeviceState{}, apperr.NotFound("game")
	}
	out := DeviceState{
		GameID: gameID, Questions: int(game.QuestionCount), Position: -1,
	}

	// The question on the table is the first one somebody has still to answer.
	answers := map[int32]map[uuid.UUID]sqlc.PlayerChoiceAtRow{}
	for position := int32(0); position < int32(game.QuestionCount); position++ {
		at, err := s.db.Q.PlayerChoiceAt(ctx, sqlc.PlayerChoiceAtParams{
			GameID: gameID, Position: int16(position),
		})
		if err != nil {
			return DeviceState{}, err
		}
		byPlayer := make(map[uuid.UUID]sqlc.PlayerChoiceAtRow, len(at))
		for _, a := range at {
			byPlayer[a.PlayerID] = a
		}
		answers[position] = byPlayer
		if len(byPlayer) < len(rows) && out.Position < 0 {
			out.Position = position
		}
	}
	if out.Position < 0 {
		// Every question has every answer: the round is over (DV-05).
		out.Position = int32(game.QuestionCount) - 1
		out.Done, out.Revealed = true, true
	}

	here := answers[out.Position]
	fastest, fastestMS := "", int32(0)
	for _, r := range rows {
		p := DevicePlayer{
			ID: r.ID, Score: r.Score, Correct: int32(r.Correct), Team: -1,
			Name: localName(r),
		}
		if r.Team.Valid {
			p.Team = int32(r.Team.Int16)
		}
		if a, ok := here[r.ID]; ok {
			p.Answered, p.Right, p.Gain, p.TimeMS = true, a.IsCorrect, a.Points, a.TimeMs
			if a.Choice.Valid {
				choice := int32(a.Choice.Int16)
				p.Choice = &choice
			}
			if a.IsCorrect && (fastest == "" || a.TimeMs < fastestMS) {
				fastest, fastestMS = p.Name, a.TimeMs
			}
		}
		out.Players = append(out.Players, p)
	}
	// The phone goes to the first person who has not answered this question yet.
	for i := range out.Players {
		if !out.Players[i].Answered {
			out.Players[i].Now = true
			out.Turn = &out.Players[i]
			break
		}
	}
	if out.Turn == nil {
		out.Revealed = true
		out.Fastest = fastest
	}
	out.Teams = deviceHasTeams(out.Players)
	for _, p := range out.Players {
		if p.Team == 0 || p.Team == 1 {
			out.Totals[p.Team] += p.Score
			out.Gains[p.Team] += p.Gain
		}
	}
	return out, nil
}

// Standings is the round so far, best first, for the pass screen and the reveal.
func (d DeviceState) Standings() []DevicePlayer {
	out := make([]DevicePlayer, len(d.Players))
	copy(out, d.Players)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Answered is how many of them have turned this question over.
func (d DeviceState) Answered() int {
	n := 0
	for _, p := range d.Players {
		if p.Answered {
			n++
		}
	}
	return n
}

// Choices is which option each player picked, so the reveal can stand their avatars
// on the answers they chose (DV-04).
func (d DeviceState) Choices(option int32) []DevicePlayer {
	var out []DevicePlayer
	for _, p := range d.Players {
		if p.Choice != nil && *p.Choice == option {
			out = append(out, p)
		}
	}
	return out
}

// ShareA is Team A's slice of the tug bar.
func (d DeviceState) ShareA() int {
	sum := d.Totals[0] + d.Totals[1]
	if sum <= 0 {
		return 50
	}
	return int(d.Totals[0] * 100 / sum)
}

func deviceHasTeams(players []DevicePlayer) bool {
	var seen [2]bool
	for _, p := range players {
		if p.Team == 0 || p.Team == 1 {
			seen[p.Team] = true
		}
	}
	return seen[0] && seen[1]
}

// localName is the name on a device player's card: a local name, or the owner's own.
func localName(p sqlc.GamePlayer) string {
	if p.LocalName.Valid {
		return p.LocalName.String
	}
	return ""
}

// MinDevicePlayers and MaxDevicePlayers are the limits of §3 of 03-BACKEND-CHANGES.
const (
	MinDevicePlayers = 2
	MaxDevicePlayers = 10
	// DeviceTeamPlayers is the smallest round that can take sides (DV-01).
	DeviceTeamPlayers = 4
)

// CheckLocals is the rule behind the names on the setup screen: two to ten players,
// one to twenty characters each, and no two the same.
func CheckLocals(names []string) error {
	if len(names)+1 < MinDevicePlayers {
		return apperr.Invalid(map[string]string{"local": "add at least one more player"})
	}
	if len(names)+1 > MaxDevicePlayers {
		return apperr.Invalid(map[string]string{"local": "up to ten players"})
	}
	seen := map[string]bool{}
	for _, n := range names {
		if len([]rune(n)) < 1 || len([]rune(n)) > 20 {
			return apperr.Invalid(map[string]string{"local": "names are 1 to 20 characters"})
		}
		if seen[n] {
			return apperr.Invalid(map[string]string{"local": "two players share a name"})
		}
		seen[n] = true
	}
	return nil
}

// SetDeviceTeams deals the round into two sides, alternating down the list the host
// typed, so "Teams" on the setup screen means something before the first question
// (DV-01). Fewer than four players play for themselves.
func (s *Service) SetDeviceTeams(ctx context.Context, gameID uuid.UUID) error {
	rows, err := s.db.Q.GamePlayersInOrder(ctx, gameID)
	if err != nil {
		return err
	}
	if len(rows) < DeviceTeamPlayers {
		return nil
	}
	for i, r := range rows {
		if err := s.db.Q.SetGamePlayerTeamByID(ctx, sqlc.SetGamePlayerTeamByIDParams{
			ID: r.ID, Team: pgtype.Int2{Int16: int16(i % 2), Valid: true},
		}); err != nil {
			return err
		}
	}
	return nil
}

// DeviceStateAt is the round as it stood at one question: the same shape as
// DeviceState, but pinned to a position the table has already finished, so the reveal
// can be read again after a reload (DV-04).
func (s *Service) DeviceStateAt(ctx context.Context, gameID uuid.UUID, position int32) (DeviceState, error) {
	state, err := s.DeviceState(ctx, gameID)
	if err != nil {
		return DeviceState{}, err
	}
	if position == state.Position {
		return state, nil
	}
	if position < 0 || int(position) >= state.Questions {
		return DeviceState{}, apperr.NotFound("question")
	}
	rows, err := s.db.Q.GamePlayersInOrder(ctx, gameID)
	if err != nil {
		return DeviceState{}, err
	}
	at, err := s.db.Q.PlayerChoiceAt(ctx, sqlc.PlayerChoiceAtParams{
		GameID: gameID, Position: int16(position),
	})
	if err != nil {
		return DeviceState{}, err
	}
	here := make(map[uuid.UUID]sqlc.PlayerChoiceAtRow, len(at))
	for _, a := range at {
		here[a.PlayerID] = a
	}
	out := DeviceState{
		GameID: gameID, Position: position, Questions: state.Questions,
		Done: state.Done,
	}
	fastest, fastestMS := "", int32(0)
	for _, r := range rows {
		p := DevicePlayer{
			ID: r.ID, Score: r.Score, Correct: int32(r.Correct), Team: -1,
			Name: localName(r),
		}
		if r.Team.Valid {
			p.Team = int32(r.Team.Int16)
		}
		if a, ok := here[r.ID]; ok {
			p.Answered, p.Right, p.Gain, p.TimeMS = true, a.IsCorrect, a.Points, a.TimeMs
			if a.Choice.Valid {
				choice := int32(a.Choice.Int16)
				p.Choice = &choice
			}
			if a.IsCorrect && (fastest == "" || a.TimeMs < fastestMS) {
				fastest, fastestMS = p.Name, a.TimeMs
			}
		}
		out.Players = append(out.Players, p)
	}
	out.Revealed = len(here) >= len(rows)
	out.Fastest = fastest
	out.Teams = deviceHasTeams(out.Players)
	for _, p := range out.Players {
		if p.Team == 0 || p.Team == 1 {
			out.Totals[p.Team] += p.Score
			out.Gains[p.Team] += p.Gain
		}
	}
	return out, nil
}

// FinishDeviceRound settles a round once every question has every answer: each local
// player gets their place, and the game closes. The owner's own row is finished by
// the engine when they answer their last question, which is also where their XP,
// mastery and streak come from — a local player has no account, so there is nothing
// to award them (DV-05, DV-06).
func (s *Service) FinishDeviceRound(ctx context.Context, gameID uuid.UUID) error {
	state, err := s.DeviceState(ctx, gameID)
	if err != nil {
		return err
	}
	if !state.Done {
		return nil
	}
	ranked := state.Standings()
	for i, p := range ranked {
		if err := s.db.Q.FinishDevicePlayer(ctx, sqlc.FinishDevicePlayerParams{
			ID: p.ID, Score: p.Score, Correct: int16(p.Correct),
			Rank: pgtype.Int2{Int16: int16(i + 1), Valid: true},
		}); err != nil {
			return err
		}
	}
	return s.db.Q.FinishGame(ctx, sqlc.FinishGameParams{
		ID: gameID, Status: sqlc.GameStatusFinished,
	})
}

// DeviceResult is the podium of a round on one phone (DV-05).
type DeviceResult struct {
	Players   []DevicePlayer
	Questions int
	Teams     bool
	Totals    [2]int32
	Winner    int
	Diff      int32
}

// DeviceResults is the final standing, best first, with the team totals when the
// round took sides.
func (s *Service) DeviceResults(ctx context.Context, gameID uuid.UUID) (DeviceResult, error) {
	state, err := s.DeviceState(ctx, gameID)
	if err != nil {
		return DeviceResult{}, err
	}
	out := DeviceResult{
		Players: state.Standings(), Questions: state.Questions,
		Teams: state.Teams, Totals: state.Totals, Winner: -1,
	}
	if out.Teams {
		switch {
		case out.Totals[0] > out.Totals[1]:
			out.Winner, out.Diff = 0, out.Totals[0]-out.Totals[1]
		case out.Totals[1] > out.Totals[0]:
			out.Winner, out.Diff = 1, out.Totals[1]-out.Totals[0]
		}
	}
	return out, nil
}

// RepeatDeviceRound is "Play again, same players": a new round on the same choice,
// with the same names in the same order and the same two sides (DV-05).
func (s *Service) RepeatDeviceRound(ctx context.Context, who *authctx.Principal, gameID uuid.UUID) (uuid.UUID, error) {
	if who == nil {
		return uuid.Nil, apperr.Unauthorized("Sign in first")
	}
	old, err := s.db.Q.GetGame(ctx, gameID)
	if err != nil || old.Mode != sqlc.GameModeDevice {
		return uuid.Nil, apperr.NotFound("game")
	}
	if !old.OwnerID.Valid || old.OwnerID.UUID != who.ID {
		return uuid.Nil, apperr.Forbidden("Only the player who set the round up can start it again")
	}
	players, err := s.db.Q.GamePlayersInOrder(ctx, gameID)
	if err != nil {
		return uuid.Nil, err
	}
	questions, err := s.db.Q.GameQuestions(ctx, sqlc.GameQuestionsParams{GameID: gameID, Lang: "en"})
	if err != nil {
		return uuid.Nil, err
	}
	ids := make([]int64, 0, len(questions))
	for _, q := range questions {
		ids = append(ids, q.ID)
	}
	locals := make([]string, 0, len(players))
	teams := map[string]int32{}
	hadTeams := false
	for _, p := range players {
		name := localName(p)
		if name == "" {
			continue // the owner is always player one and is added by the engine
		}
		locals = append(locals, name)
		if p.Team.Valid {
			teams[name], hadTeams = int32(p.Team.Int16), true
		}
	}
	var scope games.Scope
	_ = json.Unmarshal(old.Scope, &scope)
	fresh, err := s.games.StartFixed(ctx, games.FixedInput{
		Who: who, Mode: sqlc.GameModeDevice, Scope: scope,
		Seconds: int32(old.SecondsPerQuestion), QuestionIDs: ids, Locals: locals,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if hadTeams {
		if err := s.SetDeviceTeams(ctx, fresh.Row.ID); err != nil {
			return uuid.Nil, err
		}
	}
	return fresh.Row.ID, nil
}

// StartDeviceRound opens a pass-the-device round: the questions are frozen once, a
// player row is written for every name on the phone, and the sides are dealt when
// the round is played in teams (DV-01, §3 of 03-BACKEND-CHANGES).
//
// The website (P20b) and endpoint 62 both come through here, so one phone round is
// built the same way whichever asked for it (NF-04).
func (s *Service) StartDeviceRound(ctx context.Context, in CreateInput, teams bool) (uuid.UUID, error) {
	if in.Who == nil {
		return uuid.Nil, apperr.Unauthorized("Sign in or play as a guest first")
	}
	if err := CheckLocals(in.Locals); err != nil {
		return uuid.Nil, err
	}
	if in.Seconds == 0 {
		in.Seconds = 20
	}
	bank, set, err := s.questionIDs(ctx, in)
	if err != nil {
		return uuid.Nil, err
	}
	game, err := s.games.StartFixed(ctx, games.FixedInput{
		Who: in.Who, Mode: sqlc.GameModeDevice, Scope: in.Scope, Difficulty: in.Difficulty,
		Seconds: in.Seconds, QuestionIDs: bank, SetQuestionIDs: set, Locals: in.Locals,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if teams {
		if err := s.SetDeviceTeams(ctx, game.Row.ID); err != nil {
			return uuid.Nil, err
		}
	}
	return game.Row.ID, nil
}

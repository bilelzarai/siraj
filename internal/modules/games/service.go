package games

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/modules/catalog"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// QuickGameQuestions is fixed by the product rules: a quick game is always 5 (HM-04).
const QuickGameQuestions = 5

// MinSetQuestions: a player-made set is playable from 5 questions (MQ-02).
const MinSetQuestions = 5

var (
	ErrNoQuestions = apperr.Conflict("no_questions", "No question matches this choice yet")
	// ErrFewQuestions: a game runs on the number of questions that was asked for, so a
	// scope holding fewer than that cannot start (GP-02).
	ErrFewQuestions = apperr.Conflict("not_enough_questions", "This choice has fewer questions than the game needs")
	ErrNotYourGame  = apperr.Forbidden("This game belongs to another player")
	ErrGameOver     = apperr.Conflict("game_finished", "This game is already finished")
	ErrAnswered     = apperr.Conflict("already_answered", "That question is already answered")
	ErrLifelineUsed = apperr.Conflict("lifeline_used", "You already used that one")
)

type Service struct {
	db      *db.DB
	catalog *catalog.Service
	// hooks run inside the finish transaction (progress, challenges, duels).
	finishHooks []FinishHook
}

// FinishHook lets another module write inside the same transaction as the finish
// (XP, mastery, streaks, leaderboard, challenge closing) — one transaction per the
// architecture doc.
type FinishHook interface {
	OnGameFinished(ctx context.Context, q *sqlc.Queries, result FinishResult) error
}

func NewService(d *db.DB, cat *catalog.Service) *Service {
	return &Service{db: d, catalog: cat}
}

// AddFinishHook registers a module that must write when a game finishes.
func (s *Service) AddFinishHook(h FinishHook) { s.finishHooks = append(s.finishHooks, h) }

// Scope is what a game is played on: a domain, some categories, or all of the player's
// domains. The zero value means "all my domains" (DM-03).
type Scope struct {
	DomainID    int64   `json:"domain_id,omitempty"`
	CategoryIDs []int64 `json:"category_ids,omitempty"`
	AllMine     bool    `json:"all_mine,omitempty"`
	Kind        string  `json:"kind,omitempty"` // quick | custom | mix | daily
	Fifty       bool    `json:"fifty,omitempty"`
	Skip        bool    `json:"skip,omitempty"`

	// NodeID plays from the universal tree, at any depth (CUT-1). NodeScope says how
	// much of the branch counts, and is the choice §10.2 requires a challenge to make
	// explicitly rather than assume.
	NodeID    int64  `json:"node_id,omitempty"`
	NodeScope string `json:"node_scope,omitempty"` // direct | subtree | both
}

// Node scopes. "both" is the default because a branch chosen for a game almost always
// means the branch, but the choice is never implicit in the query.
const (
	ScopeDirect  = "direct"
	ScopeSubtree = "subtree"
	ScopeBoth    = "both"
)

// nodeScope keeps an unknown value from quietly selecting nothing.
func (s Scope) nodeScope() string {
	switch s.NodeScope {
	case ScopeDirect, ScopeSubtree, ScopeBoth:
		return s.NodeScope
	default:
		return ScopeBoth
	}
}

// StartInput is what the setup screen collects (GP-02).
type StartInput struct {
	Who        *authctx.Principal
	Mode       sqlc.GameMode
	Scope      Scope
	Difficulty int32 // 0 = mixed
	Count      int32
	Seconds    int32 // 0 = no timer
	Lang       string
}

// Game is the engine's view of a running game.
type Game struct {
	Row      sqlc.Game
	PlayerID uuid.UUID
	Scope    Scope
}

var allowedCounts = map[int32]bool{5: true, 10: true, 15: true, 20: true}

// 15 is not offered on the setup screen; it is the duel's own clock (DU-01).
var allowedSeconds = map[int32]bool{0: true, 10: true, 15: true, 20: true, 30: true}

// Start creates a game: it picks the questions, freezes their order, and adds the
// player. Starting in a domain that isn't one of yours adds it first (DM-04).
func (s *Service) Start(ctx context.Context, in StartInput) (Game, error) {
	if in.Who == nil {
		return Game{}, apperr.Unauthorized("Sign in or play as a guest first")
	}
	if in.Mode == "" {
		in.Mode = sqlc.GameModeSolo
	}
	if in.Scope.Kind == "quick" {
		in.Count = QuickGameQuestions // the server forces it (docs 03 §5)
	}
	if !allowedCounts[in.Count] {
		return Game{}, apperr.Invalid(map[string]string{"question_count": "5, 10, 15 or 20"})
	}
	if !allowedSeconds[in.Seconds] {
		return Game{}, apperr.Invalid(map[string]string{"seconds_per_question": "10, 20, 30 or none"})
	}
	if in.Difficulty < 0 || in.Difficulty > 3 {
		return Game{}, apperr.Invalid(map[string]string{"difficulty": "easy, medium, hard or mixed"})
	}

	// Playing in a domain that isn't yours adds it first (DM-04).
	if in.Scope.DomainID > 0 {
		if _, err := s.catalog.AddMyDomain(ctx, in.Who, in.Scope.DomainID, sqlc.UserDomainSourcePlayed); err != nil {
			return Game{}, err
		}
	}

	picked, err := s.pickDealt(ctx, in)
	if err != nil {
		return Game{}, err
	}
	if len(picked) == 0 {
		return Game{}, ErrNoQuestions
	}
	if len(picked) < int(in.Count) {
		return Game{}, ErrFewQuestions
	}

	scopeJSON, err := json.Marshal(in.Scope)
	if err != nil {
		return Game{}, err
	}
	var game sqlc.Game
	var playerID uuid.UUID
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		game, err = q.CreateGame(ctx, sqlc.CreateGameParams{
			Mode:               in.Mode,
			OwnerID:            uuid.NullUUID{UUID: in.Who.ID, Valid: true},
			Scope:              scopeJSON,
			Difficulty:         nullInt16(in.Difficulty),
			QuestionCount:      int16(len(picked)),
			SecondsPerQuestion: int16(in.Seconds),
		})
		if err != nil {
			return err
		}
		for i, round := range picked {
			if err := q.AddGameQuestion(ctx, sqlc.AddGameQuestionParams{
				GameID:     game.ID,
				Position:   int16(i),
				QuestionID: pgtype.Int8{Int64: round.QuestionID, Valid: round.QuestionID > 0},
				ItemID:     pgtype.Int8{Int64: round.ItemID, Valid: round.ItemID > 0},
			}); err != nil {
				return err
			}
		}
		player, err := q.AddGamePlayer(ctx, sqlc.AddGamePlayerParams{
			GameID: game.ID, UserID: uuid.NullUUID{UUID: in.Who.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		playerID = player.ID
		return nil
	})
	if err != nil {
		return Game{}, err
	}
	return Game{Row: game, PlayerID: playerID, Scope: in.Scope}, nil
}

// FixedInput starts a game on a question list that is already decided: a challenge
// replays the same questions for everyone (CH-01), a live room deals one set to the
// whole room (RM-03) and a device round reuses them turn after turn (RM-06).
type FixedInput struct {
	Who         *authctx.Principal
	Mode        sqlc.GameMode
	Scope       Scope
	Difficulty  int32
	Seconds     int32
	QuestionIDs []int64
	// SetQuestionIDs are questions a player wrote themselves (MQ-05); a round uses
	// one source, so exactly one of the two lists is filled.
	SetQuestionIDs []int64
	RoomID         uuid.UUID
	ChallengeID    uuid.UUID
	// Locals are the names of people sharing one phone; each becomes a player row
	// without a user id (RM-06). The signed-in player is always player one.
	Locals []string
}

// StartFixed is Start for a list of questions chosen elsewhere. It skips the bank
// query, so the same questions reach every player of a challenge or a room.
func (s *Service) StartFixed(ctx context.Context, in FixedInput) (Game, error) {
	if in.Who == nil {
		return Game{}, apperr.Unauthorized("Sign in or play as a guest first")
	}
	if len(in.QuestionIDs)+len(in.SetQuestionIDs) == 0 {
		return Game{}, ErrNoQuestions
	}
	if !allowedSeconds[in.Seconds] {
		return Game{}, apperr.Invalid(map[string]string{"seconds_per_question": "10, 20, 30 or none"})
	}
	if in.Mode == "" {
		in.Mode = sqlc.GameModeChallenge
	}
	scopeJSON, err := json.Marshal(in.Scope)
	if err != nil {
		return Game{}, err
	}
	var game sqlc.Game
	var playerID uuid.UUID
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		game, err = q.CreateGame(ctx, sqlc.CreateGameParams{
			Mode:               in.Mode,
			OwnerID:            uuid.NullUUID{UUID: in.Who.ID, Valid: true},
			Scope:              scopeJSON,
			Difficulty:         nullInt16(in.Difficulty),
			QuestionCount:      int16(len(in.QuestionIDs) + len(in.SetQuestionIDs)),
			SecondsPerQuestion: int16(in.Seconds),
			RoomID:             uuid.NullUUID{UUID: in.RoomID, Valid: in.RoomID != uuid.Nil},
			ChallengeID:        uuid.NullUUID{UUID: in.ChallengeID, Valid: in.ChallengeID != uuid.Nil},
		})
		if err != nil {
			return err
		}
		for i, id := range in.QuestionIDs {
			if err := q.AddGameQuestion(ctx, sqlc.AddGameQuestionParams{
				GameID: game.ID, Position: int16(i), QuestionID: pgtype.Int8{Int64: id, Valid: true},
			}); err != nil {
				return err
			}
		}
		for i, id := range in.SetQuestionIDs {
			if err := q.AddGameQuestion(ctx, sqlc.AddGameQuestionParams{
				GameID: game.ID, Position: int16(i), SetQuestionID: pgtype.Int8{Int64: id, Valid: true},
			}); err != nil {
				return err
			}
		}
		player, err := q.AddGamePlayer(ctx, sqlc.AddGamePlayerParams{
			GameID: game.ID, UserID: uuid.NullUUID{UUID: in.Who.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		playerID = player.ID
		for _, name := range in.Locals {
			if _, err := q.AddGamePlayer(ctx, sqlc.AddGamePlayerParams{
				GameID: game.ID, LocalName: pgtype.Text{String: name, Valid: true},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Game{}, err
	}
	return Game{Row: game, PlayerID: playerID, Scope: in.Scope}, nil
}

// PickIDs chooses a question list without creating a game, so a challenge or a room
// can freeze the same questions for everybody before anyone plays.
func (s *Service) PickIDs(ctx context.Context, in StartInput) ([]int64, error) {
	if !allowedCounts[in.Count] {
		return nil, apperr.Invalid(map[string]string{"question_count": "5, 10, 15 or 20"})
	}
	rows, err := s.pick(ctx, in)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNoQuestions
	}
	if len(rows) < int(in.Count) {
		return nil, ErrFewQuestions
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// Dealt is one round the engine has chosen. Exactly one id is set: a v1 question, or
// a content item. Migration 28 pairs them in the database, so a mirrored item fills
// both columns and everything downstream keeps working unchanged.
type Dealt struct {
	QuestionID int64
	ItemID     int64
}

// pickDealt chooses what a game will ask.
//
// A game scoped to a node reads the universal model (CUT-4b): that is the only way a
// flashcard-era item — a true/false, a typed answer — can ever be dealt, since those
// have no row in the v1 bank at all. Every other scope keeps the v1 path, so nothing
// about an existing game changes.
func (s *Service) pickDealt(ctx context.Context, in StartInput) ([]Dealt, error) {
	if in.Scope.NodeID > 0 {
		rows, err := s.db.Q.PickContent(ctx, sqlc.PickContentParams{
			NodeID: in.Scope.NodeID, NodeScope: in.Scope.nodeScope(),
			Difficulty: int16(in.Difficulty), Exclude: []int64{},
			Viewer: uuid.NullUUID{UUID: in.Who.ID, Valid: in.Who.ID != uuid.Nil},
			Lim:    int32(in.Count),
		})
		if err != nil {
			return nil, err
		}
		out := make([]Dealt, 0, len(rows))
		for _, r := range rows {
			out = append(out, Dealt{ItemID: r.ID})
		}
		return out, nil
	}
	rows, err := s.pick(ctx, in)
	if err != nil {
		return nil, err
	}
	out := make([]Dealt, 0, len(rows))
	for _, r := range rows {
		out = append(out, Dealt{QuestionID: r.ID})
	}
	return out, nil
}

// pick chooses the questions of a game inside the scope, respecting Your domains and
// Islamic-only. A mixed game spreads the difficulties as evenly as the bank allows.
func (s *Service) pick(ctx context.Context, in StartInput) ([]sqlc.PickQuestionsRow, error) {
	params := sqlc.PickQuestionsParams{
		IslamicOnly: in.Who.IslamicOnly,
		DomainID:    in.Scope.DomainID,
		CategoryIds: in.Scope.CategoryIDs,
		Node:        pgtype.Int8{Int64: in.Scope.NodeID, Valid: in.Scope.NodeID > 0},
		NodeScope:   in.Scope.nodeScope(),
		// A game played from a node is already scoped by that branch; narrowing it
		// again to the player's own domains would hide the branch they asked for.
		MineOnly:   in.Scope.NodeID == 0 && in.Scope.DomainID == 0 && len(in.Scope.CategoryIDs) == 0,
		UserID:     in.Who.ID,
		Difficulty: int16(in.Difficulty),
		Exclude:    []int64{},
		Lim:        int32(in.Count),
	}
	if params.CategoryIds == nil {
		params.CategoryIds = []int64{}
	}
	rows, err := s.db.Q.PickQuestions(ctx, params)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Available is how many questions a scope has; the setup screen uses it to say
// "only N exist, the game will use those" (GP-02).
func (s *Service) Available(ctx context.Context, who *authctx.Principal, scope Scope, difficulty int32) (int, error) {
	if who == nil {
		return 0, apperr.Unauthorized("Sign in or play as a guest first")
	}
	categoryIDs := scope.CategoryIDs
	if categoryIDs == nil {
		categoryIDs = []int64{}
	}
	n, err := s.db.Q.CountAvailableQuestions(ctx, sqlc.CountAvailableQuestionsParams{
		IslamicOnly: who.IslamicOnly,
		DomainID:    scope.DomainID,
		CategoryIds: categoryIDs,
		Node:        pgtype.Int8{Int64: scope.NodeID, Valid: scope.NodeID > 0},
		NodeScope:   scope.nodeScope(),
		// The same rule as the pick: a branch is its own scope.
		MineOnly:   scope.NodeID == 0 && scope.DomainID == 0 && len(categoryIDs) == 0,
		UserID:     who.ID,
		Difficulty: int16(difficulty),
	})
	return int(n), err
}

// LoadAs is Load for a pass-the-device round: the people sharing one phone have no
// account of their own, so the caller says which player row is taking this turn. The
// signed-in owner of the game is the only one who can ask (RM-06).
func (s *Service) LoadAs(ctx context.Context, who *authctx.Principal, gameID, playerID uuid.UUID) (Game, error) {
	row, err := s.db.Q.GetGame(ctx, gameID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Game{}, apperr.NotFound("game")
	}
	if err != nil {
		return Game{}, err
	}
	if row.Mode != sqlc.GameModeDevice || !row.OwnerID.Valid || row.OwnerID.UUID != who.ID {
		return Game{}, ErrNotYourGame
	}
	players, err := s.db.Q.GamePlayersInOrder(ctx, gameID)
	if err != nil {
		return Game{}, err
	}
	for _, p := range players {
		if p.ID == playerID {
			var scope Scope
			_ = json.Unmarshal(row.Scope, &scope)
			return Game{Row: row, PlayerID: p.ID, Scope: scope}, nil
		}
	}
	return Game{}, ErrNotYourGame
}

// Players lists a game's players in turn order, with their scores.
func (s *Service) Players(ctx context.Context, gameID uuid.UUID) ([]sqlc.GamePlayer, error) {
	return s.db.Q.GamePlayersInOrder(ctx, gameID)
}

// ResetClock clears the served stamps so the next turn of a device round is timed
// from scratch (RM-06, NF-05).
func (s *Service) ResetClock(ctx context.Context, gameID uuid.UUID) error {
	return s.db.Q.ResetServedQuestions(ctx, gameID)
}

// Load returns a game the player takes part in, with their player row.
func (s *Service) Load(ctx context.Context, who *authctx.Principal, gameID uuid.UUID) (Game, error) {
	row, err := s.db.Q.GetGame(ctx, gameID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Game{}, apperr.NotFound("game")
	}
	if err != nil {
		return Game{}, err
	}
	player, err := s.db.Q.GetGamePlayer(ctx, sqlc.GetGamePlayerParams{
		GameID: gameID, UserID: uuid.NullUUID{UUID: who.ID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Game{}, ErrNotYourGame
	}
	if err != nil {
		return Game{}, err
	}
	var scope Scope
	_ = json.Unmarshal(row.Scope, &scope)
	return Game{Row: row, PlayerID: player.ID, Scope: scope}, nil
}

// Question is one question of a game as the screens need it.
type Question struct {
	Position      int32
	ID            int64
	CategoryID    int64
	CategoryName  string
	CategoryIcon  string
	CategoryColor string
	DomainID      int64
	DomainSlug    string
	DomainName    string
	DomainColor   string
	DomainIcon    string
	DomainMotif   string
	DomainFamily  string
	Difficulty    int32
	Points        int32
	CorrectIndex  int32
	Source        string
	Body          string
	Answers       []string
	Explanation   string
	InLanguage    bool
}

// Serve marks a question as handed to the player and returns the moment it was. The
// first call wins, so reloading the page cannot restart the clock (NF-05).
func (s *Service) Serve(ctx context.Context, gameID uuid.UUID, position int32) (time.Time, error) {
	stamp, err := s.db.Q.ServeQuestion(ctx, sqlc.ServeQuestionParams{GameID: gameID, Position: int16(position)})
	if errors.Is(err, pgx.ErrNoRows) {
		// Already served: keep the first stamp.
		stamp, err = s.db.Q.QuestionServedAt(ctx, sqlc.QuestionServedAtParams{GameID: gameID, Position: int16(position)})
	}
	if err != nil {
		return time.Time{}, err
	}
	if !stamp.Valid {
		return time.Time{}, nil
	}
	return stamp.Time, nil
}

// Given is an answer the player already gave: what it was worth and what they picked.
// A screen that reloads — a language change on P10 — shows this again instead of
// jumping to the next question (GP-03, LG-01).
type Given struct {
	Choice   *int32
	Correct  bool
	Points   int32
	TimedOut bool
}

// AnswerFor reads the answer already given at a position, if there is one.
func (s *Service) AnswerFor(ctx context.Context, g Game, position int32) (Given, bool) {
	row, err := s.db.Q.AnswerAt(ctx, sqlc.AnswerAtParams{
		GameID: g.Row.ID, PlayerID: g.PlayerID, Position: int16(position),
	})
	if err != nil {
		return Given{}, false
	}
	out := Given{Correct: row.IsCorrect, Points: row.Points}
	if row.Choice.Valid {
		choice := int32(row.Choice.Int16)
		out.Choice = &choice
	} else if !(row.Lifeline.Valid && row.Lifeline.String == "skip") {
		out.TimedOut = true
	}
	return out, true
}

// QuestionAt loads the question at a position, in the player's language (LG-05 falls
// back to the question's main language).
func (s *Service) QuestionAt(ctx context.Context, gameID uuid.UUID, position int32, lang string) (Question, error) {
	row, err := s.db.Q.GameQuestionAt(ctx, sqlc.GameQuestionAtParams{
		Lang: lang, GameID: gameID, Position: int16(position),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, apperr.NotFound("question")
	}
	if err != nil {
		return Question{}, err
	}
	return Question{
		Position: int32(row.Position), ID: row.ID, CategoryID: row.CategoryID,
		CategoryName: row.CategoryName, CategoryIcon: row.CategoryIcon, CategoryColor: row.CategoryColor,
		DomainID: row.DomainID, DomainSlug: row.DomainSlug, DomainName: row.DomainName,
		DomainColor: row.DomainColor, DomainIcon: row.DomainIcon, DomainMotif: row.DomainMotif,
		DomainFamily: string(row.DomainFamily), Difficulty: int32(row.Difficulty), Points: row.Points,
		CorrectIndex: int32(row.CorrectIndex), Source: row.Source, Body: row.Body,
		Answers: row.Answers, Explanation: row.Explanation, InLanguage: row.InLanguage,
	}, nil
}

// State is where a player is in a game.
type State struct {
	Game      sqlc.Game
	PlayerID  uuid.UUID
	Scope     Scope
	Position  int32 // the question to answer next
	Answered  int32
	Score     int32
	Correct   int32
	Results   []bool // correct or not, in order, for the progress dots
	FiftyUsed bool
	SkipUsed  bool
	Done      bool
}

// State reads how far the player is, from the answers themselves (the server is the
// only source of truth, NF-05).
func (s *Service) State(ctx context.Context, g Game) (State, error) {
	answers, err := s.db.Q.PlayerAnswers(ctx, sqlc.PlayerAnswersParams{GameID: g.Row.ID, PlayerID: g.PlayerID})
	if err != nil {
		return State{}, err
	}
	st := State{Game: g.Row, PlayerID: g.PlayerID, Scope: g.Scope, FiftyUsed: g.Scope.Fifty, SkipUsed: g.Scope.Skip}
	for _, a := range answers {
		st.Answered++
		st.Score += a.Points
		if a.IsCorrect {
			st.Correct++
		}
		st.Results = append(st.Results, a.IsCorrect)
		if a.Lifeline.Valid && a.Lifeline.String == "skip" {
			st.SkipUsed = true
		}
	}
	st.Position = st.Answered
	st.Done = st.Answered >= int32(g.Row.QuestionCount) || g.Row.FinishedAt.Valid
	return st, nil
}

// AnswerInput is one submitted answer.
type AnswerInput struct {
	Position int32
	Choice   *int32 // nil = time's up
	TimeMS   int32
}

// Answer saves an answer and says what happened. The server decides whether it was
// right and how many points it is worth; the client's numbers are never trusted.
type Answer struct {
	Question   Question
	Correct    bool
	Points     int32
	Choice     *int32
	TimedOut   bool
	TotalScore int32
	Position   int32
	Last       bool
}

func (s *Service) Answer(ctx context.Context, g Game, in AnswerInput, lang string) (Answer, error) {
	if g.Row.FinishedAt.Valid {
		return Answer{}, ErrGameOver
	}
	if in.Position < 0 || in.Position >= int32(g.Row.QuestionCount) {
		return Answer{}, apperr.Invalid(map[string]string{"position": "outside the game"})
	}
	if in.Choice != nil && (*in.Choice < 0 || *in.Choice > 3) {
		return Answer{}, apperr.Invalid(map[string]string{"choice": "0 to 3"})
	}
	if _, err := s.db.Q.AnswerAt(ctx, sqlc.AnswerAtParams{
		GameID: g.Row.ID, PlayerID: g.PlayerID, Position: int16(in.Position),
	}); err == nil {
		return Answer{}, ErrAnswered
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Answer{}, err
	}

	question, err := s.QuestionAt(ctx, g.Row.ID, in.Position, lang)
	if err != nil {
		return Answer{}, err
	}

	// How long the answer took is measured from the moment the server handed the
	// question over; the client's number is only used when that stamp is missing
	// (an old game started before this was recorded). Nothing the client sends can
	// buy a bigger speed bonus (NF-05, GP-04).
	timeMS := in.TimeMS
	if timeMS < 0 {
		timeMS = 0
	}
	if servedAt, err := s.Serve(ctx, g.Row.ID, in.Position); err != nil {
		return Answer{}, err
	} else if !servedAt.IsZero() {
		timeMS = int32(time.Since(servedAt).Milliseconds())
		if timeMS < 0 {
			timeMS = 0
		}
	}
	limit := int32(g.Row.SecondsPerQuestion) * 1000
	timedOut := in.Choice == nil
	if limit > 0 && timeMS > limit+2000 { // 2 s of grace for the round trip
		timedOut = true
	}

	out := Answer{Question: question, Position: in.Position, Choice: in.Choice, TimedOut: timedOut}
	if !timedOut && int32(*in.Choice) == question.CorrectIndex {
		out.Correct = true
		out.Points = Points(question.Difficulty, timeMS, int32(g.Row.SecondsPerQuestion))
	}

	var choice pgtype.Int2
	if !timedOut {
		choice = pgtype.Int2{Int16: int16(*in.Choice), Valid: true}
	}
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.SaveAnswer(ctx, sqlc.SaveAnswerParams{
			GameID: g.Row.ID, PlayerID: g.PlayerID, Position: int16(in.Position),
			Choice: choice, IsCorrect: out.Correct, TimeMs: timeMS, Points: out.Points,
		}); err != nil {
			return err
		}
		answers, err := q.PlayerAnswers(ctx, sqlc.PlayerAnswersParams{GameID: g.Row.ID, PlayerID: g.PlayerID})
		if err != nil {
			return err
		}
		var score, correct int32
		for _, a := range answers {
			score += a.Points
			if a.IsCorrect {
				correct++
			}
		}
		out.TotalScore = score
		out.Last = int32(len(answers)) >= int32(g.Row.QuestionCount)
		return q.UpdatePlayerScore(ctx, sqlc.UpdatePlayerScoreParams{
			ID: g.PlayerID, Score: score, Correct: int16(correct),
		})
	})
	if err != nil {
		return Answer{}, err
	}
	return out, nil
}

// Fifty removes two wrong answers, once per game, solo only (GP-03).
func (s *Service) Fifty(ctx context.Context, g Game, position int32, lang string) ([]int32, error) {
	if g.Scope.Fifty {
		return nil, ErrLifelineUsed
	}
	question, err := s.QuestionAt(ctx, g.Row.ID, position, lang)
	if err != nil {
		return nil, err
	}
	keep := []int32{question.CorrectIndex}
	for i := range question.Answers {
		if int32(i) != question.CorrectIndex {
			keep = append(keep, int32(i))
			break
		}
	}
	if err := s.db.Q.SetLifelineUsed(ctx, sqlc.SetLifelineUsedParams{Path: []string{"fifty"}, ID: g.Row.ID}); err != nil {
		return nil, err
	}
	return keep, nil
}

// Skip passes a question without answering it, once per game, solo only (GP-03).
func (s *Service) Skip(ctx context.Context, g Game, position int32) error {
	if g.Scope.Skip {
		return ErrLifelineUsed
	}
	if err := s.db.Q.SaveAnswer(ctx, sqlc.SaveAnswerParams{
		GameID: g.Row.ID, PlayerID: g.PlayerID, Position: int16(position),
		IsCorrect: false, TimeMs: 0, Points: 0,
		Lifeline: pgtype.Text{String: "skip", Valid: true},
	}); err != nil {
		return err
	}
	return s.db.Q.SetLifelineUsed(ctx, sqlc.SetLifelineUsedParams{Path: []string{"skip"}, ID: g.Row.ID})
}

// FinishResult is what the finish transaction hands to the other modules.
type FinishResult struct {
	Game     sqlc.Game
	PlayerID uuid.UUID
	UserID   uuid.UUID
	Score    int32
	Correct  int32
	Total    int32
	XP       int32
	Answers  []sqlc.GameAnswer
}

// Finish closes a game and writes players, XP and (through the hooks) stats,
// activity, leaderboard and achievements in one transaction.
func (s *Service) Finish(ctx context.Context, g Game, abandoned bool) (FinishResult, error) {
	var result FinishResult
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		answers, err := q.PlayerAnswers(ctx, sqlc.PlayerAnswersParams{GameID: g.Row.ID, PlayerID: g.PlayerID})
		if err != nil {
			return err
		}
		var score, correct int32
		for _, a := range answers {
			score += a.Points
			if a.IsCorrect {
				correct++
			}
		}
		xp := XPForGame(score, correct, int32(g.Row.QuestionCount))
		if abandoned {
			xp = XPForGame(score, correct, 0)
		}
		if err := q.FinishGamePlayer(ctx, sqlc.FinishGamePlayerParams{
			ID: g.PlayerID, Score: score, Correct: int16(correct),
			Rank: pgtype.Int2{Int16: 1, Valid: g.Row.Mode == sqlc.GameModeSolo}, XpGained: xp,
		}); err != nil {
			return err
		}
		// One game can hold several players: a live room deals one set of questions
		// to everybody (RM-03) and a pass-the-device round gives each name its own
		// turn (RM-06). The game itself only closes once nobody is still playing,
		// otherwise the players after the first are sent to the results screen
		// before they have answered anything.
		left, err := q.PlayersStillPlaying(ctx, g.Row.ID)
		if err != nil {
			return err
		}
		if left == 0 {
			status := sqlc.GameStatusFinished
			if abandoned {
				status = sqlc.GameStatusAbandoned
			}
			if err := q.FinishGame(ctx, sqlc.FinishGameParams{Status: status, ID: g.Row.ID}); err != nil {
				return err
			}
		}
		result = FinishResult{
			Game: g.Row, PlayerID: g.PlayerID, UserID: ownerOf(g), Score: score,
			Correct: correct, Total: int32(g.Row.QuestionCount), XP: xp, Answers: answers,
		}
		for _, hook := range s.finishHooks {
			if err := hook.OnGameFinished(ctx, q, result); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

// Summary is the results screen (RS-01).
type Summary struct {
	Game      sqlc.Game
	Score     int32
	Correct   int32
	Total     int32
	XP        int32
	Accuracy  int32
	AverageMS int32
	ByDomain  []DomainResult
	Review    []ReviewItem
}

type DomainResult struct {
	DomainID   int64
	DomainName string
	Color      string
	Answered   int32
	Correct    int32
}

// ReviewItem is one line of the answer review (RS-03).
type ReviewItem struct {
	Question Question
	Choice   *int32
	Correct  bool
	Points   int32
	TimeMS   int32
	Skipped  bool
}

func (s *Service) Results(ctx context.Context, g Game, lang string) (Summary, error) {
	answers, err := s.db.Q.PlayerAnswers(ctx, sqlc.PlayerAnswersParams{GameID: g.Row.ID, PlayerID: g.PlayerID})
	if err != nil {
		return Summary{}, err
	}
	questions, err := s.db.Q.GameQuestions(ctx, sqlc.GameQuestionsParams{Lang: lang, GameID: g.Row.ID})
	if err != nil {
		return Summary{}, err
	}
	byPosition := make(map[int16]sqlc.GameAnswer, len(answers))
	var totalMS int32
	out := Summary{Game: g.Row, Total: int32(g.Row.QuestionCount)}
	for _, a := range answers {
		byPosition[a.Position] = a
		out.Score += a.Points
		totalMS += a.TimeMs
		if a.IsCorrect {
			out.Correct++
		}
	}
	if len(answers) > 0 {
		out.AverageMS = totalMS / int32(len(answers))
		out.Accuracy = out.Correct * 100 / int32(len(answers))
	}
	out.XP = XPForGame(out.Score, out.Correct, int32(g.Row.QuestionCount))

	for _, row := range questions {
		item := ReviewItem{Question: Question{
			Position: int32(row.Position), ID: row.ID, CategoryID: row.CategoryID,
			CategoryName: row.CategoryName, CategoryIcon: row.CategoryIcon, CategoryColor: row.CategoryColor,
			DomainID: row.DomainID, DomainSlug: row.DomainSlug, DomainName: row.DomainName,
			DomainColor: row.DomainColor, DomainIcon: row.DomainIcon, DomainFamily: string(row.DomainFamily),
			Difficulty: int32(row.Difficulty), Points: row.Points, CorrectIndex: int32(row.CorrectIndex),
			Source: row.Source, Body: row.Body, Answers: row.Answers, Explanation: row.Explanation,
		}}
		if a, ok := byPosition[row.Position]; ok {
			item.Correct = a.IsCorrect
			item.Points = a.Points
			item.TimeMS = a.TimeMs
			item.Skipped = a.Lifeline.Valid && a.Lifeline.String == "skip"
			if a.Choice.Valid {
				choice := int32(a.Choice.Int16)
				item.Choice = &choice
			}
		}
		out.Review = append(out.Review, item)
	}

	rows, err := s.db.Q.ResultsByDomain(ctx, sqlc.ResultsByDomainParams{
		Lang: lang, GameID: g.Row.ID, PlayerID: g.PlayerID,
	})
	if err != nil {
		return Summary{}, err
	}
	if len(rows) > 1 { // RS-02: only mixed games show the breakdown
		for _, r := range rows {
			out.ByDomain = append(out.ByDomain, DomainResult{
				DomainID: r.DomainID, DomainName: r.DomainName, Color: r.Color,
				Answered: r.Answered, Correct: r.Correct,
			})
		}
	}
	return out, nil
}

// History is endpoint 31 (LB-03).
type HistoryRow struct {
	GameID      uuid.UUID
	Mode        string
	Questions   int32
	Score       int32
	Correct     int32
	FinishedAt  time.Time
	Scope       Scope
	DomainName  string
	DomainColor string
	DomainIcon  string
}

// HistoryStats are the four tiles above the list.
type HistoryStats struct {
	Games     int32
	ThisWeek  int32
	BestScore int32
	Accuracy  int32
}

// ValidMode reports whether a history filter is one of the five game modes.
func ValidMode(mode string) bool {
	switch mode {
	case "", "solo", "live", "duel", "challenge", "device":
		return true
	}
	return false
}

func (s *Service) History(ctx context.Context, who *authctx.Principal, lang, mode string, page, perPage int) ([]HistoryRow, int, error) {
	if !ValidMode(mode) {
		return nil, 0, apperr.Invalid(map[string]string{"mode": "solo, live, duel, challenge or device"})
	}
	uid := uuid.NullUUID{UUID: who.ID, Valid: true}
	rows, err := s.db.Q.MyGames(ctx, sqlc.MyGamesParams{
		UserID: uid, Lang: lang, Mode: mode,
		Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountMyGames(ctx, sqlc.CountMyGamesParams{UserID: uid, Mode: mode})
	if err != nil {
		return nil, 0, err
	}
	out := make([]HistoryRow, 0, len(rows))
	for _, r := range rows {
		item := HistoryRow{
			GameID: r.ID, Mode: string(r.Mode), Questions: int32(r.QuestionCount),
			Score: r.Score, Correct: int32(r.Correct),
		}
		if r.FinishedAt.Valid {
			item.FinishedAt = r.FinishedAt.Time
		}
		item.DomainName, item.DomainColor, item.DomainIcon = r.DomainName, r.DomainColor, r.DomainIcon
		_ = json.Unmarshal(r.Scope, &item.Scope)
		out = append(out, item)
	}
	return out, int(total), nil
}

// HistoryStats fills the tiles above the history list.
func (s *Service) HistoryStats(ctx context.Context, who *authctx.Principal) (HistoryStats, error) {
	row, err := s.db.Q.MyGameStats(ctx, uuid.NullUUID{UUID: who.ID, Valid: true})
	if err != nil {
		return HistoryStats{}, err
	}
	out := HistoryStats{Games: row.Games, ThisWeek: row.ThisWeek, BestScore: row.BestScore}
	if row.Answered > 0 {
		out.Accuracy = row.Correct * 100 / row.Answered
	}
	return out, nil
}

func ownerOf(g Game) uuid.UUID {
	if g.Row.OwnerID.Valid {
		return g.Row.OwnerID.UUID
	}
	return uuid.Nil
}

func nullInt16(v int32) pgtype.Int2 {
	if v <= 0 {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: int16(v), Valid: true}
}

// DifficultyName is used in logs and the API payloads.
func DifficultyName(d int32) string {
	switch d {
	case 1:
		return "easy"
	case 2:
		return "medium"
	case 3:
		return "hard"
	default:
		return "mixed"
	}
}

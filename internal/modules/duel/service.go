// Package duel is the one-on-one match (DU-01…DU-04): two players of a similar
// rating answer the same questions at the same time, and the winner takes rating.
// Guests duel guests only, and a guest duel is never rated (RL-03).
package duel

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
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

type Service struct {
	db    *db.DB
	games *games.Service
	// live announces a pairing to both players, so the searching screen stops
	// waiting for its next poll and asks at once (RT-1).
	live *live.Hub
}

func NewService(d *db.DB, g *games.Service, h *live.Hub) *Service {
	return &Service{db: d, games: g, live: h}
}

// Questions and timer of a duel are fixed: both players must face the same race
// (DU-01: five questions of fifteen seconds).
const (
	Questions = 5
	Seconds   = 15
)

// QuestionsAvailable is how many questions a domain holds for this player: P17 keeps
// "Find a match" locked while a duel's ten would not fit (DU-02).
func (s *Service) QuestionsAvailable(ctx context.Context, who *authctx.Principal, domainID int64) (int, error) {
	return s.games.Available(ctx, who, games.Scope{Kind: "duel", DomainID: domainID}, 0)
}

// State is what P17 shows: idle, searching, or matched and ready to play.
type State struct {
	Searching bool
	GameID    uuid.UUID
	Rating    int32
	Wins      int32
	Losses    int32
}

func (s *Service) State(ctx context.Context, who *authctx.Principal) (State, error) {
	out := State{Rating: 1200}
	if who == nil {
		return out, nil
	}
	if row, err := s.db.Q.MyBestRating(ctx, who.ID); err == nil {
		out.Rating, out.Wins, out.Losses = row.Rating, row.Wins, row.Losses
	}
	row, err := s.db.Q.MyDuelQueue(ctx, who.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if row.GameID.Valid {
		out.GameID = row.GameID.UUID
		return out, nil
	}
	out.Searching = true
	return out, nil
}

// Search puts the player in the queue and matches them with whoever has waited
// longest at a similar rating. The whole pairing is one transaction, so two players
// can never be handed different games (DU-02).
func (s *Service) Search(ctx context.Context, who *authctx.Principal, domainID int64) (State, error) {
	if who == nil {
		return State{}, apperr.Unauthorized("Sign in or play as a guest first")
	}
	state, err := s.State(ctx, who)
	if err != nil {
		return State{}, err
	}
	if state.GameID != uuid.Nil {
		return state, nil
	}

	// A duel is always 10 questions on one domain: if the bank holds fewer, say so
	// before the player joins the queue rather than after a pairing (DU-02, GP-02).
	scope := games.Scope{Kind: "duel", DomainID: domainID}
	available, err := s.games.Available(ctx, who, scope, 0)
	if err != nil {
		return State{}, err
	}
	if available < Questions {
		return State{}, games.ErrFewQuestions
	}

	var opponent sqlc.DuelQueue
	var found bool
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.EnterDuelQueue(ctx, sqlc.EnterDuelQueueParams{
			UserID: who.ID, DomainID: nullInt64(domainID),
			IsGuest: who.IsGuest(), Rating: state.Rating,
		}); err != nil {
			return err
		}
		row, err := q.FindOpponent(ctx, sqlc.FindOpponentParams{
			UserID: who.ID, IsGuest: who.IsGuest(), Rating: state.Rating,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		opponent, found = row, true
		return nil
	})
	if err != nil || !found {
		state.Searching = true
		return state, err
	}

	// Both players get one game with one player row each, on the same questions.
	ids, err := s.games.PickIDs(ctx, games.StartInput{
		Who: who, Scope: scope, Count: Questions, Seconds: Seconds,
	})
	if err != nil {
		return State{}, err
	}
	game, err := s.games.StartFixed(ctx, games.FixedInput{
		Who: who, Mode: sqlc.GameModeDuel, Scope: scope,
		Seconds: Seconds, QuestionIDs: ids,
	})
	if err != nil {
		return State{}, err
	}
	if _, err := s.db.Q.AddGamePlayer(ctx, sqlc.AddGamePlayerParams{
		GameID: game.Row.ID, UserID: uuid.NullUUID{UUID: opponent.UserID, Valid: true},
	}); err != nil {
		return State{}, err
	}
	if err := s.db.Q.MatchDuel(ctx, sqlc.MatchDuelParams{
		GameID:  uuid.NullUUID{UUID: game.Row.ID, Valid: true},
		UserIds: []uuid.UUID{who.ID, opponent.UserID},
	}); err != nil {
		return State{}, err
	}
	state.Searching, state.GameID = false, game.Row.ID
	// Both players are told at once. The one who was waiting hears it on their open
	// stream instead of discovering it up to three seconds later.
	s.live.SendAll([]uuid.UUID{who.ID, opponent.UserID}, live.Duels)
	return state, nil
}

// Cancel takes the player out of the queue (DU-01).
func (s *Service) Cancel(ctx context.Context, who *authctx.Principal) error {
	return s.db.Q.LeaveDuelQueue(ctx, who.ID)
}

// Opponent is the other side of a duel, for the score bar of P18.
type Opponent struct {
	Name   string
	UserID uuid.UUID
	Score  int32
	Done   bool
	Found  bool
	Level  int32
	Rating int32
	Wins   int32
	Losses int32
	// Answered is whether they have already given the question on screen an answer,
	// which is what the bar's "thinking" line reads (DU-01).
	Answered bool
	// Results is their answer-by-answer run, so the bar shows both statuses and not
	// only both scores (DU-01).
	Results []bool
}

func (s *Service) Opponent(ctx context.Context, who *authctx.Principal, gameID uuid.UUID) (Opponent, error) {
	players, err := s.games.Players(ctx, gameID)
	if err != nil {
		return Opponent{}, err
	}
	for _, p := range players {
		if !p.UserID.Valid || p.UserID.UUID == who.ID {
			continue
		}
		out := Opponent{UserID: p.UserID.UUID, Score: p.Score, Done: p.FinishedAt.Valid, Found: true}
		if u, err := s.db.Q.GetUser(ctx, p.UserID.UUID); err == nil {
			out.Name = u.DisplayName
			out.Level = u.Level
		}
		if row, err := s.db.Q.MyBestRating(ctx, p.UserID.UUID); err == nil {
			out.Rating, out.Wins, out.Losses = row.Rating, row.Wins, row.Losses
		}
		if answers, err := s.db.Q.PlayerAnswers(ctx, sqlc.PlayerAnswersParams{GameID: gameID, PlayerID: p.ID}); err == nil {
			for _, a := range answers {
				out.Results = append(out.Results, a.IsCorrect)
			}
			// They are up to date with this player when they have answered as many
			// questions, which is all the bar needs to say "thinking" honestly.
			out.Answered = len(answers) > 0
		}
		return out, nil
	}
	return Opponent{}, nil
}

// OnGameFinished settles the rating once both sides have played (DU-04). A guest duel
// is never rated, as the welcome screen promises (RL-03).
// OnGameFinished settles the ratings, and tells both players so the score bar and the
// result stop waiting on a timer.
func (s *Service) OnGameFinished(ctx context.Context, q *sqlc.Queries, r games.FinishResult) error {
	if r.Game.Mode != sqlc.GameModeDuel {
		return nil
	}
	players, err := q.GamePlayersInOrder(ctx, r.Game.ID)
	if err != nil {
		return err
	}
	var me, other sqlc.GamePlayer
	for _, p := range players {
		switch {
		case p.UserID.Valid && p.UserID.UUID == r.UserID:
			me = p
		case p.UserID.Valid:
			other = p
		}
	}
	if !other.UserID.Valid || !other.FinishedAt.Valid {
		return nil // the opponent is still playing; the rating waits for them
	}
	user, err := q.GetUser(ctx, r.UserID)
	if err != nil {
		return err
	}
	if user.Kind == sqlc.UserKindGuest {
		return nil
	}
	domainID := duelDomain(r.Game)
	if domainID == 0 {
		return nil
	}
	delta, wins, losses, draws := settle(me.Score, other.Score)
	if err := q.BumpDuelRating(ctx, sqlc.BumpDuelRatingParams{
		UserID: r.UserID, DomainID: domainID,
		Rating: 1200 + delta, Wins: wins, Losses: losses, Draws: draws,
	}); err != nil {
		return err
	}
	// The opponent's screen is sitting on the score bar: tell it the duel is settled
	// rather than leaving it to notice on its next poll.
	s.live.SendAll([]uuid.UUID{r.UserID, other.UserID.UUID}, live.Duels)
	return nil
}

// settle is the rating move of one duel: +25 for a win, −12 for a loss, +5 for a draw
// (the numbers the design shows on the results screen).
func settle(mine, theirs int32) (delta, wins, losses, draws int32) {
	switch {
	case mine > theirs:
		return 25, 1, 0, 0
	case mine < theirs:
		return -12, 0, 1, 0
	}
	return 5, 0, 0, 1
}

func duelDomain(game sqlc.Game) int64 {
	var scope games.Scope
	if err := json.Unmarshal(game.Scope, &scope); err != nil {
		return 0
	}
	return scope.DomainID
}

func nullInt64(v int64) pgtype.Int8 {
	if v <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: v, Valid: true}
}

// Record is what P17b puts under the two faces: how these two stand against each
// other, and the duel they played last (DU-01).
type Record struct {
	Wins     int32
	Losses   int32
	Draws    int32
	HasLast  bool
	LastMine int32
	LastThem int32
	LastWhen time.Time
	LastDomn int64
}

// Won says whether the last duel went this player's way.
func (r Record) Won() bool { return r.LastMine > r.LastThem }

// Record reads both numbers in one go. A player with no history gets zeroes, which is
// what the empty state of the card shows.
func (s *Service) Record(ctx context.Context, who *authctx.Principal, other uuid.UUID) (Record, error) {
	var out Record
	if who == nil || other == uuid.Nil {
		return out, nil
	}
	if row, err := s.db.Q.DuelHeadToHead(ctx, sqlc.DuelHeadToHeadParams{
		UserID:  uuid.NullUUID{UUID: who.ID, Valid: true},
		OtherID: uuid.NullUUID{UUID: other, Valid: true},
	}); err == nil {
		out.Wins, out.Losses, out.Draws = row.Wins, row.Losses, row.Draws
	}
	row, err := s.db.Q.LastDuelAgainst(ctx, sqlc.LastDuelAgainstParams{
		UserID:  uuid.NullUUID{UUID: who.ID, Valid: true},
		OtherID: uuid.NullUUID{UUID: other, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.HasLast, out.LastMine, out.LastThem = true, row.Mine, row.Theirs
	if row.FinishedAt.Valid {
		out.LastWhen = row.FinishedAt.Time
	}
	var scope games.Scope
	if json.Unmarshal(row.Scope, &scope) == nil {
		out.LastDomn = scope.DomainID
	}
	return out, nil
}

// WinRate is the percentage the card shows beside a player's level. A player who has
// never finished a duel has no rate, which the screen shows as a dash.
func WinRate(wins, losses int32) (int, bool) {
	total := wins + losses
	if total == 0 {
		return 0, false
	}
	return int(wins * 100 / total), true
}

// MyRatings is endpoint 50: the player's rating in every domain they have duelled in.
func (s *Service) MyRatings(ctx context.Context, who *authctx.Principal, lang string) ([]sqlc.MyDuelRatingsRow, error) {
	if who == nil {
		return nil, apperr.Unauthorized("Sign in first")
	}
	return s.db.Q.MyDuelRatings(ctx, sqlc.MyDuelRatingsParams{UserID: who.ID, Lang: lang})
}

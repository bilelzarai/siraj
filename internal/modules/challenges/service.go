// Package challenges is the asynchronous side of playing together (CH-01…CH-11):
// everybody gets the same questions and plays when they can, then the scores are
// compared. A pass-the-device round (RM-06) is the same thing on one phone.
//
// One rule runs through the whole package: until a challenge closes, nobody sees
// anybody else's score. Every read goes through View / Card, which is where that is
// enforced, so a new screen cannot leak a score by accident.
package challenges

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

// Notifier is what the challenges module needs from notifications, without
// depending on it the other way round.
type Notifier interface {
	Push(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, kind sqlc.NotificationKind, link string, payload map[string]any) error
}

type Service struct {
	db     *db.DB
	games  *games.Service
	notify Notifier
	live   *live.Hub
}

func NewService(d *db.DB, g *games.Service, n Notifier) *Service {
	return &Service{db: d, games: g, notify: n}
}

// MaxPlayers is CH-01: the sender plus nine others.
const MaxPlayers = 10

// CreateInput is what P20 collects.
type CreateInput struct {
	Who        *authctx.Principal
	FriendIDs  []uuid.UUID
	Locals     []string // pass-the-device names (RM-06)
	Source     string   // bank | set | written
	SetID      uuid.UUID
	Scope      games.Scope
	Difficulty int32
	Count      int32
	Seconds    int32
	Message    string
	Days       int32 // how long they have; 0 = no deadline
	Lang       string
}

// Create freezes the questions and invites everyone. The questions are chosen once,
// here, so every player answers exactly the same list (CH-01).
func (s *Service) Create(ctx context.Context, in CreateInput) (sqlc.Challenge, error) {
	if in.Who == nil {
		return sqlc.Challenge{}, apperr.Unauthorized("Sign in to challenge a friend")
	}
	if len(in.FriendIDs) == 0 && len(in.Locals) == 0 {
		return sqlc.Challenge{}, apperr.Invalid(map[string]string{"players": "pick at least one player"})
	}
	if len(in.FriendIDs)+len(in.Locals) >= MaxPlayers {
		return sqlc.Challenge{}, apperr.Invalid(map[string]string{"players": "up to nine other players"})
	}
	if in.Seconds == 0 {
		in.Seconds = 20 // a shared round always has a timer, so the scores compare
	}

	bankIDs, setIDs, err := s.questionIDs(ctx, in)
	if err != nil {
		return sqlc.Challenge{}, err
	}
	count := len(bankIDs) + len(setIDs)
	scopeJSON, err := json.Marshal(in.Scope)
	if err != nil {
		return sqlc.Challenge{}, err
	}
	var deadline pgtype.Timestamptz
	if in.Days > 0 {
		deadline = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, int(in.Days)), Valid: true}
	}

	var challenge sqlc.Challenge
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		challenge, err = q.CreateChallenge(ctx, sqlc.CreateChallengeParams{
			CreatorID:          in.Who.ID,
			Source:             in.Source,
			Scope:              scopeJSON,
			SetID:              uuid.NullUUID{UUID: in.SetID, Valid: in.SetID != uuid.Nil},
			Difficulty:         nullInt16(in.Difficulty),
			QuestionCount:      int16(count),
			SecondsPerQuestion: int16(in.Seconds),
			Message:            text(in.Message),
			DeadlineAt:         deadline,
			IsGuest:            in.Who.IsGuest(),
		})
		if err != nil {
			return err
		}
		for i, id := range bankIDs {
			if err := q.AddChallengeQuestion(ctx, sqlc.AddChallengeQuestionParams{
				ChallengeID: challenge.ID, Position: int16(i),
				QuestionID: pgtype.Int8{Int64: id, Valid: true},
			}); err != nil {
				return err
			}
		}
		for i, id := range setIDs {
			if err := q.AddChallengeQuestion(ctx, sqlc.AddChallengeQuestionParams{
				ChallengeID: challenge.ID, Position: int16(i),
				SetQuestionID: pgtype.Int8{Int64: id, Valid: true},
			}); err != nil {
				return err
			}
		}
		// The sender is a participant too, waiting to play like everyone else.
		if err := q.AddChallengeParticipant(ctx, sqlc.AddChallengeParticipantParams{
			ChallengeID: challenge.ID, UserID: in.Who.ID, Status: sqlc.ParticipantStatusPending,
		}); err != nil {
			return err
		}
		for _, friend := range in.FriendIDs {
			if err := q.AddChallengeParticipant(ctx, sqlc.AddChallengeParticipantParams{
				ChallengeID: challenge.ID, UserID: friend, Status: sqlc.ParticipantStatusPending,
			}); err != nil {
				return err
			}
			if s.notify != nil {
				if err := s.notify.Push(ctx, q, friend, sqlc.NotificationKindChallengeReceived,
					"/challenges", map[string]any{"challenge_id": challenge.ID.String(), "actor_id": in.Who.ID}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return challenge, err
}

// questionIDs is where a round's source is resolved: the bank, one of the player's
// own sets, or questions written on the spot (CH-01, MQ-05).
func (s *Service) questionIDs(ctx context.Context, in CreateInput) (bank, set []int64, err error) {
	switch in.Source {
	case "set", "written":
		if in.SetID == uuid.Nil {
			return nil, nil, apperr.Invalid(map[string]string{"set_id": "pick one of your sets"})
		}
		rows, err := s.db.Q.SetQuestionIDs(ctx, in.SetID)
		if err != nil {
			return nil, nil, err
		}
		if len(rows) < MinSetQuestions {
			return nil, nil, apperr.Invalid(map[string]string{"set_id": "a set needs at least 5 questions"})
		}
		return nil, rows, nil
	default:
		ids, err := s.games.PickIDs(ctx, games.StartInput{
			Who: in.Who, Scope: in.Scope, Difficulty: in.Difficulty,
			Count: in.Count, Seconds: in.Seconds, Lang: in.Lang,
		})
		return ids, nil, err
	}
}

// MinSetQuestions is the rule a player-made set has to meet to be playable (MQ-03).
const MinSetQuestions = 5

// Accept starts the challenger's own game on the frozen questions (CH-03).
func (s *Service) Accept(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (games.Game, error) {
	challenge, participant, err := s.mine(ctx, who, id)
	if err != nil {
		return games.Game{}, err
	}
	if challenge.Status != sqlc.ChallengeStatusOpen {
		return games.Game{}, apperr.Conflict("challenge_closed", "This challenge is closed")
	}
	switch participant.Status {
	case sqlc.ParticipantStatusPlayed:
		return games.Game{}, apperr.Conflict("already_played", "You already played this challenge")
	case sqlc.ParticipantStatusPlaying:
		// Resume the game they already started rather than dealing a second one.
		if participant.GameID.Valid {
			return s.games.Load(ctx, who, participant.GameID.UUID)
		}
	}

	rows, err := s.db.Q.ChallengeQuestionIDs(ctx, id)
	if err != nil {
		return games.Game{}, err
	}
	bankIDs := make([]int64, 0, len(rows))
	setIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.QuestionID.Valid {
			bankIDs = append(bankIDs, r.QuestionID.Int64)
		}
		if r.SetQuestionID.Valid {
			setIDs = append(setIDs, r.SetQuestionID.Int64)
		}
	}
	var scope games.Scope
	_ = json.Unmarshal(challenge.Scope, &scope)

	game, err := s.games.StartFixed(ctx, games.FixedInput{
		Who: who, Mode: sqlc.GameModeChallenge, Scope: scope,
		Difficulty: int32(challenge.Difficulty.Int16), Seconds: int32(challenge.SecondsPerQuestion),
		QuestionIDs: bankIDs, SetQuestionIDs: setIDs, ChallengeID: id,
	})
	if err != nil {
		return games.Game{}, err
	}
	err = s.db.Q.StartParticipant(ctx, sqlc.StartParticipantParams{
		ChallengeID: id, UserID: who.ID,
		GameID: uuid.NullUUID{UUID: game.Row.ID, Valid: true},
	})
	return game, err
}

// Cancel calls a challenge off. Only the creator may, and only while nobody else has
// started: once someone is playing, the round is theirs too (CH-10).
func (s *Service) Cancel(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	challenge, _, err := s.mine(ctx, who, id)
	if err != nil {
		return err
	}
	if challenge.CreatorID != who.ID {
		return apperr.Forbidden("Only the player who sent the challenge can cancel it")
	}
	if challenge.Status != sqlc.ChallengeStatusOpen {
		return apperr.Conflict("challenge_closed", "This challenge is closed")
	}
	if err := s.db.Q.CancelChallenge(ctx, sqlc.CancelChallengeParams{
		ID: id, CreatorID: who.ID,
	}); err != nil {
		return err
	}
	// The statement does nothing when somebody is already playing, so read back.
	after, err := s.db.Q.GetChallenge(ctx, id)
	if err != nil {
		return err
	}
	if after.Status == sqlc.ChallengeStatusOpen {
		return apperr.Conflict("already_playing", "Someone has already started. You cannot cancel now.")
	}
	return nil
}

// Remind nudges everyone who has not started their round yet (CH-10).
func (s *Service) Remind(ctx context.Context, who *authctx.Principal, id uuid.UUID) (int, error) {
	challenge, _, err := s.mine(ctx, who, id)
	if err != nil {
		return 0, err
	}
	if challenge.CreatorID != who.ID {
		return 0, apperr.Forbidden("Only the player who sent the challenge can send a reminder")
	}
	waiting, err := s.db.Q.ChallengeStillWaiting(ctx, sqlc.ChallengeStillWaitingParams{
		ChallengeID: id, CreatorID: who.ID,
	})
	if err != nil {
		return 0, err
	}
	if s.notify == nil || len(waiting) == 0 {
		return len(waiting), nil
	}
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		for _, friend := range waiting {
			if err := s.notify.Push(ctx, q, friend, sqlc.NotificationKindChallengeReceived,
				"/challenges", map[string]any{"challenge_id": id.String(), "actor_id": who.ID}); err != nil {
				return err
			}
		}
		return nil
	})
	return len(waiting), err
}

// Decline removes the player from the challenge (CH-03).
func (s *Service) Decline(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	if _, _, err := s.mine(ctx, who, id); err != nil {
		return err
	}
	return s.db.Q.DeclineParticipant(ctx, sqlc.DeclineParticipantParams{ChallengeID: id, UserID: who.ID})
}

// OnGameFinished records a challenge game's score and closes the challenge once
// everyone has played (CH-07). It runs inside the finish transaction.
func (s *Service) OnGameFinished(ctx context.Context, q *sqlc.Queries, r games.FinishResult) error {
	if !r.Game.ChallengeID.Valid {
		return nil
	}
	id := r.Game.ChallengeID.UUID
	if err := q.FinishParticipant(ctx, sqlc.FinishParticipantParams{
		ChallengeID: id, UserID: r.UserID,
		Score: pgtype.Int4{Int32: r.Score, Valid: true}, Correct: nullInt16(r.Correct),
	}); err != nil {
		return err
	}
	rows, err := q.ChallengeParticipants(ctx, id)
	if err != nil {
		return err
	}
	waiting := 0
	for _, p := range rows {
		if p.Status == sqlc.ParticipantStatusPending || p.Status == sqlc.ParticipantStatusPlaying {
			waiting++
		}
	}
	if waiting > 0 {
		return nil
	}
	if err := q.RankChallenge(ctx, id); err != nil {
		return err
	}
	if err := q.CloseChallenge(ctx, sqlc.CloseChallengeParams{ID: id, CloseReason: text("all_played")}); err != nil {
		return err
	}
	if s.notify == nil {
		return nil
	}
	for _, p := range rows {
		if err := s.notify.Push(ctx, q, p.UserID, sqlc.NotificationKindChallengeResultsReady,
			"/challenges?box=finished", map[string]any{"challenge_id": id.String()}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) mine(ctx context.Context, who *authctx.Principal, id uuid.UUID) (sqlc.Challenge, sqlc.ChallengeParticipant, error) {
	if who == nil {
		return sqlc.Challenge{}, sqlc.ChallengeParticipant{}, apperr.Unauthorized("Sign in first")
	}
	challenge, err := s.db.Q.GetChallenge(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Challenge{}, sqlc.ChallengeParticipant{}, apperr.NotFound("challenge")
	}
	if err != nil {
		return sqlc.Challenge{}, sqlc.ChallengeParticipant{}, err
	}
	participant, err := s.db.Q.GetParticipant(ctx, sqlc.GetParticipantParams{ChallengeID: id, UserID: who.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Challenge{}, sqlc.ChallengeParticipant{}, apperr.NotFound("challenge")
	}
	return challenge, participant, err
}

func nullInt16(v int32) pgtype.Int2 {
	if v <= 0 {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: int16(v), Valid: true}
}

func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

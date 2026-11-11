package challenges

import (
	"context"
	"time"

	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/authctx"
)

// Player is one line of a challenge card. Score is nil while the challenge is open
// and the player is not the viewer — CH-09, the rule the whole module exists to keep.
type Player struct {
	UserID uuid.UUID
	Name   string
	Level  int32
	IsMe   bool
	Status string // pending | playing | played | declined
	Score  *int32
	Rank   int32
	Result string // won | lost | draw
}

// Card is one challenge as every screen sees it.
type Card struct {
	ID         uuid.UUID
	Creator    string
	CreatorID  uuid.UUID
	Mine       bool
	Status     string
	MyStatus   string
	MyResult   string
	Questions  int32
	Difficulty int32
	Message    string
	Deadline   *time.Time
	Category   string
	Icon       string
	Color      string
	Players    []Player
	Played     int
	Total      int
}

// Closed is true once the scores may be shown to everyone (CH-09).
func (c Card) Closed() bool { return c.Status == string(sqlc.ChallengeStatusClosed) }

// Box lists one of the three tabs of P19.
func (s *Service) Box(ctx context.Context, who *authctx.Principal, box, lang string, page, perPage int) ([]Card, int, error) {
	if who == nil {
		return nil, 0, nil
	}
	switch box {
	case "sent", "finished":
	default:
		box = "received"
	}
	rows, err := s.db.Q.MyChallenges(ctx, sqlc.MyChallengesParams{
		UserID: who.ID, Box: box,
		Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountMyChallenges(ctx, sqlc.CountMyChallengesParams{UserID: who.ID, Box: box})
	if err != nil {
		return nil, 0, err
	}
	cards := make([]Card, 0, len(rows))
	for _, r := range rows {
		card := Card{
			ID: r.ID, Creator: r.CreatorName, CreatorID: r.CreatorID,
			Mine:      r.CreatorID == who.ID,
			Status:    string(r.Status),
			MyStatus:  string(r.MyStatus),
			Questions: int32(r.QuestionCount),
			Played:    int(r.Played), Total: int(r.Players),
		}
		if r.MyResult.Valid {
			card.MyResult = string(r.MyResult.ChallengeResult)
		}
		if r.Difficulty.Valid {
			card.Difficulty = int32(r.Difficulty.Int16)
		}
		if r.Message.Valid {
			card.Message = r.Message.String
		}
		if r.DeadlineAt.Valid {
			when := r.DeadlineAt.Time
			card.Deadline = &when
		}
		if scope, err := s.db.Q.ChallengeScope(ctx, sqlc.ChallengeScopeParams{ChallengeID: r.ID, Lang: lang}); err == nil {
			card.Category, card.Icon, card.Color = scope.CategoryName, scope.Icon, scope.Color
		}
		players, err := s.players(ctx, who, r.ID, card.Closed())
		if err != nil {
			return nil, 0, err
		}
		card.Players = players
		cards = append(cards, card)
	}
	return cards, int(total), nil
}

// One is a single challenge, for the result screen and the API.
func (s *Service) One(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (Card, error) {
	challenge, participant, err := s.mine(ctx, who, id)
	if err != nil {
		return Card{}, err
	}
	creator, err := s.db.Q.GetUser(ctx, challenge.CreatorID)
	if err != nil {
		return Card{}, err
	}
	card := Card{
		ID: challenge.ID, Creator: creator.DisplayName, CreatorID: challenge.CreatorID,
		Mine:      challenge.CreatorID == who.ID,
		Status:    string(challenge.Status),
		MyStatus:  string(participant.Status),
		Questions: int32(challenge.QuestionCount),
	}
	if participant.Result.Valid {
		card.MyResult = string(participant.Result.ChallengeResult)
	}
	if challenge.Message.Valid {
		card.Message = challenge.Message.String
	}
	if challenge.DeadlineAt.Valid {
		when := challenge.DeadlineAt.Time
		card.Deadline = &when
	}
	if scope, err := s.db.Q.ChallengeScope(ctx, sqlc.ChallengeScopeParams{ChallengeID: id, Lang: lang}); err == nil {
		card.Category, card.Icon, card.Color = scope.CategoryName, scope.Icon, scope.Color
	}
	card.Players, err = s.players(ctx, who, id, card.Closed())
	for _, p := range card.Players {
		card.Total++
		if p.Status == string(sqlc.ParticipantStatusPlayed) {
			card.Played++
		}
	}
	return card, err
}

// players is the one place a challenge's scores are read. While the challenge is open
// every score but the viewer's own is dropped here, so no screen can leak it (CH-09).
func (s *Service) players(ctx context.Context, who *authctx.Principal, id uuid.UUID, closed bool) ([]Player, error) {
	rows, err := s.db.Q.ChallengeParticipants(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]Player, 0, len(rows))
	for _, r := range rows {
		p := Player{
			UserID: r.UserID, Name: r.DisplayName, Level: r.Level,
			IsMe:   who != nil && r.UserID == who.ID,
			Status: string(r.Status),
		}
		if r.Rank.Valid {
			p.Rank = int32(r.Rank.Int16)
		}
		if r.Result.Valid {
			p.Result = string(r.Result.ChallengeResult)
		}
		if r.Score.Valid && (closed || p.IsMe) {
			score := r.Score.Int32
			p.Score = &score
		}
		out = append(out, p)
	}
	return out, nil
}

// Counts feeds the three tab labels and the navigation badge (CH-11).
func (s *Service) Counts(ctx context.Context, who *authctx.Principal) (received, sent, finished int, err error) {
	if who == nil {
		return 0, 0, 0, nil
	}
	row, err := s.db.Q.ChallengeBoxCounts(ctx, who.ID)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(row.Received), int(row.Sent), int(row.Finished), nil
}

// Shareable and Card are what the Messages composer needs in order to post a
// challenge into a thread (MS-02): the rounds this player may share, and one of
// them. Scores never travel here — a card says what the round is, not how anybody
// did in it (CH-09).
type ShareCard struct {
	ID          uuid.UUID
	Title       string
	Questions   int
	ScoreToBeat int
}

// Shareable is every open challenge this player is in, newest first.
func (s *Service) Shareable(ctx context.Context, who *authctx.Principal, lang string) ([]ShareCard, error) {
	if who == nil {
		return nil, nil
	}
	out := []ShareCard{}
	for _, box := range []string{"sent", "received"} {
		cards, _, err := s.Box(ctx, who, box, lang, 1, 10)
		if err != nil {
			return nil, err
		}
		for _, card := range cards {
			if card.Closed() {
				continue
			}
			out = append(out, shareCard(card))
		}
	}
	return out, nil
}

// Card is one of them, read with the same rules.
func (s *Service) Card(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (ShareCard, error) {
	card, err := s.One(ctx, who, id, lang)
	if err != nil {
		return ShareCard{}, err
	}
	return shareCard(card), nil
}

// shareCard turns a card into the little the bubble shows. The score to beat is the
// viewer's own — the one score they are allowed to see while the round is open.
func shareCard(card Card) ShareCard {
	out := ShareCard{
		ID: card.ID, Title: card.Category, Questions: int(card.Questions),
	}
	if out.Title == "" {
		out.Title = card.Creator
	}
	for _, player := range card.Players {
		if player.IsMe && player.Score != nil {
			out.ScoreToBeat = int(*player.Score)
		}
	}
	return out
}

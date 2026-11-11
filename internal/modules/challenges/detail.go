package challenges

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

// The challenge detail screen has two faces: while it is open, who has finished and
// nothing of what they scored (CH-04); once it closes, the winner, the podium and the
// standings with the removed players at the bottom (CH-08).

// Detail is one challenge as its own page draws it.
type Detail struct {
	Card
	// Mine is the viewer's own line, which is the one score they may see early.
	Mine     Player
	HasMine  bool
	IsHost   bool
	Playing  int
	NotStart int
	Removed  []Player
	// Winner and Gap describe a closed challenge (CH-08).
	Winner Player
	Won    bool
	Tied   bool
	MyRank int32
	Ranked []Player
	// CanClose is the host's "Close now and show results": only when nobody is
	// mid-game, because a player who started keeps their window (CH-07, D-03).
	CanClose bool
	// Blocker names whoever is playing right now, so the button can say why it is
	// not available.
	Blocker string
	// WillRemove names whoever would be dropped by closing now.
	WillRemove []string
}

// Detail reads one challenge for the player looking at it. Scores stay hidden until
// it closes — this is the one place that decides it, and the DTO never carries a
// score it should not (CH-04).
func (s *Service) Detail(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (Detail, error) {
	if who == nil {
		return Detail{}, apperr.Unauthorized("Sign in first")
	}
	card, err := s.One(ctx, who, id, lang)
	if err != nil {
		return Detail{}, err
	}
	out := Detail{Card: card, IsHost: card.Mine}
	for _, p := range card.Players {
		switch {
		case p.Status == string(sqlc.ParticipantStatusRemoved) || p.Status == string(sqlc.ParticipantStatusDeclined):
			out.Removed = append(out.Removed, p)
			continue
		case p.Status == string(sqlc.ParticipantStatusPlaying):
			out.Playing++
			out.Blocker = p.Name
		case p.Status == string(sqlc.ParticipantStatusPending):
			out.NotStart++
			out.WillRemove = append(out.WillRemove, p.Name)
		}
		if p.IsMe {
			out.Mine, out.HasMine = p, true
		}
		if card.Closed() {
			out.Ranked = append(out.Ranked, p)
		}
	}
	// Closing early is the host's, and only while nobody is in the middle of a game:
	// someone who has started keeps the window the decision gives them (CH-07, D-03).
	out.CanClose = out.IsHost && !card.Closed() && out.Playing == 0 && out.NotStart > 0
	if card.Closed() && len(out.Ranked) > 0 {
		out.Winner = out.Ranked[0]
		out.Won = out.Winner.IsMe
		out.Tied = len(out.Ranked) > 1 && out.Ranked[1].Score != nil &&
			out.Winner.Score != nil && *out.Ranked[1].Score == *out.Winner.Score
		for _, p := range out.Ranked {
			if p.IsMe {
				out.MyRank = p.Rank
			}
		}
	}
	return out, nil
}

// CloseEarly is endpoint 118: the host ending a challenge nobody is mid-game in.
// Whoever has not started is removed, and the results open for the rest (CH-07).
func (s *Service) CloseEarly(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	challenge, _, err := s.mine(ctx, who, id)
	if err != nil {
		return err
	}
	if challenge.CreatorID != who.ID {
		return apperr.Forbidden("Only the player who sent the challenge can close it")
	}
	if challenge.Status == sqlc.ChallengeStatusClosed {
		return nil
	}
	rows, err := s.db.Q.ChallengeParticipants(ctx, id)
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.Status == sqlc.ParticipantStatusPlaying {
			return apperr.Conflict("someone_playing", "Someone has started. Wait until they finish.")
		}
	}
	return s.closeNow(ctx, id, "host")
}

// closeNow removes whoever never started, settles the places and opens the results.
// The deadline job and the host's own button both end here, so a challenge closes the
// same way however it got there (CH-06, CH-07).
func (s *Service) closeNow(ctx context.Context, id uuid.UUID, reason string) error {
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.RemoveNonStarters(ctx, id); err != nil {
			return err
		}
		if err := q.RankChallenge(ctx, id); err != nil {
			return err
		}
		return q.CloseChallenge(ctx, sqlc.CloseChallengeParams{ID: id, CloseReason: text(reason)})
	})
	if err != nil {
		return err
	}
	s.announceResults(ctx, id)
	return nil
}

// announceResults tells everyone still in the challenge that it is over, both through
// their notifications and on any screen they have open (CH-12, NF-02).
func (s *Service) announceResults(ctx context.Context, id uuid.UUID) {
	rows, err := s.db.Q.ChallengeParticipants(ctx, id)
	if err != nil {
		return
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, p.UserID)
		if s.notify != nil {
			_ = s.notify.Push(ctx, s.db.Q, p.UserID, sqlc.NotificationKindChallengeResultsReady,
				"/challenges/"+id.String(), map[string]any{"challenge_id": id.String()})
		}
	}
	if s.live != nil {
		s.live.SendAll(ids, live.Challenges)
	}
}

// SetLive gives the module the event stream, so a challenge screen follows the others
// playing instead of waiting for a reload (NF-02).
func (s *Service) SetLive(hub *live.Hub) { s.live = hub }

// ExpireDue is the worker's round of CH-06: every challenge past its deadline loses
// whoever never started, and opens for the rest.
func (s *Service) ExpireDue(ctx context.Context) (int, error) {
	due, err := s.db.Q.ChallengesPastDeadline(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range due {
		if err := s.closeNow(ctx, id, "deadline"); err != nil {
			return 0, err
		}
	}
	return len(due), nil
}

// Rematch is "Rematch with the same players": a new challenge on the same choice,
// with everyone who was in the last one (CH-08).
func (s *Service) Rematch(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (sqlc.Challenge, error) {
	old, _, err := s.mine(ctx, who, id)
	if err != nil {
		return sqlc.Challenge{}, err
	}
	rows, err := s.db.Q.ChallengeParticipants(ctx, id)
	if err != nil {
		return sqlc.Challenge{}, err
	}
	invited := make([]uuid.UUID, 0, len(rows))
	for _, p := range rows {
		if p.UserID != who.ID {
			invited = append(invited, p.UserID)
		}
	}
	if len(invited) == 0 {
		return sqlc.Challenge{}, apperr.Invalid(map[string]string{"players": "nobody to play again with"})
	}
	// The rematch plays the same questions' topic: the scope the first one was built
	// on is stored on the challenge itself.
	var picked games.Scope
	_ = json.Unmarshal(old.Scope, &picked)
	in := CreateInput{
		// A rematch plays from the bank on the same topic: the first round's own
		// set is not re-sent, so the source is the one Create defaults to.
		Who: who, FriendIDs: invited, Scope: picked, Source: "bank",
		Count: int32(old.QuestionCount), Seconds: int32(old.SecondsPerQuestion),
		Days: daysLeft(old), Lang: lang,
	}
	if old.Difficulty.Valid {
		in.Difficulty = int32(old.Difficulty.Int16)
	}
	return s.Create(ctx, in)
}

// daysLeft is the window a rematch gives, taken from the challenge it repeats.
func daysLeft(old sqlc.Challenge) int32 {
	if !old.DeadlineAt.Valid || !old.CreatedAt.Valid {
		return 3
	}
	days := int32(old.DeadlineAt.Time.Sub(old.CreatedAt.Time).Hours() / 24)
	if days < 1 {
		return 1
	}
	return days
}

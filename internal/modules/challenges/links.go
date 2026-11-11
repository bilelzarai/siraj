package challenges

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// A challenge can be shared as a link instead of picking friends by name (CH-10):
// whoever opens it, guests included, joins the round. The link lasts 24 hours.

// LinkTTL is how long a share link works (CH-10).
const LinkTTL = 24 * time.Hour

// linkAlphabet leaves out the characters that read alike, like a room code.
const linkAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newLinkCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = linkAlphabet[int(v)%len(linkAlphabet)]
	}
	return string(out), nil
}

// ShareLink returns the challenge's link, creating one when it has none that is
// still valid. Only somebody already in the challenge may share it.
func (s *Service) ShareLink(ctx context.Context, who *authctx.Principal, id uuid.UUID) (sqlc.ChallengeLink, error) {
	if who == nil {
		return sqlc.ChallengeLink{}, apperr.Unauthorized("Sign in first")
	}
	if _, _, err := s.mine(ctx, who, id); err != nil {
		return sqlc.ChallengeLink{}, err
	}
	link, err := s.db.Q.ChallengeLinkFor(ctx, id)
	if err == nil {
		return link, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.ChallengeLink{}, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		code, err := newLinkCode()
		if err != nil {
			return sqlc.ChallengeLink{}, err
		}
		link, err = s.db.Q.CreateChallengeLink(ctx, sqlc.CreateChallengeLinkParams{
			Code: code, ChallengeID: id,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(LinkTTL), Valid: true},
		})
		if err == nil {
			return link, nil
		}
	}
	return sqlc.ChallengeLink{}, err
}

// OpenLink is endpoint 60: what a shared link opens. The player joins the challenge
// if there is room and they are not in it yet, and the card comes back with the same
// hidden scores as everywhere else (CH-09).
func (s *Service) OpenLink(ctx context.Context, who *authctx.Principal, code, lang string) (Card, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	link, err := s.db.Q.GetChallengeLink(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return Card{}, apperr.NotFound("challenge link")
	}
	if err != nil {
		return Card{}, err
	}
	if who == nil {
		return Card{}, apperr.Unauthorized("Sign in or play as a guest to join this challenge")
	}
	challenge, err := s.db.Q.GetChallenge(ctx, link.ChallengeID)
	if err != nil {
		return Card{}, apperr.NotFound("challenge")
	}
	if challenge.Status != sqlc.ChallengeStatusOpen {
		return Card{}, apperr.Conflict("challenge_closed", "This challenge is closed")
	}

	if _, err := s.db.Q.GetParticipant(ctx, sqlc.GetParticipantParams{
		ChallengeID: challenge.ID, UserID: who.ID,
	}); errors.Is(err, pgx.ErrNoRows) {
		count, err := s.db.Q.CountChallengeParticipants(ctx, challenge.ID)
		if err != nil {
			return Card{}, err
		}
		if int(count) >= MaxPlayers {
			return Card{}, apperr.Conflict("challenge_full", "This challenge is full")
		}
		if err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
			if err := q.AddChallengeParticipant(ctx, sqlc.AddChallengeParticipantParams{
				ChallengeID: challenge.ID, UserID: who.ID, Status: sqlc.ParticipantStatusPending,
			}); err != nil {
				return err
			}
			return q.UseChallengeLink(ctx, code)
		}); err != nil {
			return Card{}, err
		}
	} else if err != nil {
		return Card{}, err
	}
	return s.One(ctx, who, challenge.ID, lang)
}

// Opponent is one player the random-opponent button can pick (CH-10).
type Opponent struct {
	UserID   uuid.UUID
	Name     string
	Username string
	Level    int32
}

// RandomOpponent is endpoint 61: a real player to challenge — a friend when the
// player has one, otherwise somebody they have already shared a game with. It never
// invents a name: with nobody to pick, it says so.
func (s *Service) RandomOpponent(ctx context.Context, who *authctx.Principal) (Opponent, error) {
	if who == nil {
		return Opponent{}, apperr.Unauthorized("Sign in first")
	}
	row, err := s.db.Q.RandomOpponent(ctx, who.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Opponent{}, apperr.NotFound("opponent")
	}
	if err != nil {
		return Opponent{}, err
	}
	out := Opponent{UserID: row.ID, Name: row.DisplayName, Level: row.Level}
	if row.Username.Valid {
		out.Username = row.Username.String
	}
	return out, nil
}

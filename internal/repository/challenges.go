package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

const challengeColumns = `
	ch.id, ch.host_id, ch.format, ch.player_source, ch.winner_team,
	ch.cancelled_at, ch.cancelled_by, ch.started_at,
	ch.rematch_of, ch.category_id, ch.difficulty,
	ch.question_ids, ch.status::text, ch.winner_id, ch.message,
	ch.created_at, ch.expires_at, ch.completed_at,
	COALESCE(ct.name, cfb.name, cat.slug, ''), COALESCE(cat.icon, '')`

const challengeJoins = `
	  LEFT JOIN categories cat ON cat.id = ch.category_id
	  LEFT JOIN category_translations ct  ON ct.category_id  = cat.id AND ct.locale  = $1
	  LEFT JOIN category_translations cfb ON cfb.category_id = cat.id AND cfb.locale = 'ar'`

func scanChallenge(row pgx.Row) (*models.Challenge, error) {
	var c models.Challenge
	err := row.Scan(
		&c.ID, &c.HostID, &c.Format, &c.PlayerSource, &c.WinnerTeam,
		&c.CancelledAt, &c.CancelledBy, &c.StartedAt,
		&c.RematchOf, &c.CategoryID, &c.Difficulty,
		&c.QuestionIDs, &c.Status, &c.WinnerID, &c.Message,
		&c.CreatedAt, &c.ExpiresAt, &c.CompletedAt,
		&c.CategoryName, &c.CategoryIcon,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) Challenge(ctx context.Context, id uuid.UUID, locale string) (*models.Challenge, error) {
	ch, err := scanChallenge(r.pool.QueryRow(ctx,
		`SELECT `+challengeColumns+` FROM challenges ch`+challengeJoins+` WHERE ch.id = $2`,
		locale, id))
	if err != nil {
		return nil, err
	}
	// A match is its players. Reading one without them gives a screen the two
	// old columns and nothing about the third person in the room.
	if ch.Players, err = r.MatchPlayers(ctx, ch.ID); err != nil {
		return nil, err
	}
	return ch, nil
}

// ChallengesFor lists duels in one of three buckets: "incoming" (pending on
// the viewer), "outgoing" (pending on the other side), "finished".
func (r *Repo) ChallengesFor(ctx context.Context, userID uuid.UUID, bucket, locale string, limit int) ([]*models.Challenge, error) {
	// Membership comes from challenge_players: a match with four people has to
	// be listed for all four, and the old columns only ever named two.
	var where string
	switch bucket {
	case "incoming":
		// Waiting on me: I was invited, or I am in and have not played.
		where = `EXISTS (SELECT 1 FROM challenge_players p
		                  WHERE p.challenge_id = ch.id AND p.user_id = $2
		                    AND p.state IN ('invited', 'joined'))
		         AND ch.status IN ('pending', 'accepted')`
	case "outgoing":
		// Mine, still running, and my part is done — I am waiting on others.
		where = `EXISTS (SELECT 1 FROM challenge_players p
		                  WHERE p.challenge_id = ch.id AND p.user_id = $2
		                    AND p.state = 'played')
		         AND ch.status IN ('pending', 'accepted')`
	default:
		where = `EXISTS (SELECT 1 FROM challenge_players p
		                  WHERE p.challenge_id = ch.id AND p.user_id = $2)
		         AND ch.status IN ('completed', 'declined', 'expired', 'cancelled')`
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+challengeColumns+` FROM challenges ch`+challengeJoins+`
		  WHERE `+where+`
		  ORDER BY ch.created_at DESC LIMIT $3`, locale, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Challenge
	for rows.Next() {
		c, err := scanChallenge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Players for the whole page in one pass rather than one query per match.
	for _, c := range out {
		if c.Players, err = r.MatchPlayers(ctx, c.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetChallengeQuestions replaces the set a match plays, which is how the host's
// own questions join it: they cannot be written until the match has an id, and
// the match cannot be created without a set.
func (r *Repo) SetChallengeQuestions(ctx context.Context, id uuid.UUID, ids []int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE challenges SET question_ids = $2 WHERE id = $1`, id, ids)
	return err
}

// SetChallengeStatus moves a match that is still open.
//
// Guarded on it being open: a cancelled match is final, and without this a late
// write — a settle that arrived after the cancel, a retried request — could
// bring it back to life as something somebody could still play.
func (r *Repo) SetChallengeStatus(ctx context.Context, id uuid.UUID, status string) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE challenges SET status = $2::challenge_status
		  WHERE id = $1 AND status IN ('pending', 'accepted')`, id, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ChallengeWins counts completed duels the user won, for the badge check.
func (r *Repo) ChallengeWins(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM challenges WHERE winner_id = $1 AND status = 'completed'`,
		userID).Scan(&n)
	return n, err
}

// ActiveChallengeBetween prevents stacking duplicate duels on the same pair.
//
// Read from challenge_players rather than the two legacy columns, which only
// ever named the first two people in a match.
func (r *Repo) ActiveChallengeBetween(ctx context.Context, a, b uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM challenges c
			  JOIN challenge_players pa ON pa.challenge_id = c.id AND pa.user_id = $1
			  JOIN challenge_players pb ON pb.challenge_id = c.id AND pb.user_id = $2
			 WHERE c.status IN ('pending', 'accepted')
			   AND c.expires_at > now()
			   AND pa.state <> 'declined' AND pb.state <> 'declined')`, a, b).Scan(&exists)
	return exists, err
}

// ActiveMatchFor is the one match somebody is currently inside, or ErrNotFound.
//
// "Inside" is the joined state and nothing else: invitations waiting to be
// answered do not count, which is what lets a person hold several at once while
// only ever playing one.
func (r *Repo) ActiveMatchFor(ctx context.Context, userID uuid.UUID, locale string) (*models.Challenge, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT p.challenge_id
		  FROM challenge_players p
		  JOIN challenges c ON c.id = p.challenge_id
		 WHERE p.user_id = $1 AND p.state = 'joined'
		   AND c.status IN ('pending', 'accepted') AND c.expires_at > now()`,
		userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.Challenge(ctx, id, locale)
}

// InActiveMatch reports whether somebody is currently inside a match, which is
// the state the one-at-a-time rule is written against.
func (r *Repo) InActiveMatch(ctx context.Context, userID uuid.UUID) (bool, error) {
	var busy bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		      FROM challenge_players p
		      JOIN challenges c ON c.id = p.challenge_id
		     WHERE p.user_id = $1 AND p.state = 'joined'
		       AND c.status IN ('pending', 'accepted') AND c.expires_at > now())`,
		userID).Scan(&busy)
	return busy, err
}

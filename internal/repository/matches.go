package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// A match is a challenge with a row per player. The 1:1 columns on `challenges`
// are still written for the history they already hold; everything read here
// comes from challenge_players, which is what lets a match have three people in
// it, or ten.

// CreateMatch opens a match and invites everyone named, with the host already
// in. It refuses a list that could not make a match: nobody to play against, or
// more people than a scoreboard can hold.
// MatchSeat is one place at the table: who, which side they are on, and
// whether they are sitting at the host's device rather than their own.
//
// A local player is put straight in rather than invited. There is nobody
// elsewhere to accept: they are the person being handed the phone, and the
// invitation they would have to answer is the host passing it over.
type MatchSeat struct {
	UserID uuid.UUID
	Team   int
	Local  bool
	// Origin is which of the three groups this person was reached through. The
	// database refuses a match whose seats do not all name the same one.
	Origin string
}

func (r *Repo) CreateMatch(ctx context.Context, ch *models.Challenge, seats []MatchSeat) error {
	if len(seats) == 0 {
		return ErrForbidden
	}
	if len(seats)+1 > models.MaxChallengePlayers {
		return ErrForbidden
	}
	format := ch.Format
	if format != models.FormatTeam {
		format = models.FormatDuel
	}
	source := ch.PlayerSource
	if !models.ValidSource(source) {
		source = models.SourceFriends
	}
	hostTeam := 0
	if format == models.FormatTeam {
		hostTeam = ch.HostTeam
		if hostTeam <= 0 {
			hostTeam = 1
		}
	}

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO challenges
				(host_id, category_id, difficulty,
				 question_ids, message, rematch_of, format, player_source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, created_at, expires_at`,
			ch.HostID, ch.CategoryID, ch.Difficulty,
			ch.QuestionIDs, ch.Message, ch.RematchOf, format, source,
		).Scan(&ch.ID, &ch.CreatedAt, &ch.ExpiresAt)
		if err != nil {
			return err
		}
		ch.Format = format
		ch.PlayerSource = source

		// The host is in from the start — they opened it. This is also where
		// "one match at a time" first bites: the unique index refuses a second
		// joined row, so opening a match while inside one fails here rather
		// than leaving two half-played rounds behind.
		if _, err := tx.Exec(ctx, `
			INSERT INTO challenge_players
				(challenge_id, user_id, is_host, state, team, origin, joined_at)
			VALUES ($1, $2, true, 'joined', $3, 'host', now())`,
			ch.ID, ch.HostID, hostTeam); err != nil {
			return err
		}

		for _, seat := range seats {
			if seat.UserID == ch.HostID {
				continue
			}
			state := "invited"
			if seat.Local {
				state = "joined"
			}
			team := 0
			if format == models.FormatTeam {
				team = seat.Team
				if team <= 0 {
					team = 1
				}
			}
			// The origin is written per seat and checked against the match's
			// own source by the database. A caller that mixed two groups is
			// refused here rather than producing a match nobody can describe.
			origin := seat.Origin
			if origin == "" {
				origin = source
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO challenge_players
					(challenge_id, user_id, state, team, origin, joined_at)
				VALUES ($1, $2, $3, $4, $5, CASE WHEN $3 = 'joined' THEN now() END)
				ON CONFLICT (challenge_id, user_id) DO NOTHING`,
				ch.ID, seat.UserID, state, team, origin); err != nil {
				return err
			}
		}
		return nil
	})
	if isCheckViolation(err, "challenge_players_one_source") {
		// Two groups in one match. The database is the last word on this; the
		// service refuses it earlier with something a person can act on.
		return ErrInvalid
	}
	if isUniqueViolation(err) {
		// The only unique constraint a match creation can trip is "one active
		// match per player".
		return ErrConflict
	}
	return err
}

// MatchPlayers loads everyone in a match, with the card each screen needs.
func (r *Repo) MatchPlayers(ctx context.Context, challengeID uuid.UUID) ([]*models.ChallengePlayer, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.user_id, p.is_host, p.state, p.score, p.played_at, p.team,
		       p.origin, u.is_temporary,
		       u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at
		  FROM challenge_players p
		  JOIN users u ON u.id = p.user_id
		 WHERE p.challenge_id = $1
		 ORDER BY p.team, p.is_host DESC, p.invited_at`, challengeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.ChallengePlayer
	for rows.Next() {
		var p models.ChallengePlayer
		var card models.UserCard
		if err := rows.Scan(&p.UserID, &p.IsHost, &p.State, &p.Score, &p.PlayedAt,
			&p.Team, &p.Origin, &p.Local,
			&card.ID, &card.Username, &card.DisplayName, &card.AvatarSeed,
			&card.Country, &card.XP, &card.LastSeenAt); err != nil {
			return nil, err
		}
		p.Card = &card
		out = append(out, &p)
	}
	return out, rows.Err()
}

// JoinMatch accepts an invitation. Only somebody who was invited can join, and
// only while the match is still open.
func (r *Repo) JoinMatch(ctx context.Context, challengeID, userID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE challenge_players p
		   SET state = 'joined', joined_at = now()
		 WHERE p.challenge_id = $1 AND p.user_id = $2 AND p.state = 'invited'
		   AND EXISTS (SELECT 1 FROM challenges c
		                WHERE c.id = $1 AND c.status IN ('pending', 'accepted')
		                  AND c.expires_at > now())`, challengeID, userID)
	if isUniqueViolation(err) {
		// Already inside another match. The index is the rule; this is the
		// only place a person can discover they have broken it.
		return ErrBusy
	}
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// DeclineMatch says no. The match carries on without them, which is the whole
// difference from a duel: one refusal used to end it for both people.
func (r *Repo) DeclineMatch(ctx context.Context, challengeID, userID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE challenge_players
		   SET state = 'declined'
		 WHERE challenge_id = $1 AND user_id = $2 AND state IN ('invited', 'joined')`,
		challengeID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// AttachMatchSession binds a player's round to their place in the match.
func (r *Repo) AttachMatchSession(ctx context.Context, challengeID, userID, sessionID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE challenge_players
		   SET session_id = $3, state = 'joined', joined_at = COALESCE(joined_at, now())
		 WHERE challenge_id = $1 AND user_id = $2 AND state IN ('invited', 'joined')`,
		challengeID, userID, sessionID)
	if isUniqueViolation(err) {
		return ErrBusy
	}
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrConflict
	}
	// A match with somebody playing in it is accepted.
	_, _ = r.pool.Exec(ctx,
		`UPDATE challenges SET status = 'accepted'
		  WHERE id = $1 AND status = 'pending'`, challengeID)
	return nil
}

// RecordMatchScore files one player's result and settles the match when nobody
// is left to play.
//
// Settling is one statement guarded on the match not already being complete, so
// two players finishing at the same moment credit the winner once.
func (r *Repo) RecordMatchScore(ctx context.Context, challengeID, userID uuid.UUID, score int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE challenge_players
			   SET state = 'played', score = $3, played_at = now()
			 WHERE challenge_id = $1 AND user_id = $2 AND state <> 'played'`,
			challengeID, userID, score); err != nil {
			return err
		}
		// Anybody who was invited and never turned up, once everybody who did
		// turn up has finished, is out of this match.
		//
		// Only those who never started: somebody mid-round is still playing and
		// the match waits for them. And only when enough people have played for
		// a result to mean anything — below that there is nothing to settle, and
		// the match is better left to expire than declared over.
		if _, err := tx.Exec(ctx, `
			UPDATE challenge_players p
			   SET state = 'eliminated'
			 WHERE p.challenge_id = $1
			   AND p.state IN ('invited', 'joined')
			   AND p.session_id IS NULL
			   AND NOT EXISTS (
			       SELECT 1 FROM challenge_players mid
			        WHERE mid.challenge_id = $1
			          AND mid.session_id IS NOT NULL
			          AND mid.state <> 'played')
			   AND (SELECT count(*) FROM challenge_players done
			         WHERE done.challenge_id = $1 AND done.state = 'played') >= $2`,
			challengeID, models.MinChallengePlayers); err != nil {
			return err
		}

		// Anybody still expected?
		var pending int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM challenge_players
			 WHERE challenge_id = $1 AND state IN ('invited', 'joined')`,
			challengeID).Scan(&pending); err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}

		// Everyone is in. The top score wins, unless two share it — and in a
		// team match the top *total* wins, which is a different question asked
		// of the same rows.
		var winner *uuid.UUID
		var winningTeam *int
		err := tx.QueryRow(ctx, `
			WITH ranked AS (
			    SELECT user_id, score,
			           rank() OVER (ORDER BY score DESC) AS place,
			           count(*) OVER (PARTITION BY score) AS sharing
			      FROM challenge_players
			     WHERE challenge_id = $1 AND state = 'played'
			),
			sides AS (
			    SELECT team, sum(score) AS total,
			           rank() OVER (ORDER BY sum(score) DESC) AS place,
			           count(*) OVER (PARTITION BY sum(score)) AS sharing
			      FROM challenge_players
			     WHERE challenge_id = $1 AND state = 'played' AND team > 0
			     GROUP BY team
			)
			UPDATE challenges c
			   SET status = 'completed', completed_at = now(),
			       winner_id = CASE WHEN c.format = 'team' THEN NULL
			                        ELSE (SELECT user_id FROM ranked
			                               WHERE place = 1 AND sharing = 1) END,
			       winner_team = CASE WHEN c.format = 'team'
			                          THEN (SELECT team FROM sides
			                                 WHERE place = 1 AND sharing = 1)
			                     END
			 WHERE c.id = $1 AND c.status <> 'completed'
			   AND (SELECT count(*) FROM challenge_players
			         WHERE challenge_id = $1 AND state = 'played') >= $2
			 RETURNING c.winner_id, c.winner_team`,
			challengeID, models.MinChallengePlayers).Scan(&winner, &winningTeam)

		if errors.Is(err, pgx.ErrNoRows) {
			return nil // already settled, or too few people played
		}
		if err != nil {
			return err
		}

		// A win is credited to the people who earned it: one player in a duel,
		// everybody on the side in a team match. Temporary players are left out
		// — the counter is on an account, and a guest has none to keep.
		switch {
		case winningTeam != nil:
			_, err = tx.Exec(ctx, `
				UPDATE users SET games_won = games_won + 1, updated_at = now()
				 WHERE NOT is_temporary AND id IN (
				     SELECT user_id FROM challenge_players
				      WHERE challenge_id = $1 AND team = $2 AND state = 'played')`,
				challengeID, *winningTeam)
			return err
		case winner != nil:
			_, err = tx.Exec(ctx, `
				UPDATE users SET games_won = games_won + 1, updated_at = now()
				 WHERE id = $1 AND NOT is_temporary`, *winner)
			return err
		}
		return nil // a draw
	})
}

// ------------------------------------------------------------ calling it off --

// CancelOutcome is what happened to a match somebody answered.
type CancelOutcome struct {
	// Cancelled reports that the match is over for everyone, not just for the
	// person who answered.
	Cancelled bool
	// ByHost distinguishes the host calling it off from the last invitation
	// being refused. The two read differently to the people who are told.
	ByHost bool
	// Freed is everyone who was still holding a place in it and has now been
	// let go — the people to tell, and the people who can join something else.
	Freed []uuid.UUID
}

// DeclineOrCancel is one person's answer to a match, and whatever follows from
// it, in one transaction.
//
// Three rules live here, and they live here together because they are decided
// from the same rows at the same moment. Splitting them across a read in Go and
// a write in SQL is what made the old version wrong: it counted the players
// from a snapshot taken when the page was rendered, so two people declining at
// once each saw the other as still present and neither closed the match.
//
//	the host says no      → the match is cancelled for everybody
//	the last invitee says no, leaving nobody who accepted
//	                      → the match is cancelled
//	somebody else says no → the match carries on without them
//
// "Nobody who accepted" is the exact test, and it is not the same as "fewer
// than two people left". An invitation nobody has answered is not a player; a
// match of a host and three unanswered invitations has one player in it, and
// the old count of three was counting envelopes.
//
// Cancelling frees everybody: every row still holding a place becomes
// eliminated, which is what releases the one-match-at-a-time index. Without
// that the people in a cancelled match stay locked out of every other one until
// the janitor runs.
func (r *Repo) DeclineOrCancel(ctx context.Context, challengeID, userID uuid.UUID) (CancelOutcome, error) {
	var out CancelOutcome

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// The row is locked first, so everything below is decided against one
		// state of the match rather than against whatever it was when the page
		// was drawn.
		var status string
		if err := tx.QueryRow(ctx,
			`SELECT status::text FROM challenges WHERE id = $1 FOR UPDATE`,
			challengeID).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != models.ChallengePending && status != models.ChallengeAccepted {
			// Already over. Saying no to a match that has ended is not an
			// error the person made, but there is nothing left to do.
			return ErrConflict
		}

		var isHost bool
		err := tx.QueryRow(ctx, `
			UPDATE challenge_players
			   SET state = 'declined'
			 WHERE challenge_id = $1 AND user_id = $2
			   AND state IN ('invited', 'joined')
			 RETURNING is_host`, challengeID, userID).Scan(&isHost)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		out.ByHost = isHost

		// Is there still a match here? Only if the host is in it and at least
		// one other person has actually accepted.
		var hostIn, accepted int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE is_host AND state IN ('joined', 'played')),
			       count(*) FILTER (WHERE NOT is_host AND state IN ('joined', 'played'))
			  FROM challenge_players WHERE challenge_id = $1`,
			challengeID).Scan(&hostIn, &accepted); err != nil {
			return err
		}

		// A match already under way is not undone by a late refusal: somebody
		// who has played has a score, and taking it away because a fourth
		// player said no would be rewriting a game that happened.
		var played int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM challenge_players
			  WHERE challenge_id = $1 AND state = 'played'`,
			challengeID).Scan(&played); err != nil {
			return err
		}

		if (hostIn > 0 && accepted > 0) || played >= models.MinChallengePlayers {
			return nil // it carries on
		}
		out.Cancelled = true

		// Everybody still holding a place is let go, and named so they can be
		// told.
		rows, err := tx.Query(ctx, `
			UPDATE challenge_players
			   SET state = 'eliminated'
			 WHERE challenge_id = $1 AND state IN ('invited', 'joined')
			 RETURNING user_id`, challengeID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			out.Freed = append(out.Freed, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE challenges
			   SET status = 'cancelled', cancelled_at = now(), cancelled_by = $2
			 WHERE id = $1 AND status IN ('pending', 'accepted')`,
			challengeID, userID)
		return err
	})
	if err != nil {
		return CancelOutcome{}, err
	}
	return out, nil
}

// CancelMatch is the host calling their own match off before it starts.
//
// It is the same operation as the host declining, and deliberately the same
// code: two ways of saying "this is not happening" that closed the match by
// different routes would be two sets of rules to keep in agreement.
func (r *Repo) CancelMatch(ctx context.Context, challengeID, hostID uuid.UUID) (CancelOutcome, error) {
	var isHost bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM challenge_players
		                WHERE challenge_id = $1 AND user_id = $2 AND is_host)`,
		challengeID, hostID).Scan(&isHost)
	if err != nil {
		return CancelOutcome{}, err
	}
	if !isHost {
		return CancelOutcome{}, ErrForbidden
	}
	return r.DeclineOrCancel(ctx, challengeID, hostID)
}

// ------------------------------------------------------ starting together --

// StartMatch opens a round for everybody who has accepted, at one moment.
//
// This is what makes a remote match a match rather than two people answering
// the same questions on different days. The host presses start; every player
// who said they were here gets their round in the same transaction, stamped
// with the same started_at, and nobody could have begun before it.
//
// It is refused if the match has already begun, so a second press — another
// tab, a retry, two hosts of a rematch chain — does not open a second round for
// anybody. The caller is handed the ids it created rounds for, which is who to
// tell.
func (r *Repo) StartMatch(ctx context.Context, challengeID, hostID uuid.UUID) ([]uuid.UUID, error) {
	var players []uuid.UUID

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var isHost, open bool
		var started *time.Time
		err := tx.QueryRow(ctx, `
			SELECT c.started_at,
			       c.status IN ('pending', 'accepted') AND c.expires_at > now(),
			       EXISTS (SELECT 1 FROM challenge_players p
			                WHERE p.challenge_id = c.id AND p.user_id = $2 AND p.is_host)
			  FROM challenges c WHERE c.id = $1
			 FOR UPDATE`, challengeID, hostID).Scan(&started, &open, &isHost)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !isHost {
			return ErrForbidden
		}
		if !open {
			return ErrConflict
		}
		if started != nil {
			// Already going. Not an error anybody made — the second press of a
			// button that worked the first time.
			return ErrAlreadyStarted
		}

		rows, err := tx.Query(ctx, `
			SELECT user_id FROM challenge_players
			 WHERE challenge_id = $1 AND state = 'joined'
			 ORDER BY is_host DESC, invited_at`, challengeID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			players = append(players, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(players) < models.MinChallengePlayers {
			return ErrNotEnoughPlayers
		}

		_, err = tx.Exec(ctx, `
			UPDATE challenges SET started_at = now(), status = 'accepted'
			 WHERE id = $1 AND started_at IS NULL`, challengeID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return players, nil
}

// MatchStarted reports whether the host has set a match going.
func (r *Repo) MatchStarted(ctx context.Context, challengeID uuid.UUID) (bool, error) {
	var started *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT started_at FROM challenges WHERE id = $1`, challengeID).Scan(&started)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return started != nil, err
}

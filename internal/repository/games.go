package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bilelzarai/siraj/internal/models"
)

const gameColumns = `
	g.id, g.user_id, g.mode::text, g.status::text, g.category_id, g.difficulty,
	g.locale, g.question_ids, g.cursor, g.total_questions, g.correct_count,
	g.best_streak, g.score, g.xp_earned, g.duration_ms, g.challenge_id,
	g.started_at, g.finished_at, g.served_at, g.elapsed_ms, g.deadline_at,
	COALESCE(ct.name, cfb.name, c.slug, ''), COALESCE(c.icon, ''),
	g.domain_id, COALESCE(dt.name, dfb.name, d.slug, ''), COALESCE(d.icon, '')`

// The domain is joined the same way the category is, and for the same reason:
// a finished round has to read back as what it was drawn from, whatever has
// happened to the taxonomy since — including a domain that was retired after
// the round was played.
const gameJoins = `
	  LEFT JOIN categories c ON c.id = g.category_id
	  LEFT JOIN category_translations ct  ON ct.category_id  = c.id AND ct.locale  = $1
	  LEFT JOIN category_translations cfb ON cfb.category_id = c.id AND cfb.locale = 'ar'
	  LEFT JOIN domains d ON d.id = g.domain_id
	  LEFT JOIN domain_translations dt  ON dt.domain_id  = d.id AND dt.locale  = $1
	  LEFT JOIN domain_translations dfb ON dfb.domain_id = d.id AND dfb.locale = 'ar'`

func scanGame(row pgx.Row) (*models.GameSession, error) {
	var g models.GameSession
	err := row.Scan(&g.ID, &g.UserID, &g.Mode, &g.Status, &g.CategoryID,
		&g.Difficulty, &g.Locale, &g.QuestionIDs, &g.Cursor, &g.TotalQuestions,
		&g.CorrectCount, &g.BestStreak, &g.Score, &g.XPEarned, &g.DurationMS,
		&g.ChallengeID, &g.StartedAt, &g.FinishedAt, &g.ServedAt, &g.ElapsedMS,
		&g.DeadlineAt, &g.CategoryName, &g.CategoryIcon,
		&g.DomainID, &g.DomainName, &g.DomainIcon)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// CreateGame starts a round. The partial unique index on (user_id) WHERE
// status='active' means a second concurrent round fails loudly instead of
// silently forking a player's progress.
func (r *Repo) CreateGame(ctx context.Context, g *models.GameSession) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO game_sessions
			(user_id, mode, category_id, domain_id, difficulty, locale,
			 question_ids, total_questions, challenge_id)
		VALUES ($1, $2::game_mode, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, started_at`,
		g.UserID, g.Mode, g.CategoryID, g.DomainID, g.Difficulty, g.Locale,
		g.QuestionIDs, g.TotalQuestions, g.ChallengeID,
	).Scan(&g.ID, &g.StartedAt)

	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (r *Repo) Game(ctx context.Context, id uuid.UUID, locale string) (*models.GameSession, error) {
	return scanGame(r.pool.QueryRow(ctx,
		`SELECT `+gameColumns+` FROM game_sessions g`+gameJoins+` WHERE g.id = $2`,
		locale, id))
}

// ActiveGame returns the player's in-progress round, or ErrNotFound.
func (r *Repo) ActiveGame(ctx context.Context, userID uuid.UUID, locale string) (*models.GameSession, error) {
	return scanGame(r.pool.QueryRow(ctx,
		`SELECT `+gameColumns+` FROM game_sessions g`+gameJoins+`
		  WHERE g.user_id = $2 AND g.status = 'active'`, locale, userID))
}

// RoundInMatch is the round this player already has in this match, if any.
//
// A match is one attempt at its questions, so this is asked before another is
// started: pressing accept twice used to abandon the first round and open a
// second, throwing away whatever had been answered in it.
func (r *Repo) RoundInMatch(ctx context.Context, userID, challengeID uuid.UUID, locale string) (*models.GameSession, error) {
	return scanGame(r.pool.QueryRow(ctx,
		`SELECT `+gameColumns+` FROM game_sessions g`+gameJoins+`
		  WHERE g.user_id = $2 AND g.challenge_id = $3`, locale, userID, challengeID))
}

// ResumableGame is the round the home screen offers to pick up again.
//
// Solo only. A challenge round is played against people who are not waiting —
// its questions expire on their own — and the daily is one attempt. Offering
// either of those as "carry on where you left off" would be a promise the rules
// do not keep.
func (r *Repo) ResumableGame(ctx context.Context, userID uuid.UUID, locale string) (*models.GameSession, error) {
	return scanGame(r.pool.QueryRow(ctx,
		`SELECT `+gameColumns+` FROM game_sessions g`+gameJoins+`
		  WHERE g.user_id = $2 AND g.status = 'active' AND g.mode = 'solo'`, locale, userID))
}

// MarkQuestionServed stamps when the current question started being read, and
// returns that instant.
//
// COALESCE, not an unconditional write: re-rendering the page must not move the
// clock, or a player could refresh just before answering and collect the full
// speed bonus every time. It is only ever NULL here because the page was
// closed and its time banked into elapsed_ms, which is the one case where a
// fresh stamp is right — the new stamp times this visit, and the banked time
// is still counted on top. RecordAnswer clears both as the round advances, so
// each position gets its own clock.
// MarkQuestionServed stamps when the question at the cursor was first shown,
// and — in a round whose questions expire — when it stops being answerable.
//
// timeoutMS of zero means no deadline at all, which is a solo round: it waits
// as long as the player needs and is still there tomorrow. Anything else sets a
// deadline once and never moves it, so leaving the page and coming back finds
// the same question with less time on it, or gone.
func (r *Repo) MarkQuestionServed(ctx context.Context, sessionID uuid.UUID, timeoutMS int) (time.Time, *time.Time, error) {
	var servedAt time.Time
	var deadline *time.Time
	err := r.pool.QueryRow(ctx, `
		UPDATE game_sessions
		   SET served_at   = COALESCE(served_at, now()),
		       deadline_at = CASE WHEN $2 > 0
		                          THEN COALESCE(deadline_at, now() + ($2 || ' milliseconds')::interval)
		                          ELSE NULL END
		 WHERE id = $1 AND status = 'active'
		 RETURNING served_at, deadline_at`, sessionID, timeoutMS).Scan(&servedAt, &deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil, ErrNotFound
	}
	return servedAt, deadline, err
}

// LapseQuestion writes off the question at the cursor as unanswered and moves
// the round on.
//
// This is what "not submitted before leaving or timing out" costs: the answer
// row exists, so the review screen shows the question and what the answer was,
// with nothing chosen. It is guarded on the position so two tabs noticing the
// same lapsed question cannot spend two questions on it.
func (r *Repo) LapseQuestion(ctx context.Context, sessionID uuid.UUID, questionID, position, timeMS int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `
			INSERT INTO game_answers
				(session_id, question_id, position, selected_index, is_correct, time_ms, points_awarded)
			SELECT $1, $2, $3, -1, false, $4, 0
			 WHERE EXISTS (SELECT 1 FROM game_sessions
			                WHERE id = $1 AND status = 'active' AND cursor = $3)
			ON CONFLICT (session_id, position) DO NOTHING`,
			sessionID, questionID, position, timeMS)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrConflict
		}

		_, err = tx.Exec(ctx, `
			UPDATE game_sessions
			   SET cursor      = cursor + 1,
			       duration_ms = duration_ms + $2,
			       served_at   = NULL,
			       elapsed_ms  = 0,
			       deadline_at = NULL
			 WHERE id = $1 AND status = 'active' AND cursor = $3`,
			sessionID, timeMS, position)
		return err
	})
}

// BankQuestionClock stops the clock on the current question, adding the time
// since it was served to what was already banked. It returns the new total.
//
// position guards against a stale call: the page is closed as part of moving
// to the next question too, and that report must not land on the question
// after it. Once the cursor has moved on, there is nothing to bank.
//
// The amount comes from the server's own served_at, never from the caller, so
// closing the page cannot be used to claim a better time than was taken.
func (r *Repo) BankQuestionClock(ctx context.Context, sessionID uuid.UUID, position int) (int, error) {
	var elapsed int
	err := r.pool.QueryRow(ctx, `
		UPDATE game_sessions
		   SET elapsed_ms = elapsed_ms
		                  + GREATEST(0, (EXTRACT(EPOCH FROM (now() - served_at)) * 1000)::int),
		       served_at  = NULL
		 WHERE id = $1 AND status = 'active'
		   AND cursor = $2 AND served_at IS NOT NULL
		 RETURNING elapsed_ms`, sessionID, position).Scan(&elapsed)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already banked, already answered, or the round has moved on. All
		// three mean there is no clock here to stop.
		return 0, ErrNotFound
	}
	return elapsed, err
}

// RecordAnswer writes the answer and advances the round counters atomically,
// refusing a duplicate submission for the same position.
func (r *Repo) RecordAnswer(ctx context.Context, sessionID uuid.UUID, a *models.GameAnswer, newStreak int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO game_answers
				(session_id, question_id, position, selected_index, is_correct, time_ms, points_awarded)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			sessionID, a.QuestionID, a.Position, a.SelectedIndex, a.IsCorrect,
			a.TimeMS, a.PointsAwarded)
		if err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE game_sessions
			   SET cursor        = cursor + 1,
			       correct_count = correct_count + CASE WHEN $2 THEN 1 ELSE 0 END,
			       score         = score + $3,
			       best_streak   = GREATEST(best_streak, $4),
			       duration_ms   = duration_ms + $5,
			       -- The next question has not been shown yet, so its clock
			       -- has not started and has nothing banked against it.
			       served_at     = NULL,
			       elapsed_ms    = 0,
			       deadline_at   = NULL
			 WHERE id = $1 AND status = 'active'`,
			sessionID, a.IsCorrect, a.PointsAwarded, newStreak, a.TimeMS)
		return err
	})
}

// CurrentStreak counts trailing correct answers, which is cheaper than
// carrying mutable streak state around.
func (r *Repo) CurrentStreak(ctx context.Context, sessionID uuid.UUID) (int, error) {
	var streak int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		  FROM (
			SELECT is_correct,
			       bool_and(is_correct) OVER (ORDER BY position DESC
			                                  ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS still
			  FROM game_answers WHERE session_id = $1
		  ) s
		 WHERE s.still`, sessionID).Scan(&streak)
	return streak, err
}

// FinishGame closes the round, credits the player, and returns the round
// resolved for locale — which the caller has to pass, because the reread used
// to be hardcoded to Arabic and handed back a session whose category name was
// in the wrong language.
//
// One transaction, because these two writes are one fact. Separately, a failure
// between them left the round closed and the experience unpaid — and finishing
// is idempotent, so the retry reported the stored outcome and never noticed the
// debt. The player had no way to ask for it again.
func (r *Repo) FinishGame(ctx context.Context, sessionID uuid.UUID, xpEarned int,
	locale string, userID uuid.UUID, coins, streak int) (*models.GameSession, error) {

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `
			UPDATE game_sessions
			   SET status = 'finished', finished_at = now(), xp_earned = $2
			 WHERE id = $1 AND status = 'active'`, sessionID, xpEarned)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			// Already finished by another request. Nothing to close and, more
			// to the point, nothing to pay for a second time.
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE users
			   SET xp           = xp + $2,
			       coins        = coins + $3,
			       games_played = games_played + 1,
			       best_streak  = GREATEST(best_streak, $4),
			       updated_at   = now()
			 WHERE id = $1`, userID, xpEarned, coins, streak)
		return err
	})
	if err != nil {
		return nil, err
	}
	return r.Game(ctx, sessionID, locale)
}

func (r *Repo) AbandonGame(ctx context.Context, sessionID uuid.UUID, userID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE game_sessions
		   SET status = 'abandoned', finished_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'active'`, sessionID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GameHistory lists a player's finished rounds, newest first.
func (r *Repo) GameHistory(ctx context.Context, userID uuid.UUID, locale, mode string, limit, offset int) ([]*models.GameSession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+gameColumns+`
		  FROM game_sessions g`+gameJoins+`
		 WHERE g.user_id = $2
		   AND g.status <> 'active'
		   AND ($3 = '' OR g.mode::text = $3)
		 ORDER BY g.started_at DESC
		 LIMIT $4 OFFSET $5`, locale, userID, mode, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// scanGame, not a second copy of the column list: this selects exactly
	// gameColumns, and the inline copy that used to be here silently stopped
	// matching the moment a column was added — every page of history 500ing
	// until someone noticed.
	var out []*models.GameSession
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Repo) CountGameHistory(ctx context.Context, userID uuid.UUID, mode string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM game_sessions
		 WHERE user_id = $1 AND status <> 'active' AND ($2 = '' OR mode::text = $2)`,
		userID, mode).Scan(&n)
	return n, err
}

// GameAnswers hydrates a finished round for the review screen.
func (r *Repo) GameAnswers(ctx context.Context, sessionID uuid.UUID, locale string) ([]*models.GameAnswer, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.session_id, a.question_id, a.position, a.selected_index,
		       a.is_correct, a.time_ms, a.points_awarded, a.answered_at,
		       COALESCE(t.prompt, fb.prompt, any_t.prompt, ''),
		       COALESCE(t.choices, fb.choices, any_t.choices, ARRAY[]::text[]),
		       COALESCE(t.explanation, fb.explanation, any_t.explanation, ''),
		       q.correct_index
		  FROM game_answers a
		  JOIN questions q ON q.id = a.question_id
		  LEFT JOIN question_translations t  ON t.question_id  = q.id AND t.locale  = $2 AND NOT t.needs_review
		  LEFT JOIN question_translations fb ON fb.question_id = q.id AND fb.locale = 'ar' AND NOT fb.needs_review
		  -- Whatever language the question does have. Two fallbacks were not
		  -- enough: a question approved only in English, reviewed by a player
		  -- reading French, left every COALESCE NULL — and NULL into a string
		  -- is a scan error, which is a 500 on the whole review page.
		  LEFT JOIN LATERAL (
		        SELECT prompt, choices, explanation
		          FROM question_translations
		         WHERE question_id = q.id AND NOT needs_review
		         ORDER BY locale LIMIT 1
		  ) any_t ON true
		 WHERE a.session_id = $1
		 ORDER BY a.position`, sessionID, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.GameAnswer
	for rows.Next() {
		var a models.GameAnswer
		if err := rows.Scan(&a.ID, &a.SessionID, &a.QuestionID, &a.Position,
			&a.SelectedIndex, &a.IsCorrect, &a.TimeMS, &a.PointsAwarded,
			&a.AnsweredAt, &a.Prompt, &a.Choices, &a.Explanation,
			&a.CorrectIndex); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// RecentActivity feeds the dashboard panel.
func (r *Repo) RecentActivity(ctx context.Context, userID uuid.UUID, locale string, limit int) ([]*models.GameSession, error) {
	return r.GameHistory(ctx, userID, locale, "", limit, 0)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// ------------------------------------------------- rounds played on one device --

// DeviceSessions loads the live rounds of a set of players inside one match.
//
// A match played round a single phone is several rounds, not one: each person
// answers for themselves, keeps their own clock and gets their own score. What
// makes it a hot seat is only the order they are served in, and that order is
// decided from these rows.
func (r *Repo) DeviceSessions(ctx context.Context, challengeID uuid.UUID, players []uuid.UUID, locale string) ([]*models.GameSession, error) {
	if len(players) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+gameColumns+` FROM game_sessions g`+gameJoins+`
		  WHERE g.challenge_id = $2 AND g.user_id = ANY($3) AND g.status = 'active'`,
		locale, challengeID, players)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.GameSession
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// MatchOf is the match a player's live round belongs to, if it belongs to one.
func (r *Repo) MatchOf(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var id *uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT challenge_id FROM game_sessions
		 WHERE user_id = $1 AND status = 'active'`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && id == nil) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	return *id, nil
}

// AnswersAt is what each of these rounds answered at one position.
//
// It is what the reveal is drawn from: once everybody at the device has
// answered a question, the screen can say what the answer was and who got it,
// which is the whole reason the verdict was held back until then.
func (r *Repo) AnswersAt(ctx context.Context, sessionIDs []uuid.UUID, position int) (map[uuid.UUID]*models.GameAnswer, error) {
	out := make(map[uuid.UUID]*models.GameAnswer, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT session_id, question_id, position, selected_index, is_correct,
		       time_ms, points_awarded
		  FROM game_answers
		 WHERE session_id = ANY($1) AND position = $2`, sessionIDs, position)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID uuid.UUID
		var a models.GameAnswer
		if err := rows.Scan(&sessionID, &a.QuestionID, &a.Position,
			&a.SelectedIndex, &a.IsCorrect, &a.TimeMS, &a.PointsAwarded); err != nil {
			return nil, err
		}
		out[sessionID] = &a
	}
	return out, rows.Err()
}

// isCheckViolation reports a named CHECK constraint refusing a write, so a
// caller can map one rule to one message instead of turning every database
// refusal into a server error.
func isCheckViolation(err error, name string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23514" && pgErr.ConstraintName == name
}

package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// The overview screen's reporting queries.
//
// PlatformStats in admin.go answers "how much of each thing is there", which is
// one round trip of counts. Everything here answers "and is that better or
// worse than it was", which needs a window and an aggregate over answers — a
// different cost and a different cadence, so it lives apart and is read only by
// the screen that draws it.

// dashboardWindow is how many days of play history the chart carries. Fourteen
// because that is two weeks: long enough that a quiet weekend is visibly a
// weekend rather than a decline.
const dashboardWindow = 14

// DashboardInsight reports play over time, the categories carrying it, and the
// languages the players are in.
//
// Five queries rather than one: they group by different things over different
// windows, and a single statement joining answers to sessions to users to
// ratings would multiply its own rows before it could count any of them.
func (r *Repo) DashboardInsight(ctx context.Context, locale string) (*models.DashboardInsight, error) {
	d := &models.DashboardInsight{}

	// The series is generated from the calendar and left-joined to play, not
	// grouped out of the sessions table: a day nobody finished a round has no
	// row to group, and a chart that skips those days reports a quiet Tuesday
	// as if it never happened.
	rows, err := r.pool.Query(ctx, `
		SELECT day::date, count(gs.id)::int
		  FROM generate_series(
		           (now() AT TIME ZONE 'UTC')::date - ($1::int - 1),
		           (now() AT TIME ZONE 'UTC')::date,
		           interval '1 day') AS day
		  LEFT JOIN game_sessions gs
		         ON gs.finished_at IS NOT NULL
		        AND (gs.finished_at AT TIME ZONE 'UTC')::date = day::date
		 GROUP BY day
		 ORDER BY day`, dashboardWindow)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day models.DayGames
		if err := rows.Scan(&day.Day, &day.Games); err != nil {
			rows.Close()
			return nil, err
		}
		d.Days = append(d.Days, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Today against yesterday, and this week's active players against the week
	// before. One row, because a delta whose two halves came from two
	// statements can straddle midnight and report a drop that is a clock.
	if err := r.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (
		    WHERE (gs.finished_at AT TIME ZONE 'UTC')::date = (now() AT TIME ZONE 'UTC')::date)::int,
		  count(*) FILTER (
		    WHERE (gs.finished_at AT TIME ZONE 'UTC')::date = (now() AT TIME ZONE 'UTC')::date - 1)::int,
		  count(DISTINCT gs.user_id) FILTER (WHERE gs.finished_at > now() - interval '7 days')::int,
		  count(DISTINCT gs.user_id) FILTER (
		    WHERE gs.finished_at > now() - interval '14 days'
		      AND gs.finished_at <= now() - interval '7 days')::int
		  FROM game_sessions gs
		 WHERE gs.finished_at IS NOT NULL
		   AND gs.finished_at > now() - interval '15 days'`).Scan(
		&d.GamesToday, &d.GamesYesterday, &d.ActivePlayers, &d.ActivePlayersPrior); err != nil {
		return nil, err
	}

	// Accuracy, this week against last. Counted over answers rather than over
	// rounds: a round's score depends on how many questions it held, and the
	// dashboard is reporting how well players are doing, not how long they play.
	if err := r.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE ga.answered_at > now() - interval '7 days')::int,
		  count(*) FILTER (WHERE ga.answered_at > now() - interval '7 days' AND ga.is_correct)::int,
		  count(*) FILTER (WHERE ga.answered_at > now() - interval '14 days'
		                     AND ga.answered_at <= now() - interval '7 days')::int,
		  count(*) FILTER (WHERE ga.answered_at > now() - interval '14 days'
		                     AND ga.answered_at <= now() - interval '7 days'
		                     AND ga.is_correct)::int
		  FROM game_answers ga
		 WHERE ga.answered_at > now() - interval '14 days'`).Scan(
		&d.AnswersThisWeek, &d.CorrectThisWeek,
		&d.AnswersPriorWeek, &d.CorrectPriorWeek); err != nil {
		return nil, err
	}

	if d.Categories, err = r.categoryPerformance(ctx, locale); err != nil {
		return nil, err
	}
	if d.Languages, err = r.playersByLanguage(ctx); err != nil {
		return nil, err
	}

	// The oldest thing waiting turns a count into a priority: twelve
	// translations submitted this morning and twelve that have been sitting for
	// a fortnight are the same number and not the same problem.
	//
	// Dated by the question rather than the translation. question_translations
	// has carried needs_review since 0003 with no timestamp beside it, and the
	// question's updated_at is touched by the same write that sets the flag —
	// so for a row still awaiting review it is when that text was last written,
	// which is the fact the screen is reporting.
	var oldest *time.Time
	if err := r.pool.QueryRow(ctx, `
		SELECT min(q.updated_at)
		  FROM question_translations qt
		  JOIN questions q ON q.id = qt.question_id
		 WHERE qt.needs_review AND q.author_id IS NULL`).Scan(&oldest); err != nil {
		return nil, err
	}
	d.OldestPendingReview = oldest

	return d, nil
}

// categoryPerformance ranks the live categories by how much they are played
// today, with how well and how they are rated beside it.
//
// The two aggregates are computed in subqueries and joined, not rolled into one
// GROUP BY: answers and ratings are independent sets hanging off the same
// questions, and joining both before counting either multiplies every answer by
// every rating.
func (r *Repo) categoryPerformance(ctx context.Context, locale string) ([]models.CategoryPerformance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, COALESCE(ct.name, c.slug), c.icon, c.color,
		       COALESCE(a.plays, 0), COALESCE(a.answered, 0), COALESCE(a.correct, 0),
		       COALESCE(v.rating, 0), COALESCE(v.votes, 0)
		  FROM categories c
		  LEFT JOIN category_translations ct
		         ON ct.category_id = c.id AND ct.locale = $1
		  LEFT JOIN LATERAL (
		        SELECT count(DISTINCT ga.session_id)::int AS plays,
		               count(*)::int                     AS answered,
		               count(*) FILTER (WHERE ga.is_correct)::int AS correct
		          FROM game_answers ga
		          JOIN questions q ON q.id = ga.question_id
		         WHERE q.category_id = c.id
		           AND ga.answered_at > now() - interval '1 day'
		  ) a ON true
		  LEFT JOIN LATERAL (
		        SELECT avg(qr.stars)::float8 AS rating, count(*)::int AS votes
		          FROM question_ratings qr
		          JOIN questions q ON q.id = qr.question_id
		         WHERE q.category_id = c.id
		  ) v ON true
		 WHERE c.is_active
		 ORDER BY COALESCE(a.answered, 0) DESC, c.sort_order, c.id`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.CategoryPerformance
	for rows.Next() {
		var c models.CategoryPerformance
		if err := rows.Scan(&c.CategoryID, &c.CategoryName, &c.CategoryIcon,
			&c.CategoryColor, &c.Plays, &c.Answered, &c.Correct,
			&c.Rating, &c.RatingVotes); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// playersByLanguage is the split of accounts across the shipped interface
// languages, largest first.
//
// Temporary players are left out. A guest inherits the locale of the device
// that created it, so counting them would report the host's language once per
// person who ever sat down at their phone.
func (r *Repo) playersByLanguage(ctx context.Context) ([]models.LanguageShare, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT locale, count(*)::int
		  FROM users
		 WHERE role = 'player' AND NOT is_temporary
		 GROUP BY locale
		 ORDER BY count(*) DESC, locale`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.LanguageShare
	total := 0
	for rows.Next() {
		var l models.LanguageShare
		if err := rows.Scan(&l.Locale, &l.Players); err != nil {
			return nil, err
		}
		total += l.Players
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The share is computed here rather than in SQL so the rounding is done
	// once, against the same total the rows were counted from.
	for i := range out {
		if total > 0 {
			out[i].Percent = out[i].Players * 100 / total
		}
	}
	return out, nil
}

// PlayerStrengths is the categories one player answers best, strongest first.
//
// Thin evidence is excluded rather than ranked: two right answers out of two is
// 100% and says nothing, and it would outrank a category the player has
// genuinely learned. The floor is passed in so the caller can say what counts.
func (r *Repo) PlayerStrengths(ctx context.Context, userID uuid.UUID, locale string,
	minAnswers, limit int) ([]models.CategoryStrength, error) {

	rows, err := r.pool.Query(ctx, `
		SELECT c.id, COALESCE(ct.name, c.slug), c.icon,
		       count(*)::int, count(*) FILTER (WHERE ga.is_correct)::int
		  FROM game_answers ga
		  JOIN game_sessions gs ON gs.id = ga.session_id
		  JOIN questions q      ON q.id  = ga.question_id
		  JOIN categories c     ON c.id  = q.category_id
		  LEFT JOIN category_translations ct
		         ON ct.category_id = c.id AND ct.locale = $2
		 WHERE gs.user_id = $1
		 GROUP BY c.id, ct.name, c.slug, c.icon
		HAVING count(*) >= $3
		 ORDER BY (count(*) FILTER (WHERE ga.is_correct))::float8 / count(*) DESC,
		          count(*) DESC
		 LIMIT $4`, userID, locale, minAnswers, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.CategoryStrength
	for rows.Next() {
		var c models.CategoryStrength
		if err := rows.Scan(&c.CategoryID, &c.CategoryName, &c.CategoryIcon,
			&c.Answered, &c.Correct); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------- content health --

// LibraryFacts is the shape of the bank, for the panel beside the score: what
// is in it, in how many languages, and when it last changed.
type LibraryFacts struct {
	Questions, Live, Retired, InReview int
	Categories, LiveCategories         int
	LastImport                         *time.Time
	LastImportFile                     string
}

// Library reads those facts in one round trip. They are counts of the whole
// bank rather than of a page, which is the point of them: this screen is the
// one that answers "is there enough, and is it sound".
func (r *Repo) Library(ctx context.Context) (LibraryFacts, error) {
	var l LibraryFacts
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE is_active)::int,
		       count(*) FILTER (WHERE NOT is_active)::int,
		       count(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM question_translations t
		            WHERE t.question_id = q.id AND t.needs_review))::int
		  FROM questions q
		 WHERE q.author_id IS NULL`).Scan(
		&l.Questions, &l.Live, &l.Retired, &l.InReview); err != nil {
		return l, err
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT count(*)::int, count(*) FILTER (WHERE is_active)::int
		  FROM categories`).Scan(&l.Categories, &l.LiveCategories); err != nil {
		return l, err
	}
	// The last import, which is usually the answer to "what changed?". Absent
	// on a bank that has only ever been seeded.
	var at *time.Time
	var file *string
	// A bank that has only ever been seeded has no import, which is not an
	// error — it is the common case on a fresh deployment.
	if err := r.pool.QueryRow(ctx, `
		SELECT created_at, filename FROM question_imports
		 WHERE committed ORDER BY created_at DESC LIMIT 1`).Scan(&at, &file); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return l, err
		}
	}
	l.LastImport = at
	if file != nil {
		l.LastImportFile = *file
	}
	return l, nil
}

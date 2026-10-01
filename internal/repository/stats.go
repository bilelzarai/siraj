package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// Stats assembles the profile dashboard in a handful of queries rather than
// one wide join, which keeps each of them indexable.
func (r *Repo) Stats(ctx context.Context, userID uuid.UUID, locale string) (*models.UserStats, error) {
	s := &models.UserStats{}

	err := r.pool.QueryRow(ctx, `
		SELECT
			count(*)                                             AS games,
			COALESCE(sum(score), 0)                              AS total_score,
			COALESCE(sum(correct_count), 0)                      AS total_correct,
			COALESCE(sum(total_questions), 0)                    AS total_questions,
			COALESCE(max(best_streak), 0)                        AS best_streak,
			count(*) FILTER (WHERE correct_count = total_questions
			                   AND total_questions > 0)          AS perfect
		  FROM game_sessions
		 WHERE user_id = $1 AND status = 'finished'`, userID,
	).Scan(&s.GamesPlayed, &s.TotalScore, &s.TotalCorrect, &s.TotalQuestions,
		&s.BestStreak, &s.PerfectRounds)
	if err != nil {
		return nil, err
	}

	if s.TotalQuestions > 0 {
		s.AvgAccuracy = s.TotalCorrect * 100 / s.TotalQuestions
	}

	if s.ChallengesWon, err = r.ChallengeWins(ctx, userID); err != nil {
		return nil, err
	}

	if s.FriendCount, err = r.FriendCount(ctx, userID); err != nil {
		return nil, err
	}

	if s.ByCategory, err = r.categoryBreakdown(ctx, userID, locale); err != nil {
		return nil, err
	}
	if s.Last7Days, err = r.dailyActivity(ctx, userID); err != nil {
		return nil, err
	}
	return s, nil
}

// categoryBreakdown reports accuracy per category, derived from the answers
// themselves so mixed-category rounds are attributed correctly.
func (r *Repo) categoryBreakdown(ctx context.Context, userID uuid.UUID, locale string) ([]models.CategoryStat, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id,
		       COALESCE(ct.name, cfb.name, c.slug),
		       c.icon, c.color,
		       count(DISTINCT a.session_id)              AS played,
		       count(*) FILTER (WHERE a.is_correct)      AS correct,
		       count(*)                                  AS total
		  FROM game_answers a
		  JOIN game_sessions g ON g.id = a.session_id
		  JOIN questions q     ON q.id = a.question_id
		  JOIN categories c    ON c.id = q.category_id
		  LEFT JOIN category_translations ct  ON ct.category_id  = c.id AND ct.locale  = $2
		  LEFT JOIN category_translations cfb ON cfb.category_id = c.id AND cfb.locale = 'ar'
		 WHERE g.user_id = $1 AND g.status = 'finished'
		 GROUP BY c.id, ct.name, cfb.name, c.slug, c.icon, c.color, c.sort_order
		 ORDER BY c.sort_order`, userID, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.CategoryStat
	for rows.Next() {
		var cs models.CategoryStat
		if err := rows.Scan(&cs.CategoryID, &cs.CategoryName, &cs.CategoryIcon,
			&cs.Color, &cs.Played, &cs.Correct, &cs.Total); err != nil {
			return nil, err
		}
		if cs.Total > 0 {
			cs.Accuracy = cs.Correct * 100 / cs.Total
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

// dailyActivity returns exactly seven rows, one per day, zero-filled.
func (r *Repo) dailyActivity(ctx context.Context, userID uuid.UUID) ([]models.DayStat, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d::date,
		       COALESCE(g.games, 0), COALESCE(g.score, 0), COALESCE(g.correct, 0)
		  FROM generate_series(
		           (now() - interval '6 days')::date, now()::date, interval '1 day') d
		  LEFT JOIN (
		        SELECT started_at::date AS day,
		               count(*)             AS games,
		               sum(score)           AS score,
		               sum(correct_count)   AS correct
		          FROM game_sessions
		         WHERE user_id = $1 AND status = 'finished'
		           AND started_at >= (now() - interval '7 days')
		         GROUP BY 1
		  ) g ON g.day = d::date
		 ORDER BY d`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.DayStat
	for rows.Next() {
		var ds models.DayStat
		if err := rows.Scan(&ds.Day, &ds.Games, &ds.Score, &ds.Correct); err != nil {
			return nil, err
		}
		out = append(out, ds)
	}
	return out, rows.Err()
}

// Leaderboard ranks by XP. scope "friends" restricts to the viewer's circle
// plus the viewer themself.
func (r *Repo) Leaderboard(ctx context.Context, viewerID uuid.UUID, scope string, limit int) ([]*models.LeaderboardEntry, error) {
	// The global filter still references $1 (with an explicit cast) because
	// Postgres cannot infer a parameter's type if it appears nowhere in the
	// statement, and the scope is chosen at runtime.
	// Accounts only, everywhere a person is ranked or counted. A temporary
	// player has no lasting XP and nobody to be compared with.
	filter := `NOT u.is_temporary AND $1::uuid IS NOT NULL`
	if scope == "friends" {
		filter = `NOT u.is_temporary AND (u.id = $1 OR EXISTS (
			SELECT 1 FROM friendships f
			 WHERE f.status = 'accepted'
			   AND ((f.requester_id = $1 AND f.addressee_id = u.id)
			     OR (f.addressee_id = $1 AND f.requester_id = u.id))))`
	}

	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country,
		       u.xp, u.last_seen_at, u.games_played, u.games_won,
		       COALESCE(acc.pct, 0)
		  FROM users u
		  LEFT JOIN LATERAL (
		        SELECT CASE WHEN sum(total_questions) > 0
		                    THEN sum(correct_count) * 100 / sum(total_questions)
		                    ELSE 0 END AS pct
		          FROM game_sessions WHERE user_id = u.id AND status = 'finished'
		  ) acc ON true
		 WHERE `+filter+`
		 ORDER BY u.xp DESC, u.games_won DESC, u.created_at ASC
		 LIMIT $2`, viewerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.LeaderboardEntry
	rank := 0
	for rows.Next() {
		var e models.LeaderboardEntry
		var c models.UserCard
		if err := rows.Scan(&c.ID, &c.Username, &c.DisplayName, &c.AvatarSeed,
			&c.Country, &c.XP, &c.LastSeenAt, &e.GamesPlayed, &e.GamesWon,
			&e.Accuracy); err != nil {
			return nil, err
		}
		rank++
		e.Rank = rank
		e.XP = c.XP
		e.User = &c
		out = append(out, &e)
	}
	return out, rows.Err()
}

// MyRank finds the viewer's position even when they fall off the page.
//
// The scope has to match the board being shown. It used to always be global, so
// the friends board said "your rank: 4,212" next to a list of six people.
func (r *Repo) MyRank(ctx context.Context, userID uuid.UUID, scope string) (int, error) {
	if scope == "friends" {
		var rank int
		err := r.pool.QueryRow(ctx, `
			SELECT count(*) + 1
			  FROM users u
			 WHERE NOT u.is_temporary
			   AND u.xp > (SELECT xp FROM users WHERE id = $1)
			   AND EXISTS (
			     SELECT 1 FROM friendships f
			      WHERE f.status = 'accepted'
			        AND ((f.requester_id = $1 AND f.addressee_id = u.id)
			          OR (f.addressee_id = $1 AND f.requester_id = u.id)))`,
			userID).Scan(&rank)
		return rank, err
	}

	var rank int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) + 1 FROM users
		 WHERE NOT is_temporary
		   AND xp > (SELECT xp FROM users WHERE id = $1)`, userID).Scan(&rank)
	return rank, err
}

// TotalPlayers powers the landing page counter.
func (r *Repo) TotalPlayers(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE NOT is_temporary`).Scan(&n)
	return n, err
}

// ------------------------------------------------------------------ badges --

// Badges lists every badge with the earned timestamp filled in where the
// user already has it, so the UI can show locked ones greyed out.
func (r *Repo) Badges(ctx context.Context, userID uuid.UUID, locale string) ([]*models.Badge, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT b.id, b.slug, b.icon, b.threshold, b.kind,
		       COALESCE(t.name, fb.name, b.slug),
		       COALESCE(t.description, fb.description, ''),
		       ub.earned_at
		  FROM badges b
		  LEFT JOIN badge_translations t  ON t.badge_id  = b.id AND t.locale  = $2
		  LEFT JOIN badge_translations fb ON fb.badge_id = b.id AND fb.locale = 'ar'
		  LEFT JOIN user_badges ub ON ub.badge_id = b.id AND ub.user_id = $1
		 ORDER BY b.id`, userID, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Badge
	for rows.Next() {
		var b models.Badge
		if err := rows.Scan(&b.ID, &b.Slug, &b.Icon, &b.Threshold, &b.Kind,
			&b.Name, &b.Description, &b.EarnedAt); err != nil {
			return nil, err
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

// BadgeDefinition is one badge's unlock rule as the database holds it.
type BadgeDefinition struct {
	Slug      string
	Kind      string // games | perfect | streak | xp | friends | duels
	Threshold int
}

// BadgeDefinitions returns the unlock rules.
//
// They live in the badges table and nowhere else. They used to be seeded here
// and re-stated as a hardcoded ladder in the service, which agreed by
// coincidence and had nothing keeping it that way.
func (r *Repo) BadgeDefinitions(ctx context.Context) ([]BadgeDefinition, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT slug, kind, threshold FROM badges ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BadgeDefinition
	for rows.Next() {
		var d BadgeDefinition
		if err := rows.Scan(&d.Slug, &d.Kind, &d.Threshold); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PerfectRoundCount is how many finished rounds the player got every question
// right in — the measure behind the flawless badge.
func (r *Repo) PerfectRoundCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM game_sessions
		 WHERE user_id = $1 AND status = 'finished'
		   AND total_questions > 0 AND correct_count = total_questions`,
		userID).Scan(&n)
	return n, err
}

// GrantBadges awards any badge slugs not already held and returns the ones
// that were newly granted, so the result screen can celebrate them.
func (r *Repo) GrantBadges(ctx context.Context, userID uuid.UUID, slugs []string, locale string) ([]*models.Badge, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		WITH granted AS (
			INSERT INTO user_badges (user_id, badge_id)
			SELECT $1, b.id FROM badges b WHERE b.slug = ANY($2)
			ON CONFLICT (user_id, badge_id) DO NOTHING
			RETURNING badge_id
		)
		SELECT b.id, b.slug, b.icon, b.threshold, b.kind,
		       COALESCE(t.name, fb.name, b.slug),
		       COALESCE(t.description, fb.description, '')
		  FROM granted g
		  JOIN badges b ON b.id = g.badge_id
		  LEFT JOIN badge_translations t  ON t.badge_id  = b.id AND t.locale  = $3
		  LEFT JOIN badge_translations fb ON fb.badge_id = b.id AND fb.locale = 'ar'`,
		userID, slugs, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Badge
	for rows.Next() {
		var b models.Badge
		if err := rows.Scan(&b.ID, &b.Slug, &b.Icon, &b.Threshold, &b.Kind,
			&b.Name, &b.Description); err != nil {
			return nil, err
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

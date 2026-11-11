// Package progress owns everything that survives a game: XP and levels, mastery per
// category and per domain, streaks and the daily goal, achievements and the leaderboards
// (LB-01…LB-03). It writes inside the game-finish transaction through games.FinishHook,
// so a finished game and its progress are never out of step.
package progress

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

type Service struct{ db *db.DB }

func NewService(d *db.DB) *Service { return &Service{db: d} }

// ---------------------------------------------------------------- levels

// XPForLevel is the XP a player needs to reach that level: 50 × (level − 1)².
// The same curve lives in AddUserXP so the cached users.level never drifts.
func XPForLevel(level int32) int32 {
	if level <= 1 {
		return 0
	}
	return 50 * (level - 1) * (level - 1)
}

// LevelForXP is the inverse, used by tests and by the shell.
func LevelForXP(xp int32) int32 {
	if xp < 0 {
		xp = 0
	}
	return 1 + int32(math.Floor(math.Sqrt(float64(xp)/50.0)))
}

// ---------------------------------------------------------------- the finish hook

// Finished is the part of games.FinishResult this module needs. Keeping it to an
// interface-free struct avoids importing the games package (and its cycle).
type Finished struct {
	GameID   uuid.UUID
	PlayerID uuid.UUID
	UserID   uuid.UUID
	Mode     string
	Score    int32
	Correct  int32
	Total    int32
	XP       int32
}

// Apply writes stats, XP, activity and the leaderboard for one player of a finished
// game. It runs on the transaction's queries, never on the pool.
func (s *Service) Apply(ctx context.Context, q *sqlc.Queries, f Finished) error {
	if f.UserID == uuid.Nil {
		return nil // a pass-the-device guest with a local name only (DV-06)
	}
	if err := q.ApplyCategoryStats(ctx, sqlc.ApplyCategoryStatsParams{UserID: f.UserID, GameID: f.GameID, PlayerID: f.PlayerID}); err != nil {
		return err
	}
	if err := q.ApplyDomainStats(ctx, sqlc.ApplyDomainStatsParams{UserID: f.UserID, GameID: f.GameID, PlayerID: f.PlayerID}); err != nil {
		return err
	}
	if err := q.RecomputeCategoryMastery(ctx, f.UserID); err != nil {
		return err
	}
	if err := q.RecomputeDomainMastery(ctx, f.UserID); err != nil {
		return err
	}
	if err := q.BumpDailyActivity(ctx, sqlc.BumpDailyActivityParams{UserID: f.UserID, Points: f.Score, Correct: f.Correct}); err != nil {
		return err
	}
	if f.XP > 0 {
		if err := q.AddXPEvent(ctx, sqlc.AddXPEventParams{
			UserID: f.UserID, Amount: f.XP, Reason: "game",
			GameID: uuid.NullUUID{UUID: f.GameID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := q.AddUserXP(ctx, sqlc.AddUserXPParams{Amount: f.XP, ID: f.UserID}); err != nil {
			return err
		}
	}
	// Leaderboards: the global board always, plus one board per domain played.
	if err := q.BumpLeaderboardWeekly(ctx, sqlc.BumpLeaderboardWeeklyParams{UserID: f.UserID, Points: f.Score}); err != nil {
		return err
	}
	byDomain, err := q.GamePointsByDomain(ctx, sqlc.GamePointsByDomainParams{GameID: f.GameID, PlayerID: f.PlayerID})
	if err != nil {
		return err
	}
	for _, d := range byDomain {
		if err := q.BumpLeaderboardWeeklyDomain(ctx, sqlc.BumpLeaderboardWeeklyDomainParams{
			DomainID: pgtype.Int8{Int64: d.DomainID, Valid: true}, UserID: f.UserID, Points: d.Points,
		}); err != nil {
			return err
		}
	}
	return s.applyAchievements(ctx, q, f)
}

// applyAchievements moves the counters the seeded achievements watch.
func (s *Service) applyAchievements(ctx context.Context, q *sqlc.Queries, f Finished) error {
	p, err := q.MyProgress(ctx, f.UserID)
	if err != nil {
		return err
	}
	set := func(code string, value int32) error {
		return q.UpsertAchievementProgress(ctx, sqlc.UpsertAchievementProgressParams{
			UserID: f.UserID, Progress: value, Code: code,
		})
	}
	if err := set("first_game", min32(p.GamesTotal, 1)); err != nil {
		return err
	}
	if err := set("games_50", p.GamesTotal); err != nil {
		return err
	}
	if err := set("streak_7", p.Streak); err != nil {
		return err
	}
	if err := set("streak_30", p.Streak); err != nil {
		return err
	}
	if f.Total > 0 && f.Correct == f.Total {
		if err := set("perfect_game", 1); err != nil {
			return err
		}
	}
	best, err := q.MasteryByDomain(ctx, sqlc.MasteryByDomainParams{
		Lang: "en", UserID: f.UserID, IslamicOnly: false, Q: "", Sort: "mastery", Lim: 1, Off: 0,
	})
	if err != nil {
		return err
	}
	if len(best) > 0 {
		return set("mastery_80", int32(best[0].Mastery))
	}
	return nil
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------- reads

// Summary is endpoint 90: the player card on Home and the head of the profile.
type Summary struct {
	XP          int32 `json:"xp"`
	Level       int32 `json:"level"`
	LevelXP     int32 `json:"level_xp"`      // XP at which this level started
	NextLevelXP int32 `json:"next_level_xp"` // XP needed for the next one
	Percent     int32 `json:"percent"`
	DailyGoal   int32 `json:"daily_goal"`
	GamesToday  int32 `json:"games_today"`
	PointsToday int32 `json:"points_today"`
	GamesTotal  int32 `json:"games_total"`
	PointsTotal int32 `json:"points_total"`
	Streak      int32 `json:"streak"`
	BestStreak  int32 `json:"best_streak"`
	DuelsWon    int32 `json:"duels_won"`
	DuelRating  int32 `json:"duel_rating"`
}

func (s *Service) Summary(ctx context.Context, userID uuid.UUID) (Summary, error) {
	row, err := s.db.Q.MyProgress(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Summary{Level: 1, DailyGoal: 5}, nil
		}
		return Summary{}, err
	}
	out := Summary{
		XP: row.Xp, Level: row.Level,
		LevelXP: XPForLevel(row.Level), NextLevelXP: XPForLevel(row.Level + 1),
		DailyGoal: row.DailyGoal, GamesToday: row.GamesToday, PointsToday: row.PointsToday,
		GamesTotal: row.GamesTotal, PointsTotal: row.PointsTotal, Streak: row.Streak,
		BestStreak: row.BestStreak, DuelsWon: row.DuelsWon,
	}
	if span := out.NextLevelXP - out.LevelXP; span > 0 {
		out.Percent = clamp32((out.XP-out.LevelXP)*100/span, 0, 100)
	}
	// The duel rating is the badge the design shows beside the level (DU-02).
	if r, err := s.db.Q.MyBestRating(ctx, userID); err == nil {
		out.DuelRating = r.Rating
	}
	return out, nil
}

func clamp32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Day is one cell of the 12-week activity grid (endpoint 94).
type Day struct {
	Day    time.Time `json:"day"`
	Games  int32     `json:"games"`
	Points int32     `json:"points"`
}

func (s *Service) Activity(ctx context.Context, userID uuid.UUID, days int32) ([]Day, error) {
	if days <= 0 || days > 400 {
		days = 84 // 12 weeks
	}
	rows, err := s.db.Q.MyActivity(ctx, sqlc.MyActivityParams{UserID: userID, Days: days})
	if err != nil {
		return nil, err
	}
	out := make([]Day, 0, len(rows))
	for _, r := range rows {
		out = append(out, Day{Day: r.Day.Time, Games: r.Games, Points: r.Points})
	}
	return out, nil
}

// DomainMastery is one row of the mastery list (endpoint 91).
type DomainMastery struct {
	ID            int64      `json:"id"`
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Family        string     `json:"family"`
	Color         string     `json:"color"`
	Icon          string     `json:"icon"`
	Motif         string     `json:"motif"`
	Mastery       int32      `json:"mastery"`
	GamesPlayed   int32      `json:"games_played"`
	Answered      int32      `json:"answered"`
	Correct       int32      `json:"correct"`
	CategoryCount int32      `json:"category_count"`
	LastPlayedAt  *time.Time `json:"last_played_at,omitempty"`
}

type MasteryInput struct {
	Who     *authctx.Principal
	Lang    string
	Q       string
	Sort    string // mastery | weakest | played | order
	Page    int
	PerPage int
}

func (s *Service) Mastery(ctx context.Context, in MasteryInput) ([]DomainMastery, int, error) {
	if in.Page < 1 {
		in.Page = 1
	}
	if in.PerPage < 1 {
		in.PerPage = 6
	}
	switch in.Sort {
	case "mastery", "weakest", "played", "":
	default:
		return nil, 0, apperr.Invalid(map[string]string{"sort": "mastery, weakest or played"})
	}
	rows, err := s.db.Q.MasteryByDomain(ctx, sqlc.MasteryByDomainParams{
		Lang: in.Lang, UserID: in.Who.ID, IslamicOnly: in.Who.IslamicOnly, Q: in.Q,
		Sort: in.Sort, Lim: int32(in.PerPage), Off: int32((in.Page - 1) * in.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountMasteryDomains(ctx, sqlc.CountMasteryDomainsParams{
		Lang: in.Lang, UserID: in.Who.ID, IslamicOnly: in.Who.IslamicOnly, Q: in.Q,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]DomainMastery, 0, len(rows))
	for _, r := range rows {
		m := DomainMastery{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Family: string(r.Family), Color: r.Color,
			Icon: r.Icon, Motif: r.Motif, Mastery: r.Mastery, GamesPlayed: r.GamesPlayed,
			Answered: r.Answered, Correct: r.Correct, CategoryCount: r.CategoryCount,
		}
		if r.LastPlayedAt.Valid {
			t := r.LastPlayedAt.Time
			m.LastPlayedAt = &t
		}
		out = append(out, m)
	}
	return out, int(total), nil
}

// CategoryMastery is endpoint 92: one domain, its categories and the viewer's bars.
type CategoryMastery struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Color         string `json:"color"`
	Icon          string `json:"icon"`
	Mastery       int32  `json:"mastery"`
	Answered      int32  `json:"answered"`
	Correct       int32  `json:"correct"`
	QuestionCount int32  `json:"question_count"`
}

func (s *Service) CategoryMastery(ctx context.Context, userID uuid.UUID, domainID int64, lang string) ([]CategoryMastery, error) {
	rows, err := s.db.Q.MasteryByCategory(ctx, sqlc.MasteryByCategoryParams{Lang: lang, UserID: userID, DomainID: domainID})
	if err != nil {
		return nil, err
	}
	out := make([]CategoryMastery, 0, len(rows))
	for _, r := range rows {
		out = append(out, CategoryMastery{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Color: r.Color, Icon: r.Icon,
			Mastery: r.Mastery, Answered: r.Answered, Correct: r.Correct, QuestionCount: r.QuestionCount,
		})
	}
	return out, nil
}

// TopCategories feeds "Strongest" and "Needs work" on the profile.
func (s *Service) TopCategories(ctx context.Context, who *authctx.Principal, lang string, weakest bool, limit int32) ([]sqlc.TopCategoriesForUserRow, error) {
	return s.db.Q.TopCategoriesForUser(ctx, sqlc.TopCategoriesForUserParams{
		Lang: lang, UserID: who.ID, IslamicOnly: who.IslamicOnly, Weakest: weakest, Lim: limit,
	})
}

// Achievement is endpoint 93.
type Achievement struct {
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	Color       string     `json:"color"`
	Target      int32      `json:"target"`
	Progress    int32      `json:"progress"`
	Percent     int32      `json:"percent"`
	UnlockedAt  *time.Time `json:"unlocked_at,omitempty"`
}

func (s *Service) Achievements(ctx context.Context, userID uuid.UUID, lang string) ([]Achievement, error) {
	rows, err := s.db.Q.MyAchievements(ctx, sqlc.MyAchievementsParams{Lang: lang, UserID: userID})
	if err != nil {
		return nil, err
	}
	out := make([]Achievement, 0, len(rows))
	for _, r := range rows {
		a := Achievement{
			Code: r.Code, Name: r.Name, Description: r.Description, Icon: r.Icon,
			Color: r.Color, Target: r.Target, Progress: r.Progress,
		}
		if a.Target > 0 {
			a.Percent = clamp32(a.Progress*100/a.Target, 0, 100)
		}
		if r.UnlockedAt.Valid {
			t := r.UnlockedAt.Time
			a.UnlockedAt = &t
		}
		out = append(out, a)
	}
	return out, nil
}

// ---------------------------------------------------------------- leaderboards

// LeaderInput is endpoint 95: a period, a board (global or one of the player's domains)
// and everyone or friends only.
type LeaderInput struct {
	Who      *authctx.Principal
	Period   string // week | month | all
	DomainID int64  // 0 = global
	Friends  bool
	Limit    int32
	Offset   int32
}

type Leader struct {
	UserID      uuid.UUID `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Level       int32     `json:"level"`
	IsGuest     bool      `json:"is_guest"`
	Points      int32     `json:"points"`
	Games       int32     `json:"games"`
	Rank        int32     `json:"rank"`
	IsMe        bool      `json:"is_me"`
}

type Standing struct {
	Rank   int32 `json:"rank"`
	Points int32 `json:"points"`
}

func validPeriod(p string) bool { return p == "week" || p == "month" || p == "all" }

func (s *Service) Leaderboard(ctx context.Context, in LeaderInput) ([]Leader, error) {
	if in.Period == "" {
		in.Period = "week"
	}
	if !validPeriod(in.Period) {
		return nil, apperr.Invalid(map[string]string{"period": "week, month or all"})
	}
	if in.Limit <= 0 || in.Limit > 100 {
		in.Limit = 20
	}
	var viewer uuid.UUID
	if in.Who != nil {
		viewer = in.Who.ID
	}
	rows, err := s.db.Q.Leaderboard(ctx, sqlc.LeaderboardParams{
		Period: in.Period, DomainID: in.DomainID, FriendsOnly: in.Friends,
		ViewerID: viewer, Lim: in.Limit, Off: in.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Leader, 0, len(rows))
	// Competition ranking: equal points share a rank, the next one skips.
	rank, prev := int32(in.Offset), int32(-1)
	for i, r := range rows {
		if r.Points != prev {
			rank = in.Offset + int32(i) + 1
			prev = r.Points
		}
		l := Leader{
			UserID: r.ID, DisplayName: r.DisplayName, Level: r.Level,
			IsGuest: r.Kind == sqlc.UserKindGuest, Points: r.Points, Games: r.Games,
			Rank: rank, IsMe: r.ID == viewer,
		}
		if r.Username.Valid {
			l.Username = r.Username.String
		}
		if r.AvatarUrl.Valid {
			l.AvatarURL = r.AvatarUrl.String
		}
		out = append(out, l)
	}
	return out, nil
}

// MyStanding is the "you" row pinned under the board.
func (s *Service) MyStanding(ctx context.Context, in LeaderInput) (Standing, error) {
	if in.Period == "" {
		in.Period = "week"
	}
	if in.Who == nil {
		return Standing{}, nil
	}
	row, err := s.db.Q.MyLeaderboardStanding(ctx, sqlc.MyLeaderboardStandingParams{
		Period: in.Period, DomainID: in.DomainID, ViewerID: in.Who.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Standing{}, nil
		}
		return Standing{}, err
	}
	return Standing{Rank: row.Rank, Points: row.Points}, nil
}

// PlayersThisWeek is the number on a domain page (DM-06).
func (s *Service) PlayersThisWeek(ctx context.Context, domainID int64) (int32, error) {
	return s.db.Q.DomainPlayersThisWeek(ctx, pgtype.Int8{Int64: domainID, Valid: true})
}

// PlayersPerDomain is PlayersThisWeek for every domain at once, for the Home cards.
func (s *Service) PlayersPerDomain(ctx context.Context) (map[int64]int32, error) {
	rows, err := s.db.Q.PlayersThisWeekByDomain(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int32, len(rows))
	for _, r := range rows {
		out[r.DomainID] = r.Players
	}
	return out, nil
}

// RefreshLeaderboards is the worker job that rolls the week up into month and all time.
func (s *Service) RefreshLeaderboards(ctx context.Context) error {
	if err := s.db.Q.RefreshLeaderboardViews(ctx); err != nil {
		return err
	}
	return s.db.Q.RefreshLeaderboardAlltime(ctx)
}

// AllCategoryMastery is the profile's mastery accordion: every category of every domain
// the player owns, grouped by domain id.
func (s *Service) AllCategoryMastery(ctx context.Context, who *authctx.Principal, lang string) (map[int64][]CategoryMastery, error) {
	rows, err := s.db.Q.MasteryAllCategories(ctx, sqlc.MasteryAllCategoriesParams{
		Lang: lang, UserID: who.ID, IslamicOnly: who.IslamicOnly,
	})
	if err != nil {
		return nil, err
	}
	out := map[int64][]CategoryMastery{}
	for _, r := range rows {
		out[r.DomainID] = append(out[r.DomainID], CategoryMastery{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Color: r.Color, Icon: r.Icon,
			Mastery: r.Mastery, Answered: r.Answered, Correct: r.Correct, QuestionCount: r.QuestionCount,
		})
	}
	return out, nil
}

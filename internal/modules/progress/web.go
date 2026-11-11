package progress

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// Web is P24 Leaderboard and P25 Profile. History (P26) belongs to the games module,
// which owns the game rows.
type Web struct {
	svc    *Service
	recent RecentGames
}

// RecentGames is the short "Recent games" list on the profile; the games module fills
// it. Keeping it an interface avoids a cycle (games already knows nothing of progress).
type RecentGames interface {
	Recent(c *gin.Context, v *view.Ctx, limit int) []pages.RecentGame
}

func NewWeb(s *Service, recent RecentGames) *Web { return &Web{svc: s, recent: recent} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/leaderboard", w.leaderboard)
	private.GET("/profile", w.profile)
}

func (w *Web) leaderboard(c *gin.Context) {
	v := view.From(c)
	period := c.DefaultQuery("period", "week")
	if !validPeriod(period) {
		period = "week"
	}
	domainID, _ := strconv.ParseInt(c.Query("domain"), 10, 64)
	// A board is global or one of the player's own domains (DM-03).
	var focus *view.Domain
	if domainID != 0 {
		for i := range v.MyDomains {
			if v.MyDomains[i].ID == domainID {
				focus = &v.MyDomains[i]
				break
			}
		}
		if focus == nil {
			domainID = 0
		}
	}
	in := LeaderInput{
		Who: v.User, Period: period, DomainID: domainID,
		Friends: c.Query("scope") == "friends", Limit: 20,
	}
	rows, err := w.svc.Leaderboard(c.Request.Context(), in)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := pages.LeaderboardData{
		Period: period, Friends: in.Friends, DomainID: domainID, Domain: focus,
	}
	inList := false
	for i, r := range rows {
		l := pages.Leader{
			Name: r.DisplayName, Initials: avatar.Initials(r.DisplayName), Color: avatar.Color(r.UserID.String()),
			Level: r.Level, Points: r.Points, Rank: r.Rank, IsMe: r.IsMe, IsGuest: r.IsGuest,
		}
		if r.IsMe {
			inList = true
		}
		if i < 3 {
			data.Top = append(data.Top, l)
		} else {
			data.Rest = append(data.Rest, l)
		}
	}
	// Pin the viewer under the list when they are not on this page of it.
	if !inList && !v.IsGuest() {
		if me, err := w.svc.MyStanding(c.Request.Context(), in); err == nil && me.Rank > 0 {
			data.Me = &pages.Leader{
				Name: v.Name(), Initials: v.Initials(), Color: avatar.Color(v.User.ID.String()),
				Level: v.Level, Points: me.Points, Rank: me.Rank, IsMe: true,
			}
		}
	}
	if focus != nil {
		v = v.WithFocus(focus)
	}
	render.Page(c, pages.Leaderboard(v, data))
}

func (w *Web) profile(c *gin.Context) {
	v := view.From(c)
	ctx := c.Request.Context()
	sum, err := w.svc.Summary(ctx, v.User.ID)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	sort := c.Query("sort")
	switch sort {
	case "mastery", "weakest", "played":
	default:
		sort = "played"
	}
	data := pages.ProfileData{
		Handle: v.Username, Level: sum.Level, XP: sum.XP, XPNext: sum.NextLevelXP,
		Percent: sum.Percent, Games: sum.GamesTotal, Points: sum.PointsTotal, Streak: sum.Streak,
		BestStreak: sum.BestStreak, DuelsWon: sum.DuelsWon, DuelRating: sum.DuelRating, Sort: sort,
		Languages: v.Tr("lang." + v.Lang),
		ShareURL:  w.shareURL(v),
	}
	if row, err := w.svc.db.Q.GetUser(ctx, v.User.ID); err == nil {
		data.Joined = row.CreatedAt.Time.Format("January 2006")
	}
	mastery, _, err := w.svc.Mastery(ctx, MasteryInput{Who: v.User, Lang: v.Lang, Sort: sort, PerPage: 50})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	cats, err := w.svc.AllCategoryMastery(ctx, v.User, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	var answered, correct int32
	for _, m := range mastery {
		row := pages.MasteryDomain{ID: m.ID, Name: m.Name, Icon: m.Icon, Color: m.Color, Mastery: m.Mastery, Played: m.GamesPlayed}
		for _, cat := range cats[m.ID] {
			row.Categories = append(row.Categories, pages.MasteryCat{
				Name: cat.Name, Icon: cat.Icon, Color: cat.Color, Mastery: cat.Mastery,
			})
		}
		answered += m.Answered
		correct += m.Correct
		data.Mastery = append(data.Mastery, row)
	}
	if answered > 0 {
		data.Accuracy = correct * 100 / answered
	}
	for _, m := range data.Mastery {
		data.Average += m.Mastery
	}
	if len(data.Mastery) > 0 {
		data.Average /= int32(len(data.Mastery))
	}
	achievements, err := w.svc.Achievements(ctx, v.User.ID, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	for _, a := range achievements {
		unlocked := a.UnlockedAt != nil
		if unlocked {
			data.Unlocked++
		}
		data.Achievements = append(data.Achievements, pages.Achievement{
			Name: a.Name, Body: a.Description, Icon: a.Icon,
			Progress: a.Progress, Target: a.Target, Unlocked: unlocked,
		})
	}
	days, err := w.svc.Activity(ctx, v.User.ID, 84)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data.Activity = activityGrid(days)
	if w.recent != nil {
		data.Recent = w.recent.Recent(c, v, 5)
	}
	render.Page(c, pages.Profile(v, data))
}

// activityGrid turns the rows of daily_activity into 84 cells, oldest first, with a
// level of 0–3 so the grid reads at a glance (the design's "darker days").
func activityGrid(days []Day) []pages.ActivityDay {
	byDay := make(map[string]int32, len(days))
	for _, d := range days {
		byDay[d.Day.Format("2006-01-02")] = d.Games
	}
	out := make([]pages.ActivityDay, 0, 84)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 83; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		games := byDay[day.Format("2006-01-02")]
		level := 0
		switch {
		case games >= 5:
			level = 3
		case games >= 3:
			level = 2
		case games >= 1:
			level = 1
		}
		out = append(out, pages.ActivityDay{
			Label: fmt.Sprintf("%s · %d", day.Format("2 Jan"), games), Level: level,
		})
	}
	return out
}

// TodayGoal implements games.ProgressReader: the daily goal on the results screen.
func (w *Web) TodayGoal(c *gin.Context) (int32, int32) {
	v := view.From(c)
	if v.User == nil {
		return 0, 5
	}
	sum, err := w.svc.Summary(c.Request.Context(), v.User.ID)
	if err != nil {
		return 0, 5
	}
	return sum.GamesToday, sum.DailyGoal
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if apiutil.WantsJSON(c) {
		apiutil.Fail(c, err)
		return
	}
	if e, ok := apperr.As(err); ok && e.Status == http.StatusNotFound {
		render.Status(c, http.StatusNotFound, pages.NotFound(v))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

// shareURL is the public link to this player's profile (LB-02). Guests have no
// username, so they get no Share button.
func (w *Web) shareURL(v *view.Ctx) string {
	if v.Username == "" {
		return ""
	}
	return "/u/" + v.Username
}

package app

import (
	"time"

	"github.com/google/uuid"

	"github.com/gin-gonic/gin"

	"siraj/internal/modules/catalog"
	"siraj/internal/modules/challenges"
	"siraj/internal/modules/progress"
	"siraj/internal/modules/questions"
	"siraj/internal/modules/social"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// homeExtras fills the parts of Home that belong to other modules: today's question
// now (HM-02), the player card and the side lists as their modules arrive.
type homeExtras struct {
	questions  *questions.Service
	progress   *progress.Service
	social     *social.Service
	catalog    *catalog.Service
	challenges *challenges.Service
}

func (h homeExtras) Home(c *gin.Context, v *view.Ctx, data *pages.HomeData) {
	// The player card (HM-03): XP, streak, accuracy, weekly rank and the daily goal.
	if sum, err := h.progress.Summary(c.Request.Context(), v.User.ID); err == nil {
		data.PointsToday, data.GamesToday = sum.PointsToday, sum.GamesToday
		data.Streak, data.Goal, data.DoneToday = sum.Streak, sum.DailyGoal, sum.GamesToday
	} else {
		_ = c.Error(err)
	}
	if !v.IsGuest() {
		in := progress.LeaderInput{Who: v.User, Period: "week"}
		if me, err := h.progress.MyStanding(c.Request.Context(), in); err == nil {
			data.WeeklyRank = me.Rank
		}
		if rows, _, err := h.progress.Mastery(c.Request.Context(), progress.MasteryInput{
			Who: v.User, Lang: v.Lang, PerPage: 50,
		}); err == nil {
			var answered, correct int32
			for _, r := range rows {
				answered += r.Answered
				correct += r.Correct
			}
			if answered > 0 {
				data.Accuracy = correct * 100 / answered
			}
		}
	}
	// "This week" (HM-07): the top five of the global weekly board, with the player
	// pinned in place when they are inside it.
	if rows, err := h.progress.Leaderboard(c.Request.Context(), progress.LeaderInput{
		Who: v.User, Period: "week", Limit: 5,
	}); err == nil {
		data.Week = make([]pages.Leader, 0, len(rows)+1)
		mine := false
		for _, r := range rows {
			data.Week = append(data.Week, pages.Leader{
				Name: r.DisplayName, Initials: avatar.Initials(r.DisplayName), Color: avatar.Color(r.UserID.String()),
				Level: r.Level, Points: r.Points, Rank: r.Rank, IsMe: r.IsMe, IsGuest: r.IsGuest,
			})
			mine = mine || r.IsMe
		}
		if !mine && v.SignedIn() {
			me := pages.Leader{
				Name: v.Name(), Initials: v.Initials(), Color: avatar.Color(v.User.ID.String()),
				Level: v.Level, IsMe: true, IsGuest: v.IsGuest(),
			}
			if !v.IsGuest() {
				if st, err := h.progress.MyStanding(c.Request.Context(), progress.LeaderInput{Who: v.User, Period: "week"}); err == nil {
					me.Rank, me.Points = st.Rank, st.Points
				}
			}
			data.Week = append(data.Week, me)
		}
	}

	// The Friends card (HM-07): the three friends most recently seen, online first,
	// each with what they are doing now.
	if !v.IsGuest() && h.social != nil {
		if people, err := h.social.Friends(c.Request.Context(), v.User, ""); err == nil {
			ids := make([]uuid.UUID, 0, len(people))
			for _, p := range people {
				if p.Online {
					data.FriendsOnline++
				}
				ids = append(ids, p.ID)
			}
			presence, _ := h.social.Presence(c.Request.Context(), ids, v.Lang)
			for i, p := range people {
				if i == 3 {
					break
				}
				data.Friends = append(data.Friends, pages.HomeFriend{
					ID: p.ID.String(), Name: p.Name, Initials: avatar.Initials(p.Name),
					Color: avatar.Color(p.ID.String()), Online: p.Online,
					Status: presenceLine(v, p.Online, presence[p.ID]),
				})
			}
		}
	}

	// Challenges waiting for an answer (CH-02). The card never shows another
	// player's score: CH-04 keeps those on the server until the challenge closes.
	if !v.IsGuest() && h.challenges != nil {
		if cards, _, err := h.challenges.Box(c.Request.Context(), v.User, "received", v.Lang, 1, 2); err == nil {
			for _, card := range cards {
				row := pages.HomeChallenge{
					ID:    card.ID.String(),
					Title: v.Tr("home.challenged_you", "name", card.Creator),
					Line: v.Tr("home.challenge_line",
						"topic", card.Category,
						"count", card.Questions,
						"played", card.Played, "total", card.Total),
					Initials: avatar.Initials(card.Creator), Color: avatar.Color(card.CreatorID.String()),
				}
				data.Challenges = append(data.Challenges, row)
			}
		}
	}

	// A guest has no mastery of their own, so their cards show how busy a domain is.
	if v.IsGuest() {
		if counts, err := h.progress.PlayersPerDomain(c.Request.Context()); err == nil {
			data.PlayersWeek = counts
		}
	}

	if daily, err := h.questions.Today(c.Request.Context(), v.User, v.Lang); err == nil {
		data.QOTD = questions.PageCard(daily, v.Lang)
	} else {
		_ = c.Error(err)
	}
	data.StreakLine = streakLine(v, data)
}

// streakLine is the sentence beside the greeting (HM-01). It follows today's question:
// a guest is pointed at it, a player who answered it right is told their streak is one
// day longer, and a player who has not answered yet is told what is at stake.
func streakLine(v *view.Ctx, data *pages.HomeData) string {
	answered := data.QOTD != nil && data.QOTD.Answered
	switch {
	case v.IsGuest():
		return v.Tr("home.guest_start_line")
	case answered && data.QOTD.Right() && data.Streak > 0:
		return v.Tr("home.streak_tomorrow", "days", data.Streak)
	case answered:
		return v.Tr("home.streak_safe")
	case data.Streak > 0:
		return v.Tr("home.streak_keep", "days", data.Streak)
	}
	return v.Tr("home.streak_start")
}

// WelcomeDomains is the row of domain pills on the welcome screen (BR-05): every
// published domain, so a visitor sees what Sirāj covers before signing up.
func (h homeExtras) WelcomeDomains(c *gin.Context, v *view.Ctx) []view.Domain {
	rows, _, err := h.catalog.ListDomains(c.Request.Context(), catalog.ListInput{
		Scope: catalog.ScopeAll, Lang: v.Lang, Page: 1, PerPage: 8,
	})
	if err != nil {
		return nil
	}
	return catalog.ViewDomains(rows)
}

// welcomeQuestion gives the signed-out welcome screen a question to try (AU-01).
func (h homeExtras) WelcomeQuestion(c *gin.Context, v *view.Ctx) *pages.QOTD {
	daily, err := h.questions.Today(c.Request.Context(), v.User, v.Lang)
	if err != nil {
		_ = c.Error(err)
		return nil
	}
	return questions.PageCard(daily, v.Lang)
}

// Domain fills a domain page's side column: how many players it had this week and the
// top five of its weekly board (DM-06, LB-01).
func (h homeExtras) Domain(c *gin.Context, v *view.Ctx, data *pages.DomainPageData) {
	id := data.Domain.ID
	if n, err := h.progress.PlayersThisWeek(c.Request.Context(), id); err == nil {
		data.PlayersWeek = n
	}
	rows, err := h.progress.Leaderboard(c.Request.Context(), progress.LeaderInput{
		Who: v.User, Period: "week", DomainID: id, Limit: 5,
	})
	if err != nil {
		return
	}
	for _, r := range rows {
		data.Top = append(data.Top, pages.LeaderRow{
			Rank: int(r.Rank), Name: r.DisplayName, Initials: avatar.Initials(r.DisplayName),
			Color: avatar.Color(r.UserID.String()), Points: int64(r.Points), Me: r.IsMe,
		})
	}
}

// presenceLine is the one-line status under a friend's name: what they are playing,
// that they are online, or when they were last seen (HM-07).
func presenceLine(v *view.Ctx, online bool, p social.Presence) string {
	if p.Playing != "" {
		return v.Tr("friends.playing_now", "topic", p.Playing)
	}
	if online {
		return v.Tr("friends.online_now")
	}
	if p.LastSeenAt == nil {
		return v.Tr("friends.offline")
	}
	since := time.Since(*p.LastSeenAt)
	switch {
	case since < time.Hour:
		return v.Tr("friends.played_minutes", "n", int(since.Minutes())+1)
	case since < 24*time.Hour:
		return v.Tr("friends.played_hours", "n", int(since.Hours()))
	}
	return v.Tr("friends.played_days", "n", int(since.Hours()/24))
}

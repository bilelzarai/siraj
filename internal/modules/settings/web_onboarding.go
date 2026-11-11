package settings

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/render"
	"siraj/internal/modules/catalog"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"
)

// registerOnboarding mounts the three-step onboarding screen (AU-04, endpoint 14).
// Each step saves what it collected, so closing the browser never loses a choice and
// the language changes the interface at once.
func (w *Web) registerOnboarding(public, private *gin.RouterGroup) {
	private.GET("/onboarding", w.onboardingPage)
	private.POST("/onboarding", w.onboardingSave)
}

func (w *Web) onboardingPage(c *gin.Context) {
	step := clampStep(c.Query("step"))
	w.renderOnboarding(c, step, "")
}

func (w *Web) renderOnboarding(c *gin.Context, step int, problem string) {
	v := view.From(c)
	data := pages.OnboardingData{Step: step, Problem: problem, IslamicOnly: v.IslamicOnly, Goal: 5}
	if step == 2 {
		// Onboarding is one of the three screens that list every domain (DM-04).
		data.Query = strings.TrimSpace(c.Query("q"))
		rows, total, err := w.svc.catalog.ListDomains(c.Request.Context(), catalog.ListInput{
			Who: v.User, Scope: catalog.ScopeAll, Q: data.Query, Lang: v.Lang,
			Page: 1, PerPage: catalog.MaxUserDomains,
		})
		if err != nil {
			render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
			return
		}
		data.Domains = catalog.ViewDomains(rows)
		data.TotalDomains = total
		if data.Query != "" {
			// The helper line counts the whole catalog, not the filtered list.
			if _, all, err := w.svc.catalog.ListDomains(c.Request.Context(), catalog.ListInput{
				Who: v.User, Scope: catalog.ScopeAll, Lang: v.Lang, Page: 1, PerPage: 1,
			}); err == nil {
				data.TotalDomains = all
			}
		}
	}
	data.Picked = map[int64]bool{}
	ids := make([]int64, 0, len(v.MyDomains))
	for _, d := range v.MyDomains {
		data.Picked[d.ID] = true
		ids = append(ids, d.ID)
	}
	// The categories of a picked domain, so the player can narrow straight away.
	if step == 2 && len(ids) > 0 {
		if byDomain, err := w.svc.catalog.CategoriesOf(c.Request.Context(), v.Lang, ids); err == nil {
			data.Categories = byDomain
		}
	}
	data.PickedCats = map[int64]bool{}
	if s, err := w.svc.Get(c.Request.Context(), v.User); err == nil {
		data.Goal = s.DailyGoal
		if s.ReminderTime.Valid {
			mins := s.ReminderTime.Microseconds / 60_000_000
			data.Reminder = twoDigits(int(mins/60)) + ":" + twoDigits(int(mins%60))
		}
	}
	render.Page(c, pages.Onboarding(v, data))
}

func (w *Web) onboardingSave(c *gin.Context) {
	v := view.From(c)
	step := clampStep(c.PostForm("step"))

	if c.PostForm("skip") != "" {
		render.Redirect(c, "/home")
		return
	}
	if c.PostForm("back") != "" {
		render.Redirect(c, "/onboarding?step="+strconv.Itoa(max(1, step-1)))
		return
	}

	switch step {
	case 1:
		if lang := c.PostForm("lang"); lang != "" {
			if err := w.svc.SetLang(c.Request.Context(), v.User, lang); err != nil {
				w.renderOnboarding(c, 1, message(err))
				return
			}
		}
		render.Redirect(c, "/onboarding?step=2")
	case 2:
		ids := idsFromForm(c.PostFormArray("domain_ids"))
		islamicOnly := c.PostForm("islamic_only") != ""
		if len(ids) == 0 {
			v.IslamicOnly = islamicOnly
			w.renderOnboarding(c, 2, v.Tr("onboarding.domains.min"))
			return
		}
		if err := w.svc.SetIslamicOnly(c.Request.Context(), v.User, islamicOnly); err != nil {
			w.renderOnboarding(c, 2, message(err))
			return
		}
		if err := w.svc.catalog.ReplaceMyDomains(c.Request.Context(), v.User, ids, sqlc.UserDomainSourceOnboarding); err != nil {
			w.renderOnboarding(c, 2, message(err))
			return
		}
		render.Redirect(c, "/onboarding?step=3")
	default:
		goal, _ := strconv.Atoi(c.PostForm("daily_goal"))
		in := OnboardingInput{DailyGoal: int32(goal), ReminderTime: c.PostForm("reminder_time"), IslamicOnly: v.IslamicOnly}
		if err := w.svc.SaveOnboarding(c.Request.Context(), v.User, in); err != nil {
			w.renderOnboarding(c, 3, message(err))
			return
		}
		render.Redirect(c, "/home")
	}
}

func clampStep(raw string) int {
	step, _ := strconv.Atoi(raw)
	if step < 1 || step > 3 {
		return 1
	}
	return step
}

func idsFromForm(values []string) []int64 {
	out := make([]int64, 0, len(values))
	seen := map[int64]bool{}
	for _, raw := range values {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func message(err error) string {
	if e, ok := apperr.As(err); ok {
		if len(e.Fields) > 0 {
			for _, v := range e.Fields {
				return v
			}
		}
		return e.Message
	}
	return "Something went wrong"
}

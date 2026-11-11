package progress

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/platform/authctx"
)

// API is endpoints 90–95: progress, mastery, achievements, activity and leaderboards.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/me/progress", a.progress)         // 90
	private.GET("/me/mastery", a.mastery)           // 91
	private.GET("/me/mastery/domains/:id", a.byCat) // 92
	private.GET("/me/achievements", a.achievements) // 93
	private.GET("/me/activity", a.activity)         // 94
	public.GET("/leaderboards", a.leaderboard)      // 95
}

func (a *API) progress(c *gin.Context) {
	who := authctx.Get(c)
	out, err := a.svc.Summary(c.Request.Context(), who.ID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, out)
}

func (a *API) mastery(c *gin.Context) {
	who := authctx.Get(c)
	page, per := apiutil.PageParams(c, 10, 50)
	rows, total, err := a.svc.Mastery(c.Request.Context(), MasteryInput{
		Who: who, Lang: apiutil.Lang(c), Q: c.Query("q"), Sort: c.Query("sort"), Page: page, PerPage: per,
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Paged(c, rows, apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

func (a *API) byCat(c *gin.Context) {
	who := authctx.Get(c)
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	rows, err := a.svc.CategoryMastery(c.Request.Context(), who.ID, id, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, rows)
}

func (a *API) achievements(c *gin.Context) {
	who := authctx.Get(c)
	rows, err := a.svc.Achievements(c.Request.Context(), who.ID, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, rows)
}

func (a *API) activity(c *gin.Context) {
	who := authctx.Get(c)
	days, _ := strconv.Atoi(c.Query("days"))
	rows, err := a.svc.Activity(c.Request.Context(), who.ID, int32(days))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, rows)
}

func (a *API) leaderboard(c *gin.Context) {
	who := authctx.Get(c)
	domainID, _ := strconv.ParseInt(c.Query("domain_id"), 10, 64)
	page, per := apiutil.PageParams(c, 20, 100)
	in := LeaderInput{
		Who: who, Period: c.DefaultQuery("period", "week"), DomainID: domainID,
		Friends: c.Query("scope") == "friends", Limit: int32(per), Offset: int32((page - 1) * per),
	}
	rows, err := a.svc.Leaderboard(c.Request.Context(), in)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	me, err := a.svc.MyStanding(c.Request.Context(), in)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": rows, "me": me, "page": apiutil.PageInfo{Page: page, PerPage: per, Total: len(rows)}})
}

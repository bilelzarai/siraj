package catalog

import (
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// DomainDTO is a domain as clients see it. Colour, icon and motif theme the domain's screens.
type DomainDTO struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Family        string `json:"family"` // islamic | general
	Icon          string `json:"icon"`
	Color         string `json:"color"`
	Motif         string `json:"motif"`
	IsFeatured    bool   `json:"is_featured"`
	IsNew         bool   `json:"is_new"`
	IsMine        bool   `json:"is_mine"`
	CategoryCount int32  `json:"category_count"`
	QuestionCount int32  `json:"question_count"`
}

type CategoryDTO struct {
	ID            int64  `json:"id"`
	DomainID      int64  `json:"domain_id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Icon          string `json:"icon"`
	Color         string `json:"color"`
	QuestionCount int32  `json:"question_count"`
}

func domainDTO(r sqlc.ListDomainsRow) DomainDTO {
	return DomainDTO{r.ID, r.Slug, r.Name, r.Description, string(r.Family), r.Icon, r.Color, r.Motif, r.IsFeatured, r.IsNew, r.IsMine, r.CategoryCount, r.QuestionCount}
}

func domainDTOFromGet(r sqlc.GetDomainRow) DomainDTO {
	return DomainDTO{r.ID, r.Slug, r.Name, r.Description, string(r.Family), r.Icon, r.Color, r.Motif, r.IsFeatured, r.IsNew, r.IsMine, r.CategoryCount, r.QuestionCount}
}

func domainDTOs(rows []sqlc.ListDomainsRow) []DomainDTO {
	out := make([]DomainDTO, len(rows))
	for i, r := range rows {
		out[i] = domainDTO(r)
	}
	return out
}

type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

// Register mounts endpoints 17–19 and 114–117 under /api/v1.
func (a *API) Register(public, private *gin.RouterGroup) {
	public.GET("/domains", a.listDomains)               // #17  ?scope=mine|all&q=&page=&per_page=
	public.GET("/domains/:id", a.getDomain)             // #18
	public.GET("/domains/:id/categories", a.categories) // #19
	public.GET("/categories/:id", a.category)           // #20
	public.GET("/search", a.search)                     // #21  ?scope=mine|all&q=
	private.GET("/me/domains", a.myDomains)             // #114
	private.PUT("/me/domains", a.replaceMyDomains)      // #115
	private.POST("/me/domains/:id", a.addMyDomain)      // #116
	private.DELETE("/me/domains/:id", a.removeMyDomain) // #117
}

func (a *API) listDomains(c *gin.Context) {
	page, per := apiutil.PageParams(c, 6, 50) // the design pages domains 6 at a time
	who := authctx.Get(c)
	scope := Scope(c.DefaultQuery("scope", ""))
	if scope == "" {
		scope = ScopeMine
		if who == nil {
			scope = ScopeAll // anonymous visitors can only browse
		}
	}
	rows, total, err := a.svc.ListDomains(c.Request.Context(), ListInput{
		Who: who, Scope: scope, Q: strings.TrimSpace(c.Query("q")), Lang: apiutil.Lang(c), Page: page, PerPage: per,
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Paged(c, domainDTOs(rows), apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

func (a *API) getDomain(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	d, err := a.svc.GetDomain(c.Request.Context(), authctx.Get(c), id, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, domainDTOFromGet(d))
}

func (a *API) categories(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	rows, err := a.svc.Categories(c.Request.Context(), authctx.Get(c), id, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]CategoryDTO, len(rows))
	for i, r := range rows {
		out[i] = CategoryDTO{r.ID, r.DomainID, r.Slug, r.Name, r.Icon, r.Color, r.QuestionCount}
	}
	apiutil.OK(c, out)
}

// category is endpoint 20.
func (a *API) category(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	row, err := a.svc.Category(c.Request.Context(), authctx.Get(c), id, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{
		"id": row.ID, "domain_id": row.DomainID, "slug": row.Slug, "name": row.Name,
		"icon": row.Icon, "color": row.Color, "question_count": row.QuestionCount,
		"domain": gin.H{
			"id": row.DomainID, "slug": row.DomainSlug, "name": row.DomainName,
			"family": string(row.DomainFamily), "is_mine": row.IsMine,
		},
	})
}

// search is endpoint 21: domains and categories in one answer.
func (a *API) search(c *gin.Context) {
	res, err := a.svc.Search(c.Request.Context(), authctx.Get(c), Scope(c.Query("scope")),
		strings.TrimSpace(c.Query("q")), apiutil.Lang(c), 20)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	cats := make([]gin.H, len(res.Categories))
	for i, r := range res.Categories {
		cats[i] = gin.H{
			"id": r.ID, "domain_id": r.DomainID, "slug": r.Slug, "name": r.Name,
			"icon": r.Icon, "color": r.Color, "question_count": r.QuestionCount,
			"domain_name": r.DomainName, "is_mine": r.IsMine,
		}
	}
	apiutil.OK(c, gin.H{"domains": domainDTOs(res.Domains), "categories": cats})
}

func (a *API) myDomains(c *gin.Context) {
	rows, err := a.svc.MyDomains(c.Request.Context(), authctx.Get(c), apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, domainDTOs(rows))
}

type replaceReq struct {
	DomainIDs []int64 `json:"domain_ids"`
	Source    string  `json:"source"` // onboarding (first run) or added (Manage); default added
}

func (a *API) replaceMyDomains(c *gin.Context) {
	var req replaceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apperr.BadRequest("bad_json", err.Error()))
		return
	}
	src := sqlc.UserDomainSourceAdded
	if req.Source == string(sqlc.UserDomainSourceOnboarding) {
		src = sqlc.UserDomainSourceOnboarding
	}
	who := authctx.Get(c)
	if err := a.svc.ReplaceMyDomains(c.Request.Context(), who, req.DomainIDs, src); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.myDomains(c)
}

func (a *API) addMyDomain(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if _, err := a.svc.AddMyDomain(c.Request.Context(), authctx.Get(c), id, sqlc.UserDomainSourceAdded); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.myDomains(c)
}

func (a *API) removeMyDomain(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.RemoveMyDomain(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.myDomains(c)
}

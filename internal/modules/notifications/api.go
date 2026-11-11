package notifications

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/platform/authctx"
)

// API is endpoints 87–89.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/notifications", a.list)                // 87
	private.POST("/notifications/read", a.read)          // 88
	private.GET("/notifications/unread-count", a.unread) // 89
}

func (a *API) list(c *gin.Context) {
	page, per := apiutil.PageParams(c, 25, 50)
	rows, total, err := a.svc.List(c.Request.Context(), authctx.Get(c), c.Query("filter") == "unread", page, per)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Paged(c, rows, apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

func (a *API) read(c *gin.Context) {
	var body struct {
		IDs []uuid.UUID `json:"ids"`
	}
	_ = c.ShouldBindJSON(&body)
	if err := a.svc.MarkRead(c.Request.Context(), authctx.Get(c), body.IDs); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) unread(c *gin.Context) {
	n, err := a.svc.Unread(c.Request.Context(), authctx.Get(c).ID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"count": n})
}

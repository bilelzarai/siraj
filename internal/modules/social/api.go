package social

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// API is endpoints 63–74.
type API struct {
	svc     *Service
	baseURL string
}

func NewAPI(s *Service, baseURL string) *API { return &API{svc: s, baseURL: baseURL} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/friends", a.friends)                      // 63
	private.DELETE("/friends/:userId", a.removeFriend)      // 64
	private.GET("/friend-requests", a.requests)             // 65
	private.POST("/friend-requests", a.send)                // 66
	private.POST("/friend-requests/:id/accept", a.accept)   // 67
	private.POST("/friend-requests/:id/decline", a.decline) // 68
	private.GET("/users/search", a.search)                  // 69
	public.GET("/users/:username", a.profile)               // 70
	private.GET("/users-suggestions", a.suggestions)        // 71
	private.POST("/blocks", a.block)                        // 72
	private.DELETE("/blocks/:userId", a.unblock)            // 73
	private.POST("/invites", a.invite)                      // 74
}

func (a *API) friends(c *gin.Context) {
	rows, err := a.svc.Friends(c.Request.Context(), authctx.Get(c), c.Query("q"))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, rows)
}

func (a *API) removeFriend(c *gin.Context) {
	id, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		apiutil.Fail(c, apperr.BadRequest("bad_id", "userId must be a uuid"))
		return
	}
	if err := a.svc.Remove(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) requests(c *gin.Context) {
	who := authctx.Get(c)
	received, err := a.svc.Requests(c.Request.Context(), who, true)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	sent, err := a.svc.Requests(c.Request.Context(), who, false)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"received": received, "sent": sent})
}

func (a *API) send(c *gin.Context) {
	var body struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.SendRequest(c.Request.Context(), authctx.Get(c), body.UserID); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) accept(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Accept(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) decline(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Decline(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) search(c *gin.Context) {
	rows, err := a.svc.Search(c.Request.Context(), authctx.Get(c), c.Query("q"), 20)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, rows)
}

func (a *API) profile(c *gin.Context) {
	row, err := a.svc.db.Q.GetUserByUsername(c.Request.Context(), toText(c.Param("username")))
	if err != nil {
		apiutil.Fail(c, apperr.NotFound("user"))
		return
	}
	apiutil.OK(c, gin.H{
		"id": row.ID, "username": row.Username.String, "display_name": row.DisplayName,
		"level": row.Level, "xp": row.Xp, "online": row.Online, "joined_at": row.CreatedAt.Time,
	})
}

func (a *API) suggestions(c *gin.Context) {
	rows, err := a.svc.Suggestions(c.Request.Context(), authctx.Get(c), 10)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, rows)
}

func (a *API) block(c *gin.Context) {
	var body struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.Block(c.Request.Context(), authctx.Get(c), body.UserID); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) unblock(c *gin.Context) {
	id, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		apiutil.Fail(c, apperr.BadRequest("bad_id", "userId must be a uuid"))
		return
	}
	if err := a.svc.Unblock(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) invite(c *gin.Context) {
	code, err := a.svc.InviteLink(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{"code": code, "url": a.baseURL + "/i/" + code})
}

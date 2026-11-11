package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/http/middleware"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// UserDTO is what clients see of a user. Never return sqlc rows directly.
type UserDTO struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Role        string     `json:"role"`
	DisplayName string     `json:"display_name"`
	Username    *string    `json:"username"`
	Lang        string     `json:"lang"`
	Level       int32      `json:"level"`
	XP          int32      `json:"xp"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

func toUserDTO(u sqlc.User) UserDTO {
	d := UserDTO{ID: u.ID.String(), Kind: string(u.Kind), Role: string(u.Role), DisplayName: u.DisplayName, Lang: u.Lang, Level: u.Level, XP: u.Xp}
	if u.Username.Valid {
		d.Username = &u.Username.String
	}
	if u.ExpiresAt.Valid {
		t := u.ExpiresAt.Time
		d.ExpiresAt = &t
	}
	return d
}

type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

// Register mounts the auth group (endpoints 1–16) under /api/v1.
func (a *API) Register(public, private *gin.RouterGroup) {
	public.POST("/auth/guest", a.startGuest) // #4
	private.POST("/auth/logout", a.logout)   // #7
	private.GET("/me", a.me)                 // #11
	a.registerAccounts(public, private)
	a.registerCodes(public, private)
}

type guestReq struct {
	Name   string `json:"name"`
	Client string `json:"client"` // web | ios | android
}

func (a *API) startGuest(c *gin.Context) {
	var req guestReq
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			apiutil.Fail(c, apperr.BadRequest("bad_json", err.Error()))
			return
		}
	}
	if req.Client != "" && req.Client != "web" && req.Client != "ios" && req.Client != "android" {
		apiutil.Fail(c, apperr.Invalid(map[string]string{"client": "web, ios or android"}))
		return
	}
	s, err := a.svc.StartGuest(c.Request.Context(), GuestInput{
		Name: req.Name, Lang: apiutil.Lang(c), Client: req.Client,
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	// Web gets an HttpOnly cookie; mobile uses the token as a Bearer header.
	if req.Client == "" || req.Client == "web" {
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(middleware.CookieName, s.Token, int(time.Until(s.ExpiresAt).Seconds()), "/", "", c.Request.TLS != nil, true)
	}
	apiutil.Created(c, gin.H{"token": s.Token, "expires_at": s.ExpiresAt, "user": toUserDTO(s.User)})
}

func (a *API) logout(c *gin.Context) {
	if err := a.svc.Logout(c.Request.Context(), middleware.TokenFrom(c)); err != nil {
		apiutil.Fail(c, err)
		return
	}
	c.SetCookie(middleware.CookieName, "", -1, "/", "", false, true)
	apiutil.NoContent(c)
}

func (a *API) me(c *gin.Context) {
	u, err := a.svc.Me(c.Request.Context(), authctx.Get(c).ID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, toUserDTO(u))
}

package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/middleware"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// registerAccounts mounts endpoints 1–3, 5, 6, 8–10, 12, 15 and 16.
func (a *API) registerAccounts(public, private *gin.RouterGroup) {
	public.POST("/auth/register", a.register)              // #1
	public.POST("/auth/login", a.login)                    // #2
	public.POST("/auth/google", a.google)                  // #3
	private.POST("/auth/guest/upgrade", a.upgrade)         // #5
	public.POST("/auth/refresh", a.refresh)                // #6
	public.POST("/auth/password/forgot", a.forgot)         // #8
	public.POST("/auth/password/reset", a.reset)           // #9
	public.GET("/auth/username-available", a.usernameFree) // #10
	private.PATCH("/me", a.updateProfile)                  // #12
	private.POST("/me/devices", a.registerDevice)          // #15
	private.DELETE("/me", a.deleteAccount)                 // #16
}

// setSessionCookie gives browsers an HttpOnly cookie; mobile clients use the Bearer token.
func setSessionCookie(c *gin.Context, token string, expires time.Time) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(middleware.CookieName, token, int(time.Until(expires).Seconds()), "/", "", c.Request.TLS != nil, true)
}

func (a *API) sessionResponse(c *gin.Context, s Session, created bool) {
	if c.GetHeader("Authorization") == "" {
		setSessionCookie(c, s.Token, s.ExpiresAt)
	}
	body := gin.H{"token": s.Token, "expires_at": s.ExpiresAt, "user": toUserDTO(s.User)}
	if created {
		apiutil.Created(c, body)
		return
	}
	apiutil.OK(c, body)
}

type registerReq struct {
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	AcceptTerms bool   `json:"accept_terms"`
	Client      string `json:"client"`
}

func (a *API) register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	s, err := a.svc.Register(c.Request.Context(), RegisterInput{
		DisplayName: req.DisplayName, Username: req.Username, Email: req.Email,
		Password: req.Password, AcceptTerms: req.AcceptTerms, Lang: apiutil.Lang(c),
		Client: req.Client, UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.sessionResponse(c, s, true)
}

type loginReq struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	Client   string `json:"client"`
}

func (a *API) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	s, err := a.svc.Login(c.Request.Context(), LoginInput{
		Login: req.Login, Password: req.Password, Client: req.Client,
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.sessionResponse(c, s, false)
}

type googleReq struct {
	IDToken string `json:"id_token"`
	Client  string `json:"client"`
}

func (a *API) google(c *gin.Context) {
	var req googleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	s, err := a.svc.Google(c.Request.Context(), GoogleInput{
		IDToken: req.IDToken, Lang: apiutil.Lang(c), Client: req.Client,
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Upgrade: authctx.Get(c),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.sessionResponse(c, s, false)
}

// upgrade is endpoint 5: a guest becomes an account and keeps the same id (RL-04).
func (a *API) upgrade(c *gin.Context) {
	who := authctx.Get(c)
	if !who.IsGuest() {
		apiutil.Fail(c, ErrNotGuest)
		return
	}
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	s, err := a.svc.Register(c.Request.Context(), RegisterInput{
		DisplayName: req.DisplayName, Username: req.Username, Email: req.Email,
		Password: req.Password, AcceptTerms: req.AcceptTerms, Lang: apiutil.Lang(c),
		Client: req.Client, UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
		UpgradeGuest: who,
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.sessionResponse(c, s, true)
}

// refresh is endpoint 6. The token comes from the Authorization header, the way a
// client that is simply keeping its session alive sends it; a client that keeps the
// token in storage rather than in a header may send it in the body instead, which is
// the usual REST shape of a refresh call. Either way it is the same token.
func (a *API) refresh(c *gin.Context) {
	token := middleware.TokenFrom(c)
	if token == "" {
		var body struct {
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			token = strings.TrimSpace(body.Token)
		}
	}
	if token == "" {
		apiutil.Fail(c, apperr.Unauthorized("Send the session token, as a Bearer header or in the body"))
		return
	}
	s, err := a.svc.Refresh(c.Request.Context(), token)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.sessionResponse(c, s, false)
}

type forgotReq struct {
	Email string `json:"email"`
}

func (a *API) forgot(c *gin.Context) {
	var req forgotReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.ForgotPassword(c.Request.Context(), req.Email, apiutil.Lang(c), a.svc.BaseURL()); err != nil {
		apiutil.Fail(c, err)
		return
	}
	// Always the same answer, whether or not that email has an account.
	apiutil.OK(c, gin.H{"sent": true})
}

type resetReq struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (a *API) reset(c *gin.Context) {
	var req resetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.ResetPassword(c.Request.Context(), req.Token, req.Password); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"reset": true})
}

func (a *API) usernameFree(c *gin.Context) {
	free, err := a.svc.UsernameAvailable(c.Request.Context(), c.Query("username"))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"available": free})
}

type profileReq struct {
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Lang        *string `json:"lang"`
}

func (a *API) updateProfile(c *gin.Context) {
	var req profileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	u, err := a.svc.UpdateProfile(c.Request.Context(), authctx.Get(c), ProfileInput{
		DisplayName: req.DisplayName, AvatarURL: req.AvatarURL, Lang: req.Lang,
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, toUserDTO(u))
}

type deviceReq struct {
	Platform  string `json:"platform"`
	PushToken string `json:"push_token"`
}

func (a *API) registerDevice(c *gin.Context) {
	var req deviceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.RegisterDevice(c.Request.Context(), authctx.Get(c), req.Platform, req.PushToken); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

type deleteReq struct {
	Confirm string `json:"confirm"`
}

func (a *API) deleteAccount(c *gin.Context) {
	var req deleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.DeleteAccount(c.Request.Context(), authctx.Get(c), req.Confirm); err != nil {
		apiutil.Fail(c, err)
		return
	}
	c.SetCookie(middleware.CookieName, "", -1, "/", "", false, true)
	apiutil.NoContent(c)
}

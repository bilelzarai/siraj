package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/middleware"
	"siraj/internal/platform/authctx"
)

// The V5 sign-in endpoints: ask for a code, prove it, and — when the number is new
// — finish the account. A mobile client walks the same three calls the website does.

func (a *API) registerCodes(public, private *gin.RouterGroup) {
	public.GET("/auth/countries", a.countries)     // the picker's list and its rules
	public.POST("/auth/code", a.startCode)         // send a code to a phone or an email
	public.POST("/auth/code/verify", a.verifyCode) // prove it
	public.POST("/auth/complete", a.complete)      // name the new account
	private.POST("/auth/logout-everywhere", a.logoutEverywhere)
	private.GET("/me/devices", a.listDevices)
	private.DELETE("/me/devices/:id", a.revokeDevice)
}

// countries is what the phone field needs to validate before anything is sent.
func (a *API) countries(c *gin.Context) {
	out := make([]gin.H, 0, len(Countries))
	for _, country := range Countries {
		out = append(out, gin.H{
			"code": country.Code, "name": country.Name,
			"dial": country.Dial, "digits": country.Digits,
		})
	}
	apiutil.OK(c, gin.H{"countries": out, "default": DefaultCountry})
}

type codeBody struct {
	Channel  string `json:"channel"` // phone | email
	Country  string `json:"country"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	Call     bool   `json:"call"`
	Code     string `json:"code"`
	Remember *bool  `json:"remember"`
	Client   string `json:"client"`
	// "Create account" gives the name and the password with the first call, the
	// way the screen's "1 · Your details" step does. Signing in sends none of it
	// and the code decides at verification which of the two it was.
	Creating    bool   `json:"creating"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	AcceptTerms bool   `json:"accept_terms"`
	Resend      bool   `json:"resend"`
}

func (b codeBody) channel() Channel {
	if b.Channel == string(ChannelEmail) {
		return ChannelEmail
	}
	return ChannelPhone
}

func (b codeBody) remember() bool { return b.Remember == nil || *b.Remember }

// startCode sends the six digits.
func (a *API) startCode(c *gin.Context) {
	var body codeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	sent, err := a.svc.StartCode(c.Request.Context(), StartCodeInput{
		Channel: body.channel(), Country: body.Country, Phone: body.Phone,
		Email: body.Email, Call: body.Call, Guest: authctx.Get(c),
		Lang: apiutil.Lang(c), IP: c.ClientIP(),
		Creating: body.Creating, DisplayName: body.DisplayName,
		Password: body.Password, AcceptTerms: body.AcceptTerms, Resend: body.Resend,
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := gin.H{
		"channel": string(sent.Channel), "destination": sent.Destination,
		"exists": sent.Exists, "resend_in": int(sent.ResendIn.Seconds()),
		"expires_at": sent.ExpiresAt, "length": CodeLength, "tries": CodeTries,
	}
	// Development only: no gateway is configured, so the code comes back here
	// rather than being unreachable.
	if sent.DevCode != "" {
		out["dev_code"] = sent.DevCode
	}
	apiutil.OK(c, out)
}

// verifyCode answers either with a session, or with "this number is new".
func (a *API) verifyCode(c *gin.Context) {
	var body codeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	v, err := a.svc.VerifyCode(c.Request.Context(), VerifyCodeInput{
		Channel: body.channel(), Country: body.Country, Phone: body.Phone,
		Email: body.Email, Code: body.Code, Remember: body.remember(),
		Guest: authctx.Get(c), Lang: apiutil.Lang(c), Client: body.Client,
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if !v.SignedIn {
		apiutil.OK(c, gin.H{
			"new_account": true, "channel": string(v.Channel), "destination": v.Destination,
		})
		return
	}
	if c.GetHeader("Authorization") == "" {
		setSessionCookie(c, v.Session.Token, v.Session.ExpiresAt)
	}
	apiutil.OK(c, gin.H{
		"new_account": false, "created": v.Created, "moved_points": v.MovedPoints,
		"token": v.Session.Token, "expires_at": v.Session.ExpiresAt,
		"user": toUserDTO(v.Session.User),
	})
}

// complete names the new account and signs the player in.
func (a *API) complete(c *gin.Context) {
	var body struct {
		Channel     string `json:"channel"`
		Destination string `json:"destination"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		AcceptTerms bool   `json:"accept_terms"`
		Remember    *bool  `json:"remember"`
		Client      string `json:"client"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	channel := ChannelPhone
	if body.Channel == string(ChannelEmail) {
		channel = ChannelEmail
	}
	session, err := a.svc.Complete(c.Request.Context(), CompleteInput{
		Channel: channel, Destination: body.Destination, DisplayName: body.DisplayName,
		Email: body.Email, AcceptTerms: body.AcceptTerms,
		Remember: body.Remember == nil || *body.Remember,
		Guest:    authctx.Get(c), Lang: apiutil.Lang(c), Client: body.Client,
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.sessionResponse(c, session, true)
}

// logoutEverywhere is the second button of the sign-out confirmation.
func (a *API) logoutEverywhere(c *gin.Context) {
	n, err := a.svc.SignOutEverywhere(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	c.SetCookie(middleware.CookieName, "", -1, "/", "", false, true)
	apiutil.OK(c, gin.H{"signed_out": n})
}

// listDevices is "Linked devices" in the account menu.
func (a *API) listDevices(c *gin.Context) {
	rows, err := a.svc.Devices(c.Request.Context(), authctx.Get(c), middleware.TokenFrom(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		device := gin.H{
			"id": r.ID, "client": r.Client, "current": r.IsCurrent,
			"since": r.CreatedAt.Time, "until": r.ExpiresAt.Time,
		}
		if r.UserAgent.Valid {
			device["user_agent"] = r.UserAgent.String
		}
		out[i] = device
	}
	apiutil.OK(c, out)
}

func (a *API) revokeDevice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.RevokeDevice(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/middleware"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/view"
	"siraj/web/templates/pages"
	authpages "siraj/web/templates/pages/auth"
)

// Web holds the entry screens of the website (prototype screens 1–5) and the
// locked page a guest sees instead of the screens that need an account (RL-03).
// Every handler calls the same service the mobile API calls (NF-04).
type Web struct {
	svc   *Service
	qotd  WelcomeQuestion
	after string // where a fresh session lands
}

// WelcomeQuestion lets the welcome screen show today's question without the auth
// module depending on the questions module (wired in internal/app).
type WelcomeQuestion interface {
	WelcomeQuestion(c *gin.Context, v *view.Ctx) *pages.QOTD
	WelcomeDomains(c *gin.Context, v *view.Ctx) []view.Domain
}

func NewWeb(s *Service, qotd WelcomeQuestion) *Web { return &Web{svc: s, qotd: qotd, after: "/home"} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	public.GET("/", w.root)
	public.GET("/welcome", w.welcome)
	// The V5 entry flow: one screen, four steps (sign in, create, code, name).
	public.GET("/signin", w.signinPage)
	public.POST("/signin", w.signin)
	public.GET("/signup", w.signupPage)
	public.POST("/signup", w.signup)
	w.registerEntry(public, private)
	public.GET("/signup/username", w.usernameHint)
	public.GET("/guest", w.guestPage)
	public.POST("/guest", w.startGuest)
	public.GET("/forgot-password", w.forgotPage)
	public.POST("/forgot-password", w.forgot)
	public.GET("/reset-password", w.resetPage)
	public.POST("/reset-password", w.reset)
	public.POST("/signout", w.signout)
}

func (w *Web) root(c *gin.Context) {
	if view.From(c).SignedIn() {
		render.Redirect(c, "/home")
		return
	}
	render.Redirect(c, "/welcome")
}

func (w *Web) welcome(c *gin.Context) {
	v := view.From(c)
	if v.SignedIn() {
		render.Redirect(c, "/home")
		return
	}
	var daily *pages.QOTD
	var domains []view.Domain
	if w.qotd != nil {
		daily = w.qotd.WelcomeQuestion(c, v)
		domains = w.qotd.WelcomeDomains(c, v)
	}
	render.Page(c, pages.Welcome(v, domains, daily))
}

func (w *Web) signinPage(c *gin.Context) {
	w.renderEntry(c, w.entry(c, "signin"))
}

// signin is the first step of the V5 flow: a code to a phone or an email, or the
// password of whoever set one.
func (w *Web) signin(c *gin.Context) { w.start(c, false) }

func (w *Web) signupPage(c *gin.Context) { w.renderEntry(c, w.entry(c, "create")) }

// signup is the same first step, worded for somebody who has no account yet.
func (w *Web) signup(c *gin.Context) { w.start(c, true) }

// usernameHint answers the htmx request sent while the player types (#10).
func (w *Web) usernameHint(c *gin.Context) {
	v := view.From(c)
	username := strings.TrimSpace(c.Query("username"))
	if username == "" {
		render.Page(c, authpages.UsernameHint(v, "", true, v.Tr("signup.username_help")))
		return
	}
	if err := ValidateUsername(username); err != nil {
		render.Page(c, authpages.UsernameHint(v, username, false, v.Tr("auth.username_rules")))
		return
	}
	free, err := w.svc.UsernameAvailable(c.Request.Context(), username)
	if err != nil {
		render.Page(c, authpages.UsernameHint(v, username, false, v.Tr("auth.username_rules")))
		return
	}
	render.Page(c, authpages.UsernameHint(v, username, free, ""))
}

func (w *Web) guestPage(c *gin.Context) {
	v := view.From(c)
	if v.SignedIn() {
		render.Redirect(c, w.after)
		return
	}
	// Over the entry screen when it asks that way (screen 19), as a page otherwise.
	if c.Query("ns") == "popup" {
		d := w.entry(c, "signin")
		d.Name = SuggestGuestName()
		render.Page(c, authpages.GuestPopup(v, d))
		return
	}
	render.Page(c, authpages.Guest(v, w.form(map[string]string{
		"then": safeNext(c.Query("then")),
		"name": SuggestGuestName(),
	})))
}

func (w *Web) startGuest(c *gin.Context) {
	v := view.From(c)
	if v.SignedIn() {
		render.Redirect(c, safeNext(c.PostForm("then")))
		return
	}
	s, err := w.svc.StartGuest(c.Request.Context(), GuestInput{
		Name: c.PostForm("name"), Lang: v.Lang, Client: "web",
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		f := w.form(map[string]string{"name": c.PostForm("name"), "then": safeNext(c.PostForm("then"))})
		if e, ok := apperr.As(err); ok {
			f.Errors = mergeFields(f.Errors, e.Fields)
			f.Error = e.Message
		} else {
			f.Error = v.Tr("error.body")
		}
		render.Status(c, statusOf(err), authpages.Guest(v, f))
		return
	}
	setSessionCookie(c, s.Token, s.ExpiresAt)
	render.Redirect(c, safeNext(c.PostForm("then")))
}

func (w *Web) forgotPage(c *gin.Context) {
	v := view.From(c)
	// Over the entry screen when it asks that way, as a page otherwise.
	if c.Query("ns") == "popup" {
		render.Page(c, authpages.ForgotPopup(v, "", "", false))
		return
	}
	render.Page(c, authpages.Forgot(v, w.form(nil), false))
}

func (w *Web) forgot(c *gin.Context) {
	v := view.From(c)
	email := strings.TrimSpace(c.PostForm("email"))
	// The entry screen asked inside a popup, so its refusal belongs there too.
	popup := c.PostForm("from") == "entry"
	if err := w.svc.ForgotPassword(c.Request.Context(), email, v.Lang, w.svc.BaseURL()); err != nil {
		f := w.form(map[string]string{"email": email})
		if e, ok := apperr.As(err); ok {
			f.Errors = mergeFields(f.Errors, e.Fields)
		}
		if popup {
			render.Status(c, statusOf(err), authpages.ForgotPopup(v, email, problemOf(f, v), false))
			return
		}
		render.Status(c, statusOf(err), authpages.Forgot(v, f, false))
		return
	}
	if popup {
		render.Page(c, authpages.ForgotPopup(v, email, "", true))
		return
	}
	render.Page(c, authpages.Forgot(v, w.form(nil), true))
}

// problemOf is the one line a popup has room for: the field's own refusal, or the
// general one when the service did not name a field.
func problemOf(f authpages.Form, v *view.Ctx) string {
	if why := f.Err("email"); why != "" {
		return why
	}
	if f.Error != "" {
		return f.Error
	}
	return v.Tr("error.generic")
}

func (w *Web) resetPage(c *gin.Context) {
	f := w.form(nil)
	f.Token = c.Query("token")
	render.Page(c, authpages.Reset(view.From(c), f, false))
}

func (w *Web) reset(c *gin.Context) {
	v := view.From(c)
	f := w.form(nil)
	f.Token = c.PostForm("token")
	if err := w.svc.ResetPassword(c.Request.Context(), f.Token, c.PostForm("password")); err != nil {
		if e, ok := apperr.As(err); ok {
			f.Errors = mergeFields(f.Errors, e.Fields)
			if e.Code == "bad_token" {
				f.Error = v.Tr("auth.reset.bad_token")
			}
		} else {
			f.Error = v.Tr("error.body")
		}
		render.Status(c, statusOf(err), authpages.Reset(v, f, false))
		return
	}
	render.Page(c, authpages.Reset(v, f, true))
}

func (w *Web) signout(c *gin.Context) {
	_ = w.svc.Logout(c.Request.Context(), middleware.TokenFrom(c))
	c.SetCookie(middleware.CookieName, "", -1, "/", "", false, true)
	render.Redirect(c, "/welcome")
}

func (w *Web) form(values map[string]string) authpages.Form {
	if values == nil {
		values = map[string]string{}
	}
	return authpages.Form{Values: values, Errors: map[string]string{}, Google: w.svc.GoogleEnabled()}
}

func mergeFields(into, from map[string]string) map[string]string {
	if into == nil {
		into = map[string]string{}
	}
	for k, v := range from {
		into[k] = v
	}
	return into
}

func message(err error, fallback string) string {
	if e, ok := apperr.As(err); ok && e.Message != "" {
		return e.Message
	}
	return fallback
}

func statusOf(err error) int {
	if e, ok := apperr.As(err); ok {
		return e.Status
	}
	return http.StatusInternalServerError
}

// safeNext keeps redirects inside the app.
func safeNext(next string) string {
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") {
		return "/home"
	}
	return next
}

// LockedPage is the handler the locked sections use (RL-03). It is mounted by the
// modules that own those screens, and by the router for the ones not built yet.
func LockedPage(section string) gin.HandlerFunc {
	key, ok := view.LockedForGuest[section]
	if !ok {
		key = "guest.locked.what.default"
	}
	return func(c *gin.Context) {
		render.Status(c, http.StatusOK, authpages.Locked(view.From(c), key))
	}
}

// GuardGuest sends a guest to the locked page instead of the screen (RL-03).
func GuardGuest(section string) gin.HandlerFunc {
	locked := LockedPage(section)
	return func(c *gin.Context) {
		if authctx.Get(c).IsGuest() {
			locked(c)
			c.Abort()
			return
		}
		c.Next()
	}
}

// EnsureGuest turns a visitor into a guest in the middle of a request, so a screen
// that needs a player can carry on instead of sending them away to make one first.
// The entry screen's "Join a room" is why it exists: the design shows a visitor
// typing a code and landing in the lobby as "Guest 4821" (screens 28–30).
func (w *Web) EnsureGuest(c *gin.Context, name string) error {
	v := view.From(c)
	if v.SignedIn() {
		return nil
	}
	s, err := w.svc.StartGuest(c.Request.Context(), GuestInput{
		Name: name, Lang: v.Lang, Client: "web",
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		return err
	}
	setSessionCookie(c, s.Token, s.ExpiresAt)
	// The rest of this request has to see the player who has just been made.
	p := &authctx.Principal{
		ID: s.User.ID, Kind: string(s.User.Kind), Role: string(s.User.Role), Lang: s.User.Lang,
	}
	authctx.Set(c, p)
	v.User = p
	v.DisplayName = s.User.DisplayName
	return nil
}

// SuggestName is the "Guest 4821" the popups put in the name field.
func (w *Web) SuggestName() string { return SuggestGuestName() }

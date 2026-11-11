package auth

import (
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/middleware"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/view"
	authpages "siraj/web/templates/pages/auth"
)

// The V5 entry flow on the website. Each step posts back into the right-hand panel
// — with htmx when it is there, as a whole page when it is not — and every rule it
// shows comes from the service, not from the screen.

func (w *Web) registerEntry(public, private *gin.RouterGroup) {
	public.POST("/signin/code", w.verifyCode)      // the six digits
	public.POST("/signin/resend", w.resendCode)    // a new code, or "call me instead"
	public.POST("/signin/complete", w.completeNew) // naming a new account
	private.POST("/signout/everywhere", w.signoutEverywhere)
}

// entry fills the panel's data: the countries, the guest's points, which providers
// are configured.
func (w *Web) entry(c *gin.Context, step string) authpages.Entry {
	v := view.From(c)
	d := authpages.Entry{
		Step: step, Channel: "phone", Country: DefaultCountry, Remember: true,
		CodeLength: CodeLength, Google: w.svc.GoogleEnabled(), Apple: w.svc.AppleEnabled(),
		Errors: map[string]string{},
	}
	for _, country := range Countries {
		d.Countries = append(d.Countries, authpages.Country{
			Code: country.Code, Name: country.Name, Dial: country.Dial, Digits: country.Digits,
		})
	}
	// What this browser has earned so far travels with the player (AU-05).
	if v.User != nil && v.User.IsGuest() {
		if user, err := w.svc.Me(c.Request.Context(), v.User.ID); err == nil {
			d.GuestPoints = int(user.Xp)
			d.GuestName = user.DisplayName
		}
	}
	return d
}

func fillFromForm(c *gin.Context, d *authpages.Entry) {
	if channel := c.PostForm("channel"); channel == "email" || channel == "phone" {
		d.Channel = channel
	}
	if country := c.PostForm("country"); country != "" {
		d.Country = country
	}
	d.Phone = strings.TrimSpace(c.PostForm("phone"))
	d.Email = strings.TrimSpace(c.PostForm("email"))
	d.Remember = c.PostForm("remember") != ""
	d.UsePassword = c.PostForm("use_password") == "true" || c.PostForm("use_password") == "1"
	// "Your details" travels with the code step so the back link and the steps bar
	// know which of the two flows the player is in.
	d.Name = strings.TrimSpace(c.PostForm("display_name"))
	d.AcceptTerms = c.PostForm("accept_terms") != ""
	if c.PostForm("creating") != "" {
		d.Creating = true
	}
}

// renderEntry answers with the panel alone for htmx, and the whole page otherwise.
func (w *Web) renderEntry(c *gin.Context, d authpages.Entry) {
	v := view.From(c)
	if render.IsHTMX(c) {
		render.Page(c, authpages.EntryCard(v, d))
		return
	}
	render.Page(c, authpages.EntryPage(v, d))
}

// start is screens 01–07: the first step of signing in or creating an account. The
// same handler serves both; which one it is shows in the copy, not in the rules.
func (w *Web) start(c *gin.Context, creating bool) {
	v := view.From(c)
	d := w.entry(c, "signin")
	if creating {
		d.Step = "create"
	}
	fillFromForm(c, &d)
	d.Creating = creating

	// The password path is the old one, and still the quickest for whoever set one.
	if d.Channel == "email" && d.UsePassword && !creating {
		session, err := w.svc.Login(c.Request.Context(), LoginInput{
			Login: d.Email, Password: c.PostForm("password"), Client: "web",
			UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
		})
		if err != nil {
			d.Errors["password"] = message(err, v.Tr("auth.bad_credentials"))
			w.renderEntry(c, d)
			return
		}
		setSessionCookie(c, session.Token, session.ExpiresAt)
		w.signedIn(c, session)
		return
	}

	sent, err := w.svc.StartCode(c.Request.Context(), StartCodeInput{
		Channel: Channel(d.Channel), Country: d.Country, Phone: d.Phone, Email: d.Email,
		Guest: v.User, Lang: v.Lang, IP: c.ClientIP(),
		Creating: creating, DisplayName: d.Name, Password: c.PostForm("password"),
		AcceptTerms: d.AcceptTerms,
	})
	if err != nil {
		if d.Creating {
			d.Step = "create"
		}
		w.codeError(c, &d, err)
		return
	}
	w.toCodeStep(c, d, sent)
}

// toCodeStep hands the panel over to the six boxes.
func (w *Web) toCodeStep(c *gin.Context, d authpages.Entry, sent CodeSent) {
	d.Step = "code"
	d.Destination = sent.Destination
	d.ResendIn = int(sent.ResendIn.Seconds())
	d.DevCode = sent.DevCode
	render.Toast(c, view.From(c).Tr("entry.code_sent", "to", sent.Destination))
	w.renderEntry(c, d)
}

// codeError puts the refusal where it belongs: on the field, or over the button.
func (w *Web) codeError(c *gin.Context, d *authpages.Entry, err error) {
	v := view.From(c)
	if e, ok := apperr.As(err); ok {
		for field, why := range e.Fields {
			d.Errors[field] = why
		}
		if len(e.Fields) == 0 {
			d.Error = e.Message
		}
	} else {
		d.Error = v.Tr("error.generic")
	}
	w.renderEntry(c, *d)
}

// verifyCode is screens 10–14: wrong, right, new number, or welcome back.
func (w *Web) verifyCode(c *gin.Context) {
	v := view.From(c)
	d := w.entry(c, "code")
	fillFromForm(c, &d)

	verified, err := w.svc.VerifyCode(c.Request.Context(), VerifyCodeInput{
		Channel: Channel(d.Channel), Country: d.Country, Phone: d.Phone, Email: d.Email,
		Code: c.PostForm("code"), Remember: d.Remember, Guest: v.User, Lang: v.Lang,
		Client: "web", UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		d.Destination = destinationOf(d)
		w.codeError(c, &d, err)
		return
	}
	if verified.Created {
		setSessionCookie(c, verified.Session.Token, verified.Session.ExpiresAt)
		render.Toast(c, v.Tr("entry.account_created", "name", verified.Session.User.DisplayName))
		w.signedIn(c, verified.Session)
		return
	}
	if verified.NewAccount {
		d.Step = "newaccount"
		d.Destination = verified.Destination
		d.Name = ""
		w.renderEntry(c, d)
		return
	}

	setSessionCookie(c, verified.Session.Token, verified.Session.ExpiresAt)
	// A returning player is greeted by name, with whatever their guest run earned
	// added to the account (screen 14).
	d.Step = "welcome"
	d.Name = verified.Session.User.DisplayName
	d.Level = verified.Session.User.Level
	d.MovedPoints = verified.MovedPoints
	if progress, err := w.svc.db.Q.MyProgress(c.Request.Context(), verified.Session.User.ID); err == nil {
		d.Streak = int(progress.Streak)
	}
	render.Toast(c, v.Tr("entry.signed_in_as", "name", d.Name))
	w.renderEntry(c, d)
}

// resendCode is the timer running out, and "Call me instead".
func (w *Web) resendCode(c *gin.Context) {
	v := view.From(c)
	d := w.entry(c, "code")
	fillFromForm(c, &d)
	sent, err := w.svc.StartCode(c.Request.Context(), StartCodeInput{
		Channel: Channel(d.Channel), Country: d.Country, Phone: d.Phone, Email: d.Email,
		Call: c.PostForm("call") != "", Guest: v.User, Lang: v.Lang, IP: c.ClientIP(),
		Creating: d.Creating, Resend: true,
	})
	if err != nil {
		d.Destination = destinationOf(d)
		w.codeError(c, &d, err)
		return
	}
	d.ByCall = c.PostForm("call") != ""
	w.toCodeStep(c, d, sent)
}

// completeNew is screen 12's button: the account is made and the player is in.
func (w *Web) completeNew(c *gin.Context) {
	v := view.From(c)
	d := w.entry(c, "newaccount")
	fillFromForm(c, &d)
	d.Destination = c.PostForm("destination")
	d.Name = strings.TrimSpace(c.PostForm("display_name"))

	session, err := w.svc.Complete(c.Request.Context(), CompleteInput{
		Channel: Channel(d.Channel), Destination: d.Destination, DisplayName: d.Name,
		Email: strings.TrimSpace(c.PostForm("email")), AcceptTerms: c.PostForm("accept_terms") != "",
		Remember: d.Remember, Guest: v.User, Lang: v.Lang, Client: "web",
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(),
	})
	if err != nil {
		w.codeError(c, &d, err)
		return
	}
	setSessionCookie(c, session.Token, session.ExpiresAt)
	render.Toast(c, v.Tr("entry.account_created", "name", session.User.DisplayName))
	w.signedIn(c, session)
}

// signedIn sends the player where a fresh session lands.
func (w *Web) signedIn(c *gin.Context, session Session) {
	render.Redirect(c, w.after)
}

// signoutEverywhere is the second button of the sign-out confirmation (screen 17).
func (w *Web) signoutEverywhere(c *gin.Context) {
	v := view.From(c)
	if _, err := w.svc.SignOutEverywhere(c.Request.Context(), authctx.Get(c)); err != nil {
		render.Toast(c, message(err, v.Tr("error.generic")))
		render.Redirect(c, "/home")
		return
	}
	c.SetCookie(middleware.CookieName, "", -1, "/", "", false, true)
	render.Toast(c, v.Tr("entry.signed_out_all"))
	render.Redirect(c, "/welcome")
}

func destinationOf(d authpages.Entry) string {
	if d.Channel == "email" {
		return strings.ToLower(d.Email)
	}
	number, _ := NormalisePhone(d.Country, d.Phone)
	return number
}

package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// ForgotForm asks for the email address.
func (h *Handlers) ForgotForm(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusOK, views.Forgot(c, views.ForgotData{}))
}

// Forgot issues a reset link.
//
// It answers identically whether or not the address belongs to an account.
// Saying "no account with that address" would turn this open form into a
// membership check for any address anyone cares to type.
func (h *Handlers) Forgot(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	email := strings.TrimSpace(r.PostFormValue("email"))

	raw, target, err := h.reset.Request(r.Context(), email, service.ClientKey(r))
	if err != nil {
		var fe *service.FieldError
		if errors.As(err, &fe) {
			h.render(w, r, http.StatusTooManyRequests, views.Forgot(c, views.ForgotData{
				Email:   email,
				General: c.T(fe.Key, fe.Args...),
			}))
			return
		}
		h.serverError(w, r, err)
		return
	}

	if target != nil {
		link := h.cfg.BaseURL + "/reset?token=" + url.QueryEscape(raw)
		// The person's own language, not the language of whoever is sitting at
		// this browser — a reset can be requested from anywhere.
		p := h.bundle.Printer(target.Locale)
		h.mailer.SendAsync(service.Mail{
			To:       target.Email,
			Subject:  p.T("auth.reset.mail.subject"),
			Locale:   target.Locale,
			TextBody: p.T("auth.reset.mail.text", target.DisplayName, link, int(service.ResetTTL.Minutes())),
			HTMLBody: service.MailHTML(service.MailBody{
				Greeting: p.T("auth.reset.mail.greeting", target.DisplayName),
				Body:     p.T("auth.reset.mail.body", int(service.ResetTTL.Minutes())),
				Button:   p.T("auth.reset.mail.button"),
				Link:     link,
				Footer:   p.T("auth.reset.mail.ignore"),
			}),
		})
	}

	h.render(w, r, http.StatusOK, views.Forgot(c, views.ForgotData{Sent: true, Email: email}))
}

// ResetForm renders the new-password form for a token.
func (h *Handlers) ResetForm(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	token := r.URL.Query().Get("token")

	if err := h.reset.Check(r.Context(), token); err != nil {
		var fe *service.FieldError
		if errors.As(err, &fe) {
			h.render(w, r, http.StatusGone, views.Reset(c, views.ResetData{Expired: true}))
			return
		}
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.Reset(c, views.ResetData{Token: token}))
}

// Reset redeems the token and sets the new password.
func (h *Handlers) Reset(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	token := r.PostFormValue("token")

	err := h.reset.Redeem(r.Context(), token,
		r.PostFormValue("password"), r.PostFormValue("password_confirm"))

	if err != nil {
		var fe *service.FieldError
		if errors.As(err, &fe) {
			if fe.Field == "" {
				// Expired or already spent: there is nothing to retype.
				h.render(w, r, http.StatusGone, views.Reset(c, views.ResetData{Expired: true}))
				return
			}
			h.render(w, r, http.StatusUnprocessableEntity, views.Reset(c, views.ResetData{
				Token:  token,
				Errors: map[string]string{fe.Field: c.T(fe.Key, fe.Args...)},
			}))
			return
		}
		h.serverError(w, r, err)
		return
	}

	// Deliberately not signing them in: whoever redeemed the link proved they
	// can read the mailbox, which is not the same as proving they know the
	// password they just set.
	h.flash(w, "success", c.T("auth.reset.done"))
	redirect(w, r, "/login")
}

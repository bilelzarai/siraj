package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// Landing shows the marketing page, or bounces signed-in users to the app.
func (h *Handlers) Landing(w http.ResponseWriter, r *http.Request) {
	if userFrom(r) != nil {
		redirect(w, r, "/app")
		return
	}

	c := h.viewCtx(w, r)
	questions, err := h.repo.TotalQuestions(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	cats, err := h.repo.Categories(r.Context(), c.Locale, 0)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	players, _ := h.repo.TotalPlayers(r.Context())

	h.render(w, r, http.StatusOK, views.Landing(c, questions, len(cats), players))
}

func (h *Handlers) LoginForm(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusOK, views.Login(c, views.AuthForm{}))
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	identifier := strings.TrimSpace(r.PostFormValue("identifier"))
	password := r.PostFormValue("password")

	user, err := h.auth.Login(r.Context(), identifier, password, service.ClientKey(r))
	if err != nil {
		var fe *service.FieldError
		if errors.As(err, &fe) {
			form := views.AuthForm{Identifier: identifier, General: c.T(fe.Key, fe.Args...)}

			// Outline both credential fields rather than naming the one that
			// failed: saying "no such user" would let anyone enumerate which
			// accounts exist. The rate-limit and suspension errors are not
			// about a field at all, so they stay as a banner only.
			if fe.Key == "auth.error.invalidCredentials" {
				form.Errors = map[string]string{
					"identifier": c.T("auth.error.checkBoth"),
					"password":   c.T("auth.error.checkBoth"),
				}
			}

			h.render(w, r, http.StatusUnauthorized, views.Login(c, form))
			return
		}
		h.serverError(w, r, err)
		return
	}

	if err := h.auth.StartSession(r.Context(), w, r, user.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	setLocaleCookie(w, h.cfg.SecureCookies, user.Locale)
	// The account is who they are now. Letting the guest cookie stand would
	// mean a later "play without an account" picked up the temporary player
	// they used to be, along with whatever half-finished round it was holding.
	h.players.Retire(r.Context(), w, r)
	h.clearSeat(w)

	redirect(w, r, safeNext(r.URL.Query().Get("next")))
}

func (h *Handlers) RegisterForm(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusOK, views.Register(c, views.AuthForm{}))
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	// Asked, not spent: a rejected form creates no account, and counting it
	// would turn the fifth mistyped confirmation into a refusal to say what was
	// wrong with it.
	if h.tooManyCreations(w, r, service.LimitRegister) {
		return
	}

	in := service.RegisterInput{
		Username:        r.PostFormValue("username"),
		Email:           r.PostFormValue("email"),
		DisplayName:     r.PostFormValue("display_name"),
		Password:        r.PostFormValue("password"),
		PasswordConfirm: r.PostFormValue("password_confirm"),
		Locale:          c.Locale,
	}

	user, err := h.auth.Register(r.Context(), in)
	if err != nil {
		form := views.AuthForm{
			Username:    in.Username,
			Email:       in.Email,
			DisplayName: in.DisplayName,
		}

		// Validation reports every bad field at once; a uniqueness clash comes
		// back from the database as a single one.
		var fes *service.FieldErrors
		var fe *service.FieldError
		switch {
		case errors.As(err, &fes):
			form.Errors = fes.Map(c.T)
		case errors.As(err, &fe):
			if fe.Field == "" {
				form.General = c.T(fe.Key, fe.Args...)
			} else {
				form.Errors = map[string]string{fe.Field: c.T(fe.Key, fe.Args...)}
			}
		default:
			h.serverError(w, r, err)
			return
		}

		h.render(w, r, http.StatusUnprocessableEntity, views.Register(c, form))
		return
	}

	h.recordCreation(r, service.LimitRegister)

	if err := h.auth.StartSession(r.Context(), w, r, user.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	// Whatever they were playing as a guest stays with the guest and is swept
	// on its own timer. Anonymous progress is temporary by design, and pulling
	// it into a fresh account would make that promise conditional.
	h.players.Retire(r.Context(), w, r)
	h.clearSeat(w)
	redirect(w, r, safeNext(r.URL.Query().Get("next")))
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	if err := h.auth.EndSession(r.Context(), w, sessionIDFrom(r)); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.clearSeat(w)
	h.flash(w, "success", c.T("auth.loggedOut"))
	redirect(w, r, "/")
}

// SetLocale persists the language choice for guests (cookie) and for
// signed-in users (account preference), then returns where they came from.
func (h *Handlers) SetLocale(w http.ResponseWriter, r *http.Request) {
	locale := r.PostFormValue("locale")
	if !i18n.IsSupported(locale) {
		locale = h.cfg.DefaultLocale
	}
	setLocaleCookie(w, h.cfg.SecureCookies, locale)

	if user := userFrom(r); user != nil {
		if err := h.repo.UpdatePreferences(r.Context(), user.ID, locale, user.Theme); err != nil {
			h.serverError(w, r, err)
			return
		}
	}

	redirect(w, r, safeNext(r.PostFormValue("redirect")))
}

// safeNext only allows same-site relative paths, so a crafted ?next= cannot
// bounce a freshly authenticated user to another origin.
func safeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/app"
	}
	return next
}

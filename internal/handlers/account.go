package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// History lists the player's finished rounds with simple paging.
func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	ctx := r.Context()

	mode := r.URL.Query().Get("mode")
	if mode != "solo" && mode != "challenge" && mode != "daily" {
		mode = ""
	}

	total, err := h.repo.CountGameHistory(ctx, c.User.ID, mode)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging := h.paging(w, r).withTotal(total)

	games, err := h.repo.GameHistory(ctx, c.User.ID, c.Locale, mode,
		paging.Size, paging.Offset())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.History(c, views.HistoryData{
		Games: games,
		Mode:  mode,
		Total: total,
		Pager: pagerFor(paging),
	}))
}

// Review shows every question of one round with the correct answers.
func (h *Handlers) Review(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}

	session, err := h.repo.Game(r.Context(), id, c.Locale)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	if session.UserID != c.User.ID {
		h.forbidden(w, r)
		return
	}

	answers, err := h.repo.GameAnswers(r.Context(), id, c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// One batched query for the whole page rather than one per question.
	ids := make([]int64, 0, len(answers))
	for _, a := range answers {
		ids = append(ids, int64(a.QuestionID))
	}
	counts, err := h.repo.CommentCounts(r.Context(), ids)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	d := views.ReviewData{Session: session, Answers: answers, CommentCounts: counts}
	if session.ChallengeID != nil {
		if ch, err := h.repo.Challenge(r.Context(), *session.ChallengeID, c.Locale); err == nil {
			d.Challenge = ch
		}
	}

	h.render(w, r, http.StatusOK, views.Review(c, d))
}

// -------------------------------------------------------------- settings --

func (h *Handlers) Settings(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	sessions, err := h.repo.ListSessions(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.Settings(c, views.SettingsData{
		User:          c.User,
		Sessions:      sessions,
		CurrentSessID: sessionIDFrom(r),
	}))
}

func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	err := h.auth.ChangePassword(r.Context(), c.User,
		r.PostFormValue("current_password"),
		r.PostFormValue("new_password"))

	if err != nil {
		var fe *service.FieldError
		if errors.As(err, &fe) {
			sessions, _ := h.repo.ListSessions(r.Context(), c.User.ID)
			data := views.SettingsData{
				User:          c.User,
				Sessions:      sessions,
				CurrentSessID: sessionIDFrom(r),
			}
			if fe.Field != "" {
				data.PasswordErrors = map[string]string{fe.Field: c.T(fe.Key, fe.Args...)}
			} else {
				data.PasswordError = c.T(fe.Key, fe.Args...)
			}
			h.render(w, r, http.StatusUnprocessableEntity, views.Settings(c, data))
			return
		}
		h.serverError(w, r, err)
		return
	}

	// Rotating the password invalidates every other device.
	if err := h.repo.DeleteOtherSessions(r.Context(), c.User.ID, sessionIDFrom(r)); err != nil {
		h.serverError(w, r, err)
		return
	}

	h.flash(w, "success", c.T("settings.password.saved"))
	redirect(w, r, "/settings")
}

func (h *Handlers) RevokeSessions(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	if err := h.repo.DeleteOtherSessions(r.Context(), c.User.ID, sessionIDFrom(r)); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.flash(w, "success", c.T("settings.sessions.revoked"))
	redirect(w, r, "/settings")
}

// DeleteAccount requires the account's own password.
//
// It used to ask for the username typed back — a string on the screen, in the
// URL and in the page title, so a borrowed session was enough to destroy the
// account and everything cascading from it. The username is still asked for,
// as the deliberate step that stops a mis-click; the password is what proves
// it is the owner asking.
func (h *Handlers) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	typed := strings.TrimSpace(r.PostFormValue("username"))
	if !strings.EqualFold(typed, c.User.Username) {
		h.flash(w, "error", c.T("settings.delete.confirm"))
		redirect(w, r, "/settings")
		return
	}
	if !h.auth.PasswordMatches(c.User, r.PostFormValue("password")) {
		h.flash(w, "error", c.T("settings.delete.wrongPassword"))
		redirect(w, r, "/settings")
		return
	}

	if err := h.repo.DeleteUser(r.Context(), c.User.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	_ = h.auth.EndSession(r.Context(), w, sessionIDFrom(r))
	redirect(w, r, "/")
}

// Health is the liveness probe; it verifies the database round trip.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	if err := h.repo.Pool().Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

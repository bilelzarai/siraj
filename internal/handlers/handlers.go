package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/config"
	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSessionID
	ctxLocale
	// ctxActor is who the request is being made on behalf of. Normally the
	// signed-in user; on a shared device it can be a guest sitting at the same
	// keyboard, whose turn it is.
	ctxActor
)

// Handlers wires every dependency the HTTP layer needs.
type Handlers struct {
	cfg      *config.Config
	repo     *repository.Repo
	bundle   *i18n.Bundle
	auth     *service.Auth
	game     *service.Game
	social   *service.Social
	hub      *service.Hub
	presence *service.Presence
	// players makes and retires the people who are not accounts: the anonymous
	// visitor, and the guests they or an account hand the device to.
	players *service.Players

	importer *service.Importer
	// stash holds a previewed upload so applying it does not ask for the file
	// a second time.
	stash *service.ImportStash
	// comparer answers "are these two the same question?" for both screens that
	// raise a duplicate: the import preview and the content-health sweep.
	comparer *service.Comparer
	// writes caps how often one account can create something.
	writes *service.WriteLimits
	// duplicates holds the content-health sweep, which is too slow to run on
	// every visit and too stale-tolerant to need to be.
	duplicates *service.Duplicates
	translator service.Translator
	reset      *service.Reset
	mailer     *service.Mailer
	// uploads stores what people send. Built here from the config rather than
	// passed in: it needs nothing a handler does not already hold, and every
	// caller of New would otherwise have to know about it.
	uploads *service.Uploads

	// assetV is the static-asset content hash, set once in Routes().
	assetV string
}

func New(
	cfg *config.Config,
	repo *repository.Repo,
	bundle *i18n.Bundle,
	auth *service.Auth,
	game *service.Game,
	social *service.Social,
	hub *service.Hub,
	presence *service.Presence,
	importer *service.Importer,
	translator service.Translator,
	reset *service.Reset,
	mailer *service.Mailer,
) *Handlers {
	return &Handlers{
		cfg: cfg, repo: repo, bundle: bundle,
		auth: auth, game: game, social: social,
		hub: hub, presence: presence,
		players:  service.NewPlayers(repo, auth, cfg.SecureCookies),
		importer: importer, stash: service.NewImportStash(),
		comparer:   service.NewComparer(repo),
		writes:     service.NewWriteLimits(),
		duplicates: service.NewDuplicates(repo),
		translator: translator,
		reset:      reset, mailer: mailer,
		uploads: service.NewUploads(repo, cfg.UploadDir, cfg.MaxUploadBytes),
	}
}

// ----------------------------------------------------------- request ctx --

func userFrom(r *http.Request) *models.User {
	u, _ := r.Context().Value(ctxUser).(*models.User)
	return u
}

// actorFrom is whose round, whose score, whose turn this request is about.
//
// It is the signed-in user in every ordinary case. It differs only when a
// device is being shared and the seat cookie names a guest the signed-in user
// created — which the session middleware has already verified, so anything
// reaching here is allowed to be acted for.
func actorFrom(r *http.Request) *models.User {
	if a, ok := r.Context().Value(ctxActor).(*models.User); ok && a != nil {
		return a
	}
	return userFrom(r)
}

func sessionIDFrom(r *http.Request) string {
	s, _ := r.Context().Value(ctxSessionID).(string)
	return s
}

func localeFrom(r *http.Request) string {
	l, _ := r.Context().Value(ctxLocale).(string)
	if l == "" {
		return i18n.DefaultLocale
	}
	return l
}

// viewCtx assembles the per-request template bundle, including the navbar
// counters. Counters are only queried for signed-in users.
func (h *Handlers) viewCtx(w http.ResponseWriter, r *http.Request) views.Ctx {
	locale := localeFrom(r)
	user := userFrom(r)

	c := views.Ctx{
		Tr:     h.bundle.Printer(locale),
		User:   user,
		Acting: actorFrom(r),
		CSRF:   h.auth.IssueCSRF(w, r),
		Path:   r.URL.Path,
		Locale: locale,
		Dir:    i18n.DirOf(locale),
		Theme:  themeFrom(r, user),
		AssetV: h.assetV,
	}

	// Every badge in one query. Five of them, asked one after another, is five
	// round trips before any page starts rendering — invisible on a socket,
	// the dominant cost the moment the database is a network hop away.
	if user != nil {
		if counts, err := h.repo.BadgeCounts(r.Context(), user.ID); err == nil {
			c.UnreadMessages = counts.UnreadMessages
			c.PendingChallenges = counts.PendingChallenges
			c.FriendRequests = counts.FriendRequests
			c.SupportUnread = counts.SupportUnread
			c.Notifications = counts.Notifications
		} else {
			slog.ErrorContext(r.Context(), "badge counts failed", "error", err)
		}
	}

	if f := h.readFlash(w, r); f != nil {
		c.Flash = f
	}
	return c
}

// themeFrom prefers the cookie (set by the client toggle) over the stored
// account preference, so the toggle takes effect without a round trip.
func themeFrom(r *http.Request, user *models.User) string {
	if c, err := r.Cookie(service.ThemeCookie); err == nil {
		switch c.Value {
		case "light", "dark":
			return c.Value
		}
	}
	if user != nil && (user.Theme == "light" || user.Theme == "dark") {
		return user.Theme
	}
	return ""
}

// ------------------------------------------------------------- rendering --

func (h *Handlers) render(w http.ResponseWriter, r *http.Request, status int, comp templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := comp.Render(r.Context(), w); err != nil {
		slog.ErrorContext(r.Context(), "render failed", "path", r.URL.Path, "error", err)
	}
}

// renderFragment writes one component with no page around it, for a panel the
// browser is filling in without navigating anywhere.
func (h *Handlers) renderFragment(w http.ResponseWriter, r *http.Request, comp templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := comp.Render(r.Context(), w); err != nil {
		slog.ErrorContext(r.Context(), "fragment render failed", "path", r.URL.Path, "error", err)
	}
}

// tooManyWrites refuses one write and says so in the person's own language.
// It returns true when the caller should stop.
func (h *Handlers) tooManyWrites(w http.ResponseWriter, r *http.Request, kind string) bool {
	var actor uuid.UUID
	if user := userFrom(r); user != nil {
		actor = user.ID
	}
	if h.writes.Allow(kind, actor, r) {
		return false
	}
	return h.refuseWrite(w, r, kind)
}

// tooManyCreations is the same refusal for the kinds counted on success rather
// than on attempt: it asks whether there is budget, and spends nothing.
func (h *Handlers) tooManyCreations(w http.ResponseWriter, r *http.Request, kind string) bool {
	var actor uuid.UUID
	if user := userFrom(r); user != nil {
		actor = user.ID
	}
	if h.writes.Permit(kind, actor, r) {
		return false
	}
	return h.refuseWrite(w, r, kind)
}

// recordCreation spends one, once the thing has happened.
func (h *Handlers) recordCreation(r *http.Request, kind string) {
	var actor uuid.UUID
	if user := userFrom(r); user != nil {
		actor = user.ID
	}
	h.writes.Record(kind, actor, r)
}

func (h *Handlers) refuseWrite(w http.ResponseWriter, r *http.Request, kind string) bool {
	if isAPIRequest(r) {
		writeJSONError(w, http.StatusTooManyRequests, "slow down")
		return true
	}
	c := h.viewCtx(w, r)
	h.flash(w, "error", c.T("error.tooFast"))
	redirect(w, r, sameSiteReferer(r, "/app"))
	return true
}

func (h *Handlers) NotFound(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusNotFound, views.ErrorPage(c, 404, "error.404.title", "error.404.body"))
}

func (h *Handlers) forbidden(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusForbidden, views.ErrorPage(c, 403, "error.403.title", "error.403.body"))
}

// serverError logs the cause and shows a generic page — the detail never
// reaches the browser.
func (h *Handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "request failed",
		"path", r.URL.Path, "method", r.Method, "error", err)
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusInternalServerError,
		views.ErrorPage(c, 500, "error.500.title", "error.500.body"))
}

// notFoundOrError routes a repository miss to 404 and anything else to 500.
func (h *Handlers) notFoundOrError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		h.NotFound(w, r)
		return
	}
	h.serverError(w, r, err)
}

func redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// --------------------------------------------------------------- flashes --

const flashCookie = "siraj_flash"

// flashKinds is the closed set of banner styles. A value outside it would be
// interpolated straight into a class name.
var flashKinds = map[string]bool{"success": true, "error": true, "info": true, "warning": true}

// setFlash stores a one-shot message in a cookie, consumed on next render.
//
// The payload is signed: the browser holds text that the next page renders as
// its own, so without a signature anyone who can set a cookie — a shared
// machine, a subdomain, an extension — could put arbitrary words in the
// application's voice. The signature makes the cookie a carrier for something
// the server said, not a place the client can write.
func (h *Handlers) flash(w http.ResponseWriter, kind, text string) {
	if !flashKinds[kind] {
		kind = "info"
	}
	payload := kind + "|" + text
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.signFlash(payload)

	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30,
	})
}

func (h *Handlers) readFlash(w http.ResponseWriter, r *http.Request) *views.Flash {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	// Clear it whatever happens next: a flash is one-shot, and a forged one
	// should not survive to be retried against a later request.
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: h.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})

	encoded, mac, ok := strings.Cut(c.Value, ".")
	if !ok {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	payload := string(raw)
	if subtle.ConstantTimeCompare([]byte(mac), []byte(h.signFlash(payload))) != 1 {
		return nil
	}

	kind, text, ok := strings.Cut(payload, "|")
	if !ok || text == "" || !flashKinds[kind] {
		return nil
	}
	return &views.Flash{Kind: kind, Text: text}
}

func (h *Handlers) signFlash(payload string) string {
	mac := hmac.New(sha256.New, []byte(h.cfg.SessionSecret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------- params --

func intParam(r *http.Request, name string, fallback int) int {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func queryInt(r *http.Request, name string, fallback int) int {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// nilIfZero converts the "all categories" sentinel into a NULL filter.
func nilIfZero(id int) *int {
	if id <= 0 {
		return nil
	}
	return &id
}

func parseUUID(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

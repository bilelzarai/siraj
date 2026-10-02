package handlers

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/service"
)

// Session resolves the signed-in user and the negotiated locale, and puts
// both in the request context for everything downstream.
func (h *Handlers) Session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		user, sessionID := h.auth.ResolveSession(ctx, r)

		userLocale := ""
		if user != nil {
			userLocale = user.Locale
		}
		locale := i18n.Negotiate(r, userLocale, h.cfg.DefaultLocale)

		// An explicit ?lang= is sticky for guests via the cookie.
		if q := r.URL.Query().Get("lang"); i18n.IsSupported(q) && q != userLocale {
			setLocaleCookie(w, h.cfg.SecureCookies, q)
		}

		ctx = context.WithValue(ctx, ctxLocale, locale)
		if user != nil {
			ctx = context.WithValue(ctx, ctxUser, user)
			ctx = context.WithValue(ctx, ctxSessionID, sessionID)
			h.presence.Touch(ctx, user.ID)
			// A temporary player is swept on a timer, so every sign of life
			// pushes that timer out. Without this a round played slowly, or a
			// match waiting on somebody else, could be deleted from under the
			// person still looking at it.
			if user.IsGuest() {
				h.players.Touch(ctx, user.ID)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth redirects anonymous visitors to the login page, preserving
// where they were trying to go, and turns away suspended accounts.
//
// Suspending revokes the account's sessions, so a suspended user normally has
// no cookie to present. This is the second lock: it makes "suspended means
// locked out" true of the request rather than true of whichever code path did
// the suspending, so a status set any other way — a direct UPDATE, a future
// bulk tool — cannot leave a working session behind.
func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r)
		if user == nil {
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next := r.URL.RequestURI()
			redirect(w, r, "/login?next="+urlEscape(next))
			return
		}
		if user.IsSuspended() {
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusForbidden, "suspended")
				return
			}
			_ = h.auth.EndSession(r.Context(), w, sessionIDFrom(r))
			c := h.viewCtx(w, r)
			h.flash(w, "error", c.T("auth.error.suspended"))
			redirect(w, r, "/login")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAccount gates everything an account is for.
//
// Anonymous play is deliberately complete as a game — rounds, challenges,
// scores — and deliberately empty of everything that outlives a game: saved
// progress, friends, messages, a history. That split is a rule rather than a
// layout decision, so it is enforced here, on the request, and not by leaving
// links off a page.
//
// It runs inside RequireAuth, so by this point there is a player; the only
// question left is whether they are an account.
func (h *Handlers) RequireAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r)
		if user == nil || !user.IsGuest() {
			next.ServeHTTP(w, r)
			return
		}
		if isAPIRequest(r) {
			writeJSONError(w, http.StatusForbidden, "account required")
			return
		}
		c := h.viewCtx(w, r)
		h.flash(w, "info", c.T("guest.accountNeeded"))
		redirect(w, r, "/register?next="+urlEscape(r.URL.RequestURI()))
	})
}

// RequireGuest keeps signed-in users off the login and register pages.
//
// A temporary player is not signed in for this purpose, and treating them as
// if they were shut the only door out of anonymous play: the avatar menu
// offers "Create an account", the register page bounced it straight back to
// the dashboard, and there was no way to become a real player without first
// working out that logging out was the trick. Signing up is the one thing an
// anonymous player is most likely to want to do.
func (h *Handlers) RequireGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user := userFrom(r); user != nil && !user.IsGuest() {
			redirect(w, r, "/app")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin gates the administrative area.
func (h *Handlers) RequireAdmin(next http.Handler) http.Handler {
	return h.requireRole(next, func(u *models.User) bool { return u.IsAdmin() })
}

// RequireModerator gates content work. Admins satisfy it too.
func (h *Handlers) RequireModerator(next http.Handler) http.Handler {
	return h.requireRole(next, func(u *models.User) bool { return u.CanModerate() })
}

func (h *Handlers) requireRole(next http.Handler, allowed func(*models.User) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r)
		if user == nil {
			redirect(w, r, "/login?next="+urlEscape(r.URL.RequestURI()))
			return
		}
		if !allowed(user) {
			// 404 rather than 403: an unprivileged account should not learn
			// that the admin area exists.
			h.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRF rejects unsafe methods whose token does not match the cookie.
func (h *Handlers) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}

		// PostFormValue is needed for the form-field variant; parsing here
		// is harmless because handlers re-read the cached form.
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			// A hard ceiling on the whole body before anything reads it.
			// ParseMultipartForm's argument is only how much it keeps in
			// memory — the rest goes to temporary files, so on its own it
			// bounds nothing.
			r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes())
			// ParseForm does not read a multipart body, so the token would
			// look absent on every file upload.
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				c := h.viewCtx(w, r)
				if isAPIRequest(r) {
					writeJSON(w, http.StatusRequestEntityTooLarge,
						map[string]any{"error": c.T("upload.tooLarge")})
					return
				}
				h.flash(w, "error", c.T("upload.tooLarge"))
				redirect(w, r, backTo(r, "/app"))
				return
			}
		} else if err := r.ParseForm(); err != nil && !isJSONRequest(r) {
			writeJSONError(w, http.StatusBadRequest, "bad form")
			return
		}

		if !h.auth.VerifyCSRF(r) {
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusForbidden, "csrf")
				return
			}
			c := h.viewCtx(w, r)
			h.flash(w, "error", c.T("error.csrf"))
			// Back to where the form was, but only if that is a page on this
			// site. Referer is set by whoever made the request, so sending the
			// browser to it unchecked turns every CSRF rejection into an open
			// redirect — and an absent Referer into an empty Location, which
			// browsers resolve to the current URL and loop on.
			redirect(w, r, sameSiteReferer(r, "/"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RealIP rewrites RemoteAddr from X-Forwarded-For, counting `hops` entries
// from the *right*.
//
// The direction is the whole point. A forwarding chain reads
// "client, proxy1, proxy2", and every entry to the left of our own proxies was
// written by someone we do not control — the client can simply send
// `X-Forwarded-For: 1.2.3.4` and have it prepended. Only the last `hops`
// entries were appended by infrastructure we trust, so that is where the real
// peer is. chi's own RealIP takes the leftmost value and is deprecated for
// exactly this reason.
//
// hops is how many proxies sit in front of this process: 1 for a single
// reverse proxy, 2 for a CDN in front of one, and so on.
func RealIP(hops int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip := forwardedFor(r.Header.Get("X-Forwarded-For"), hops); ip != "" {
				r.RemoteAddr = ip
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forwardedFor picks the entry `hops` from the end of a comma-separated
// X-Forwarded-For chain, or "" when the header is shorter than the trusted
// chain — a request that reached us with fewer hops than configured did not
// come the way we were told, so its claim is worth nothing.
func forwardedFor(header string, hops int) string {
	if header == "" || hops <= 0 {
		return ""
	}
	parts := strings.Split(header, ",")
	idx := len(parts) - hops
	if idx < 0 {
		return ""
	}
	candidate := strings.TrimSpace(parts[idx])
	if net.ParseIP(candidate) == nil {
		return ""
	}
	return candidate
}

// Recover turns a panic into a 500 instead of a dropped connection.
func (h *Handlers) Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("panic recovered",
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()))
				if !headerWritten(w) {
					h.serverError(w, r, errPanic)
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Logger emits one structured line per request with its status and duration.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if rec.status >= 400 {
			level = slog.LevelWarn
		}
		slog.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"ms", time.Since(start).Milliseconds())
	})
}

// contentSecurityPolicy is deliberately strict on scripts and deliberately
// not on styles: templ emits style attributes throughout, so 'unsafe-inline'
// for styles is the cost of server-rendered markup, while every line of
// JavaScript lives in /static and needs no exception. There is no CDN, no
// remote font and no third-party frame, so everything else is 'self' or 'none'.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	// blob: is for the profile-photo preview, which shows the picture you
	// picked before it has been anywhere near the server.
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"object-src 'none'"

// SecureHeaders sets a conservative baseline.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		hdr.Set("Content-Security-Policy", contentSecurityPolicy)
		hdr.Set("X-Content-Type-Options", "nosniff")
		hdr.Set("X-Frame-Options", "DENY")
		hdr.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Voice notes need the microphone, and nothing else here does.
		hdr.Set("Permissions-Policy", "geolocation=(), microphone=(self), camera=()")
		next.ServeHTTP(w, r)
	})
}

// maxBodyBytes is the largest multipart body any route accepts: the bigger of
// a question import and an attachment, with room for the form around it.
func (h *Handlers) maxBodyBytes() int64 {
	limit := int64(service.MaxImportBytes)
	// One message may carry several files, and the body has to hold all of
	// them. Sized from the same two numbers the composer is told, so the
	// ceiling and the promise cannot drift apart.
	if send := h.cfg.MaxUploadBytes * int64(h.cfg.FilesPerMessage()); send > limit {
		limit = send
	}
	return limit + (1 << 20)
}

// ------------------------------------------------------------- internals --

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.written {
		return
	}
	s.status = code
	s.written = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(b)
}

// Flush keeps SSE working through the recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func headerWritten(w http.ResponseWriter) bool {
	if rec, ok := w.(*statusRecorder); ok {
		return rec.written
	}
	return false
}

func setLocaleCookie(w http.ResponseWriter, secure bool, locale string) {
	http.SetCookie(w, &http.Cookie{
		Name:     service.LocaleCookie,
		Value:    locale,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
	})
}

// isAPIRequest distinguishes fetch() callers so they get JSON, not HTML.
func isAPIRequest(r *http.Request) bool {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		return true
	}
	return r.Header.Get("X-Requested-With") == "fetch"
}

func isJSONRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
}

func urlEscape(s string) string {
	return url.QueryEscape(s)
}

// sameSiteReferer turns the request's Referer into a path on this site, or
// returns the fallback. It is the only place a Referer is allowed to influence
// where the browser goes next, because the header is set by the caller: an
// absolute URL on another host, a scheme-relative "//evil", and an absent
// header all have to resolve to somewhere of our choosing.
func sameSiteReferer(r *http.Request, fallback string) string {
	ref := strings.TrimSpace(r.Referer())
	if ref == "" {
		return fallback
	}

	u, err := url.Parse(ref)
	if err != nil {
		return fallback
	}
	// A relative Referer is already a path on this site; an absolute one counts
	// only when its host matches the host that served this request.
	if u.IsAbs() || u.Host != "" {
		if !strings.EqualFold(u.Host, r.Host) {
			return fallback
		}
	}
	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return fallback
	}

	out := u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}

type sentinelError string

func (e sentinelError) Error() string { return string(e) }

const errPanic sentinelError = "panic recovered"

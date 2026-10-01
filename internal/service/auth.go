package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

const (
	SessionCookie = "siraj_session"
	LocaleCookie  = "locale"
	ThemeCookie   = "theme"
	CSRFCookie    = "siraj_csrf"
	CSRFField     = "csrf_token"
	CSRFHeader    = "X-CSRF-Token"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.]{3,24}$`)

// FieldError carries a translation key for a specific form field, plus any
// arguments that key formats — a suspension reason, for instance, which is
// collected from an admin and was previously stored and never shown to the
// person it is about.
type FieldError struct {
	Field string
	Key   string
	Args  []any
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Key }

// FieldErrors reports every field that failed in one pass. Validating one rule
// at a time and stopping sends someone round the submit loop once per mistake,
// which is the whole reason a long form feels hostile.
type FieldErrors struct {
	Fields []FieldError
}

func (e *FieldErrors) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Error())
	}
	return strings.Join(parts, "; ")
}

// Map renders the failures through a translator, ready for a form struct.
func (e *FieldErrors) Map(t func(string, ...any) string) map[string]string {
	out := make(map[string]string, len(e.Fields))
	for _, f := range e.Fields {
		if _, seen := out[f.Field]; !seen {
			out[f.Field] = t(f.Key, f.Args...)
		}
	}
	return out
}

// Auth owns credentials, sessions and the login rate limiter.
type Auth struct {
	repo     *repository.Repo
	lifetime time.Duration
	secure   bool
	limiter  *attemptLimiter
}

func NewAuth(repo *repository.Repo, lifetime time.Duration, secure bool) *Auth {
	return &Auth{
		repo:     repo,
		lifetime: lifetime,
		secure:   secure,
		limiter:  newAttemptLimiter(8, 10*time.Minute),
	}
}

// RegisterInput is the validated shape of the sign-up form.
type RegisterInput struct {
	Username        string
	Email           string
	DisplayName     string
	Password        string
	PasswordConfirm string
	Locale          string
}

// Register creates a normal player account from the public sign-up form.
func (a *Auth) Register(ctx context.Context, in RegisterInput) (*models.User, error) {
	return a.register(ctx, in, models.RolePlayer, "", false)
}

// RegisterAs is the admin path: the same validation, but the caller picks the
// role and may set a country.
func (a *Auth) RegisterAs(ctx context.Context, in RegisterInput, role, country string) (*models.User, error) {
	switch role {
	case models.RolePlayer, models.RoleModerator, models.RoleAdmin:
	default:
		role = models.RolePlayer
	}
	return a.register(ctx, in, role, country, true)
}

func (a *Auth) register(ctx context.Context, in RegisterInput, role, country string, byAdmin bool) (*models.User, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	in.DisplayName = strings.TrimSpace(in.DisplayName)

	var bad FieldErrors
	if !usernamePattern.MatchString(in.Username) {
		bad.Fields = append(bad.Fields, FieldError{Field: "username", Key: "auth.error.usernameFormat"})
	}
	if _, err := mail.ParseAddress(in.Email); err != nil {
		bad.Fields = append(bad.Fields, FieldError{Field: "email", Key: "auth.error.emailFormat"})
	}
	if len([]rune(in.DisplayName)) < 2 {
		bad.Fields = append(bad.Fields, FieldError{Field: "display_name", Key: "auth.error.displayNameShort"})
	}
	if len(in.Password) < 8 {
		bad.Fields = append(bad.Fields, FieldError{Field: "password", Key: "auth.error.passwordShort"})
	} else if in.Password != in.PasswordConfirm {
		// Only worth reporting once the password itself is usable; otherwise a
		// single typo reads as two separate faults.
		bad.Fields = append(bad.Fields, FieldError{Field: "password_confirm", Key: "auth.error.passwordMismatch"})
	}
	if len(bad.Fields) > 0 {
		return nil, &bad
	}
	if !i18n.IsSupported(in.Locale) {
		in.Locale = i18n.DefaultLocale
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &models.User{
		Username:     in.Username,
		Email:        in.Email,
		DisplayName:  in.DisplayName,
		PasswordHash: string(hash),
		AvatarSeed:   newAvatarSeed(),
		Locale:       in.Locale,
		Role:         role,
		Status:       models.StatusUserActive,
		Country:      country,
	}

	create := a.repo.CreateUser
	if byAdmin {
		create = a.repo.CreateUserWithRole
	}

	if err := create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			if strings.Contains(err.Error(), "email") {
				return nil, &FieldError{Field: "email", Key: "auth.error.emailTaken"}
			}
			return nil, &FieldError{Field: "username", Key: "auth.error.usernameTaken"}
		}
		return nil, err
	}
	return user, nil
}

// Login verifies credentials with a constant-cost path on failure so a
// missing account is not distinguishable by timing.
func (a *Auth) Login(ctx context.Context, identifier, password, clientKey string) (*models.User, error) {
	identifier = strings.TrimSpace(identifier)

	if !a.limiter.allow(clientKey) {
		return nil, &FieldError{Field: "", Key: "auth.error.rateLimited"}
	}

	user, err := a.repo.UserByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Burn a comparable amount of time before reporting failure.
			bcrypt.CompareHashAndPassword(
				[]byte("$2a$10$ovIG89ZuW5r9o0Gh5rUvhO1E8.TDCqmS9NIFP5b8kpfTC0HjZ4zLC"),
				[]byte(password))
			return nil, &FieldError{Field: "", Key: "auth.error.invalidCredentials"}
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, &FieldError{Field: "", Key: "auth.error.invalidCredentials"}
	}

	// A suspended account authenticates correctly but must not get a session.
	// Say why when there is a reason recorded: the alternative is telling
	// someone their account is gone and leaving them with no way to know what
	// to appeal. The credentials already checked out, so this discloses nothing
	// to anyone but the account holder.
	if user.IsSuspended() {
		if reason := strings.TrimSpace(user.SuspendedReason); reason != "" {
			return nil, &FieldError{Key: "auth.error.suspendedWithReason", Args: []any{reason}}
		}
		return nil, &FieldError{Key: "auth.error.suspended"}
	}

	a.limiter.reset(clientKey)
	return user, nil
}

// PasswordMatches reports whether this is the account's current password. It
// is the check a destructive action asks for when there is nothing to rotate —
// deleting the account, for one.
func (a *Auth) PasswordMatches(user *models.User, password string) bool {
	if user == nil || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
}

// ChangePassword re-verifies the current password before rotating the hash.
func (a *Auth) ChangePassword(ctx context.Context, user *models.User, current, next string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)); err != nil {
		return &FieldError{Field: "current_password", Key: "auth.error.wrongPassword"}
	}
	if len(next) < 8 {
		return &FieldError{Field: "new_password", Key: "auth.error.passwordShort"}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.repo.UpdatePassword(ctx, user.ID, string(hash))
}

// StartSession persists a session row and writes the cookie.
func (a *Auth) StartSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	id, err := randomToken(32)
	if err != nil {
		return err
	}

	sess := &models.Session{
		ID:        id,
		UserID:    userID,
		UserAgent: truncate(r.UserAgent(), 256),
		IP:        clientIP(r),
		ExpiresAt: time.Now().Add(a.lifetime),
	}
	if err := a.repo.CreateSession(ctx, sess); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
		MaxAge:   int(a.lifetime.Seconds()),
	})
	return nil
}

// EndSession deletes the row and clears the cookie.
func (a *Auth) EndSession(ctx context.Context, w http.ResponseWriter, sessionID string) error {
	if sessionID != "" {
		if err := a.repo.DeleteSession(ctx, sessionID); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	return nil
}

// ResolveSession looks up the user behind the request's session cookie.
func (a *Auth) ResolveSession(ctx context.Context, r *http.Request) (*models.User, string) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, ""
	}
	user, err := a.repo.SessionUser(ctx, c.Value)
	if err != nil {
		return nil, ""
	}
	return user, c.Value
}

// ------------------------------------------------------------------- CSRF --

// IssueCSRF returns the request's CSRF token, minting and setting one when
// the cookie is missing. The double-submit pattern is enough here because
// the session cookie is SameSite=Lax.
func (a *Auth) IssueCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(CSRFCookie); err == nil && len(c.Value) >= 32 {
		return c.Value
	}
	token, err := randomToken(32)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // read by fetch() for the header variant
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((12 * time.Hour).Seconds()),
	})
	return token
}

// VerifyCSRF compares the submitted token against the cookie.
func (a *Auth) VerifyCSRF(r *http.Request) bool {
	c, err := r.Cookie(CSRFCookie)
	if err != nil || c.Value == "" {
		return false
	}
	submitted := r.Header.Get(CSRFHeader)
	if submitted == "" {
		submitted = r.PostFormValue(CSRFField)
	}
	if submitted == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(submitted)) == 1
}

// -------------------------------------------------------------- internals --

// attemptLimiter is a small in-memory sliding counter keyed by client IP.
// It intentionally keeps no history beyond the window.
type attemptLimiter struct {
	mu      sync.Mutex
	entries map[string]*attemptEntry
	max     int
	window  time.Duration
}

type attemptEntry struct {
	count int
	first time.Time
}

func newAttemptLimiter(max int, window time.Duration) *attemptLimiter {
	l := &attemptLimiter{
		entries: make(map[string]*attemptEntry),
		max:     max,
		window:  window,
	}
	go l.sweep()
	return l
}

// peek reports whether there is budget left, without spending any. It is what
// a caller uses when the thing being limited is the *outcome* rather than the
// attempt — a mistyped form creates nothing, and should not count against
// somebody the way a created account does.
func (l *attemptLimiter) peek(key string) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok || time.Since(e.first) > l.window {
		return true
	}
	return e.count < l.max
}

func (l *attemptLimiter) allow(key string) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	now := time.Now()
	if !ok || now.Sub(e.first) > l.window {
		l.entries[key] = &attemptEntry{count: 1, first: now}
		return true
	}
	e.count++
	return e.count <= l.max
}

func (l *attemptLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *attemptLimiter) sweep() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-l.window)
		l.mu.Lock()
		for k, e := range l.entries {
			if e.first.Before(cutoff) {
				delete(l.entries, k)
			}
		}
		l.mu.Unlock()
	}
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// newAvatarSeed picks a deterministic gradient identity for a new account.
func newAvatarSeed() string {
	token, err := randomToken(6)
	if err != nil {
		return "default"
	}
	return token
}

// truncate caps a string at n bytes without splitting a rune.
//
// Slicing bytes outright can cut a multi-byte character in half, and Postgres
// rejects invalid UTF-8 — so a crafted User-Agent straddling the limit turned
// session creation, and therefore signing in, into a 500.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// clientIP is the address the rate limiters and the session record key off.
//
// It reads RemoteAddr and nothing else. Resolving the forwarding headers is a
// routing concern with a trust decision attached, so it happens once, in the
// router, where chi's RealIP rewrites RemoteAddr — and only when the operator
// has declared a proxy. Re-reading X-Forwarded-For here would reinstate the
// bypass that decision exists to prevent.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	// RealIP leaves a bare address with no port; so does a unix socket peer.
	return strings.TrimSpace(r.RemoteAddr)
}

// ClientKey exposes the limiter key so handlers can pass it to Login.
func ClientKey(r *http.Request) string { return clientIP(r) }

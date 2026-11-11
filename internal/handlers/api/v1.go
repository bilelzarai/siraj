package api

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// Version 1 of the public surface: one read-only route, serving the same rows
// the game draws from, under the same conditions (D2).
//
// It is read-only on purpose. A mutation here would need a second answer to
// every question the session side has already answered — who may write, what
// a conflict means, how a cross-site request is refused — and the moment it
// gains one it needs a version and a written contract first.

const (
	// defaultLimit is what a caller gets for asking for nothing, and maxLimit
	// is the ceiling. The cap is the server's: a consumer asking for the whole
	// bank in one response is a consumer who has not noticed the cursor.
	defaultLimit = 50
	maxLimit     = 200

	// perMinute is how often one key may ask. Generous for mirroring the bank
	// and narrow enough that the answer sheet cannot be scraped in a loop —
	// which is the product decision behind the endpoint being credentialed at
	// all.
	perMinute = 120
)

// V1 is the public surface, built once and mounted above the session layer.
type V1 struct {
	repo  *repository.Repo
	keys  *Keys
	limit *rateLimiter
}

func NewV1(repo *repository.Repo) *V1 {
	return &V1{repo: repo, keys: NewKeys(repo), limit: newRateLimiter(perMinute, time.Minute)}
}

// Routes is the whole surface. Mounted at /api/v1 by the router, above the
// session middleware: no cookie is read here and no cross-site token is
// involved, because a bearer key is a different kind of credential and the two
// must not be interchangeable.
func (v *V1) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(v.keys.Require)
	r.Use(v.throttle)
	r.Get("/questions", v.Questions)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "this surface is read-only")
	})
	return r
}

type questionsResponse struct {
	Data       []*models.APIQuestion `json:"data"`
	NextCursor string                `json:"next_cursor,omitempty"`
	Total      int                   `json:"total"`
}

// Questions serves one page of the bank.
//
// Every condition the game's own draw applies is applied here — active
// question, translated into the asked language, past review, active category,
// active subject area — because this is the same bank seen from outside, and a
// second way of deciding what is servable is a second place to forget that
// unreviewed text is held.
func (v *V1) Questions(w http.ResponseWriter, r *http.Request) {
	locale := strings.TrimSpace(r.URL.Query().Get("locale"))
	if locale == "" {
		locale = i18n.DefaultLocale
	}
	if !i18n.IsSupported(locale) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("locale must be one of %s", strings.Join(supportedCodes(), ", ")))
		return
	}

	difficulty, err := intParam(r, "difficulty")
	if err != nil || difficulty < 0 || difficulty > 3 {
		writeError(w, http.StatusBadRequest, "difficulty must be 1, 2 or 3")
		return
	}

	limit, err := intParam(r, "limit")
	if err != nil || limit < 0 {
		writeError(w, http.StatusBadRequest, "limit must be a positive number")
		return
	}
	switch {
	case limit == 0:
		limit = defaultLimit
	case limit > maxLimit:
		limit = maxLimit
	}

	after, ok := decodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		writeError(w, http.StatusBadRequest, "cursor is not one this endpoint issued")
		return
	}

	questions, total, err := v.repo.APIQuestions(r.Context(), repository.APIQuestionFilter{
		Locale:     locale,
		Category:   strings.TrimSpace(r.URL.Query().Get("category")),
		Domain:     strings.TrimSpace(r.URL.Query().Get("domain")),
		Difficulty: difficulty,
		AfterID:    after,
		Limit:      limit,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "api questions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "unavailable")
		return
	}

	body := questionsResponse{Data: questions, Total: total}
	if body.Data == nil {
		// An empty page is [] and never null: a consumer should not have to
		// handle two shapes for "nothing here".
		body.Data = []*models.APIQuestion{}
	}
	// A cursor only when a full page came back. Issuing one on a short page
	// invites a request that can only return nothing.
	if len(questions) == limit && limit > 0 {
		body.NextCursor = encodeCursor(questions[len(questions)-1].ID)
	}
	writeJSON(w, http.StatusOK, body)
}

func supportedCodes() []string {
	out := make([]string, 0, len(i18n.Supported))
	for _, loc := range i18n.Supported {
		out = append(out, loc.Code)
	}
	return out
}

func intParam(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}

// The cursor is opaque on purpose: it is this endpoint's business that it is
// the last id of the page, and a consumer that learns to build one by hand is
// a consumer that breaks when the ordering changes.
func encodeCursor(id int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("id:" + strconv.Itoa(id)))
}

func decodeCursor(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, false
	}
	value, found := strings.CutPrefix(string(decoded), "id:")
	if !found {
		return 0, false
	}
	id, err := strconv.Atoi(value)
	if err != nil || id < 0 {
		return 0, false
	}
	return id, true
}

// throttle caps how often one key may ask.
//
// Keyed on the credential rather than the address, because an address is a
// building and a key is a consumer. In memory and per process, like the write
// limiter the session side uses: a second instance doubles the allowance,
// which is a note for the day this runs on more than one.
func (v *V1) throttle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := KeyFrom(r.Context())
		if key != nil && !v.limit.allow(key.ID) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type rateLimiter struct {
	mu     sync.Mutex
	hits   map[int][]time.Time
	allowN int
	window time.Duration
}

func newRateLimiter(n int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: map[int][]time.Time{}, allowN: n, window: window}
}

func (l *rateLimiter) allow(id int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-l.window)
	kept := l.hits[id][:0]
	for _, at := range l.hits[id] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= l.allowN {
		l.hits[id] = kept
		return false
	}
	l.hits[id] = append(kept, time.Now())
	return true
}

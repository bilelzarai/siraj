// Package api is the public REST surface: read-only, credentialed, versioned,
// and mounted above the session layer so no cookie is read and no cross-site
// token is involved. A browser session and a bearer key are different kinds of
// credential, and keeping them on different sides of that line is what stops
// one being accepted where the other was meant.
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// KeyPrefix makes a leaked credential recognisable for what it is — in a log,
// in a paste, in a scanner — rather than looking like any other random string.
const KeyPrefix = "siraj_"

// keyBytes is the entropy behind a key. Thirty-two bytes is the same ceiling
// the session secret is held to, and a credential that outlives a browser
// session should not be weaker than one.
const keyBytes = 32

// Mint makes a new key. The plaintext is returned once, to the caller, and
// nothing anywhere stores it: what is written is the hash, which cannot be
// turned back into the key that produced it.
func Mint() (plaintext string, hash []byte, err error) {
	raw := make([]byte, keyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	plaintext = KeyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, sum[:], nil
}

// HashKey is how a presented key is looked up. One function, so minting and
// checking cannot drift into hashing different things.
func HashKey(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}

// Keys answers "who is calling" for the public surface.
type Keys struct {
	repo *repository.Repo
}

func NewKeys(repo *repository.Repo) *Keys { return &Keys{repo: repo} }

// contextKey is unexported so nothing outside this package can plant a key in
// a request and be believed.
type contextKey struct{}

// KeyFrom is the credential this request was authenticated with, if any.
func KeyFrom(ctx context.Context) *models.APIKey {
	key, _ := ctx.Value(contextKey{}).(*models.APIKey)
	return key
}

// Require is the middleware every public route sits behind.
//
//	no header            → 401, and the realm says what is wanted
//	unknown key          → 401, the same answer, so the endpoint cannot be used
//	                       to learn which keys exist
//	revoked key          → 403: this credential is finished, which is a
//	                       different thing to be told than "I do not know you"
func (k *Keys) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented, ok := bearer(r)
		if !ok {
			unauthorized(w, "a bearer key is required")
			return
		}

		key, err := k.repo.APIKeyByHash(r.Context(), HashKey(presented))
		switch {
		case errors.Is(err, repository.ErrNotFound):
			// Deliberately the same answer as a missing header: telling a
			// caller that a key exists but is wrong is telling them a key
			// exists.
			unauthorized(w, "a bearer key is required")
			return
		case err != nil:
			slog.ErrorContext(r.Context(), "api key lookup failed", "error", err)
			writeError(w, http.StatusInternalServerError, "unavailable")
			return
		}

		if !key.Live() {
			writeError(w, http.StatusForbidden, "this key has been revoked")
			return
		}

		if err := k.repo.TouchAPIKey(r.Context(), key.ID); err != nil {
			// Worth knowing, never worth refusing the request over: the answer
			// is correct whether or not the timestamp moved.
			slog.WarnContext(r.Context(), "could not record api key use", "error", err)
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, key)))
	})
}

// bearer reads the header, case-insensitively on the scheme, because clients
// disagree about its capitalisation and the specification does not.
func bearer(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", false
	}
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="siraj"`)
	writeError(w, http.StatusUnauthorized, message)
}

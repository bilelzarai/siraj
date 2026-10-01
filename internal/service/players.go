package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// Playing without an account, and playing several to a device.
//
// Both are the same idea seen from two sides: a player who is not an account.
// The anonymous visitor owns themselves and is remembered by a cookie; a local
// player is made by somebody at the keyboard so a second, third or fourth
// person can take a turn on the same phone. Neither can be befriended, written
// to, ranked or found by name, and everything either of them does is deleted
// when their time is up.

// GuestCookie remembers which anonymous player this browser is.
//
// The session cookie already identifies them, so this is the second rope: a
// session that expires or is cleared would otherwise strand a round in
// progress behind an identity nobody can name any more.
const GuestCookie = "siraj_guest"

var (
	// ErrTooManyLocalPlayers is a device that already has as many people at it
	// as a match can hold.
	ErrTooManyLocalPlayers = errors.New("too many players at this device")
	// ErrNoName is a local player added with nothing to call them. A
	// scoreboard of "Player" and "Player" is not a scoreboard.
	ErrNoName = errors.New("a name is needed")
)

// Players makes and retires the people who are not accounts.
type Players struct {
	repo   *repository.Repo
	auth   *Auth
	secure bool

	// touched throttles the expiry refresh. A temporary player's lifetime is
	// measured in hours, so writing it on every request would be a database
	// round trip per page in service of a decision that moves once an hour.
	mu      sync.Mutex
	touched map[uuid.UUID]time.Time
}

func NewPlayers(repo *repository.Repo, auth *Auth, secureCookies bool) *Players {
	return &Players{
		repo: repo, auth: auth, secure: secureCookies,
		touched: make(map[uuid.UUID]time.Time),
	}
}

// touchWindow is how often a guest's expiry is actually pushed out. Short
// against the lifetime it extends, long against the rate of requests.
const touchWindow = 10 * time.Minute

// Touch keeps a temporary player alive while they are using the site.
func (p *Players) Touch(ctx context.Context, userID uuid.UUID) {
	p.mu.Lock()
	last, seen := p.touched[userID]
	if seen && time.Since(last) < touchWindow {
		p.mu.Unlock()
		return
	}
	p.touched[userID] = time.Now()
	// The map is swept on the same pass rather than by a goroutine of its own:
	// entries older than the window buy nothing, and there are never many.
	if len(p.touched) > 512 {
		cutoff := time.Now().Add(-touchWindow)
		for id, at := range p.touched {
			if at.Before(cutoff) {
				delete(p.touched, id)
			}
		}
	}
	p.mu.Unlock()

	_ = p.repo.TouchGuest(ctx, userID)
}

// StartAnonymous gives this browser a player and signs them in as it.
//
// Returning with a guest cookie whose player is still alive reuses it, so
// pressing "play without an account" twice does not leave a trail of abandoned
// identities — and the round already in progress is still there.
func (p *Players) StartAnonymous(ctx context.Context, w http.ResponseWriter, r *http.Request,
	displayName, locale string) (*models.User, error) {

	key := guestKeyFrom(r)
	if key != "" {
		if existing, err := p.repo.GuestByKey(ctx, key); err == nil {
			if err := p.auth.StartSession(ctx, w, r, existing.ID); err != nil {
				return nil, err
			}
			_ = p.repo.TouchGuest(ctx, existing.ID)
			p.setGuestCookie(w, key)
			return existing, nil
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}

	fresh, err := newGuestKey()
	if err != nil {
		return nil, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = defaultGuestName
	}

	user, err := p.repo.CreateGuest(ctx, nil, fresh, displayName, locale)
	if err != nil {
		return nil, err
	}
	if err := p.auth.StartSession(ctx, w, r, user.ID); err != nil {
		return nil, err
	}
	p.setGuestCookie(w, user.GuestKey)
	return user, nil
}

// AddLocal puts another person at this device, under whoever is signed in.
//
// The host may itself be a temporary player: somebody who came in anonymously
// and then handed the phone round is exactly the case this exists for.
func (p *Players) AddLocal(ctx context.Context, hostID uuid.UUID, displayName, locale string) (*models.User, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil, ErrNoName
	}

	n, err := p.repo.LocalPlayerCount(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if n >= repository.MaxLocalPlayers {
		return nil, ErrTooManyLocalPlayers
	}
	return p.repo.CreateGuest(ctx, &hostID, "", displayName, locale)
}

// RemoveLocal retires a guest and everything they did.
func (p *Players) RemoveLocal(ctx context.Context, hostID, playerID uuid.UUID) error {
	return p.repo.DeleteLocalPlayer(ctx, hostID, playerID)
}

// ClearGuestCookie is called when somebody signs in or registers: the account
// is now who they are, and the anonymous player they were is finished with.
func (p *Players) ClearGuestCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     GuestCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   p.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (p *Players) setGuestCookie(w http.ResponseWriter, key string) {
	if key == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     GuestCookie,
		Value:    key,
		Path:     "/",
		HttpOnly: true,
		Secure:   p.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(repository.GuestLifetime / time.Second),
	})
}

// defaultGuestName is what an anonymous player is called before they say
// otherwise. It is translated at the call site; this is the last resort.
const defaultGuestName = "Guest"

func guestKeyFrom(r *http.Request) string {
	c, err := r.Cookie(GuestCookie)
	if err != nil {
		return ""
	}
	// A cookie is written by whoever holds it. Nothing is trusted about this
	// value beyond its shape — it is looked up, and a key that matches no live
	// guest simply produces a new one.
	key := strings.TrimSpace(c.Value)
	if len(key) > 64 {
		return ""
	}
	for _, r := range key {
		isSafe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_'
		if !isSafe {
			return ""
		}
	}
	return key
}

func newGuestKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

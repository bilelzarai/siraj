package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// Playing without an account, and sharing one device between several people.
//
// A guest is a player and nothing more: they answer questions, hold a place in
// a match and get a score. Everything an account is for — keeping that score,
// friends, messages, a history — is refused for them by RequireAccount rather
// than by leaving a button off a page.

// SeatCookie names which player at this device the next move belongs to.
//
// A cookie rather than a query parameter because the round is a sequence of
// pages — the question, the verdict, the next question — and threading "who is
// answering" through every link and form is how one of them ends up missing it
// and scoring a guest's answer against the host.
const SeatCookie = "siraj_seat"

// seatLifetime is deliberately short. A seat is for the minute somebody else is
// holding the phone; if the device is put down mid-turn, the next person to
// pick it up should be themselves again rather than whoever last played.
const seatLifetime = 2 * time.Hour

// StartGuest gives an anonymous visitor a player and drops them into a round.
//
// This is the whole of "no account required": one press, no form, nothing
// asked. What it makes is temporary and says so.
func (h *Handlers) StartGuest(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	// Already signed in — as an account or as a guest — so there is nothing to
	// start. Following a stale "play as guest" button should not quietly throw
	// away the identity that is already there.
	if c.User != nil {
		redirect(w, r, "/play")
		return
	}

	name := r.PostFormValue("name")
	if name == "" {
		name = c.T("guest.defaultName")
	}
	if _, err := h.players.StartAnonymous(r.Context(), w, r, name, c.Locale); err != nil {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, "/play")
}

// AddLocalPlayer puts another person at this device.
func (h *Handlers) AddLocalPlayer(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	if h.tooManyWrites(w, r, service.LimitFriend) {
		return
	}

	// The host is whoever is signed in, never the seat: a guest cannot make
	// more guests, or one person at a device could fill a match on their own
	// while appearing to be four people.
	host := c.User

	claimed, _, err := h.claimRequest(r, host.ID, "player.add")
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !claimed {
		redirect(w, r, backTo(r, "/play"))
		return
	}

	_, err = h.players.AddLocal(r.Context(), host.ID, r.PostFormValue("name"), c.Locale)
	if err != nil {
		h.releaseRequest(r)
		switch {
		case errors.Is(err, service.ErrNoName), errors.Is(err, repository.ErrInvalid):
			h.flash(w, "error", c.T("players.needName"))
		case errors.Is(err, service.ErrTooManyLocalPlayers):
			h.flash(w, "error", c.T("players.tooMany", repository.MaxLocalPlayers))
		default:
			h.serverError(w, r, err)
			return
		}
		redirect(w, r, backTo(r, "/play"))
		return
	}
	h.completeRequest(r, backTo(r, "/play"))
	h.flash(w, "success", c.T("players.added"))
	redirect(w, r, backTo(r, "/play"))
}

// RemoveLocalPlayer retires a guest and everything they did.
func (h *Handlers) RemoveLocalPlayer(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	playerID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	err := h.players.RemoveLocal(r.Context(), c.User.ID, playerID)
	switch {
	case err == nil:
		// Whoever is being removed cannot go on holding the seat.
		if seatFrom(r) == playerID.String() {
			h.clearSeat(w)
		}
		h.flash(w, "success", c.T("players.removed"))
	case errors.Is(err, repository.ErrConflict):
		h.flash(w, "error", c.T("players.inMatch"))
	case errors.Is(err, repository.ErrNotFound):
		h.NotFound(w, r)
		return
	default:
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, backTo(r, "/play"))
}

// TakeSeat hands the device to one of the players at it, or back to the owner.
//
// It changes nothing but whose turn the next page is about — the round itself
// already exists, or is about to be started for whoever is sitting down.
func (h *Handlers) TakeSeat(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id := r.PostFormValue("player")
	if id == "" || id == c.User.ID.String() {
		h.clearSeat(w)
		redirect(w, r, backTo(r, "/play"))
		return
	}

	playerID, ok := parseUUID(id)
	if !ok {
		h.NotFound(w, r)
		return
	}
	// Verified here as well as in the middleware, so a seat cookie can never
	// be set to somebody the signed-in user does not own.
	player, err := h.repo.LocalPlayer(r.Context(), c.User.ID, playerID)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	h.setSeat(w, player.ID.String())
	h.flash(w, "info", c.T("players.nowPlaying", player.DisplayName))
	redirect(w, r, backTo(r, "/play"))
}

// Seat wires the seat cookie into the request.
//
// It runs after Session, so the signed-in user is already known: the seat is
// only ever resolved against them, and a cookie naming a player they did not
// create is ignored rather than refused — a stale seat should not lock somebody
// out of their own account.
func (h *Handlers) Seat(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r)
		seat := seatFrom(r)
		if user == nil || seat == "" || seat == user.ID.String() {
			next.ServeHTTP(w, r)
			return
		}
		playerID, ok := parseUUID(seat)
		if !ok {
			h.clearSeat(w)
			next.ServeHTTP(w, r)
			return
		}
		player, err := h.repo.LocalPlayer(r.Context(), user.ID, playerID)
		if err != nil {
			h.clearSeat(w)
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(withActor(r, player)))
	})
}

func (h *Handlers) setSeat(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SeatCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(seatLifetime / time.Second),
	})
}

func (h *Handlers) clearSeat(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SeatCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func seatFrom(r *http.Request) string {
	c, err := r.Cookie(SeatCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// localPlayerData is the shared-device panel: who is at this keyboard, and
// whose turn it is. Loaded for the play and challenge screens; a device with
// nobody else at it renders nothing.
func (h *Handlers) localPlayerData(r *http.Request) views.SeatData {
	user := userFrom(r)
	if user == nil {
		return views.SeatData{}
	}
	players, err := h.repo.LocalPlayers(r.Context(), user.ID)
	if err != nil {
		return views.SeatData{}
	}
	// Whether anybody at this device is mid-round. A seat cookie outlives the
	// match it was set for, so without asking, the panel announced a turn in a
	// game that ended days ago.
	playing := false
	if _, err := h.repo.MatchOf(r.Context(), actorFrom(r).ID); err == nil {
		playing = true
	}

	return views.SeatData{
		Host:    user,
		Players: players,
		Acting:  actorFrom(r),
		Max:     repository.MaxLocalPlayers,
		Playing: playing,
	}
}

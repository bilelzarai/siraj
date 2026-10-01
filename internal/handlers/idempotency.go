package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// Making sure one press creates one thing.
//
// The client half of this is a disabled button, which covers a double click and
// nothing else. Everything that actually duplicates work arrives as a separate,
// well-formed request: a second tab with the same form open, a retry after a
// timeout, the browser's "resend?" on a back-and-forward, a flaky connection
// that sent the POST twice.
//
// So the form carries a key, the first request to claim it does the work, and
// every later request with that key is sent where the first one ended up.
//
// requestKeyField is the hidden input the forms carry it in.
const requestKeyField = "request_key"

// claimRequest takes the key this request carries.
//
// It returns claimed=false when the work has already been done, along with
// wherever the first attempt ended up — which may be empty if that attempt is
// still running or died halfway. Either way the caller must not do the work a
// second time.
//
// A request with no key claims nothing and always proceeds, so a form that has
// not been given one behaves exactly as it did before.
func (h *Handlers) claimRequest(r *http.Request, userID uuid.UUID, scope string) (bool, string, error) {
	return h.repo.ClaimRequestKey(r.Context(), r.PostFormValue(requestKeyField), userID, scope)
}

// completeRequest records where a claimed request ended up, so a repeat is sent
// to the same place rather than being told nothing happened.
func (h *Handlers) completeRequest(r *http.Request, result string) {
	key := r.PostFormValue(requestKeyField)
	if key == "" {
		return
	}
	// Deliberately not on the request's context: the response is about to be
	// written and the caller may already be redirecting, but the record of what
	// was made has to survive that.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), requestKeyTimeout)
	defer cancel()
	_ = h.repo.CompleteRequestKey(ctx, key, result)
}

// releaseRequest gives the key back after the work it claimed was refused.
//
// Without it a rejected match — not enough questions, nobody left to invite —
// would take its key down with it, and the corrected resubmission of the same
// form would be told it had already happened.
func (h *Handlers) releaseRequest(r *http.Request) {
	key := r.PostFormValue(requestKeyField)
	if key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), requestKeyTimeout)
	defer cancel()
	_ = h.repo.ReleaseRequestKey(ctx, key)
}

// requestKeyTimeout bounds the bookkeeping write that happens after the work
// itself is done, on a context detached from the request.
const requestKeyTimeout = 3 * time.Second

// withActor puts the player a request is being made on behalf of into its
// context.
func withActor(r *http.Request, player *models.User) context.Context {
	return context.WithValue(r.Context(), ctxActor, player)
}

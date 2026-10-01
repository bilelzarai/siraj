package service

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// WriteLimits caps how often one person can perform each kind of write.
//
// Only sign-in and password reset were limited. Everything else a request can
// create — an account, a comment, a message, a support ticket, a rating, a
// friend request — could be repeated as fast as a loop can post. Ticket
// creation was the sharp one: it writes a row and mails an admin.
//
// Keyed per user where there is one, and per address for the routes that run
// before sign-in. An account is a much better identity than an IP: a household
// or a school is one address, and a signed-in abuser is one account however
// many addresses they come from.
type WriteLimits struct {
	buckets map[string]*attemptLimiter
}

// The kinds a route can ask about. Each is a separate budget, so a chatty
// conversation cannot use up someone's ability to open a support ticket.
const (
	LimitRegister = "register"
	LimitMessage  = "message"
	LimitComment  = "comment"
	LimitTicket   = "ticket"
	LimitRating   = "rating"
	LimitFriend   = "friend"
)

// NewWriteLimits builds the budgets. The numbers are per window and are meant
// to be invisible to a person using the screen and obvious to a script: nobody
// opens four support tickets in an hour by hand.
//
// Registration is the loose one, and deliberately. It is the only kind keyed on
// an address rather than an account, and an address is a building: a school, an
// office, a café signing up a class between them is one IP to us. Ten created
// accounts an hour from one address is generous for a household and still stops
// a loop.
func NewWriteLimits() *WriteLimits {
	return &WriteLimits{buckets: map[string]*attemptLimiter{
		LimitRegister: newAttemptLimiter(10, time.Hour),
		LimitMessage:  newAttemptLimiter(60, time.Minute),
		LimitComment:  newAttemptLimiter(20, 10*time.Minute),
		LimitTicket:   newAttemptLimiter(4, time.Hour),
		LimitRating:   newAttemptLimiter(60, 10*time.Minute),
		LimitFriend:   newAttemptLimiter(30, 10*time.Minute),
	}}
}

// Allow reports whether this actor may perform one more write of this kind, and
// spends one from the budget.
//
// An unknown kind is allowed: a new route that forgets to declare its budget
// should not be silently blocked, it should be noticed and given one.
func (w *WriteLimits) Allow(kind string, actor uuid.UUID, r *http.Request) bool {
	bucket, ok := w.buckets[kind]
	if !ok {
		return true
	}
	return bucket.allow(actorKey(actor, r))
}

// Permit is Allow without spending, for the kinds where what deserves limiting
// is the outcome and not the attempt. Pair it with Record.
//
// Registration is the case: a person mistyping their password confirmation
// three times has created nothing and is not the thing this protects against,
// which is accounts appearing in bulk. Counting rejected forms against them
// also made every validation message a lottery once they had tried a few times.
func (w *WriteLimits) Permit(kind string, actor uuid.UUID, r *http.Request) bool {
	bucket, ok := w.buckets[kind]
	if !ok {
		return true
	}
	return bucket.peek(actorKey(actor, r))
}

// Record spends one from the budget, after the thing actually happened.
func (w *WriteLimits) Record(kind string, actor uuid.UUID, r *http.Request) {
	if bucket, ok := w.buckets[kind]; ok {
		bucket.allow(actorKey(actor, r))
	}
}

// actorKey prefers the account, and falls back to the client address for the
// routes that run before there is one.
func actorKey(actor uuid.UUID, r *http.Request) string {
	if actor != uuid.Nil {
		return "u:" + actor.String()
	}
	if r == nil {
		return ""
	}
	return "ip:" + ClientKey(r)
}

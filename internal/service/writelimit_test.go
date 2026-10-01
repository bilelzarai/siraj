package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// Each kind is its own budget, so a chatty conversation cannot use up the
// ability to open a support ticket.
func TestWriteLimitsAreSeparatePerKind(t *testing.T) {
	limits := NewWriteLimits()
	actor := uuid.New()
	r := httptest.NewRequest(http.MethodPost, "/", nil)

	// Four tickets an hour is the budget; the fifth waits.
	for i := 1; i <= 4; i++ {
		if !limits.Allow(LimitTicket, actor, r) {
			t.Fatalf("ticket %d was refused, want the first four allowed", i)
		}
	}
	if limits.Allow(LimitTicket, actor, r) {
		t.Error("the fifth ticket in an hour was allowed")
	}

	// And that has not touched anything else.
	if !limits.Allow(LimitMessage, actor, r) {
		t.Error("sending a message was refused because tickets ran out")
	}
}

// The account is the identity, not the address: a household is one address, and
// one abuser with a proxy is many.
func TestWriteLimitsKeyOnTheAccount(t *testing.T) {
	limits := NewWriteLimits()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	one, two := uuid.New(), uuid.New()

	for i := 0; i < 4; i++ {
		limits.Allow(LimitTicket, one, r)
	}
	if limits.Allow(LimitTicket, one, r) {
		t.Error("the first account is over budget and was allowed")
	}
	if !limits.Allow(LimitTicket, two, r) {
		t.Error("a different account was refused because of someone else's writes")
	}
}

// Registration happens before there is an account, so it falls back to the
// address it came from.
func TestWriteLimitsFallBackToTheAddress(t *testing.T) {
	limits := NewWriteLimits()
	r := httptest.NewRequest(http.MethodPost, "/register", nil)
	r.RemoteAddr = "203.0.113.7:51000"

	for i := 1; i <= 10; i++ {
		if !limits.Allow(LimitRegister, uuid.Nil, r) {
			t.Fatalf("registration %d was refused, want the first ten allowed", i)
		}
	}
	if limits.Allow(LimitRegister, uuid.Nil, r) {
		t.Error("the eleventh registration from one address in an hour was allowed")
	}

	other := httptest.NewRequest(http.MethodPost, "/register", nil)
	other.RemoteAddr = "203.0.113.8:51000"
	if !limits.Allow(LimitRegister, uuid.Nil, other) {
		t.Error("a different address was refused because of the first one")
	}
}

// Registration counts accounts created, not forms submitted. Somebody mistyping
// their password confirmation has created nothing — and counting it meant the
// fifth attempt was refused with "slow down" instead of being told what was
// wrong, which is how a smoke test of six registrations fell over and how a
// school signing up a class would too.
func TestRegistrationCountsCreationsNotAttempts(t *testing.T) {
	limits := NewWriteLimits()
	r := httptest.NewRequest(http.MethodPost, "/register", nil)
	r.RemoteAddr = "198.51.100.4:40000"

	// Twenty rejected forms in a row leave the budget untouched.
	for i := 0; i < 20; i++ {
		if !limits.Permit(LimitRegister, uuid.Nil, r) {
			t.Fatalf("a rejected form at attempt %d used up the budget", i)
		}
	}

	// Ten accounts actually created do spend it.
	for i := 1; i <= 10; i++ {
		if !limits.Permit(LimitRegister, uuid.Nil, r) {
			t.Fatalf("creating account %d was refused, want the first ten allowed", i)
		}
		limits.Record(LimitRegister, uuid.Nil, r)
	}
	if limits.Permit(LimitRegister, uuid.Nil, r) {
		t.Error("the eleventh account from one address in an hour was allowed")
	}

	// A different building is unaffected.
	other := httptest.NewRequest(http.MethodPost, "/register", nil)
	other.RemoteAddr = "198.51.100.5:40000"
	if !limits.Permit(LimitRegister, uuid.Nil, other) {
		t.Error("a different address was refused because of someone else's signups")
	}
}

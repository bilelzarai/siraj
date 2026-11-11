package social

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"siraj/internal/platform/apperr"
)

// uuidOf builds a predictable id, so the pairing test reads clearly.
func uuidOf(n byte) uuid.UUID {
	var id uuid.UUID
	id[0] = n
	return id
}

// A guest has no account to keep a friendship in, at either end of the request:
// they cannot send one, and nobody can send them one (RL-03).
func TestGuestsAreNeverEitherEndOfAFriendship(t *testing.T) {
	e, ok := apperr.As(ErrGuestSocial)
	if !ok {
		t.Fatal("the guest rule must be an apperr so the screen can explain it")
	}
	if e.Status != http.StatusForbidden {
		t.Errorf("refusing a guest is a 403, got %d", e.Status)
	}
	// It must stay below 500: a refusal is the player's doing, not a fault, and the
	// Friends screen shows the reason instead of the error page.
	if e.Status >= http.StatusInternalServerError {
		t.Error("a refused request must not read as a server error")
	}
	if e.Message == "" {
		t.Error("the refusal has to say something the player can act on")
	}
}

// pair orders a friendship the way the table's CHECK (user_a < user_b) needs, so one
// pair is one row whichever of them asked (FR-03).
func TestPairIsStableWhicheverWayRound(t *testing.T) {
	a, b := pair(uuidOf(1), uuidOf(2))
	c, d := pair(uuidOf(2), uuidOf(1))
	if a != c || b != d {
		t.Fatalf("the same two people gave two different rows: %v/%v vs %v/%v", a, b, c, d)
	}
	if a.String() >= b.String() {
		t.Errorf("the pair is out of order: %v then %v", a, b)
	}
}

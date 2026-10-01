package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/service"
)

func sessionFor(userID uuid.UUID, id string) *models.Session {
	return &models.Session{
		ID: id, UserID: userID, UserAgent: "test", IP: "127.0.0.1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

// The reset flow is the one place where a mistake hands over an account, and it
// was the one flow with no test. These pin the four properties it relies on.
func TestPasswordResetIsSingleUse(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	user := newUser(t, "reset_once", "reset.once@example.com")

	raw := "raw-token-for-single-use"
	hash := service.HashResetToken(raw)
	if err := r.CreatePasswordReset(ctx, hash, user.ID, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := r.PasswordResetUser(ctx, hash, time.Now())
	if err != nil || got != user.ID {
		t.Fatalf("lookup = %v, %v; want the account it belongs to", got, err)
	}
	if err := r.RedeemPasswordReset(ctx, hash, user.ID, "new-hash"); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	// The second redemption of the same link finds nothing to spend.
	if err := r.RedeemPasswordReset(ctx, hash, user.ID, "newer-hash"); err == nil {
		t.Error("the same token was redeemed twice")
	}
	if _, err := r.PasswordResetUser(ctx, hash, time.Now()); err == nil {
		t.Error("a spent token still resolves to its account")
	}
}

func TestPasswordResetExpires(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	user := newUser(t, "reset_stale", "reset.stale@example.com")

	hash := service.HashResetToken("raw-token-already-stale")
	if err := r.CreatePasswordReset(ctx, hash, user.ID, time.Now().Add(-time.Minute), "test"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := r.PasswordResetUser(ctx, hash, time.Now()); err == nil {
		t.Error("an expired token resolved to its account")
	}
	if err := r.RedeemPasswordReset(ctx, hash, user.ID, "new-hash"); err == nil {
		t.Error("an expired token was redeemed")
	}
}

// Issuing a link kills the ones before it, so a forwarded or intercepted older
// email cannot be used once the owner has asked again.
func TestPasswordResetSupersedesTheOneBefore(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	user := newUser(t, "reset_again", "reset.again@example.com")

	first := service.HashResetToken("first-link")
	second := service.HashResetToken("second-link")
	if err := r.CreatePasswordReset(ctx, first, user.ID, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := r.CreatePasswordReset(ctx, second, user.ID, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := r.PasswordResetUser(ctx, first, time.Now()); err == nil {
		t.Error("the superseded link still works")
	}
	if _, err := r.PasswordResetUser(ctx, second, time.Now()); err != nil {
		t.Errorf("the newest link does not work: %v", err)
	}
}

// Redeeming signs every device out: whoever forced the reset, the other party
// is gone by the time it returns.
func TestPasswordResetEndsEverySession(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	user := newUser(t, "reset_sessions", "reset.sessions@example.com")

	for _, id := range []string{"session-one", "session-two"} {
		if err := r.CreateSession(ctx, sessionFor(user.ID, id)); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}
	if sessions, err := r.ListSessions(ctx, user.ID); err != nil || len(sessions) != 2 {
		t.Fatalf("sessions before = %d, %v; want 2", len(sessions), err)
	}

	hash := service.HashResetToken("kick-everyone-out")
	if err := r.CreatePasswordReset(ctx, hash, user.ID, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.RedeemPasswordReset(ctx, hash, user.ID, "new-hash"); err != nil {
		t.Fatalf("redeem: %v", err)
	}

	sessions, err := r.ListSessions(ctx, user.ID)
	if err != nil {
		t.Fatalf("sessions after: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("%d sessions survived the reset, want none", len(sessions))
	}
}

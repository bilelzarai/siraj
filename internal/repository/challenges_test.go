package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// A match that has been called off is over. Attaching a round to it, or
// settling it, must not quietly bring it back to something playable.
//
// This replaces a test about the two-player path that no longer exists; the
// property it was protecting — a closed match stays closed, and a failed
// attempt does not revive it — is the same one, on the path that is live.
func TestAClosedMatchCannotBeRejoined(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	host := newUser(t, "duel_a", "duel.a@example.com")
	rival := newUser(t, "duel_b", "duel.b@example.com")
	befriend(t, host.ID, rival.ID)
	q := addQuestion(t, 930001, "Which prophet is called Kalimullah?")

	ch := &models.Challenge{
		HostID:     host.ID,
		Difficulty: 1, QuestionIDs: []int{q},
	}
	if err := r.CreateMatch(ctx, ch, []repository.MatchSeat{{UserID: rival.ID}}); err != nil {
		t.Fatalf("create match: %v", err)
	}
	if err := r.JoinMatch(ctx, ch.ID, rival.ID); err != nil {
		t.Fatalf("join: %v", err)
	}

	session := &models.GameSession{
		UserID: rival.ID, Mode: models.ModeChallenge, Locale: "en",
		QuestionIDs: []int{q}, TotalQuestions: 1, ChallengeID: &ch.ID,
	}
	if err := r.CreateGame(ctx, session); err != nil {
		t.Fatalf("create round: %v", err)
	}

	// While it is open, attaching works.
	if err := r.AttachMatchSession(ctx, ch.ID, rival.ID, session.ID); err != nil {
		t.Fatalf("attaching to an open match: %v", err)
	}

	// The host calls it off.
	outcome, err := r.CancelMatch(ctx, ch.ID, host.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !outcome.Cancelled || !outcome.ByHost {
		t.Fatalf("the host's cancel did not cancel: %+v", outcome)
	}

	after, err := r.Challenge(ctx, ch.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != models.ChallengeCancelled {
		t.Fatalf("status = %q, want cancelled", after.Status)
	}

	// Nobody is left holding it.
	for _, who := range []uuid.UUID{host.ID, rival.ID} {
		busy, err := r.InActiveMatch(ctx, who)
		if err != nil {
			t.Fatal(err)
		}
		if busy {
			t.Errorf("%v is still held by a cancelled match", who)
		}
	}

	// And nothing brings it back: not a late join, not a status write that
	// thinks the match is still open.
	if err := r.JoinMatch(ctx, ch.ID, rival.ID); !errors.Is(err, repository.ErrConflict) {
		t.Errorf("joining a cancelled match returned %v, want a conflict", err)
	}
	if err := r.SetChallengeStatus(ctx, ch.ID, models.ChallengeAccepted); err == nil {
		reloaded, _ := r.Challenge(ctx, ch.ID, "en")
		if reloaded != nil && reloaded.Status != models.ChallengeCancelled {
			t.Errorf("a cancelled match was reopened as %q", reloaded.Status)
		}
	}
}

// Nothing else deletes from these two tables, so the sweeps are the only thing
// standing between them and unbounded growth.
func TestPurgesDropOnlyWhatIsSpent(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	user := newUser(t, "sweep_probe", "sweep.probe@example.com")

	if err := r.Notify(ctx, user.ID, "friend.request", map[string]any{"user_id": user.ID.String()}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	// A fresh notification survives a sweep of everything older than an hour.
	if _, err := r.PurgeNotificationsOlderThan(ctx, time.Hour); err != nil {
		t.Fatalf("purge notifications: %v", err)
	}
	items, err := r.Notifications(ctx, user.ID, 10, 0)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d notifications, want the fresh one kept", len(items))
	}

	// A live reset token survives; an expired one does not. Two accounts,
	// because issuing a token deliberately deletes that account's earlier
	// unused ones — one outstanding link per person is the whole design.
	other := newUser(t, "sweep_probe_2", "sweep.probe.2@example.com")
	live := "live-token-hash"
	dead := "dead-token-hash"
	if err := r.CreatePasswordReset(ctx, live, user.ID, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("create live reset: %v", err)
	}
	if err := r.CreatePasswordReset(ctx, dead, other.ID, time.Now().Add(-time.Hour), "test"); err != nil {
		t.Fatalf("create expired reset: %v", err)
	}
	if _, err := r.PurgeSpentPasswordResets(ctx); err != nil {
		t.Fatalf("purge resets: %v", err)
	}
	if _, err := r.PasswordResetUser(ctx, live, time.Now()); err != nil {
		t.Errorf("the live token was swept: %v", err)
	}
	if _, err := r.PasswordResetUser(ctx, dead, time.Now()); err == nil {
		t.Error("the expired token is still redeemable")
	}
}

// A notification carries its subject's id in jsonb, which no foreign key can
// police. Delete the ticket and the notification stays, pointing at a page that
// answers "not found" — which is what a player sees when they press it.
func TestPurgeOrphanedNotifications(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	staff := newUser(t, "orphan_staff", "orphan.staff@example.com")
	owner := newUser(t, "orphan_owner", "orphan.owner@example.com")

	ticket := &models.Ticket{
		UserID: owner.ID, Kind: models.TicketQuestion,
		Subject: "Still here", Priority: models.PriorityNormal,
	}
	if err := r.CreateTicket(ctx, ticket, "Body."); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	// One notification about a ticket that exists, one about a ticket that
	// never will.
	gone := uuid.New()
	if err := r.Notify(ctx, staff.ID, "support.new", map[string]any{"ticket_id": ticket.ID.String()}); err != nil {
		t.Fatalf("notify live: %v", err)
	}
	if err := r.Notify(ctx, staff.ID, "support.new", map[string]any{"ticket_id": gone.String()}); err != nil {
		t.Fatalf("notify orphan: %v", err)
	}

	if _, err := r.PurgeOrphanedNotifications(ctx); err != nil {
		t.Fatalf("purge: %v", err)
	}

	items, err := r.Notifications(ctx, staff.ID, 10, 0)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d notifications, want only the one whose ticket exists", len(items))
	}
	if got := items[0].Payload["ticket_id"]; got != ticket.ID.String() {
		t.Errorf("the surviving notification points at %v, want the live ticket", got)
	}
}

// Opening one notification clears that one. Marking the whole list read was the
// only way to clear anything, so a list you had worked through still looked
// untouched.
func TestMarkNotificationReadClearsOnlyThatOne(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	user := newUser(t, "notif_read", "notif.read@example.com")

	for i := 0; i < 2; i++ {
		if err := r.Notify(ctx, user.ID, "friend.request",
			map[string]any{"user_id": user.ID.String()}); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	items, err := r.Notifications(ctx, user.ID, 10, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("setup: %d notifications, %v", len(items), err)
	}

	if err := r.MarkNotificationRead(ctx, items[0].ID, user.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	unread, err := r.UnreadNotificationCount(ctx, user.ID)
	if err != nil {
		t.Fatalf("unread: %v", err)
	}
	if unread != 1 {
		t.Errorf("unread = %d after opening one of two, want 1", unread)
	}

	// And somebody else cannot clear it for them.
	other := newUser(t, "notif_other", "notif.other@example.com")
	if err := r.MarkNotificationRead(ctx, items[1].ID, other.ID); err != nil {
		t.Fatalf("mark read as a stranger: %v", err)
	}
	if again, _ := r.UnreadNotificationCount(ctx, user.ID); again != 1 {
		t.Errorf("unread = %d after a stranger tried to clear it, want 1", again)
	}
	if _, err := r.Notification(ctx, items[1].ID, other.ID); err == nil {
		t.Error("a stranger loaded someone else's notification")
	}
}

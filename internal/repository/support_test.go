package repository_test

import (
	"context"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// A person who solved their own problem can close their own ticket — and only
// their own.
func TestCloseTicketAsOwner(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	owner := newUser(t, "ticket_owner", "ticket.owner@example.com")
	stranger := newUser(t, "ticket_other", "ticket.other@example.com")

	ticket := &models.Ticket{
		UserID:   owner.ID,
		Kind:     models.TicketQuestion,
		Subject:  "How do duels work?",
		Priority: models.PriorityNormal,
	}
	if err := r.CreateTicket(ctx, ticket, "Asking."); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	// Somebody else's ticket is not theirs to close.
	if err := r.CloseTicketAsOwner(ctx, ticket.ID, stranger.ID); err == nil {
		t.Error("a stranger closed someone else's ticket")
	}

	if err := r.CloseTicketAsOwner(ctx, ticket.ID, owner.ID); err != nil {
		t.Fatalf("owner closing their own ticket: %v", err)
	}
	after, err := r.Ticket(ctx, ticket.ID, owner.ID, false)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Status != models.TicketClosed {
		t.Errorf("status = %q, want closed", after.Status)
	}

	// Closing twice is not an error the screen should show; it is already done.
	if err := r.CloseTicketAsOwner(ctx, ticket.ID, owner.ID); err == nil {
		t.Error("closing an already-closed ticket reported success")
	}
}

// A moderator who has read a question's ratings and judged it sound can say so,
// and it leaves the alert — until somebody rates it again.
func TestDismissRatingsEmptiesTheAlertUntilTheNextRating(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	q := addQuestion(t, 940001, "Which night is better than a thousand months?")
	raters := []string{"rater_one", "rater_two", "rater_three"}
	for i, name := range raters {
		u := newUser(t, name, name+"@example.com")
		if err := r.RateQuestion(ctx, int64(q), u.ID, 1); err != nil {
			t.Fatalf("rate %d: %v", i, err)
		}
	}

	flagged, err := r.CountPoorlyRated(ctx, 2.0, 3)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if flagged == 0 {
		t.Fatal("three one-star ratings did not flag the question")
	}

	if err := r.DismissRatings(ctx, q); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	after, err := r.CountPoorlyRated(ctx, 2.0, 3)
	if err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != flagged-1 {
		t.Errorf("flagged = %d after dismissing one of %d", after, flagged)
	}

	// A new rating brings it back: dismissing settled the ratings that existed,
	// not every rating the question will ever get.
	fresh := newUser(t, "rater_four", "rater_four@example.com")
	if err := r.RateQuestion(ctx, int64(q), fresh.ID, 1); err != nil {
		t.Fatalf("rate again: %v", err)
	}
	again, err := r.CountPoorlyRated(ctx, 2.0, 3)
	if err != nil {
		t.Fatalf("count again: %v", err)
	}
	if again != flagged {
		t.Errorf("flagged = %d after a new rating, want it back at %d", again, flagged)
	}
}

// A closed ticket is not a wall: the problem coming back is the most likely
// reason anybody returns to one, and opening a second ticket would lose
// everything already said about it.
func TestReopenTicket(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	owner := newUser(t, "reopen_owner", "reopen.owner@example.com")
	stranger := newUser(t, "reopen_other", "reopen.other@example.com")

	ticket := &models.Ticket{
		UserID: owner.ID, Kind: models.TicketBug,
		Subject: "It happened again", Priority: models.PriorityNormal,
	}
	if err := r.CreateTicket(ctx, ticket, "The first time."); err != nil {
		t.Fatalf("create: %v", err)
	}

	// An open ticket has nothing to reopen.
	if err := r.ReopenTicket(ctx, ticket.ID, owner.ID); err == nil {
		t.Error("an open ticket was reopened")
	}

	if err := r.CloseTicketAsOwner(ctx, ticket.ID, owner.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	// And only its owner may.
	if err := r.ReopenTicket(ctx, ticket.ID, stranger.ID); err == nil {
		t.Error("a stranger reopened somebody else's ticket")
	}
	if err := r.ReopenTicket(ctx, ticket.ID, owner.ID); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	after, err := r.Ticket(ctx, ticket.ID, owner.ID, false)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Status != models.TicketOpen {
		t.Errorf("status = %q, want open", after.Status)
	}
	if after.ResolvedAt != nil {
		t.Error("the resolved stamp survived reopening")
	}
}

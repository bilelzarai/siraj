package handlers_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// The writes that were dereferencing nil until the walk found them. Each one is
// a thing a person does, checked by doing it.
func TestEveryWritePathAnswers(t *testing.T) {
	a := newApp(t)
	a.register("writer")

	// A support ticket, and a reply on it.
	status, loc := a.post("/support/new", url.Values{
		"kind": {models.TicketQuestion}, "subject": {"Does this work?"},
		"body": {"Asking because it did not."},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("opening a ticket → %d, want a redirect", status)
	}
	ticketPath := loc
	if !strings.HasPrefix(ticketPath, "/support/") {
		t.Fatalf("opening a ticket redirected to %q", ticketPath)
	}
	if code, _ := a.get(ticketPath); code != http.StatusOK {
		t.Errorf("the new ticket's page → %d", code)
	}
	if code, _ := a.post(ticketPath+"/reply", url.Values{"body": {"Following up."}}); code != http.StatusSeeOther {
		t.Errorf("replying on a ticket → %d", code)
	}
	// And closing it, which the owner could not do before this week.
	if code, _ := a.post(ticketPath+"/close", url.Values{}); code != http.StatusSeeOther {
		t.Errorf("closing your own ticket → %d", code)
	}

	// A friend request to somebody who exists.
	other := newApp(t)
	other.register("writee")
	if code, _ := a.post("/friends/request", url.Values{"username": {"writee"}}); code != http.StatusSeeOther {
		t.Errorf("sending a friend request → %d", code)
	}
	// Blocking, which had no way to happen at all until this week.
	if code, _ := a.post("/friends/block", url.Values{"username": {"writee"}}); code != http.StatusSeeOther {
		t.Errorf("blocking someone → %d", code)
	}
	if code, _ := a.post("/friends/unblock", url.Values{"username": {"writee"}}); code != http.StatusSeeOther {
		t.Errorf("unblocking someone → %d", code)
	}

	// Preferences and the language switcher.
	if code, _ := a.post("/settings/locale", url.Values{"locale": {"fr"}, "redirect": {"/app"}}); code != http.StatusSeeOther {
		t.Errorf("switching language → %d", code)
	}
	if code, _ := a.post("/profile/edit", url.Values{"display_name": {"Writer"}, "bio": {"Testing."}}); code != http.StatusSeeOther {
		t.Errorf("saving a profile → %d", code)
	}
}

// Opening a notification marks it read and lands on what it is about — and when
// that thing is gone, on the list rather than "Page not found", which is the
// 404 a stale support notification produced.
func TestOpeningANotificationLands(t *testing.T) {
	a := newApp(t)
	a.register("notifier")

	user, err := a.repo.UserByUsername(t.Context(), "notifier")
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	// A notification about a ticket that does not exist — exactly the state
	// twenty-four of them were in.
	if err := a.repo.Notify(t.Context(), user.ID, "support.reply",
		map[string]any{"ticket_id": "0c390c0c-97c1-4293-8c50-09c3b1180003"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	items, err := a.repo.Notifications(t.Context(), user.ID, 10, 0)
	if err != nil || len(items) == 0 {
		t.Fatalf("notifications: %d, %v", len(items), err)
	}

	status, body := a.get("/notifications")
	if status != http.StatusOK {
		t.Fatalf("the notifications screen → %d", status)
	}
	if !strings.Contains(body, "/open") {
		t.Error("notifications do not link through the opener")
	}

	res, err := a.client.Get(a.server.URL + "/notifications/" + strconv.FormatInt(items[0].ID, 10) + "/open")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("opening a notification → %d, want a redirect", res.StatusCode)
	}
	if got := res.Header.Get("Location"); got != "/support" {
		t.Errorf("a notification whose ticket is gone went to %q, want the support list", got)
	}

	// And it is no longer unread.
	unread, err := a.repo.UnreadNotificationCount(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("unread: %v", err)
	}
	if unread != 0 {
		t.Errorf("unread = %d after opening the only one, want 0", unread)
	}
}

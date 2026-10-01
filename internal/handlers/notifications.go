package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"

	"github.com/bilelzarai/siraj/internal/views"
)

// Notifications is the history of everything that happened while the player was
// away.
//
// Seven places write into this table — friend requests and accepts, duel
// invitations, declines and outcomes, new messages, new tickets — and until this
// screen existed nothing read any of them back. The badges cover what is
// outstanding right now; this covers what happened.
func (h *Handlers) Notifications(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	total, err := h.repo.CountNotifications(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging := h.paging(w, r).withTotal(total)

	items, err := h.repo.Notifications(r.Context(), c.User.ID, paging.Size, paging.Offset())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	unread, _ := h.repo.UnreadNotificationCount(r.Context(), c.User.ID)

	h.render(w, r, http.StatusOK, views.Notifications(c, views.NotificationsData{
		Items:  items,
		Unread: unread,
		Pager:  pagerFor(paging),
	}))
}

// OpenNotification is what pressing one does: mark it read, then go to what it
// is about.
//
// Following the raw target had two faults. It left the notification bold, so a
// list you had worked through still looked untouched; and when the subject had
// been deleted — a ticket closed and removed, a duel expired away — the link
// landed on "Page not found", which is the app telling you your own
// notification was a lie. A missing subject now lands on that section's list.
func (h *Handlers) OpenNotification(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}

	n, err := h.repo.Notification(r.Context(), id, c.User.ID)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	if err := h.repo.MarkNotificationRead(r.Context(), id, c.User.ID); err != nil {
		slogError(r, err)
	}

	target, ok := h.notificationTarget(r, n)
	if !ok {
		h.flash(w, "info", c.T("notifications.gone"))
	}
	redirect(w, r, target)
}

// notificationTarget resolves what a notification is about, and reports false
// when that thing no longer exists — in which case the destination is the
// section it belonged to rather than a page that is not there.
func (h *Handlers) notificationTarget(r *http.Request, n *models.Notification) (string, bool) {
	ctx := r.Context()
	user := userFrom(r)

	switch n.Kind {
	case "support.new":
		id, ok := notificationUUID(n, "ticket_id")
		if !ok {
			return "/admin/support", false
		}
		if _, err := h.repo.Ticket(ctx, id, user.ID, true); err != nil {
			return "/admin/support", false
		}
		return "/admin/support/" + id.String(), true

	case "support.reply":
		id, ok := notificationUUID(n, "ticket_id")
		if !ok {
			return "/support", false
		}
		if _, err := h.repo.Ticket(ctx, id, user.ID, false); err != nil {
			return "/support", false
		}
		return "/support/" + id.String(), true

	case "message.new":
		id, ok := notificationUUID(n, "conversation_id")
		if !ok {
			return "/messages", false
		}
		if _, err := h.repo.Conversation(ctx, id, user.ID); err != nil {
			return "/messages", false
		}
		return "/messages/" + id.String(), true

	case "challenge.received", "challenge.declined", "challenge.played", "challenge.completed":
		id, ok := notificationUUID(n, "challenge_id")
		if !ok {
			return "/challenges", false
		}
		if _, err := h.repo.Challenge(ctx, id, localeFrom(r)); err != nil {
			return "/challenges", false
		}
		return "/challenges", true

	case "friend.request", "friend.accepted":
		return "/friends?tab=requests", true
	}
	return "/app", true
}

func notificationUUID(n *models.Notification, key string) (uuid.UUID, bool) {
	raw, ok := n.Payload[key].(string)
	if !ok {
		return uuid.Nil, false
	}
	return parseUUID(raw)
}

// MarkNotificationsRead clears the unread state for the whole list.
func (h *Handlers) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	if err := h.repo.MarkNotificationsRead(r.Context(), c.User.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.flash(w, "success", c.T("notifications.allRead"))
	redirect(w, r, "/notifications")
}

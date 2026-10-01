package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// Threads with more than two people in them.
//
// A group is people somebody chose and is closed; a room is a subject and is
// open. They share every screen a direct thread uses — the same panel, the
// same composer, the same live stream — and differ only in who may get in.

// NewGroup is the screen for starting a group: a name, and which friends.
func (h *Handlers) NewGroup(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	friends, err := h.repo.Friends(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, views.NewThread(c, views.NewThreadData{
		Kind: models.ConversationGroup, Friends: friends,
	}))
}

// NewRoom is the screen for opening a room: a name and what it is about.
func (h *Handlers) NewRoom(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusOK, views.NewThread(c, views.NewThreadData{
		Kind: models.ConversationRoom,
	}))
}

// CreateThread opens a group or a room and sends its owner straight into it.
func (h *Handlers) CreateThread(w http.ResponseWriter, r *http.Request) {
	if h.tooManyWrites(w, r, service.LimitMessage) {
		return
	}
	c := h.viewCtx(w, r)

	kind := r.PostFormValue("kind")
	title := r.PostFormValue("title")
	topic := r.PostFormValue("topic")

	// Who was ticked. Usernames rather than ids, because that is what the
	// form shows and what a person can check by reading it.
	var members []uuid.UUID
	for _, name := range r.PostForm["member"] {
		card, err := h.repo.UserCardByUsername(r.Context(), strings.TrimSpace(name))
		if err != nil {
			continue
		}
		members = append(members, card.ID)
	}

	// One press, one group. A second tab with the same form open, or a resent
	// POST, otherwise made a second group with the same name and the same
	// people in it.
	claimed, previous, err := h.claimRequest(r, c.User.ID, "thread.create")
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !claimed {
		if previous != "" {
			redirect(w, r, previous)
			return
		}
		redirect(w, r, "/messages")
		return
	}

	conv, err := h.social.OpenThread(r.Context(), kind, title, topic, c.User.ID, members)
	if err != nil {
		h.releaseRequest(r)
		key := "threads.failed"
		switch {
		case errors.Is(err, service.ErrInvalidThread), errors.Is(err, repository.ErrInvalid):
			key = "threads.needName"
		case errors.Is(err, service.ErrNotFriends):
			key = "threads.friendsOnly"
		case errors.Is(err, service.ErrInRoom), errors.Is(err, repository.ErrBusy):
			key = "rooms.leaveFirst"
		default:
			h.serverError(w, r, err)
			return
		}
		h.flash(w, "error", c.T(key))
		if kind == models.ConversationRoom {
			redirect(w, r, "/messages/new/room")
		} else {
			redirect(w, r, "/messages/new/group")
		}
		return
	}
	h.completeRequest(r, "/messages/"+conv.ID.String())
	redirect(w, r, "/messages/"+conv.ID.String())
}

// JoinRoom is somebody walking into a room. It is the one thread anybody may
// enter without being invited, which is what makes it a room.
func (h *Handlers) JoinRoom(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	left, err := h.social.JoinRoom(r.Context(), convID, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.NotFound(w, r)
			return
		}
		h.serverError(w, r, err)
		return
	}

	// A person is in one room at a time, so walking in here walked them out of
	// wherever they were. Said plainly: the alternative is discovering it by
	// noticing you are missing from a list.
	if left != nil {
		c := h.viewCtx(w, r)
		if previous, err := h.repo.Conversation(r.Context(), *left, user.ID); err == nil {
			h.flash(w, "info", c.T("rooms.switched", previous.Name()))
		} else {
			h.flash(w, "info", c.T("rooms.left"))
		}
	}
	redirect(w, r, "/messages/"+convID.String())
}

// LeaveThread takes somebody out of a group or a room. Their copy of what was
// said stays where it is; they simply stop being somewhere it can reach them.
func (h *Handlers) LeaveThread(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	if err := h.repo.LeaveThread(r.Context(), convID, user.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	c := h.viewCtx(w, r)
	h.flash(w, "success", c.T("threads.left"))
	redirect(w, r, "/messages?tab=room")
}

// ThreadMembers is who is in a group or a room.
func (h *Handlers) ThreadMembers(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	conv, err := h.repo.Conversation(r.Context(), convID, user.ID)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	members, err := h.repo.Members(r.Context(), convID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	c := h.viewCtx(w, r)

	// Who is here is a panel, not a page. The thread screen already renders it
	// and opens it without going anywhere; this route is what a browser with no
	// script falls back to, and what the panel re-fetches when somebody joins.
	if isAPIRequest(r) {
		h.renderFragment(w, r, views.MemberList(c, conv, members))
		return
	}
	h.render(w, r, http.StatusOK, views.ThreadMembers(c, conv, members))
}

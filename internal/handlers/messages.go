package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

const messagePageSize = 80

// hasJS reports whether the request comes from a browser running our script,
// which sets the cookie on first load.
//
// It decides who is trusted to say when a message was read. A browser with
// JavaScript tells us on focus — that is the only moment somebody is actually
// looking. Anything else (a curl, a reader, a browser with scripting off) has
// no way to say so, and for those the page render is the best evidence there
// is. Without the split, either reading is announced by a background tab or it
// is never announced at all.
func hasJS(r *http.Request) bool {
	c, err := r.Cookie("js")
	return err == nil && c.Value == "1"
}

// Messages renders the inbox, optionally with one thread opened.
func (h *Handlers) Messages(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	ctx := r.Context()

	conversations, err := h.repo.Conversations(ctx, c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	friends, err := h.repo.Friends(ctx, c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// Which of the three the panel is showing. A thread you open from another
	// tab wins, so following a link into a room does not leave the panel
	// pointing somewhere else.
	tab := r.URL.Query().Get("tab")
	switch tab {
	case models.ConversationDirect, models.ConversationGroup, models.ConversationRoom:
	default:
		tab = models.ConversationDirect
	}
	// A guest has one of the three. Rooms are the whole of messaging without
	// an account, so the panel opens on them and the other two segments are
	// not drawn — there is nothing behind them and the server refuses both.
	if c.IsGuest() {
		tab = models.ConversationRoom
	}

	d := views.MessagesData{
		Conversations: conversations,
		Tab:           tab,
		Counts:        map[string]int{},
		Query:         strings.TrimSpace(r.URL.Query().Get("q")),
		FileLimit:     h.cfg.FilesPerMessage(),
	}
	// What each segment of the switcher carries, counted before the list is
	// narrowed to one of them.
	for _, conv := range conversations {
		d.Counts[conv.Kind] += conv.UnreadCount
	}

	// One field over the whole panel: the people, and what was said to them.
	if d.Query != "" {
		hits, err := h.repo.SearchConversations(ctx, c.User.ID, d.Query, 20)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		d.Conversations = matchingConversations(conversations, hits, d.Query)
	}
	// Friends you have never written to belong on the screen for writing to
	// people. Sending someone to the friends list to come back was a round
	// trip through a screen about something else.
	d.Friends = friendsWithoutThread(friends, conversations, d.Query)
	d.AllFriends = friends

	// The people in the room you are standing in. They are reachable without
	// being friends — that is what a room is for — so the picker has to offer
	// them, and offering exactly the set the server accepts is what stops the
	// dialog promising something the next screen refuses.
	if peers, err := h.repo.RoomPeers(ctx, c.User.ID, "", repository.RoomPeersShown, 0); err == nil {
		d.RoomPeers = peers
	}

	// Every room there is, joined or not. This is the one listing that shows
	// a thread to somebody who is not in it — that is what open means.
	if tab == models.ConversationRoom {
		rooms, err := h.repo.OpenRooms(ctx, c.User.ID, d.Query, 40)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		d.Rooms = rooms
	}

	if idParam := chi.URLParam(r, "id"); idParam != "" {
		convID, ok := parseUUID(idParam)
		if !ok {
			h.NotFound(w, r)
			return
		}

		conv, err := h.repo.Conversation(ctx, convID, c.User.ID)
		if errors.Is(err, repository.ErrNotFound) {
			// Not in it — which for a room is not an error at all. Every room
			// in the directory links here, so answering "no such page" to a
			// room the same screen is inviting you to join was the link
			// contradicting itself.
			preview, perr := h.repo.RoomPreview(ctx, convID, c.User.ID)
			if perr != nil {
				h.notFoundOrError(w, r, err)
				return
			}
			// Deliberately without the member list: from outside a room, how
			// many people are in it is public and who they are is not.
			d.Active = preview
			d.Tab = models.ConversationRoom
			h.render(w, r, http.StatusOK, views.Messages(c, d))
			return
		}
		if err != nil {
			h.notFoundOrError(w, r, err)
			return
		}

		// Everyone in it, not the handful of faces the panel draws. A name
		// over a message has to be findable for whoever wrote it, including
		// the twentieth person in a room.
		if !conv.IsDirect() {
			members, err := h.repo.Members(ctx, convID)
			if err != nil {
				h.serverError(w, r, err)
				return
			}
			conv.Members = members
			// The same list again, for the panel that says who is here. It
			// travels with the page so opening it is a fragment and not a
			// page load.
			d.Members = members
		}

		// Where they left off, read before anything marks the thread read — a
		// moment later there is no unread message to point at.
		firstUnread, err := h.repo.FirstUnreadID(ctx, convID, c.User.ID)
		if err != nil {
			h.serverError(w, r, err)
			return
		}

		messages, err := h.repo.Messages(ctx, convID, c.User.ID, 0, messagePageSize)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		d.HasOlder = len(messages) == messagePageSize

		// Searching a thread is the same screen with fewer messages on it.
		if find := strings.TrimSpace(r.URL.Query().Get("find")); find != "" {
			found, err := h.repo.SearchMessages(ctx, convID, c.User.ID, find, messagePageSize)
			if err != nil {
				h.serverError(w, r, err)
				return
			}
			d.Find = find
			messages = found
			d.HasOlder = false
		}

		// Looking for something somebody said is not reading the thread. It
		// used to consume every unread message and delete the divider that
		// would have shown you where you were.
		if !hasJS(r) && d.Find == "" {
			if err := h.markThreadRead(ctx, conv, c.User.ID); err != nil {
				h.serverError(w, r, err)
				return
			}
			// The badge counts were read before the marking; recompute so the
			// chrome does not show a stale number on this render.
			if n, err := h.repo.TotalUnread(ctx, c.User.ID); err == nil {
				c.UnreadMessages = n
			}
			for _, cv := range d.Conversations {
				if cv.ID == convID {
					cv.UnreadCount = 0
				}
			}
		}

		d.Active = conv
		d.Tab = conv.Kind
		d.Messages = messages
		d.FirstUnread = firstUnread
		if len(messages) > 0 {
			d.LastID = messages[len(messages)-1].ID
			d.FirstID = messages[0].ID
		}
	}

	h.render(w, r, http.StatusOK, views.Messages(c, d))
}

// markThreadRead stamps the inbound messages and tells the sender — but only
// if there was something to stamp.
//
// The guard is the whole point. Announcing a read that changed nothing meant
// the other side refreshed its receipts, which re-read the thread, which
// announced a read back, which never stopped.
func (h *Handlers) markThreadRead(ctx context.Context, conv *models.Conversation, viewerID uuid.UUID) error {
	n, err := h.repo.MarkRead(ctx, conv.ID, viewerID)
	if err != nil {
		return err
	}
	// The bell counted the messages a second time. Reading the thread is what
	// those notifications were pointing at, so reading it spends them.
	if _, err := h.repo.MarkConversationNotificationsRead(ctx, viewerID, conv.ID); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	// Everyone else in the thread. In a pair that is the one person whose
	// ticks change; in a group it is everybody, because any of them may have
	// been waiting to see whether this was read.
	members, err := h.repo.MemberIDs(ctx, conv.ID)
	if err != nil {
		return err
	}
	for _, id := range members {
		if id == viewerID {
			continue
		}
		h.hub.Publish(id, service.Event{
			Type: service.EventMessageRead, ConversationID: conv.ID.String(),
		})
	}
	return nil
}

// MarkThreadRead is the client saying "I am looking at this now". It is the
// only route that marks a message read, so nothing can do it by accident.
func (h *Handlers) MarkThreadRead(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	conv, err := h.repo.Conversation(r.Context(), convID, user.ID)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := h.markThreadRead(r.Context(), conv, user.ID); err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	badges, _ := h.repo.BadgeCounts(r.Context(), user.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"unread":        badges.UnreadMessages,
		"notifications": badges.Notifications,
	})
}

// ConversationSummary is the panel of people, as numbers and one line each.
//
// The live stream says "something arrived"; it deliberately does not say what,
// because an event large enough to redraw a screen with is an event carrying
// somebody's words past whoever is allowed to read them. So the page asks, and
// asks as the reader — which is the only way the preview beside a name can be
// a real line and not a guess assembled in the browser.
func (h *Handlers) ConversationSummary(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	conversations, err := h.repo.Conversations(r.Context(), c.User.ID)
	if err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}

	rows := make([]map[string]any, 0, len(conversations))
	for _, conv := range conversations {
		rows = append(rows, map[string]any{
			"id":      conv.ID.String(),
			"unread":  conv.UnreadCount,
			"preview": views.ConversationPreview(c, conv),
			"at":      c.Tr.RelativeTime(conv.LastMessageAt),
			"muted":   conv.Muted,
			// Which of the three it is. The panel draws one kind at a time, so
			// without this it saw every group and room as a conversation it had
			// never drawn and offered to reload the page — on every send.
			"kind": conv.Kind,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversations": rows,
		"unread":        c.UnreadMessages,
		"notifications": c.Notifications,
	})
}

// Receipts is the read state of your own messages, and nothing else. Updating
// one tick used to mean refetching the entire conversation.
func (h *Handlers) Receipts(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := h.repo.Conversation(r.Context(), convID, user.ID); err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	read, err := h.repo.Receipts(r.Context(), convID, user.ID)
	if err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	out := make([]map[string]any, 0, len(read))
	for id, ok := range read {
		out = append(out, map[string]any{"id": id, "read": ok})
	}
	writeJSON(w, http.StatusOK, map[string]any{"receipts": out})
}

// MessagesWith opens (or creates) the thread with a given username.
func (h *Handlers) MessagesWith(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	other, err := h.repo.UserCardByUsername(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	convID, err := h.social.OpenConversation(r.Context(), user.ID, other.ID)
	if err != nil {
		if errors.Is(err, service.ErrNotFriends) ||
			errors.Is(err, service.ErrUnreachable) ||
			errors.Is(err, service.ErrSelfTarget) {
			// Writing to somebody is for friends and for the people in your
			// room, and for nobody else. Said plainly, because the two ways in
			// are both things the reader can act on.
			c := h.viewCtx(w, r)
			h.flash(w, "error", c.T("messages.notReachable"))
			redirect(w, r, "/u/"+other.Username)
			return
		}
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, "/messages/"+convID.String())
}

// SendMessage posts into a thread. It answers JSON for the in-page composer
// and falls back to a redirect for a plain form submit.
func (h *Handlers) SendMessage(w http.ResponseWriter, r *http.Request) {
	if h.tooManyWrites(w, r, service.LimitMessage) {
		return
	}

	c := h.viewCtx(w, r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}

	// Files arrive with the message rather than before it, so a failed send
	// leaves nothing orphaned on disk. The CSRF middleware has already parsed
	// the multipart body, which is why the parts can simply be read here.
	var parts []*multipart.FileHeader
	if r.MultipartForm != nil {
		parts = r.MultipartForm.File["file"]
	}
	if len(parts) > h.cfg.FilesPerMessage() {
		if isAPIRequest(r) {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": c.T("upload.tooMany", h.cfg.FilesPerMessage()),
			})
			return
		}
		h.flash(w, "error", c.T("upload.tooMany", h.cfg.FilesPerMessage()))
		redirect(w, r, "/messages/"+convID.String())
		return
	}

	// Every file is stored before any message is written. A photograph that
	// fails halfway through a set of four would otherwise leave three
	// messages in the thread and an error on the screen, with no way to tell
	// which of the four is missing.
	attachments := make([]uuid.UUID, 0, len(parts))
	for i, part := range parts {
		file, ferr := part.Open()
		if ferr != nil {
			h.uploadFailed(w, r, ferr)
			return
		}
		att, err := h.uploads.StoreRecording(r.Context(), c.User.ID, part.Filename, file, durationAt(r, i))
		file.Close()
		if err != nil {
			h.uploadFailed(w, r, err)
			return
		}
		attachments = append(attachments, att.ID)
	}

	body := r.PostFormValue("body")
	replyTo, _ := strconv.ParseInt(r.PostFormValue("reply_to"), 10, 64)

	// Loaded for the author a group message carries. It also proves the
	// sender is in the thread, which is the whole access rule.
	conv, err := h.repo.Conversation(r.Context(), convID, c.User.ID)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	if !conv.IsDirect() {
		if members, merr := h.repo.Members(r.Context(), convID); merr == nil {
			conv.Members = members
		}
	}

	sent, err := h.sendAll(r.Context(), convID, c.User.ID, body, replyTo, attachments)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmptyMessage):
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusBadRequest, "empty")
				return
			}
			redirect(w, r, "/messages/"+convID.String())
		case errors.Is(err, service.ErrBlocked):
			c := h.viewCtx(w, r)
			h.flash(w, "error", c.T("messages.blockedNotice"))
			redirect(w, r, "/messages")
		case errors.Is(err, repository.ErrNotFound):
			h.NotFound(w, r)
		default:
			h.serverError(w, r, err)
		}
		return
	}

	if isAPIRequest(r) {
		out := make([]messageDTO, 0, len(sent))
		for _, msg := range sent {
			out = append(out, toMessageDTOIn(conv, msg, c.User.ID))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"messages": out,
			"lastId":   sent[len(sent)-1].ID,
		})
		return
	}
	redirect(w, r, "/messages/"+convID.String())
}

// sendAll writes one message per file, and the typed line rides with the first
// of them.
//
// Several files in one send are several messages rather than one message with
// several files: a thread is a column of things said, and a bubble holding
// four photographs is a thing the reader cannot answer one of. The words go on
// the first because that is the one they were typed above; the quote goes
// there too, since a reply repeated over four bubbles answers nothing four
// times.
func (h *Handlers) sendAll(ctx context.Context, convID, senderID uuid.UUID, body string, replyTo int64, attachments []uuid.UUID) ([]*models.Message, error) {
	if len(attachments) == 0 {
		msg, err := h.social.Send(ctx, convID, senderID, body, replyTo, nil)
		if err != nil {
			return nil, err
		}
		return []*models.Message{msg}, nil
	}

	sent := make([]*models.Message, 0, len(attachments))
	for i := range attachments {
		line, quote := "", int64(0)
		if i == 0 {
			line, quote = body, replyTo
		}
		msg, err := h.social.Send(ctx, convID, senderID, line, quote, &attachments[i])
		if err != nil {
			// Whatever was already sent has been said; the caller reports the
			// failure and the thread keeps what arrived.
			if len(sent) > 0 {
				return sent, nil
			}
			return nil, err
		}
		sent = append(sent, msg)
	}
	return sent, nil
}

// durationAt is the length claimed for the file at one position. The composer
// sends a duration for every file it sends, in the same order, so that a voice
// note recorded alongside two photographs is still matched with its own
// length. Anything unparseable is no claim at all.
func durationAt(r *http.Request, i int) int {
	if r.MultipartForm == nil {
		return 0
	}
	claims := r.MultipartForm.Value["duration_ms"]
	if i >= len(claims) {
		return 0
	}
	ms, err := strconv.Atoi(claims[i])
	if err != nil || ms <= 0 {
		return 0
	}
	return ms
}

// uploadFailed says why in the caller's language, for both kinds of client.
func (h *Handlers) uploadFailed(w http.ResponseWriter, r *http.Request, err error) {
	c := h.viewCtx(w, r)
	key := "upload.failed"
	switch {
	case errors.Is(err, service.ErrUploadTooLarge):
		key = "upload.tooLarge"
	case errors.Is(err, service.ErrUploadType):
		key = "upload.badType"
	case errors.Is(err, service.ErrUploadsOff):
		key = "upload.off"
	case errors.Is(err, service.ErrUploadEmpty):
		key = "upload.empty"
	default:
		slogError(r, err)
	}
	if isAPIRequest(r) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": c.T(key)})
		return
	}
	h.flash(w, "error", c.T(key))
	redirect(w, r, backTo(r, "/messages"))
}

// Files serves an upload to somebody allowed to see it.
func (h *Handlers) Files(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	// Asked before the bytes are opened: a guessed id must not reach a file
	// belonging to two other people.
	allowed, err := h.repo.CanSeeAttachment(r.Context(), id, user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !allowed {
		h.NotFound(w, r)
		return
	}
	att, err := h.repo.Attachment(r.Context(), id)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	file, err := h.uploads.Open(id)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	defer file.Close()

	hdr := w.Header()
	hdr.Set("Content-Type", att.Mime)
	// Never inline: a browser deciding for itself how to render somebody
	// else's upload is how a text file becomes a page on this origin.
	if att.Kind == models.AttachmentFile {
		hdr.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(att.Name))
	}
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, att.Name, att.CreatedAt, file)
}

// DeleteConversation clears the thread for the person asking, and only them.
func (h *Handlers) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	if _, err := h.repo.Conversation(r.Context(), convID, user.ID); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	if err := h.repo.ClearConversation(r.Context(), convID, user.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	c := h.viewCtx(w, r)
	h.flash(w, "success", c.T("messages.deleted"))
	redirect(w, r, "/messages")
}

// MuteConversation turns the notification off for one thread. The messages
// still arrive; nothing announces them.
func (h *Handlers) MuteConversation(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	if _, err := h.repo.Conversation(r.Context(), convID, user.ID); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	muted := r.PostFormValue("muted") == "1"
	if err := h.repo.MuteConversation(r.Context(), convID, user.ID, muted); err != nil {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, "/messages/"+convID.String())
}

// messageDTO is the wire shape the chat client consumes.
type messageDTO struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	Mine bool   `json:"mine"`
	Time string `json:"time"`
	// Day is the calendar day the message belongs to, so the client can draw
	// the same separators the server does rather than inventing its own.
	Day string `json:"day"`
	// SentAt and ReadAt are what the details panel shows. They are machine
	// timestamps: the browser formats them in its own locale, which is the one
	// place a time is already known to be right.
	SentAt    string `json:"sentAt"`
	ReadAt    string `json:"readAt,omitempty"`
	Read      bool   `json:"read"`
	Withdrawn bool   `json:"withdrawn"`
	// Reply is the line this message answers, quoted above it.
	Reply *quoteDTO `json:"reply,omitempty"`
	// File is what the message carries besides words.
	File *fileDTO `json:"file,omitempty"`
	// Author is who wrote it, in a thread where more than one person could
	// have. Absent in a pair, where the side of the screen already says.
	Author *authorDTO `json:"author,omitempty"`
}

// authorDTO is who wrote a message, already painted.
//
// The colours come from a SHA-256 of the seed, which the browser cannot do
// without waiting on a promise — so the server sends what it would have drawn
// rather than the seed to draw it from. One arithmetic, one answer.
type authorDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Initial string `json:"initial"`
	Style   string `json:"style"`
	Photo   bool   `json:"photo"`
}

type fileDTO struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Size string `json:"size"`
	// Length is how long a recording runs. The file does not carry it, so
	// without this the player draws 0:00 for every voice note ever sent.
	Length string `json:"length,omitempty"`
}

type quoteDTO struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	Mine      bool   `json:"mine"`
	Withdrawn bool   `json:"withdrawn"`
}

// toMessageDTO renders one message for the script. The conversation is passed
// so a group message can carry who wrote it; nil means a pair, where it does
// not need to.
func toMessageDTOIn(conv *models.Conversation, m *models.Message, viewerID uuid.UUID) messageDTO {
	dto := toMessageDTO(m, viewerID)
	if author := views.SenderCard(conv, m, viewerID); author != nil {
		seed := views.AvatarSeedFor(author.ID, author.AvatarSeed, author.Username)
		dto.Author = &authorDTO{
			ID:      author.ID.String(),
			Name:    author.DisplayName,
			Initial: views.Initial(author.DisplayName),
			Style:   views.AvatarStyle(seed),
			Photo:   views.HasAvatarPhoto(author.AvatarSeed),
		}
	}
	return dto
}

func toMessageDTO(m *models.Message, viewerID uuid.UUID) messageDTO {
	return messageDTO{
		ID:        m.ID,
		Body:      m.Body,
		Mine:      m.SenderID == viewerID,
		Time:      m.CreatedAt.Format("15:04"),
		Day:       m.CreatedAt.Format("2006-01-02"),
		SentAt:    m.CreatedAt.Format(time.RFC3339),
		ReadAt:    readStamp(m),
		Read:      m.Read(),
		Withdrawn: m.Withdrawn(),
		Reply:     toQuoteDTO(m.ReplyTo, viewerID),
		File:      toFileDTO(m.Attachment),
	}
}

func toFileDTO(a *models.Attachment) *fileDTO {
	if a == nil {
		return nil
	}
	return &fileDTO{
		ID: a.ID.String(), Kind: a.Kind, Name: a.Name,
		Size: a.Size(), Length: a.Length(),
	}
}

func toQuoteDTO(q *models.MessageQuote, viewerID uuid.UUID) *quoteDTO {
	if q == nil {
		return nil
	}
	return &quoteDTO{
		ID: q.ID, Body: q.Body, Mine: q.SenderID == viewerID, Withdrawn: q.Withdrawn,
	}
}

func readStamp(m *models.Message) string {
	if m.ReadAt == nil {
		return ""
	}
	return m.ReadAt.Format(time.RFC3339)
}

// WithdrawMessage takes back one of your own messages.
func (h *Handlers) WithdrawMessage(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	messageID, err := strconv.ParseInt(chi.URLParam(r, "message"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	conv, err := h.repo.Conversation(r.Context(), convID, user.ID)
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.repo.WithdrawMessage(r.Context(), messageID, user.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Not yours, or already withdrawn. Both are "nothing to do", and
			// neither is worth telling an attacker apart.
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}

	// The other side is looking at a sentence that is no longer there.
	h.hub.Publish(conv.Other.ID, service.Event{
		Type: service.EventMessageWithdrawn, ConversationID: convID.String(),
	})
	writeJSON(w, http.StatusOK, map[string]any{"withdrawn": messageID})
}

// HideMessage drops one message from the asking person's copy.
//
// The other half of withdrawing. Withdrawing is the sender taking a sentence
// out of the conversation; this is a reader taking it out of theirs, which
// anyone may do to anything and which the other side never sees.
func (h *Handlers) HideMessage(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	messageID, err := strconv.ParseInt(chi.URLParam(r, "message"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := h.repo.Conversation(r.Context(), convID, user.ID); err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := h.repo.HideMessage(r.Context(), messageID, user.ID); err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hidden": messageID})
}

// PollMessages returns messages the client does not have yet. It reads and
// nothing else: no marking, no announcing.
//
// ?after= is the live tail. ?before= is the page above the one on screen,
// without which a thread was only ever its newest eighty messages.
func (h *Handlers) PollMessages(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	convID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}

	if _, err := h.repo.Conversation(r.Context(), convID, user.ID); err != nil {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	ctx := r.Context()
	var messages []*models.Message
	var err error
	older := false

	if raw := r.URL.Query().Get("before"); raw != "" {
		before, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || before <= 0 {
			writeJSONError(w, http.StatusBadRequest, "bad before")
			return
		}
		older = true
		messages, err = h.repo.MessagesBefore(ctx, convID, user.ID, before, messagePageSize)
	} else {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		messages, err = h.repo.Messages(ctx, convID, user.ID, after, messagePageSize)
	}
	if err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}

	// Who wrote each one, for a thread where that is not obvious from the
	// side of the screen it lands on.
	conv, cerr := h.repo.Conversation(ctx, convID, user.ID)
	if cerr == nil && !conv.IsDirect() {
		if members, merr := h.repo.Members(ctx, convID); merr == nil {
			conv.Members = members
		}
	}
	out := make([]messageDTO, 0, len(messages))
	for _, m := range messages {
		out = append(out, toMessageDTOIn(conv, m, user.ID))
	}

	payload := map[string]any{"messages": out}
	if older {
		payload["hasOlder"] = len(messages) == messagePageSize
		if len(messages) > 0 {
			payload["firstId"] = messages[0].ID
		}
	} else {
		lastID, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if len(messages) > 0 {
			lastID = messages[len(messages)-1].ID
		}
		payload["lastId"] = lastID
	}
	writeJSON(w, http.StatusOK, payload)
}

// matchingConversations is the panel search: threads whose person matches the
// text, plus threads carrying it in something that was said.
func matchingConversations(all, hits []*models.Conversation, query string) []*models.Conversation {
	needle := strings.ToLower(query)
	seen := map[uuid.UUID]bool{}
	out := make([]*models.Conversation, 0, len(all))

	for _, c := range all {
		if c.Other == nil {
			continue
		}
		if strings.Contains(strings.ToLower(c.Other.DisplayName), needle) ||
			strings.Contains(strings.ToLower(c.Other.Username), needle) {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	for _, c := range hits {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	return out
}

// friendsWithoutThread is everyone you could write to but have not.
func friendsWithoutThread(friends []*models.UserCard, conversations []*models.Conversation, query string) []*models.UserCard {
	talking := map[uuid.UUID]bool{}
	for _, c := range conversations {
		if c.Other != nil {
			talking[c.Other.ID] = true
		}
	}
	needle := strings.ToLower(query)

	out := make([]*models.UserCard, 0, len(friends))
	for _, f := range friends {
		if talking[f.ID] {
			continue
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(f.DisplayName), needle) &&
			!strings.Contains(strings.ToLower(f.Username), needle) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Events is the SSE stream that nudges the client to refresh. It carries no
// message content — the client re-fetches through the authorised endpoints.
func (h *Handlers) Events(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, unsubscribe := h.hub.Subscribe(user.ID)
	defer unsubscribe()

	fmt.Fprint(w, "retry: 4000\n\n")
	flusher.Flush()

	// A heartbeat keeps proxies from closing an idle stream.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, open := <-events:
			if !open {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
			flusher.Flush()
		}
	}
}

// UnreadCounts backs the periodic badge refresh in the navbar. It has to
// return every counter the chrome renders, or a badge that was painted on
// first load stops tracking the moment the page starts refreshing itself —
// which is what happened to support.
func (h *Handlers) UnreadCounts(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	ctx := r.Context()

	badges, _ := h.repo.BadgeCounts(ctx, user.ID)
	counts := map[string]int{
		"messages":      badges.UnreadMessages,
		"challenges":    badges.PendingChallenges,
		"friends":       badges.FriendRequests,
		"support":       badges.SupportUnread,
		"notifications": badges.Notifications,
	}
	// Staff get the queue depth too, for the badge on the admin link.
	if user.CanModerate() {
		if tickets, err := h.repo.TicketCounts(ctx, user.ID, true); err == nil {
			counts["supportQueue"] = tickets.AwaitingReply
		}
	}

	writeJSON(w, http.StatusOK, counts)
}

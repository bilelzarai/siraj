package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

const maxTicketBody = 4000

// ------------------------------------------------------------ player side --

// Support lists the player's own conversations.
func (h *Handlers) Support(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	tickets, err := h.repo.MyTickets(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.Support(c, views.SupportData{
		Tickets: tickets,
	}))
}

// SupportNewForm offers the categorised intake form. Asking for the kind up
// front is what makes the staff queue triageable later.
func (h *Handlers) SupportNewForm(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	categories, _ := h.repo.Categories(r.Context(), c.Locale, 0)

	h.render(w, r, http.StatusOK, views.SupportNew(c, views.SupportNewData{
		Kind:       validTicketKind(r.URL.Query().Get("kind")),
		Categories: categories,
		QuestionID: queryInt(r, "question", 0),
		PagePath:   clip(r.URL.Query().Get("from"), 200),
		// Who a report could be about, so the field suggests rather than
		// asking somebody to remember a username exactly.
		Known:    h.reportablePeople(r),
		Reported: strings.TrimSpace(r.URL.Query().Get("player")),
	}))
}

func (h *Handlers) SupportCreate(w http.ResponseWriter, r *http.Request) {
	if h.tooManyWrites(w, r, service.LimitTicket) {
		return
	}

	c := h.viewCtx(w, r)

	form := views.SupportNewData{
		Kind:     validTicketKind(r.PostFormValue("kind")),
		Subject:  clip(strings.TrimSpace(r.PostFormValue("subject")), 140),
		Body:     clip(strings.TrimSpace(r.PostFormValue("body")), maxTicketBody),
		PagePath: clip(r.PostFormValue("page_path"), 200),
		Reported: clip(strings.TrimSpace(r.PostFormValue("reported")), 40),
	}
	form.Categories, _ = h.repo.Categories(r.Context(), c.Locale, 0)
	form.Known = h.reportablePeople(r)

	// A category belongs to a message about a question. Read only for the kinds
	// it can mean something for, so a report of abuse cannot arrive carrying
	// one because the field happened to be on the page.
	if categoryID := intParam(r, "category", 0); categoryID > 0 && models.TicketNeedsCategory(form.Kind) {
		form.CategoryID = categoryID
	}
	if questionID := intParam(r, "question", 0); questionID > 0 {
		form.QuestionID = questionID
	}

	// Collect every problem at once rather than stopping at the first, so the
	// person is not sent round the loop once per mistake.
	form.Errors = map[string]string{}
	if form.Kind == "" {
		form.Errors["kind"] = c.T("support.error.kind")
	}
	if len([]rune(form.Subject)) < 4 {
		form.Errors["subject"] = c.T("support.error.subject")
	}
	if len([]rune(form.Body)) < 10 {
		form.Errors["body"] = c.T("support.error.body")
	}

	// A report about a player has to say which player, and it has to be
	// somebody real. Checked here rather than left to staff to work out from
	// prose: two reports about the same person should be two reports about the
	// same person, not two unrelated messages.
	var reported *uuid.UUID
	if models.TicketNeedsPlayer(form.Kind) {
		switch {
		case form.Reported == "":
			form.Errors["reported"] = c.T("support.error.reportedMissing")
		default:
			card, err := h.repo.UserCardByUsername(r.Context(),
				strings.TrimPrefix(form.Reported, "@"))
			switch {
			case err != nil:
				form.Errors["reported"] = c.T("support.error.reportedUnknown")
			case card.ID == c.User.ID:
				form.Errors["reported"] = c.T("support.error.reportedSelf")
			default:
				reported = &card.ID
			}
		}
	}
	if len(form.Errors) > 0 {
		h.render(w, r, http.StatusUnprocessableEntity, views.SupportNew(c, form))
		return
	}

	ticket := &models.Ticket{
		UserID:         c.User.ID,
		Kind:           form.Kind,
		Priority:       models.PriorityNormal,
		Subject:        form.Subject,
		Locale:         c.Locale,
		PagePath:       form.PagePath,
		UserAgent:      clip(r.UserAgent(), 400),
		ReportedUserID: reported,
	}
	// Reports of abuse are the one kind that should not sit in a normal
	// queue behind feature suggestions.
	if form.Kind == models.TicketAbuse {
		ticket.Priority = models.PriorityHigh
	}
	if form.CategoryID > 0 {
		ticket.CategoryID = &form.CategoryID
	}
	if form.QuestionID > 0 {
		ticket.QuestionID = &form.QuestionID
	}

	if err := h.repo.CreateTicket(r.Context(), ticket, form.Body); err != nil {
		h.serverError(w, r, err)
		return
	}

	h.notifyStaff(r, ticket)
	h.flash(w, "success", c.T("support.created"))
	redirect(w, r, "/support/"+ticket.ID.String())
}

// SupportThread shows one of the player's own conversations.
func (h *Handlers) SupportThread(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}

	ticket, err := h.repo.Ticket(r.Context(), id, c.User.ID, false)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	// A player never sees internal staff notes.
	messages, err := h.repo.TicketMessages(r.Context(), id, false)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if err := h.repo.MarkTicketRead(r.Context(), id, false); err != nil {
		h.serverError(w, r, err)
		return
	}
	ticket.UserUnread = 0

	h.render(w, r, http.StatusOK, views.SupportThread(c, views.SupportThreadData{
		Ticket:   ticket,
		Messages: messages,
	}))
}

// SupportClose lets the person who opened a ticket close it.
func (h *Handlers) SupportClose(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	if err := h.repo.CloseTicketAsOwner(r.Context(), id, c.User.ID); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	h.flash(w, "success", c.T("support.closed"))
	redirect(w, r, "/support/"+id.String())
}

func (h *Handlers) SupportReply(w http.ResponseWriter, r *http.Request) {
	if h.tooManyWrites(w, r, service.LimitTicket) {
		return
	}

	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	ticket, err := h.repo.Ticket(r.Context(), id, c.User.ID, false)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	body := clip(strings.TrimSpace(r.PostFormValue("body")), maxTicketBody)
	if body == "" {
		redirect(w, r, "/support/"+id.String())
		return
	}

	if err := h.repo.ReplyToTicket(r.Context(), id, c.User.ID, "user", body, false); err != nil {
		h.serverError(w, r, err)
		return
	}

	// Replying to a closed ticket reopens it. The problem coming back is the
	// most likely reason anybody returns to one, and sending them to a fresh
	// ticket would lose everything already said about it.
	if ticket.Status == models.TicketClosed || ticket.Status == models.TicketResolved {
		if err := h.repo.ReopenTicket(r.Context(), id, c.User.ID); err != nil {
			slogError(r, err)
		} else {
			h.flash(w, "info", c.T("support.reopened"))
			h.notifyStaffReopened(r, ticket)
		}
	}
	redirect(w, r, "/support/"+id.String())
}

// ------------------------------------------------------------- staff side --

// AdminSupport is the triage inbox: every filter staff need to understand a
// queue without opening each conversation.
func (h *Handlers) AdminSupport(w http.ResponseWriter, r *http.Request) {
	h.supportInbox(w, r, uuid.Nil)
}

// supportInbox renders the inbox, with one conversation open in it when there
// is one.
//
// One screen rather than two. The list and the thread are read together — you
// work down a queue, and the next ticket is the one under the one you just
// answered — so a thread on a page of its own means losing the queue every
// time a reply is sent. Both routes land here; the thread route arrives with
// an id.
func (h *Handlers) supportInbox(w http.ResponseWriter, r *http.Request, open uuid.UUID) {
	c, chrome := h.adminCtx(w, r)

	paging := h.paging(w, r)
	filter := repository.TicketFilter{
		Query:    clip(strings.TrimSpace(r.URL.Query().Get("q")), 120),
		Kind:     validTicketKind(r.URL.Query().Get("kind")),
		Status:   validTicketStatus(r.URL.Query().Get("status")),
		Priority: validTicketPriority(r.URL.Query().Get("priority")),
		Assignee: strings.TrimSpace(r.URL.Query().Get("assignee")),
		ViewerID: c.User.ID,
		Waiting:  r.URL.Query().Get("waiting") == "1",
		Limit:    paging.Size,
		Offset:   paging.Offset(),
	}

	tickets, total, err := h.repo.Tickets(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging = paging.withTotal(total)

	counts, _ := h.repo.TicketCounts(r.Context(), c.User.ID, true)
	breakdown, _ := h.repo.TicketKindBreakdown(r.Context())
	staff, _ := h.repo.StaffMembers(r.Context())

	data := views.AdminSupportData{
		Tickets:   tickets,
		Total:     total,
		Pager:     pagerFor(paging),
		Counts:    counts,
		Breakdown: breakdown,
		Staff:     staff,
		Filter: views.TicketFilterState{
			Query:    filter.Query,
			Kind:     filter.Kind,
			Status:   filter.Status,
			Priority: filter.Priority,
			Assignee: filter.Assignee,
			Waiting:  filter.Waiting,
		},
	}

	if open != uuid.Nil {
		ticket, err := h.repo.Ticket(r.Context(), open, c.User.ID, true)
		if err != nil {
			h.notFoundOrError(w, r, err)
			return
		}
		// Staff see the internal notes; that is what they are for.
		messages, err := h.repo.TicketMessages(r.Context(), open, true)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		// Opening a conversation is reading it, so the unread count it was
		// carrying in the queue goes with the opening.
		if err := h.repo.MarkTicketRead(r.Context(), open, true); err != nil {
			slogError(r, fmt.Errorf("mark ticket read: %w", err))
		}
		canned, _ := h.repo.CannedReplies(r.Context(), ticket.Locale)

		data.Open = ticket
		data.Messages = messages
		data.Canned = canned
	}

	h.render(w, r, http.StatusOK, views.AdminSupport(c, chrome, data))
}

// AdminSupportThread is the inbox with one conversation open in it.
func (h *Handlers) AdminSupportThread(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	h.supportInbox(w, r, id)
}

// AdminSupportReply posts a staff reply or an internal note.
func (h *Handlers) AdminSupportReply(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	ticket, err := h.repo.Ticket(r.Context(), id, c.User.ID, true)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	body := clip(strings.TrimSpace(r.PostFormValue("body")), maxTicketBody)
	if body == "" {
		redirect(w, r, "/admin/support/"+id.String())
		return
	}
	internal := r.PostFormValue("internal") == "1"

	if err := h.repo.ReplyToTicket(r.Context(), id, c.User.ID, "staff", body, internal); err != nil {
		h.serverError(w, r, err)
		return
	}

	action := "support.reply"
	if internal {
		action = "support.note"
	} else {
		// Nudge the player's open tab so a reply is visible without a reload,
		// and write to them for the far more likely case that there is no open
		// tab at all.
		h.hub.Publish(ticket.UserID, service.Event{Type: service.EventSupportReply})
		h.mailTicketReply(r, id, body)
	}
	h.audit(r, action, "ticket", id.String(), map[string]any{"internal": internal})

	// "Send and resolve" is the common ending: the answer is the last thing
	// the ticket needed, and asking staff to then find the status select is
	// asking them to do one job in two places. An internal note never
	// resolves — a note to each other is not an answer to the player.
	if r.PostFormValue("resolve") == "1" && !internal {
		if err := h.repo.UpdateTicket(r.Context(), id, models.TicketResolved,
			"", "", nil, false); err != nil {
			h.serverError(w, r, err)
			return
		}
		h.auditChange(r, "support.triage", "ticket", id.String(), ticket.Subject,
			map[string]any{"status": ticket.Status},
			map[string]any{"status": models.TicketResolved})
	}

	redirect(w, r, backTo(r, "/admin/support/"+id.String()))
}

// AdminSupportUpdate applies triage: status, priority, kind, assignment.
func (h *Handlers) AdminSupportUpdate(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}

	status := validTicketStatus(r.PostFormValue("status"))
	priority := validTicketPriority(r.PostFormValue("priority"))
	kind := validTicketKind(r.PostFormValue("kind"))

	// Read before the write, so the trail can say what triage changed rather
	// than only what it was set to. A ticket that is already high priority and
	// one that was just escalated look identical otherwise.
	before, err := h.repo.Ticket(r.Context(), id, c.User.ID, true)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	var assignee *uuid.UUID
	clearAssignee := false
	switch raw := strings.TrimSpace(r.PostFormValue("assignee")); raw {
	case "":
		// not part of this submission
	case "unassigned":
		clearAssignee = true
	case "me":
		assignee = &c.User.ID
	default:
		if parsed, err := uuid.Parse(raw); err == nil {
			assignee = &parsed
		}
	}

	if err := h.repo.UpdateTicket(r.Context(), id, status, priority, kind,
		assignee, clearAssignee); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	// Only the fields this submission actually carried, and only where the
	// value moved. An empty string means "not part of this form", which is not
	// the same as "cleared".
	was, now := map[string]any{}, map[string]any{}
	for _, f := range []struct{ name, old, new string }{
		{"status", before.Status, status},
		{"priority", before.Priority, priority},
		{"topic", before.Kind, kind},
	} {
		if f.new != "" && f.new != f.old {
			was[f.name], now[f.name] = f.old, f.new
		}
	}
	switch {
	case clearAssignee && before.AssignedTo != nil:
		was["assignee"], now["assignee"] = before.AssigneeUsername, "unassigned"
	case assignee != nil && (before.AssignedTo == nil || *before.AssignedTo != *assignee):
		was["assignee"] = before.AssigneeUsername
		if before.AssignedTo == nil {
			was["assignee"] = "unassigned"
		}
		now["assignee"] = assignee.String()
	}
	h.auditChange(r, "support.triage", "ticket", id.String(), before.Subject, was, now)
	redirect(w, r, backTo(r, "/admin/support/"+id.String()))
}

// ---------------------------------------------------------------- helpers --

// notifyStaff pings every admin and moderator about a new conversation.
// notifyStaff tells everyone who can answer that a ticket has arrived — in the
// app, on the stream, and by email.
//
// The mail is the part that was missing. A badge and a stream event only reach
// somebody who is already looking at the screen; a ticket opened at night sat
// unseen until an admin happened to sign in, which for a report of abuse is the
// wrong amount of time.
func (h *Handlers) notifyStaff(r *http.Request, t *models.Ticket) {
	staff, err := h.repo.StaffContacts(r.Context())
	if err != nil {
		slogError(r, fmt.Errorf("staff contacts: %w", err))
		return
	}
	link := h.cfg.BaseURL + "/admin/support/" + t.ID.String()

	for _, member := range staff {
		_ = h.repo.Notify(r.Context(), member.ID, "support.new", map[string]any{
			"ticket_id": t.ID.String(),
			"kind":      t.Kind,
			"subject":   t.Subject,
		})
		h.hub.Publish(member.ID, service.Event{Type: service.EventSupportNew})

		// Each in their own language, not the language of whoever opened the
		// ticket.
		p := h.bundle.Printer(member.Locale)
		h.mailer.SendAsync(service.Mail{
			To:       member.Email,
			ToName:   member.DisplayName,
			Locale:   member.Locale,
			Subject:  p.T("support.mail.new.subject", t.Subject),
			TextBody: p.T("support.mail.new.text", member.DisplayName, t.Subject, link),
			HTMLBody: service.MailHTML(service.MailBody{
				Greeting: p.T("support.mail.greeting", member.DisplayName),
				Body:     p.T("support.mail.new.body", t.Subject),
				Button:   p.T("support.mail.new.button"),
				Link:     link,
				Footer:   p.T("support.mail.footer"),
			}),
		})
	}
}

// notifyStaffReopened tells the queue that something they had closed is back.
func (h *Handlers) notifyStaffReopened(r *http.Request, t *models.Ticket) {
	staff, err := h.repo.StaffContacts(r.Context())
	if err != nil {
		return
	}
	for _, member := range staff {
		_ = h.repo.Notify(r.Context(), member.ID, "support.new", map[string]any{
			"ticket_id": t.ID.String(),
			"kind":      t.Kind,
			"subject":   t.Subject,
		})
		h.hub.Publish(member.ID, service.Event{Type: service.EventSupportNew})
	}
}

// mailTicketReply writes to the person who opened a ticket when staff answer.
//
// The reply reached them as a badge and a stream event, both of which require
// them to already be on the site — so an answer to a question they asked three
// days ago waited for them to think to come back and look.
func (h *Handlers) mailTicketReply(r *http.Request, ticketID uuid.UUID, body string) {
	owner, err := h.repo.TicketOwnerContact(r.Context(), ticketID)
	if err != nil {
		slogError(r, fmt.Errorf("ticket owner contact: %w", err))
		return
	}
	link := h.cfg.BaseURL + "/support/" + ticketID.String()

	p := h.bundle.Printer(owner.Locale)
	h.mailer.SendAsync(service.Mail{
		To:       owner.Email,
		ToName:   owner.DisplayName,
		Locale:   owner.Locale,
		Subject:  p.T("support.mail.reply.subject"),
		TextBody: p.T("support.mail.reply.text", owner.DisplayName, clip(body, 300), link),
		HTMLBody: service.MailHTML(service.MailBody{
			Greeting: p.T("support.mail.greeting", owner.DisplayName),
			// The reply itself, so a short answer needs no trip at all.
			Body:   clip(body, 300),
			Button: p.T("support.mail.reply.button"),
			Link:   link,
			Footer: p.T("support.mail.footer"),
		}),
	})
}

func validTicketKind(kind string) string {
	for _, k := range models.TicketKinds {
		if k == kind {
			return kind
		}
	}
	return ""
}

func validTicketStatus(status string) string {
	for _, s := range models.TicketStatuses {
		if s == status {
			return status
		}
	}
	return ""
}

func validTicketPriority(priority string) string {
	for _, p := range models.TicketPriorities {
		if p == priority {
			return priority
		}
	}
	return ""
}

// reportablePeople is who the report form can name.
//
// Friends and the people in the sender's room — the same reach everything else
// uses. A report is about somebody you have actually met here; offering the
// whole user list would make the form a directory, which is the one thing the
// scoped search exists to stop it being. Staff can still act on a name typed by
// hand: the field is a suggestion list, not a whitelist.
func (h *Handlers) reportablePeople(r *http.Request) []*models.UserCard {
	user := userFrom(r)
	if user == nil {
		return nil
	}
	out, err := h.repo.SearchFriends(r.Context(), user.ID, "", 50, 0)
	if err != nil {
		return nil
	}
	if peers, err := h.repo.RoomPeers(r.Context(), user.ID, "", 50, 0); err == nil {
		seen := make(map[string]bool, len(out))
		for _, u := range out {
			seen[u.Username] = true
		}
		for _, p := range peers {
			if !seen[p.Username] {
				out = append(out, p)
			}
		}
	}
	return out
}

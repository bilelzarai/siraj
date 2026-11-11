package support

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"
)

// Web is P30 Contact us: the topic picker, the form for that topic and the thread.
// Visitors can write too (they give an email), so it lives on the public group.
type Web struct{ svc *Service }

func NewWeb(s *Service) *Web { return &Web{svc: s} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	public.GET("/support", w.page)
	public.POST("/support", w.send)
	private.GET("/support/:id", w.thread)
	private.POST("/support/:id/reply", w.reply)
}

func topicCards() []pages.SupportTopic {
	out := make([]pages.SupportTopic, 0, len(Topics))
	for _, t := range Topics {
		out = append(out, pages.SupportTopic{Key: string(t), Icon: TopicIcon[t]})
	}
	return out
}

func (w *Web) page(c *gin.Context) {
	v := view.From(c)
	topic := c.Query("topic")
	if !ValidTopic(topic) {
		topic = ""
	}
	d := pages.SupportData{
		Topics: topicCards(), Topic: topic,
		Values: map[string]string{}, Errors: map[string]string{},
		SignedIn: v.SignedIn(),
	}
	if v.SignedIn() && !v.IsGuest() {
		rows, _, err := w.svc.Mine(c.Request.Context(), v.User, 1, 10)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, t := range rows {
			d.Tickets = append(d.Tickets, ticketRow(t))
		}
	}
	render.Page(c, pages.Support(v, d))
}

func ticketRow(t Ticket) pages.SupportTicketRow {
	when := t.Created
	if len(when) >= 10 {
		when = when[:10]
	}
	return pages.SupportTicketRow{
		ID: t.ID.String(), Number: t.Number, Subject: t.Subject,
		Topic: t.Topic, Status: t.Status, When: when,
	}
}

func (w *Web) send(c *gin.Context) {
	v := view.From(c)
	in := Input{
		Who: v.User, Email: c.PostForm("email"), Topic: c.PostForm("topic"),
		Subject: c.PostForm("subject"), Body: c.PostForm("body"),
		Context: map[string]any{},
	}
	for _, key := range []string{"question_number", "account", "username", "reason", "link"} {
		if value := strings.TrimSpace(c.PostForm(key)); value != "" {
			in.Context[key] = value
		}
	}
	in.Context["device"] = c.Request.UserAgent()
	ticket, err := w.svc.Create(c.Request.Context(), in)
	if err != nil {
		e, ok := apperr.As(err)
		if !ok || e.Status >= 500 {
			w.fail(c, v, err)
			return
		}
		d := pages.SupportData{
			Topics: topicCards(), Topic: in.Topic, SignedIn: v.SignedIn(),
			Values: map[string]string{
				"subject": in.Subject, "body": in.Body, "email": in.Email,
				"username": c.PostForm("username"), "account": c.PostForm("account"),
				"question_number": c.PostForm("question_number"), "link": c.PostForm("link"),
				"reason": c.PostForm("reason"),
			},
			Errors: e.Fields,
		}
		if d.Errors == nil {
			d.Errors = map[string]string{}
		}
		render.Status(c, http.StatusUnprocessableEntity, pages.Support(v, d))
		return
	}
	render.Page(c, pages.SupportSent(v, ticket.Number, ticket.Topic))
}

func (w *Web) thread(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	t, messages, err := w.svc.Thread(c.Request.Context(), v.User, id, false)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	rows := make([]pages.SupportMessage, 0, len(messages))
	for _, m := range messages {
		author := m.Author
		if author == "" {
			author = v.Tr("support.title")
		}
		when := m.Created
		if len(when) >= 16 {
			when = strings.Replace(when[:16], "T", " ", 1)
		}
		rows = append(rows, pages.SupportMessage{Author: author, Body: m.Body, When: when, IsStaff: m.IsStaff})
	}
	render.Page(c, pages.SupportThread(v, ticketRow(t), rows))
}

func (w *Web) reply(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Reply(c.Request.Context(), v.User, id, c.PostForm("body"), false, false); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/support/"+id.String())
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if apiutil.WantsJSON(c) {
		apiutil.Fail(c, err)
		return
	}
	if e, ok := apperr.As(err); ok && e.Status == http.StatusNotFound {
		render.Status(c, http.StatusNotFound, pages.NotFound(v))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

var _ = sqlc.TicketTopicBug

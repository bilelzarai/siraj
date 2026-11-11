package support

import (
	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/platform/authctx"
)

// API is endpoints 106–110.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	public.GET("/support/topics", a.topics)                // 106
	public.POST("/support/tickets", a.create)              // 107
	private.GET("/support/tickets", a.mine)                // 108
	private.GET("/support/tickets/:id", a.thread)          // 109
	private.POST("/support/tickets/:id/messages", a.reply) // 110
}

// topics tells a client which fields each topic needs, so the mobile app can build
// the same form as the website without hard-coding it.
func (a *API) topics(c *gin.Context) {
	extra := map[sqlc.TicketTopic][]string{
		sqlc.TicketTopicBug:        {"category_id", "device"},
		sqlc.TicketTopicQuestion:   {"category_id", "question_number"},
		sqlc.TicketTopicAccount:    {"account"},
		sqlc.TicketTopicReportUser: {"username", "reason", "link"},
		sqlc.TicketTopicSuggestion: {"category_id"},
		sqlc.TicketTopicOther:      {},
	}
	out := make([]gin.H, 0, len(Topics))
	for _, t := range Topics {
		out = append(out, gin.H{"key": string(t), "icon": TopicIcon[t], "fields": extra[t]})
	}
	apiutil.OK(c, gin.H{"topics": out, "min_message": MinMessage})
}

func (a *API) create(c *gin.Context) {
	var body struct {
		Email   string         `json:"email"`
		Topic   string         `json:"topic"`
		Subject string         `json:"subject"`
		Body    string         `json:"body"`
		Context map[string]any `json:"context"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	ticket, err := a.svc.Create(c.Request.Context(), Input{
		Who: authctx.Get(c), Email: body.Email, Topic: body.Topic,
		Subject: body.Subject, Body: body.Body, Context: body.Context,
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, ticket)
}

func (a *API) mine(c *gin.Context) {
	page, per := apiutil.PageParams(c, 20, 50)
	rows, total, err := a.svc.Mine(c.Request.Context(), authctx.Get(c), page, per)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Paged(c, rows, apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

func (a *API) thread(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	t, messages, err := a.svc.Thread(c.Request.Context(), authctx.Get(c), id, false)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"ticket": t, "messages": messages})
}

func (a *API) reply(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.Reply(c.Request.Context(), authctx.Get(c), id, body.Body, false, false); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

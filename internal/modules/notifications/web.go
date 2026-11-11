package notifications

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"
)

// Web is P27 Notifications. Guests get the locked page (RL-03).
type Web struct{ svc *Service }

func NewWeb(s *Service) *Web { return &Web{svc: s} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/notifications", w.page)
	private.GET("/notifications/:id", w.open)
	private.POST("/notifications/read", w.readAll)
}

// tone and icon per kind, the circle in front of each row.
var look = map[string][2]string{
	"friend_request":                 {"green", "user"},
	"friend_accepted":                {"green", "users"},
	"challenge_received":             {"blue", "swords"},
	"challenge_participant_finished": {"blue", "send"},
	"challenge_results_ready":        {"blue", "trophy"},
	"challenge_removed":              {"blue", "x"},
	"message":                        {"blue", "chat"},
	"room_invite":                    {"night", "radio"},
	"duel_result":                    {"night", "swords"},
	"question_approved":              {"green", "check"},
	"question_rejected":              {"gold", "x"},
	"question_sent_back":             {"gold", "pen"},
	"support_reply":                  {"gold", "help"},
	"achievement":                    {"gold", "star"},
	"streak_reminder":                {"gold", "flame"},
	"system":                         {"gold", "bell"},
}

func (w *Web) page(c *gin.Context) {
	v := view.From(c)
	if v.IsGuest() {
		render.Page(c, pages.LockedForGuest(v, "notifications"))
		return
	}
	unreadOnly := c.Query("filter") == "unread"
	page, per := apiutil.PageParams(c, 25, 50)
	rows, total, err := w.svc.List(c.Request.Context(), v.User, unreadOnly, page, per)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	unread, err := w.svc.Unread(c.Request.Context(), v.User.ID)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := pages.NotificationsData{
		UnreadOnly: unreadOnly, Unread: unread, Page: page,
		Pages: (total + per - 1) / per,
	}
	if data.Pages < 1 {
		data.Pages = 1
	}
	for _, n := range rows {
		l, ok := look[n.Kind]
		if !ok {
			l = [2]string{"gold", "bell"}
		}
		data.Rows = append(data.Rows, pages.Notification{
			ID: n.ID.String(), Text: text(v, n), When: ago(v, n.Created),
			Icon: l[1], Tone: l[0], Link: n.Link, Unread: n.Unread,
		})
	}
	render.Page(c, pages.Notifications(v, data))
}

// text builds the sentence from the kind and the payload, in the viewer's language.
// A notification written before its sender was recorded has no name to put in, so the
// sentence says "Someone" rather than showing the placeholder itself.
func text(v *view.Ctx, n Notification) string {
	args := make([]any, 0, 8)
	for _, key := range []string{"name", "number", "title"} {
		if raw, ok := n.Payload[key]; ok && raw != "" {
			args = append(args, key, raw)
		}
	}
	// Last, so a real name always wins: the replacer takes the first pair that matches.
	args = append(args, "name", v.Tr("notif.someone"))
	return v.Tr("notif."+n.Kind, args...)
}

func ago(v *view.Ctx, created string) string {
	t, err := time.Parse("2006-01-02T15:04:05Z", created)
	if err != nil {
		return ""
	}
	return t.Local().Format("2 Jan 15:04")
}

func (w *Web) open(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	link, err := w.svc.Open(c.Request.Context(), v.User, id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, link)
}

func (w *Web) readAll(c *gin.Context) {
	v := view.From(c)
	if err := w.svc.MarkRead(c.Request.Context(), v.User, nil); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/notifications")
}

// Unread implements the shell's badge source (CH-11, MS-01).
func (w *Web) UnreadCount(c *gin.Context) int {
	v := view.From(c)
	if v.User == nil {
		return 0
	}
	n, err := w.svc.Unread(c.Request.Context(), v.User.ID)
	if err != nil {
		return 0
	}
	return n
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

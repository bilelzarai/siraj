package messages

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

type Web struct {
	svc    *Service
	blocks BlockAction
}

// BlockAction is "Block" on a conversation: the social module owns the rule, this
// screen only asks for it (FR-07).
type BlockAction interface {
	BlockUser(c *gin.Context, other uuid.UUID) error
}

func NewWeb(s *Service) *Web { return &Web{svc: s} }

// SetBlocks wires the block action in.
func (w *Web) SetBlocks(b BlockAction) { w.blocks = b }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/messages", w.page)
	private.POST("/messages/:id", w.send)
	private.POST("/messages/:id/pin", w.pin)
	private.POST("/messages/:id/mute", w.mute)
	private.POST("/messages/:id/unread", w.unread)
	private.POST("/messages/:id/block", w.block)
	private.POST("/messages/:id/delete", w.deleteConversation)
	// A single message: its own small actions (MS-04).
	private.POST("/messages/m/:id/hide", w.hideMessage)
	private.POST("/messages/m/:id/delete", w.deleteMessage)
	private.POST("/messages/m/:id/report", w.reportMessage)
	// What players attached to a message. The name on disk is random and the path is
	// checked, so nothing outside the uploads directory can be reached.
	private.GET("/uploads/:name", w.upload)
	w.registerGroups(public, private)
}

func (w *Web) upload(c *gin.Context) {
	name := c.Param("name")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		c.Status(http.StatusNotFound)
		return
	}
	c.File(filepath.Join(w.svc.uploads, name))
}

// page is P22. "with" is a thread of mine, a room code, or the player to write to:
// coming from a friend's card opens (or starts) the thread in one step (MS-01, MS-02).
func (w *Web) page(c *gin.Context) {
	v := view.From(c)
	target := strings.TrimSpace(c.Query("with"))

	// "with" a person who is not yet a thread: make it, then open it.
	if id, err := uuid.Parse(target); err == nil {
		mine, err := w.svc.IsMember(c.Request.Context(), v.User, id)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		if !mine {
			conversation, err := w.svc.WithPerson(c.Request.Context(), v.User, id)
			if err != nil {
				w.fail(c, v, err)
				return
			}
			render.Redirect(c, "/messages?with="+conversation.ID.String())
			return
		}
	}

	d, err := w.data(c, v, target)
	if err != nil {
		w.fail(c, v, err)
		return
	}

	// The two parts the stream refreshes on their own.
	switch c.Query("partial") {
	case "list":
		render.Page(c, pages.InboxList(v, d))
		return
	case "thread":
		render.Page(c, pages.MessageThread(v, d))
		return
	}
	if what := c.Query("confirm"); what == "block" || what == "delete" {
		render.Page(c, pages.MessagesWithConfirm(v, d, what, target))
		return
	}
	render.Page(c, pages.Messages(v, d))
}

// data reads everything one view of the screen needs.
func (w *Web) data(c *gin.Context, v *view.Ctx, target string) (pages.MessagesData, error) {
	ctx := c.Request.Context()
	tab := ParseTab(c.Query("tab"))
	d := pages.MessagesData{
		Tab: string(tab), Query: c.Query("q"), Guest: v.IsGuest(),
		NewOpen: c.Query("new") != "", NewKind: c.Query("new"),
		// The composer refuses a file the upload handler would refuse, using the
		// handler's own number (uploads.go) rather than a second copy of it.
		MaxUploadBytes: MaxUpload,
		// The people a group dialog picks from are the same candidates.
	}
	counts, err := w.svc.TabCounts(ctx, v.User)
	if err != nil {
		return d, err
	}
	for _, t := range []Tab{TabPeople, TabGroups, TabRooms} {
		d.Tabs = append(d.Tabs, pages.MessageTab{
			Key: string(t), Label: v.Tr("messages.tab_" + string(t)),
			Unread: counts[t], On: t == tab,
		})
	}
	d.TabNote = v.Tr("messages.tab_note_" + string(tab))

	rows, err := w.svc.Inbox(ctx, v.User, tab, d.Query)
	if err != nil {
		return d, err
	}
	for _, r := range rows {
		conv := pages.Conversation{
			ID: r.ID, Name: r.Name, Preview: preview(v, r.Preview),
			Unread: r.Unread, Online: r.Online, Pinned: r.Pinned, Muted: r.Muted,
			Guest: r.Guest, Group: r.Group, Room: r.Room, System: r.System,
			Active: r.ID == target, Initials: avatar.Initials(r.Name), Color: faceColor(r),
		}
		if !r.At.IsZero() {
			conv.When = when(v, r.At)
		}
		d.Conversations = append(d.Conversations, conv)
	}

	if d.NewOpen {
		people, err := w.svc.Candidates(ctx, v.User)
		if err != nil {
			return d, err
		}
		for _, p := range people {
			d.Candidates = append(d.Candidates, pages.MessagePerson{
				ID: p.ID.String(), Name: p.Name, Note: p.Note, Online: p.Online,
				Initials: avatar.Initials(p.Name), Color: avatar.Color(p.ID.String()),
			})
		}
	}
	if target == "" {
		return d, nil
	}
	// The members panel and the challenge picker open over the thread.
	if id, err := uuid.Parse(target); err == nil {
		w.groupData(c, v, &d, id)
	}

	head, bubbles, err := w.svc.Open(ctx, v.User, target)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Status == http.StatusNotFound {
			return d, nil // the thread is gone; the list still stands
		}
		return d, err
	}
	d.Open, d.ThreadID, d.CanReply, d.RoomCode = true, target, head.CanReply, head.RoomCode
	// The names under a title belong to a group or a room; a thread of two shows
	// what the other person is doing instead (MS-02).
	if head.Group || head.Room {
		d.People = head.People
	}
	d.Presence = presence(v, head)
	d.Head = pages.Conversation{
		ID: idOf(head), Name: head.Name, Initials: avatar.Initials(head.Name),
		Color: avatar.Color(headFace(head)), Online: head.Online,
		Group: head.Group, Room: head.Room, System: head.System,
	}
	day := ""
	for i, b := range bubbles {
		row := pages.Message{
			ID: b.ID, Kind: b.Kind, Body: b.Body, Mine: b.Mine, From: b.From,
			Deleted: b.Deleted, Read: b.Read, System: b.System,
			Quote: quoteText(v, b.Quote), QuoteWho: b.QuoteWho,
			FileName: b.FileName, FileURL: b.FileURL, Title: b.Title,
			Game: b.ChallengeID,
		}
		if b.FileSize > 0 {
			row.FileSize = size(b.FileSize)
		}
		if b.Duration > 0 {
			row.Duration = clock(b.Duration)
		}
		if b.Line != "" {
			var questions, score int
			_, _ = fmt.Sscanf(b.Line, "%d|%d", &questions, &score)
			row.Line = v.Tr("messages.challenge_line", "questions", questions, "score", score)
		}
		if !b.At.IsZero() {
			row.When = b.At.Local().Format("15:04")
			if d := dayOf(v, b.At); d != day {
				row.Day, day = d, d
			}
		}
		// In a group the sender's name sits over their first bubble of a run.
		row.ShowFrom = (head.Group || head.Room) && !b.Mine &&
			(i == 0 || bubbles[i-1].From != b.From)
		d.Messages = append(d.Messages, row)
		if b.ID == c.Query("reply") {
			d.ReplyTo, d.ReplyWho, d.ReplyBody = b.ID, replyWho(v, b), b.Body
		}
	}
	return d, nil
}

func (w *Web) send(c *gin.Context) {
	v := view.From(c)
	id := c.Param("id")
	in := Draft{Body: c.PostForm("body")}
	if reply, err := uuid.Parse(c.PostForm("reply_to")); err == nil {
		in.ReplyTo = reply
	}
	// An attachment rides with the line it was sent beside (MS-02).
	if file, err := c.FormFile("upload"); err == nil && file != nil {
		upload, kind, err := w.svc.SaveUpload(c.Request.Context(), v.User, file)
		if err != nil {
			// A file that is too big is the player's doing, not a fault: the thread
			// says so, with the size it had to be under (MS-02).
			if e, ok := apperr.As(err); ok && e.Status < http.StatusInternalServerError {
				render.Toast(c, uploadProblem(v, e))
				render.Redirect(c, backToThread(c, id))
				return
			}
			w.fail(c, v, err)
			return
		}
		in.UploadID, in.Kind = upload, kind
	}
	if err := w.svc.Send(c.Request.Context(), v.User, id, in); err != nil {
		if e, ok := apperr.As(err); ok && e.Status < 500 {
			render.Toast(c, e.Message)
			render.Redirect(c, backToThread(c, id))
			return
		}
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, backToThread(c, id))
}

// uploadProblem is what went wrong with an attachment, in words: the field errors
// carry the detail, the message carries the shape.
func uploadProblem(v *view.Ctx, e *apperr.Error) string {
	if why, ok := e.Fields["upload"]; ok && why != "" {
		return v.Tr("messages.attach") + ": " + why
	}
	return e.Message
}

// backToThread is where a sent message returns to: the same thread, on the same tab,
// with the same search still in the box (MS-01).
func backToThread(c *gin.Context, id string) string {
	out := "/messages?tab=" + string(ParseTab(c.PostForm("tab"))) + "&with=" + id
	if q := c.PostForm("q"); q != "" {
		out += "&q=" + url.QueryEscape(q)
	}
	return out
}

func (w *Web) pin(c *gin.Context)  { w.conversationAction(c, "pin") }
func (w *Web) mute(c *gin.Context) { w.conversationAction(c, "mute") }
func (w *Web) unread(c *gin.Context) {
	w.conversationAction(c, "unread")
}
func (w *Web) deleteConversation(c *gin.Context) { w.conversationAction(c, "delete") }

// conversationAction runs one line of the row menu and comes back to the list.
func (w *Web) conversationAction(c *gin.Context, what string) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("conversation"))
		return
	}
	ctx := c.Request.Context()
	back := backToThread(c, id.String())
	switch what {
	case "pin":
		err = w.svc.Pin(ctx, v.User, id, c.PostForm("pinned") == "true")
	case "mute":
		err = w.svc.Mute(ctx, v.User, id, c.PostForm("muted") == "true")
		if err == nil {
			render.Toast(c, v.Tr("messages.muted_toast"))
		}
	case "unread":
		err = w.svc.MarkUnread(ctx, v.User, id)
		back = "/messages?tab=" + string(ParseTab(c.PostForm("tab")))
	case "delete":
		err = w.svc.Hide(ctx, v.User, id)
		back = "/messages?tab=" + string(ParseTab(c.PostForm("tab")))
	}
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, back)
}

// block is the dialog's red button: the social module owns the rule (FR-07).
func (w *Web) block(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || w.blocks == nil {
		w.fail(c, v, apperr.NotFound("conversation"))
		return
	}
	head, _, err := w.svc.Open(c.Request.Context(), v.User, id.String())
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.blocks.BlockUser(c, head.UserID); err != nil {
		w.fail(c, v, err)
		return
	}
	// A blocked person's thread leaves the list, as the design shows.
	_ = w.svc.Hide(c.Request.Context(), v.User, id)
	render.Toast(c, v.Tr("messages.blocked_toast"))
	render.Redirect(c, "/messages")
}

func (w *Web) hideMessage(c *gin.Context)   { w.messageAction(c, "hide") }
func (w *Web) deleteMessage(c *gin.Context) { w.messageAction(c, "delete") }
func (w *Web) reportMessage(c *gin.Context) { w.messageAction(c, "report") }

func (w *Web) messageAction(c *gin.Context, what string) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("message"))
		return
	}
	ctx := c.Request.Context()
	switch what {
	case "hide":
		err = w.svc.DeleteForMe(ctx, v.User, id)
	case "delete":
		err = w.svc.DeleteForEveryone(ctx, v.User, id)
	case "report":
		err = w.svc.Report(ctx, v.User, id, c.PostForm("reason"), c.PostForm("note"))
		if err == nil {
			render.Toast(c, v.Tr("messages.reported_toast"))
		}
	}
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Status < 500 {
			render.Toast(c, e.Message)
			render.Redirect(c, backTo(c))
			return
		}
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, backTo(c))
}

// backTo keeps the player on the thread the action came from.
func backTo(c *gin.Context) string {
	if with := c.PostForm("with"); with != "" {
		return "/messages?with=" + with
	}
	if ref := c.GetHeader("Referer"); strings.Contains(ref, "/messages") {
		if i := strings.Index(ref, "/messages"); i >= 0 {
			return ref[i:]
		}
	}
	return "/messages"
}

// preview, quoteText and replyWho turn the service's placeholders — {voice}, {image},
// {file}, {you} — into the player's own language (LG-05).
func preview(v *view.Ctx, s string) string {
	s = strings.Replace(s, "{you}", v.Tr("messages.you_prefix"), 1)
	return quoteText(v, s)
}

func quoteText(v *view.Ctx, s string) string {
	for key, tr := range map[string]string{
		"{voice}": "messages.a_voice_note", "{image}": "messages.a_photo",
		"{file}": "messages.a_file", "{challenge}": "messages.a_challenge",
	} {
		s = strings.Replace(s, key, v.Tr(tr), 1)
	}
	return s
}

func replyWho(v *view.Ctx, b Bubble) string {
	if b.Mine {
		return v.Tr("home.you")
	}
	return b.From
}

// faceColor keeps a person the same colour wherever they appear: it comes from their
// name, which every screen has. A group or a room is coloured by its own id.
func faceColor(r Row) string {
	if r.Group || r.Room {
		return avatar.Color(r.ID)
	}
	return avatar.Color(r.Name)
}

func headFace(head Head) string {
	if head.Group || head.Room {
		return head.ID
	}
	return head.Name
}

// presence is the line under a name: what they are doing, or when they last played.
func presence(v *view.Ctx, head Head) string {
	switch {
	case head.System:
		return v.Tr("messages.system_sub")
	case head.Room:
		return head.People
	case head.Guest && head.Online:
		return v.Tr("guest.badge") + " · " + v.Tr("friends.online_now")
	case head.Online:
		return v.Tr("friends.online_now")
	}
	return v.Tr("messages.offline")
}

func idOf(head Head) string {
	if head.UserID != uuid.Nil {
		return head.UserID.String()
	}
	return head.ID
}

// dayOf is the separator over the first message of a day (MS-02).
func dayOf(v *view.Ctx, t time.Time) string {
	local, now := t.Local(), time.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case !local.Before(today):
		return v.Tr("time.today")
	case !local.Before(today.AddDate(0, 0, -1)):
		return v.Tr("time.yesterday")
	case !local.Before(today.AddDate(0, 0, -6)):
		return local.Format("Mon")
	}
	return local.Format("2 Jan")
}

// when is the time on a row of the list: a clock today, a weekday this week, a date
// before that.
func when(v *view.Ctx, t time.Time) string {
	local, now := t.Local(), time.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case !local.Before(today):
		return local.Format("15:04")
	case !local.Before(today.AddDate(0, 0, -1)):
		return v.Tr("time.yesterday")
	case !local.Before(today.AddDate(0, 0, -6)):
		return local.Format("Mon")
	}
	return local.Format("2 Jan")
}

// size reads a file the way the design prints it: 248 KB, 2 KB, 1.4 MB.
func size(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%d KB", bytes/(1<<10))
	}
	return fmt.Sprintf("%d B", bytes)
}

// clock is a voice note's length: 0:06.
func clock(ms int) string {
	seconds := ms / 1000
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok {
		if e.Status == http.StatusNotFound {
			render.Status(c, http.StatusNotFound, pages.NotFound(v))
			return
		}
		withFlash := v.Clone()
		withFlash.Flash = e.Message
		render.Status(c, e.Status, pages.ServerError(withFlash))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

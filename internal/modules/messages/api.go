package messages

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// API is endpoints 75–86: the inbox, one thread, what is said in it and what is
// attached to it. Every answer comes from the same service the Messages screen uses
// (NF-04); a room's chat is read through the same thread endpoints, with the room
// code in place of the conversation id.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/conversations", a.list)                  // 75
	private.POST("/conversations", a.create)               // 76
	private.GET("/conversations/:id", a.get)               // 77
	private.PATCH("/conversations/:id", a.patch)           // 78
	private.POST("/conversations/:id/read", a.read)        // 79
	private.POST("/conversations/:id/unread", a.unread)    // 80
	private.DELETE("/conversations/:id", a.hide)           // 81
	private.GET("/conversations/:id/messages", a.messages) // 82
	private.POST("/conversations/:id/messages", a.send)    // 83
	private.DELETE("/messages/:id", a.deleteMessage)       // 84
	private.POST("/messages/:id/reports", a.report)        // 85
	private.POST("/uploads", a.upload)                     // 86
	// Managing a group, and putting a challenge into a thread (MS-01, MS-02).
	private.GET("/conversations/:id/members", a.members)
	private.POST("/conversations/:id/members", a.addMembers)
	private.DELETE("/conversations/:id/members/:userId", a.removeMember)
	private.POST("/conversations/:id/leave", a.leave)
	private.POST("/conversations/:id/challenge", a.sendChallenge)
	private.GET("/challenges-shareable", a.shareable)
	private.GET("/conversations-candidates", a.candidates)    // 76, who can be written to
	private.GET("/conversations-unread-count", a.unreadCount) // the badge (MS-06)
}

func rowDTO(r Row) gin.H {
	return gin.H{
		"id": r.ID, "user_id": r.UserID, "name": r.Name, "preview": r.Preview,
		"at": at(r.At), "unread": r.Unread, "online": r.Online, "pinned": r.Pinned,
		"muted": r.Muted, "guest": r.Guest, "group": r.Group, "room": r.Room,
		"system": r.System, "members": r.Members, "room_code": r.RoomCode,
	}
}

func at(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// list is endpoint 75: ?tab=people|groups|rooms&q=
func (a *API) list(c *gin.Context) {
	page, per := apiutil.PageParams(c, 25, 100)
	rows, total, err := a.svc.InboxPage(c.Request.Context(), authctx.Get(c),
		ParseTab(c.Query("tab")), c.Query("q"), page, per)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = rowDTO(r)
	}
	// A list says where the reader is, so a client can ask for the next page (§17).
	apiutil.Paged(c, out, apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

// create is endpoint 76: a thread with one person, or a group with several.
func (a *API) create(c *gin.Context) {
	var body struct {
		UserID    string   `json:"user_id"`
		UserIDs   []string `json:"user_ids"`
		Title     string   `json:"title"`
		GroupFlag bool     `json:"group"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	who := authctx.Get(c)
	ids := make([]uuid.UUID, 0, len(body.UserIDs)+1)
	for _, raw := range append(body.UserIDs, body.UserID) {
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			apiutil.Fail(c, apperr.BadRequest("bad_id", "user ids must be UUIDs"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		apiutil.Fail(c, apperr.Invalid(map[string]string{"user_ids": "pick at least one person"}))
		return
	}
	if !body.GroupFlag && len(ids) == 1 {
		conversation, err := a.svc.WithPerson(c.Request.Context(), who, ids[0])
		if err != nil {
			apiutil.Fail(c, err)
			return
		}
		apiutil.Created(c, gin.H{"id": conversation.ID, "kind": string(conversation.Kind)})
		return
	}
	conversation, err := a.svc.StartGroup(c.Request.Context(), who, body.Title, ids)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{"id": conversation.ID, "kind": string(conversation.Kind), "title": body.Title})
}

// get is endpoint 77: the thread's head, with the bubbles alongside so one call
// opens a screen. Opening marks it read, the way the screen does (MS-02).
func (a *API) get(c *gin.Context) {
	head, bubbles, err := a.svc.Open(c.Request.Context(), authctx.Get(c), c.Param("id"))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"conversation": headDTO(head), "messages": bubbleDTOs(bubbles)})
}

func headDTO(h Head) gin.H {
	return gin.H{
		"id": h.ID, "name": h.Name, "user_id": h.UserID, "presence": h.Presence,
		"online": h.Online, "group": h.Group, "room": h.Room, "system": h.System,
		"guest": h.Guest, "people": h.People, "room_code": h.RoomCode,
		"can_reply": h.CanReply,
	}
}

func bubbleDTOs(bubbles []Bubble) []gin.H {
	out := make([]gin.H, len(bubbles))
	for i, b := range bubbles {
		out[i] = gin.H{
			"id": b.ID, "kind": b.Kind, "body": b.Body, "at": at(b.At), "mine": b.Mine,
			"from": b.From, "deleted": b.Deleted, "read": b.Read, "system": b.System,
			"quote": b.Quote, "quote_who": b.QuoteWho,
			"file_name": b.FileName, "file_size": b.FileSize, "file_url": b.FileURL,
			"duration": b.Duration, "title": b.Title, "line": b.Line,
			"challenge_id": b.ChallengeID,
		}
	}
	return out
}

// patch is endpoint 78: pin, mute or rename.
func (a *API) patch(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body struct {
		Pinned *bool   `json:"pinned"`
		Muted  *bool   `json:"muted"`
		Title  *string `json:"title"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	ctx, who := c.Request.Context(), authctx.Get(c)
	if body.Pinned != nil {
		if err := a.svc.Pin(ctx, who, id, *body.Pinned); err != nil {
			apiutil.Fail(c, err)
			return
		}
	}
	if body.Muted != nil {
		if err := a.svc.Mute(ctx, who, id, *body.Muted); err != nil {
			apiutil.Fail(c, err)
			return
		}
	}
	if body.Title != nil {
		if err := a.svc.Rename(ctx, who, id, *body.Title); err != nil {
			apiutil.Fail(c, err)
			return
		}
	}
	apiutil.NoContent(c)
}

// read is endpoint 79.
func (a *API) read(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.MarkRead(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// unread is endpoint 80.
func (a *API) unread(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.MarkUnread(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// hide is endpoint 81: deleted for me, still there for the other side (MS-04).
func (a *API) hide(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Hide(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// messages is endpoint 82.
func (a *API) messages(c *gin.Context) {
	_, bubbles, err := a.svc.Open(c.Request.Context(), authctx.Get(c), c.Param("id"))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, bubbleDTOs(bubbles))
}

// send is endpoint 83.
func (a *API) send(c *gin.Context) {
	var body struct {
		Body     string `json:"body"`
		ReplyTo  string `json:"reply_to"`
		UploadID string `json:"upload_id"`
		Kind     string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	draft := Draft{Body: body.Body, Kind: body.Kind}
	for raw, field := range map[string]*uuid.UUID{body.ReplyTo: &draft.ReplyTo, body.UploadID: &draft.UploadID} {
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			apiutil.Fail(c, apperr.BadRequest("bad_id", "reply_to and upload_id must be UUIDs"))
			return
		}
		*field = id
	}
	if err := a.svc.Send(c.Request.Context(), authctx.Get(c), c.Param("id"), draft); err != nil {
		apiutil.Fail(c, err)
		return
	}
	// Answer with the thread as it now stands, so a client never has to guess what
	// the server made of the draft.
	a.messages(c)
}

// deleteMessage is endpoint 84: ?scope=everyone takes it back for both sides.
func (a *API) deleteMessage(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	ctx, who := c.Request.Context(), authctx.Get(c)
	if c.Query("scope") == "everyone" {
		err = a.svc.DeleteForEveryone(ctx, who, id)
	} else {
		err = a.svc.DeleteForMe(ctx, who, id)
	}
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// report is endpoint 85.
func (a *API) report(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body struct {
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.Report(c.Request.Context(), authctx.Get(c), id, body.Reason, body.Note); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// upload is endpoint 86: multipart, one file per call, and the id it answers with is
// what a message carries (MS-02).
func (a *API) upload(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		apiutil.Fail(c, apperr.Invalid(map[string]string{"file": "attach a file"}))
		return
	}
	id, kind, err := a.svc.SaveUpload(c.Request.Context(), authctx.Get(c), fh)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{"id": id, "kind": kind, "name": fh.Filename, "bytes": fh.Size})
}

// candidates is who the "New message" dialog may offer (§4 of 03-BACKEND-CHANGES).
func (a *API) candidates(c *gin.Context) {
	rows, err := a.svc.Candidates(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = gin.H{"user_id": r.ID, "name": r.Name, "note": r.Note, "online": r.Online, "guest": r.Guest}
	}
	apiutil.OK(c, out)
}

// members is the group panel's list.
func (a *API) members(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	group, err := a.svc.Group(c.Request.Context(), authctx.Get(c), id)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	members := make([]gin.H, len(group.Members))
	for i, m := range group.Members {
		members[i] = gin.H{
			"user_id": m.ID, "name": m.Name, "online": m.Online,
			"guest": m.Guest, "owner": m.Owner, "is_me": m.Me,
		}
	}
	candidates := make([]gin.H, len(group.Candidates))
	for i, p := range group.Candidates {
		candidates[i] = gin.H{"user_id": p.ID, "name": p.Name, "online": p.Online, "note": p.Note}
	}
	apiutil.OK(c, gin.H{
		"id": group.ID, "title": group.Title, "i_am_owner": group.IAmOwner,
		"members": members, "can_add": candidates,
	})
}

// addMembers puts friends into the group.
func (a *API) addMembers(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	people := make([]uuid.UUID, 0, len(body.UserIDs))
	for _, raw := range body.UserIDs {
		person, err := uuid.Parse(raw)
		if err != nil {
			apiutil.Fail(c, apperr.BadRequest("bad_id", "user ids must be UUIDs"))
			return
		}
		people = append(people, person)
	}
	if err := a.svc.AddPeople(c.Request.Context(), authctx.Get(c), id, people); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.members(c)
}

// removeMember takes one out; leaving is the same call pointed at yourself.
func (a *API) removeMember(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	person, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		apiutil.Fail(c, apperr.BadRequest("bad_id", "userId must be a UUID"))
		return
	}
	if err := a.svc.RemovePerson(c.Request.Context(), authctx.Get(c), id, person); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) leave(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Leave(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// shareable is what the composer may attach.
func (a *API) shareable(c *gin.Context) {
	rows, err := a.svc.Shareable(c.Request.Context(), authctx.Get(c), apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = gin.H{
			"challenge_id": r.ID, "title": r.Title,
			"questions": r.Questions, "score_to_beat": r.ScoreToBeat,
		}
	}
	apiutil.OK(c, out)
}

// sendChallenge posts the card into the thread.
func (a *API) sendChallenge(c *gin.Context) {
	var body struct {
		ChallengeID string `json:"challenge_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	challengeID, err := uuid.Parse(body.ChallengeID)
	if err != nil {
		apiutil.Fail(c, apperr.BadRequest("bad_id", "challenge_id must be a UUID"))
		return
	}
	if err := a.svc.SendChallenge(c.Request.Context(), authctx.Get(c), c.Param("id"), challengeID, apiutil.Lang(c)); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.messages(c)
}

// unreadCount feeds the navigation badge (MS-06).
func (a *API) unreadCount(c *gin.Context) {
	who := authctx.Get(c)
	n, err := a.svc.Unread(c.Request.Context(), who.ID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"unread": n})
}

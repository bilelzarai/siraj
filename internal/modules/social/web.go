package social

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// Web is P23 Friends and the small posts behind it. Friends is a member screen:
// guests get the "create a free account" page (RL-03).
type Web struct {
	svc     *Service
	baseURL string
}

func NewWeb(s *Service, baseURL string) *Web { return &Web{svc: s, baseURL: baseURL} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/friends", w.page)
	private.POST("/friend-requests", w.send)
	private.POST("/friend-requests/:id/accept", w.accept)
	private.POST("/friend-requests/:id/decline", w.decline)
	private.POST("/friends/:id/remove", w.remove)
	private.POST("/blocks/:id", w.block)
	private.POST("/blocks/:id/remove", w.unblock)
	private.GET("/u/:username", w.publicProfile)
}

func (w *Web) page(c *gin.Context) {
	v := view.From(c)
	if v.IsGuest() {
		render.Page(c, pages.LockedForGuest(v, "friends"))
		return
	}
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	friends, err := w.svc.Friends(ctx, v.User, q)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := pages.FriendsData{Q: q}
	for _, p := range friends {
		row := person(p)
		row.HasRecord = p.Wins+p.Losses > 0
		data.Friends = append(data.Friends, row)
		if p.Online {
			data.Online = append(data.Online, row)
		}
	}
	requests, err := w.svc.Requests(ctx, v.User, true)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	for _, r := range requests {
		data.Requests = append(data.Requests, pages.FriendRequest{ID: r.ID.String(), Person: person(r.Person)})
	}
	if q != "" {
		found, err := w.svc.Search(ctx, v.User, q, 10)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, p := range found {
			if !p.IsFriend {
				data.Found = append(data.Found, person(p))
			}
		}
	}
	suggest, err := w.svc.Suggestions(ctx, v.User, 5)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	for _, p := range suggest {
		data.Suggest = append(data.Suggest, person(p))
	}
	code, err := w.svc.InviteLink(ctx, v.User)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data.InviteLink = w.baseURL + "/i/" + code
	render.Page(c, pages.Friends(v, data))
}

func (w *Web) send(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.PostForm("to"))
	if err != nil {
		w.fail(c, v, apperr.BadRequest("bad_id", "to must be a uuid"))
		return
	}
	if err := w.svc.SendRequest(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("friends.request_sent", "name", c.PostForm("name")))
	w.backToFriends(c)
}

func (w *Web) accept(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Accept(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	w.backToFriends(c)
}

func (w *Web) decline(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Decline(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	w.backToFriends(c)
}

func (w *Web) remove(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Remove(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	w.backToFriends(c)
}

func (w *Web) block(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Block(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	w.backToFriends(c)
}

func (w *Web) unblock(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Unblock(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	w.backToFriends(c)
}

// publicProfile is endpoint 70 on the website: another player's card.
func (w *Web) publicProfile(c *gin.Context) {
	v := view.From(c)
	row, err := w.svc.db.Q.GetUserByUsername(c.Request.Context(), toText(c.Param("username")))
	if err != nil {
		render.Status(c, http.StatusNotFound, pages.NotFound(v))
		return
	}
	p := pages.Person{
		ID: row.ID.String(), Name: row.DisplayName, Level: row.Level, Online: row.Online,
		Initials: avatar.Initials(row.DisplayName), Color: avatar.Color(row.ID.String()),
	}
	if row.Username.Valid {
		p.Username = row.Username.String
	}
	friends, _ := w.svc.AreFriends(c.Request.Context(), v.User.ID, row.ID)
	p.IsFriend = friends
	render.Page(c, pages.PublicProfile(v, p, row.Xp))
}

func (w *Web) backToFriends(c *gin.Context) {
	// A request sent from a podium comes back to the podium (RM-08); everything else
	// lands on the friends screen.
	if back := c.PostForm("back"); strings.HasPrefix(back, "/") {
		render.Redirect(c, back)
		return
	}
	back := c.GetHeader("HX-Current-URL")
	if back == "" || !strings.HasPrefix(back, "/") {
		back = "/friends"
	}
	render.Redirect(c, back)
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if apiutil.WantsJSON(c) {
		apiutil.Fail(c, err)
		return
	}
	if e, ok := apperr.As(err); ok {
		if e.Status == http.StatusNotFound {
			render.Status(c, http.StatusNotFound, pages.NotFound(v))
			return
		}
		// A refused request is something the player did, not something that broke:
		// the screen says why instead of showing the error page.
		if e.Status < http.StatusInternalServerError {
			render.Toast(c, e.Message)
			w.backToFriends(c)
			return
		}
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

// FriendCards lists who the player can challenge (P20, CH-01). Online friends come
// first, the same order the Friends screen uses.
func (w *Web) FriendCards(c *gin.Context, v *view.Ctx) []pages.RoundFriend {
	people, err := w.svc.Friends(c.Request.Context(), v.User, "")
	if err != nil {
		return nil
	}
	out := make([]pages.RoundFriend, 0, len(people))
	for _, p := range people {
		status := v.Tr("friends.level", "level", p.Level)
		if p.Online {
			status = v.Tr("friends.online_now")
		}
		out = append(out, pages.RoundFriend{
			ID: p.ID.String(), Name: p.Name, Initials: avatar.Initials(p.Name),
			Color: avatar.Color(p.ID.String()), Status: status, Online: p.Online,
		})
	}
	return out
}

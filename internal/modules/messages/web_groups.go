package messages

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// Starting a group, managing one, and putting a challenge into a thread. Each of
// these is a small form over the Messages screen, and each one posts back to it.

func (w *Web) registerGroups(public, private *gin.RouterGroup) {
	private.POST("/messages/group", w.createGroup)
	private.POST("/messages/:id/rename", w.renameGroup)
	private.POST("/messages/:id/members", w.addMembers)
	private.POST("/messages/:id/members/:userId/remove", w.removeMember)
	private.POST("/messages/:id/leave", w.leaveGroup)
	private.POST("/messages/:id/challenge", w.sendChallenge)
}

// createGroup is the "New group" dialog's button.
func (w *Web) createGroup(c *gin.Context) {
	v := view.From(c)
	people := pickedPeople(c)
	conversation, err := w.svc.StartGroup(c.Request.Context(), v.User, c.PostForm("title"), people)
	if err != nil {
		w.toast(c, v, err)
		render.Redirect(c, "/messages?new=group")
		return
	}
	render.Toast(c, v.Tr("messages.group_created"))
	render.Redirect(c, "/messages?tab=groups&with="+conversation.ID.String())
}

// renameGroup is the field at the top of the members panel.
func (w *Web) renameGroup(c *gin.Context) {
	v := view.From(c)
	id, ok := w.threadID(c, v)
	if !ok {
		return
	}
	if err := w.svc.Rename(c.Request.Context(), v.User, id, c.PostForm("title")); err != nil {
		w.toast(c, v, err)
	} else {
		render.Toast(c, v.Tr("messages.group_renamed"))
	}
	render.Redirect(c, "/messages?tab=groups&with="+id.String()+"&panel=1")
}

// addMembers puts the ticked friends into the group.
func (w *Web) addMembers(c *gin.Context) {
	v := view.From(c)
	id, ok := w.threadID(c, v)
	if !ok {
		return
	}
	if err := w.svc.AddPeople(c.Request.Context(), v.User, id, pickedPeople(c)); err != nil {
		w.toast(c, v, err)
	} else {
		render.Toast(c, v.Tr("messages.people_added"))
	}
	render.Redirect(c, "/messages?tab=groups&with="+id.String()+"&panel=1")
}

// removeMember is the owner taking somebody out.
func (w *Web) removeMember(c *gin.Context) {
	v := view.From(c)
	id, ok := w.threadID(c, v)
	if !ok {
		return
	}
	person, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		w.toast(c, v, apperr.BadRequest("bad_id", "that is not a player"))
		render.Redirect(c, "/messages?tab=groups&with="+id.String()+"&panel=1")
		return
	}
	if err := w.svc.RemovePerson(c.Request.Context(), v.User, id, person); err != nil {
		w.toast(c, v, err)
	} else {
		render.Toast(c, v.Tr("messages.person_removed"))
	}
	render.Redirect(c, "/messages?tab=groups&with="+id.String()+"&panel=1")
}

// leaveGroup is the panel's last button.
func (w *Web) leaveGroup(c *gin.Context) {
	v := view.From(c)
	id, ok := w.threadID(c, v)
	if !ok {
		return
	}
	if err := w.svc.Leave(c.Request.Context(), v.User, id); err != nil {
		w.toast(c, v, err)
		render.Redirect(c, "/messages?tab=groups&with="+id.String()+"&panel=1")
		return
	}
	render.Toast(c, v.Tr("messages.group_left"))
	render.Redirect(c, "/messages?tab=groups")
}

// sendChallenge posts the card the picker chose.
func (w *Web) sendChallenge(c *gin.Context) {
	v := view.From(c)
	id, ok := w.threadID(c, v)
	if !ok {
		return
	}
	challengeID, err := uuid.Parse(c.PostForm("challenge_id"))
	if err != nil {
		w.toast(c, v, apperr.BadRequest("bad_id", "pick a challenge"))
		render.Redirect(c, "/messages?with="+id.String())
		return
	}
	if err := w.svc.SendChallenge(c.Request.Context(), v.User, id.String(), challengeID, v.Lang); err != nil {
		w.toast(c, v, err)
	} else {
		render.Toast(c, v.Tr("messages.challenge_sent"))
	}
	render.Redirect(c, "/messages?with="+id.String())
}

// groupData fills the members panel and the challenge picker when they are open.
func (w *Web) groupData(c *gin.Context, v *view.Ctx, d *pages.MessagesData, id uuid.UUID) {
	ctx := c.Request.Context()
	if c.Query("panel") != "" {
		group, err := w.svc.Group(ctx, v.User, id)
		if err == nil {
			d.GroupOpen, d.GroupTitle, d.IAmOwner = true, group.Title, group.IAmOwner
			for _, m := range group.Members {
				note := v.Tr("messages.member")
				if m.Owner {
					note = v.Tr("messages.owner")
				}
				if m.Me {
					note = v.Tr("messages.you")
					if m.Owner {
						note = v.Tr("messages.you") + " · " + v.Tr("messages.owner")
					}
				}
				d.Members = append(d.Members, pages.MessagePerson{
					ID: m.ID.String(), Name: m.Name, Note: note, Online: m.Online,
					Initials: avatar.Initials(m.Name), Color: avatar.Color(m.ID.String()),
					Me: m.Me, Owner: m.Owner,
				})
			}
			for _, p := range group.Candidates {
				d.CanAdd = append(d.CanAdd, pages.MessagePerson{
					ID: p.ID.String(), Name: p.Name, Online: p.Online,
					Initials: avatar.Initials(p.Name), Color: avatar.Color(p.ID.String()),
				})
			}
		}
	}
	if c.Query("pick") != "" {
		d.PickChallenge = true
		if rounds, err := w.svc.Shareable(ctx, v.User, v.Lang); err == nil {
			for _, r := range rounds {
				d.Shareable = append(d.Shareable, pages.ShareableChallenge{
					ID: r.ID.String(), Title: r.Title, Questions: r.Questions, Score: r.ScoreToBeat,
				})
			}
		}
	}
}

// threadID reads the conversation out of the path.
func (w *Web) threadID(c *gin.Context, v *view.Ctx) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		w.toast(c, v, apperr.BadRequest("bad_id", "that is not a conversation"))
		render.Redirect(c, "/messages")
		return uuid.Nil, false
	}
	return id, true
}

// pickedPeople reads the ticked boxes of a dialog.
func pickedPeople(c *gin.Context) []uuid.UUID {
	raw := c.PostFormArray("user_id")
	out := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		if id, err := uuid.Parse(value); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// toast says what went wrong in the words the service used.
func (w *Web) toast(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok {
		if len(e.Fields) > 0 {
			for _, why := range e.Fields {
				render.Toast(c, why)
				return
			}
		}
		render.Toast(c, e.Message)
		return
	}
	render.Toast(c, v.Tr("error.generic"))
}

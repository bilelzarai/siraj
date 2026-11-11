package nodes

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/modules/content"
	"siraj/internal/modules/perm"
	"siraj/internal/modules/social"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// My library (checklist OWN-1 and OWN-2): the player's side of the same tree the
// console edits.
//
// Every read and write goes through the service, which is where the rules live — a
// player sees their own branches and the official published tree, may build anywhere
// they own, may extend an official node, and may never change one. The handler adds
// nothing to that: a request straight to a URL meets exactly the checks the screen
// does (§7, §25 #8).

// LibraryPerPage is how many branches one page of the library shows.
const LibraryPerPage = 25

type Web struct {
	svc *Service
	// content is what hangs on a branch (§8). The library is where a player writes
	// it, so the two live on the same screen.
	content *content.Service
	// perm answers every access question, including who a branch is open to.
	perm *perm.Service
	// people is how a player finds somebody to share with.
	people PeopleSearch
}

// PeopleSearch is the slice of the social module this needs: finding somebody by
// name. Declared here rather than imported whole, so the hierarchy does not depend on
// everything social does.
type PeopleSearch interface {
	Search(ctx context.Context, who *authctx.Principal, q string, limit int32) ([]social.Person, error)
}

func NewWeb(s *Service, c *content.Service, p *perm.Service, people PeopleSearch) *Web {
	return &Web{svc: s, content: c, perm: p, people: people}
}

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/library", w.library)
	private.POST("/library", w.create)
	private.POST("/library/:id/archive", w.archive)
	// Content on a branch: written here, never in a separate place that would need
	// its own idea of who may write it.
	private.POST("/library/content", w.addContent)
	private.POST("/library/content/:id/delete", w.deleteContent)
	// Extending an official node from the screen that shows it: the player lands in
	// their own new branch, under content they do not own and have not changed.
	private.POST("/nodes/:id/extend", w.extend)
	// Sharing a branch (PERM-2): who may see it, at what level, and taking it back.
	private.GET("/library/:id/share", w.share)
	private.POST("/library/:id/share", w.grantShare)
	private.POST("/library/:id/revoke", w.revokeShare)
}

func (w *Web) library(c *gin.Context) {
	v := view.From(c)
	viewer := ViewerOf(v.User, v.Lang)
	parent, _ := strconv.ParseInt(c.Query("parent"), 10, 64)
	page, per := apiutil.PageParams(c, LibraryPerPage, 100)

	d := pages.LibraryData{Parent: parent, Page: page, Languages: i18n.Langs, CanWrite: v.User != nil}
	var err error

	if parent > 0 {
		// Reading the branch first is also the permission check: a branch the player
		// may not see answers not-found before anything is listed.
		node, err := w.svc.Get(c.Request.Context(), viewer, parent)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		d.ParentName, d.ParentOfficial = node.Name, node.Official()
		trail, err := w.svc.Trail(c.Request.Context(), viewer, parent)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, cr := range trail {
			d.Trail = append(d.Trail, pages.LibraryCrumb{ID: cr.ID, Name: cr.Name, Official: cr.Official})
		}
	}

	// At the top level the library lists what the player owns — their own roots and
	// their extensions, wherever those sit in the official tree. Inside a branch it
	// lists that branch's children, official ones included, because that is what
	// navigating a tree means.
	var rows []Node
	var total int
	if parent == 0 {
		rows, total, err = w.svc.Library(c.Request.Context(), viewer, page, per)
	} else {
		rows, total, err = w.svc.Children(c.Request.Context(), viewer, parent, page, per)
	}
	if err != nil {
		w.fail(c, v, err)
		return
	}
	for _, n := range rows {
		d.Nodes = append(d.Nodes, pages.LibraryNode{
			ID: n.ID, Name: n.Name, Icon: n.Icon, Color: n.Color,
			Children: n.Children, Questions: n.Questions, Status: n.Status,
			Official: n.Official(), Extension: n.ExtendsID > 0, Under: n.Under,
			Mine: v.User != nil && n.OwnerID == v.User.ID,
		})
	}
	d.Total, d.Pages = total, (total+per-1)/per
	if d.Pages < 1 {
		d.Pages = 1
	}
	if parent > 0 {
		who := perm.SubjectOf(v.User)
		if types, err := w.content.Types(c.Request.Context()); err == nil {
			for _, t := range types {
				d.Types = append(d.Types, pages.LibraryType{Code: t.Code, Icon: t.Icon, Fields: t.Fields})
			}
		}
		items, total, err := w.content.OnNode(c.Request.Context(), who, parent, "", false, 1, 50, v.Lang)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		d.ItemsTotal = total
		for _, it := range items {
			row := pages.LibraryItem{
				ID: it.ID, Type: it.Type, Icon: it.Icon, Status: it.Status,
				Title: it.Title(nil),
				Mine:  v.User != nil && it.OwnerID == v.User.ID,
			}
			if back, ok := it.Fields["back"].(string); ok {
				row.Back = back
			}
			d.Items = append(d.Items, row)
		}
	}
	render.Page(c, pages.Library(v, d))
}

// addContent writes a piece of content onto the branch in view. The fields posted are
// named for the kind they belong to, so the form can carry every kind at once and the
// handler reads only the one chosen.
func (w *Web) addContent(c *gin.Context) {
	v := view.From(c)
	node, _ := strconv.ParseInt(c.PostForm("node"), 10, 64)
	kind := c.PostForm("type")

	types, err := w.content.Types(c.Request.Context())
	if err != nil {
		w.fail(c, v, err)
		return
	}
	fields := map[string]any{}
	for _, t := range types {
		if t.Code != kind {
			continue
		}
		for _, f := range t.Fields {
			if val := strings.TrimSpace(c.PostForm("field_" + t.Code + "_" + f)); val != "" {
				fields[f] = val
			}
		}
	}

	nodeRes, err := w.svc.Resource(c.Request.Context(), node)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if _, err := w.content.Create(c.Request.Context(), perm.SubjectOf(v.User), nodeRes, content.Input{
		NodeID: node, Type: kind,
		Fields: map[string]map[string]any{v.Lang: fields},
	}); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("library.content_added"))
	render.Redirect(c, "/library?parent="+strconv.FormatInt(node, 10))
}

func (w *Web) deleteContent(c *gin.Context) {
	v := view.From(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, apperr.NotFound("content"))
		return
	}
	node := int64(0)
	if item, err := w.content.Get(c.Request.Context(), perm.SubjectOf(v.User), id, v.Lang); err == nil {
		node = item.NodeID
	}
	if err := w.content.Delete(c.Request.Context(), perm.SubjectOf(v.User), id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("library.content_deleted"))
	render.Redirect(c, "/library?parent="+strconv.FormatInt(node, 10))
}

// create adds a branch under what the player is looking at. Under an official node the
// service records it as an extension; the official row is never written.
func (w *Web) create(c *gin.Context) {
	v := view.From(c)
	viewer := ViewerOf(v.User, v.Lang)
	parent, _ := strconv.ParseInt(c.PostForm("parent"), 10, 64)

	name := strings.TrimSpace(c.PostForm("name_" + v.Lang))
	in := Input{
		ParentID: parent,
		// A player names their branch; the slug is derived, because a slug is a
		// developer's idea of a name and has to be unique among its siblings.
		Slug:         slugOf(name),
		Type:         "node",
		Icon:         "book",
		Names:        map[string]string{v.Lang: name},
		Descriptions: map[string]string{v.Lang: strings.TrimSpace(c.PostForm("desc_" + v.Lang))},
	}
	// The default language carries the name too when the player writes in another, so
	// the node still has something to show a reader who has not got their language.
	if v.Lang != i18n.Fallback {
		in.Names[i18n.Fallback] = name
	}
	node, err := w.svc.Create(c.Request.Context(), viewer, in)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("library.created"))
	render.Redirect(c, "/library?parent="+strconv.FormatInt(node.ID, 10))
}

// extend is the "make this mine to build on" action from an official node's screen.
func (w *Web) extend(c *gin.Context) {
	v := view.From(c)
	viewer := ViewerOf(v.User, v.Lang)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, apperr.NotFound("node"))
		return
	}
	official, err := w.svc.Get(c.Request.Context(), viewer, id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = v.Tr("library.my_notes_on", "name", official.Name)
	}
	node, err := w.svc.Create(c.Request.Context(), viewer, Input{
		ParentID: id, Slug: slugOf(name), Type: "node", Icon: "book",
		Names: map[string]string{v.Lang: name, i18n.Fallback: name},
	})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("library.extended"))
	render.Redirect(c, "/library?parent="+strconv.FormatInt(node.ID, 10))
}

func (w *Web) archive(c *gin.Context) {
	v := view.From(c)
	viewer := ViewerOf(v.User, v.Lang)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, apperr.NotFound("node"))
		return
	}
	parent := int64(0)
	if node, err := w.svc.Get(c.Request.Context(), viewer, id); err == nil {
		parent = node.ParentID
	}
	if err := w.svc.Archive(c.Request.Context(), viewer, id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("library.archived"))
	render.Redirect(c, "/library?parent="+strconv.FormatInt(parent, 10))
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok {
		// Not-found covers both "no such node" and "not yours": the service already
		// decided which, and the screen must not say more than it did.
		if e.Status == http.StatusNotFound || e.Status == http.StatusForbidden {
			render.Status(c, http.StatusNotFound, pages.NotFound(v))
			return
		}
		render.Status(c, e.Status, pages.NotFound(v))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

// slugOf turns a name into something unique among its siblings and safe in a URL.
// A player should never be asked to invent one.
func slugOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		// A name with no Latin letters — Arabic, for instance — still needs a slug,
		// and the name itself is already carried in the translations.
		out = "node"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// Sharing a branch (§7, PERM-2). The service and the schema have been able to do this
// since migration 22; this is the screen that lets a player reach it.

// shareLevels are the four levels in the order they widen. "play" is the default the
// form offers: handing somebody a branch to learn from is the common case, and
// letting them edit it is the deliberate one.
var shareLevels = []string{"view", "play", "edit", "manage"}

func (w *Web) share(c *gin.Context) {
	v := view.From(c)
	viewer := ViewerOf(v.User, v.Lang)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, apperr.NotFound("node"))
		return
	}
	node, err := w.svc.Get(c.Request.Context(), viewer, id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	res, err := w.svc.Resource(c.Request.Context(), id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	who := perm.SubjectOf(v.User)

	// Reading who it is shared with is itself the permission check: only somebody who
	// may manage access sees the list.
	shares, err := w.perm.SharedWith(c.Request.Context(), who, res)
	if err != nil {
		w.fail(c, v, err)
		return
	}

	d := pages.ShareData{
		NodeID: id, NodeName: node.Name, Levels: shareLevels,
		Query: strings.TrimSpace(c.Query("q")),
	}
	for _, sh := range shares {
		d.Shares = append(d.Shares, pages.LibraryShare{
			UserID: sh.UserID.String(), Name: sh.Name, Level: sh.Level,
			Initials: avatar.Initials(sh.Name), Color: avatar.Color(sh.Name),
		})
	}
	if d.Query != "" {
		people, err := w.people.Search(c.Request.Context(), v.User, d.Query, 10)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		already := map[string]bool{}
		for _, sh := range d.Shares {
			already[sh.UserID] = true
		}
		for _, p := range people {
			// Somebody who already has access is not a search result; they are in the
			// list below, with their level and a way to take it back.
			if already[p.ID.String()] || (v.User != nil && p.ID == v.User.ID) {
				continue
			}
			d.Found = append(d.Found, pages.LibraryPerson{
				UserID: p.ID.String(), Name: p.Name, Handle: p.Username,
				Initials: avatar.Initials(p.Name), Color: avatar.Color(p.Name),
			})
		}
	}
	render.Page(c, pages.Share(v, d))
}

func (w *Web) grantShare(c *gin.Context) {
	v := view.From(c)
	id, res, ok := w.shareTarget(c, v)
	if !ok {
		return
	}
	to, err := uuid.Parse(c.PostForm("user"))
	if err != nil {
		w.fail(c, v, apperr.Invalid(map[string]string{"user": "who?"}))
		return
	}
	if err := w.perm.Grant(c.Request.Context(), perm.SubjectOf(v.User), res, to, c.PostForm("level")); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("share.invited"))
	render.Redirect(c, fmt.Sprintf("/library/%d/share", id))
}

func (w *Web) revokeShare(c *gin.Context) {
	v := view.From(c)
	id, res, ok := w.shareTarget(c, v)
	if !ok {
		return
	}
	from, err := uuid.Parse(c.PostForm("user"))
	if err != nil {
		w.fail(c, v, apperr.Invalid(map[string]string{"user": "who?"}))
		return
	}
	if err := w.perm.Revoke(c.Request.Context(), perm.SubjectOf(v.User), res, from); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("share.revoked"))
	render.Redirect(c, fmt.Sprintf("/library/%d/share", id))
}

// shareTarget reads the branch a sharing write is about. The permission check itself
// happens in Grant and Revoke, which is the one place that decides it.
func (w *Web) shareTarget(c *gin.Context, v *view.Ctx) (int64, perm.Resource, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, apperr.NotFound("node"))
		return 0, perm.Resource{}, false
	}
	res, err := w.svc.Resource(c.Request.Context(), id)
	if err != nil {
		w.fail(c, v, err)
		return 0, perm.Resource{}, false
	}
	return id, res, true
}

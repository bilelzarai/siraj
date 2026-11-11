package admin

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/modules/nodes"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	adminpages "siraj/web/templates/admin"
)

// The hierarchy screen (checklist HIER-3): the one place a node below the old second
// level can be created. It reads and writes through the nodes service, never the v1
// domain and category tables, so it is already on the architecture the rest of the
// console is migrating towards.

// TreePerPage is how many children one page of the tree shows. Opening a branch
// costs the same whether it has three children or thirty thousand.
const TreePerPage = 50

func (w *Web) tree(c *gin.Context) {
	v := view.From(c)
	viewer := nodes.Viewer{ID: v.User.ID, Staff: true, Lang: v.Lang}
	parent, _ := strconv.ParseInt(c.Query("parent"), 10, 64)
	page, per := apiutil.PageParams(c, TreePerPage, 200)
	query := strings.TrimSpace(c.Query("q"))

	d := adminpages.TreeData{
		Parent: parent, Page: page, Query: query, Languages: i18n.Langs,
	}

	// A search looks through the branch in view; with no branch it looks everywhere.
	if query != "" {
		found, err := w.nodes.Search(c.Request.Context(), viewer, query, parent, per)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, r := range found {
			d.Nodes = append(d.Nodes, adminpages.TreeNode{
				ID: r.ID, Name: r.Name, Slug: r.Trail, Depth: r.Depth,
				Icon: r.Icon, Color: r.Color, Official: true, Status: "published",
			})
		}
		d.Total, d.Pages, d.From, d.To = len(d.Nodes), 1, 1, len(d.Nodes)
	} else {
		rows, total, err := w.nodes.Children(c.Request.Context(), viewer, parent, page, per)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, n := range rows {
			d.Nodes = append(d.Nodes, adminpages.TreeNode{
				ID: n.ID, Name: n.Name, Slug: n.Slug, Type: n.Type,
				Icon: n.Icon, Color: n.Color, Status: n.Status, Visible: n.Visible,
				Depth: n.Depth, Children: n.Children, Questions: n.Questions,
				Official: n.Official(), Extension: n.ExtendsID > 0,
			})
		}
		d.Total, d.Pages = total, pageCount(total, per)
		d.From, d.To = (page-1)*per+1, (page-1)*per+len(rows)
		if total == 0 {
			d.From = 0
		}
	}

	if parent > 0 {
		trail, err := w.nodes.Trail(c.Request.Context(), viewer, parent)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, cr := range trail {
			d.Trail = append(d.Trail, adminpages.TreeCrumb{ID: cr.ID, Name: cr.Name, Official: cr.Official})
		}
		if stats, err := w.nodes.Stats(c.Request.Context(), parent); err == nil {
			d.Stats = adminpages.TreeStats{
				Descendants:      stats.Descendants,
				DirectQuestions:  stats.DirectQuestions,
				SubtreeQuestions: stats.SubtreeQuestions,
			}
		}
	}
	render.Page(c, adminpages.Tree(v, w.shell(c, v.Tr("admin.hierarchy"), "tree"), d))
}

// createNode adds a child under whatever the screen is showing. Staff create official
// nodes, so no owner is recorded (§5).
func (w *Web) createNode(c *gin.Context) {
	v := view.From(c)
	viewer := nodes.Viewer{ID: v.User.ID, Staff: true, Lang: v.Lang}
	parent, _ := strconv.ParseInt(c.PostForm("parent"), 10, 64)

	in := nodes.Input{
		ParentID: parent,
		Slug:     strings.TrimSpace(c.PostForm("slug")),
		Type:     first(strings.TrimSpace(c.PostForm("node_type")), "node"),
		Icon:     strings.TrimSpace(c.PostForm("icon")),
		Color:    strings.TrimSpace(c.PostForm("color")),
		Names:    map[string]string{},
	}
	for _, lang := range i18n.Langs {
		in.Names[lang] = strings.TrimSpace(c.PostForm("name_" + lang))
	}
	node, err := w.nodes.Create(c.Request.Context(), viewer, in)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Write(c.Request.Context(), w.actor(c), "node.create", "node",
		strconv.FormatInt(node.ID, 10), map[string]any{"slug": node.Slug, "parent": parent}); err != nil {
		_ = c.Error(err)
	}
	render.Toast(c, v.Tr("admin.saved"))
	render.Redirect(c, "/admin/tree?parent="+strconv.FormatInt(parent, 10))
}

// setNodeStatus publishes or withdraws a branch.
func (w *Web) setNodeStatus(c *gin.Context) {
	v := view.From(c)
	viewer := nodes.Viewer{ID: v.User.ID, Staff: true, Lang: v.Lang}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	status := c.PostForm("status")
	switch status {
	case "draft", "review", "published", "archived":
	default:
		w.fail(c, v, apperr.Invalid(map[string]string{"status": "draft, review, published or archived"}))
		return
	}
	node, err := w.nodes.Update(c.Request.Context(), viewer, id, nodes.Input{Status: status})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Write(c.Request.Context(), w.actor(c), "node.status", "node",
		strconv.FormatInt(id, 10), map[string]any{"status": status}); err != nil {
		_ = c.Error(err)
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.back(c, "/admin/tree?parent="+strconv.FormatInt(node.ParentID, 10))
}

// archiveNode takes a branch and everything under it out of play.
func (w *Web) archiveNode(c *gin.Context) {
	v := view.From(c)
	viewer := nodes.Viewer{ID: v.User.ID, Staff: true, Lang: v.Lang}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	// Read the parent before archiving, so the redirect lands where the reader was.
	parent := int64(0)
	if node, err := w.nodes.Get(c.Request.Context(), viewer, id); err == nil {
		parent = node.ParentID
	}
	if err := w.nodes.Archive(c.Request.Context(), viewer, id); err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Write(c.Request.Context(), w.actor(c), "node.archive", "node",
		strconv.FormatInt(id, 10), nil); err != nil {
		_ = c.Error(err)
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.back(c, "/admin/tree?parent="+strconv.FormatInt(parent, 10))
}

// moveNode re-parents a branch. The database refuses a loop and carries the subtree.
func (w *Web) moveNode(c *gin.Context) {
	v := view.From(c)
	viewer := nodes.Viewer{ID: v.User.ID, Staff: true, Lang: v.Lang}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	parent, _ := strconv.ParseInt(c.PostForm("parent"), 10, 64)
	if err := w.nodes.Move(c.Request.Context(), viewer, id, parent); err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Write(c.Request.Context(), w.actor(c), "node.move", "node",
		strconv.FormatInt(id, 10), map[string]any{"parent": parent}); err != nil {
		_ = c.Error(err)
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.back(c, "/admin/tree?parent="+strconv.FormatInt(parent, 10))
}

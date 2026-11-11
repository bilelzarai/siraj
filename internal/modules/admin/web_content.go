package admin

import (
	"time"

	"fmt"
	"github.com/google/uuid"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/modules/support"
	"siraj/internal/view"
	adminpages "siraj/web/templates/admin"
)

// ---------------------------------------------------------------- A03 support

func (w *Web) supportInbox(c *gin.Context) {
	v := view.From(c)
	page, per := apiutil.PageParams(c, 25, 100)
	f := support.InboxFilter{
		Status: c.Query("status"), Topic: c.Query("topic"),
		Priority: c.Query("priority"), Assignee: c.Query("assignee"),
		Q: c.Query("q"), Me: v.User.ID, Page: page, PerPage: per,
	}
	rows, total, err := w.support.Inbox(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := adminpages.InboxData{
		Status: f.Status, Topic: f.Topic, Priority: f.Priority, Assignee: f.Assignee,
		Query: f.Q, Total: total,
		Page: page, Pages: pageCount(total, per), Query2: c.Request.URL.RawQuery,
	}
	if k, err := w.support.KPIs(c.Request.Context(), v.User.ID); err == nil {
		d.Open, d.Awaiting, d.HighPri, d.AssignedMe = k.Open, k.Waiting, k.High, k.Mine
	}
	for _, t := range w.support.TopicCounts(c.Request.Context()) {
		d.Topics = append(d.Topics, adminpages.TopicChip{
			Key: t.Topic, Label: v.Tr("support.topic." + t.Topic), Count: t.Count,
			On: f.Topic == t.Topic,
		})
	}
	open := c.Query("t")
	if open == "" && len(rows) > 0 {
		open = rows[0].ID.String()
	}
	for _, r := range rows {
		row := adminpages.InboxTicket{
			ID: r.ID.String(), Number: r.Number, Subject: r.Subject,
			Topic: v.Tr("support.topic." + string(r.Topic)), TopicKey: string(r.Topic),
			Status: string(r.Status), Priority: r.Priority,
			Mine:   r.AssigneeID.Valid && r.AssigneeID.UUID == v.User.ID,
			Active: r.ID.String() == open,
		}
		if r.UserName.Valid {
			row.Who = r.UserName.String
		} else if r.Email.Valid {
			row.Who = r.Email.String
		}
		if r.CreatedAt.Valid {
			row.When = r.CreatedAt.Time.Local().Format("2 Jan 15:04")
		}
		d.Tickets = append(d.Tickets, row)
	}
	if open != "" {
		if id, err := uuid.Parse(open); err == nil {
			if thread, ok := w.threadData(c, v, id); ok {
				d.Thread = &thread
			}
		}
	}
	render.Page(c, adminpages.SupportInbox(v, w.shell(c, v.Tr("admin.support"), "support"), d))
}

// threadData is the right-hand pane of A03 and the whole of A03b.
func (w *Web) threadData(c *gin.Context, v *view.Ctx, id uuid.UUID) (adminpages.ThreadData, bool) {
	t, messages, err := w.support.Thread(c.Request.Context(), v.User, id, true)
	if err != nil {
		return adminpages.ThreadData{}, false
	}
	d := adminpages.ThreadData{
		ID: t.ID.String(), Number: t.Number, Subject: t.Subject,
		Topic: v.Tr("support.topic." + t.Topic), Status: t.Status,
		Priority: t.Priority, Who: t.Who, AssignedToMe: t.AssignedToMe,
	}
	for _, m := range messages {
		d.Messages = append(d.Messages, adminpages.ThreadMessage{
			Author: m.Author, Body: m.Body, When: whenLabel(m.Created),
			IsStaff: m.IsStaff, IsNote: m.IsNote,
		})
	}
	return d, true
}

func (w *Web) supportThread(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	t, messages, err := w.support.Thread(c.Request.Context(), v.User, id, true)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := adminpages.ThreadData{
		ID: t.ID.String(), Number: t.Number, Subject: t.Subject,
		Topic: v.Tr("support.topic." + t.Topic), Status: t.Status,
	}
	for _, m := range messages {
		when := m.Created
		if len(when) >= 16 {
			when = strings.Replace(when[:16], "T", " ", 1)
		}
		d.Messages = append(d.Messages, adminpages.ThreadMessage{
			Author: m.Author, Body: m.Body, When: when, IsStaff: m.IsStaff, IsNote: m.IsNote,
		})
	}
	render.Page(c, adminpages.SupportThread(v, w.shell(c, t.Number, "support"), d))
}

func (w *Web) supportReply(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	note := c.PostForm("note") != ""
	if err := w.support.Reply(c.Request.Context(), v.User, id, c.PostForm("body"), true, note); err != nil {
		w.fail(c, v, err)
		return
	}
	if c.PostForm("resolve") != "" {
		if err := w.support.SetStatus(c.Request.Context(), id, "resolved"); err != nil {
			w.fail(c, v, err)
			return
		}
	}
	_ = w.svc.Write(c.Request.Context(), w.actor(c), "ticket.reply", "ticket", id.String(), nil)
	render.Redirect(c, "/admin/support?t="+id.String())
}

func (w *Web) supportStatus(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	ctx := c.Request.Context()
	if status := c.PostForm("status"); status != "" {
		if err := w.support.SetStatus(ctx, id, status); err != nil {
			w.fail(c, v, err)
			return
		}
		_ = w.svc.Write(ctx, w.actor(c), "ticket.status", "ticket", id.String(), nil)
	}
	if priority := c.PostForm("priority"); priority != "" {
		if err := w.support.SetPriority(ctx, id, priority); err != nil {
			w.fail(c, v, err)
			return
		}
	}
	if _, ok := c.GetPostForm("assignee"); ok {
		assignee := uuid.NullUUID{}
		if c.PostForm("assignee") == "me" {
			assignee = uuid.NullUUID{UUID: v.User.ID, Valid: true}
		}
		if err := w.support.Assign(ctx, id, assignee); err != nil {
			w.fail(c, v, err)
			return
		}
	}
	render.Redirect(c, "/admin/support?t="+id.String())
}

// ---------------------------------------------------------------- A04 domains

func (w *Web) domains(c *gin.Context) {
	v := view.From(c)
	page, per := apiutil.PageParams(c, 25, 100)
	f := DomainFilter{
		Lang: v.Lang, Family: c.Query("family"), Q: c.Query("q"),
		Sort: c.Query("sort"), Asc: c.Query("dir") != "desc",
		Page: page, PerPage: per,
	}
	rows, total, err := w.svc.Domains(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	counts, err := w.svc.DomainCounts(c.Request.Context())
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := adminpages.TableData{
		Title: v.Tr("admin.domains"), Sub: v.Tr("admin.domains_sub"),
		BasePath: "/admin/domains", Query: c.Request.URL.RawQuery,
		NewLabel: v.Tr("admin.new_domain"), NewHref: "/admin/domains/new", Drawer: true,
		ExportPath: "/admin/domains/export.csv", TemplatePath: "/admin/domains/template.csv",
		ImportPath: "/admin/domains/import",
		SearchName: "q", Search: f.Q, SearchLabel: v.Tr("admin.search_domains"),
		Sort: f.sort(), Asc: f.Asc,
		SegmentKey: "family", Segment: f.Family,
		Segments: []adminpages.Segment{
			{Value: "", Label: v.Tr("admin.all"), Count: counts.All},
			{Value: "islamic", Label: v.Tr("admin.family_islamic"), Count: counts.Islamic},
			{Value: "general", Label: v.Tr("admin.family_general"), Count: counts.General},
		},
		Columns: []adminpages.Column{
			{Label: v.Tr("admin.name"), Sort: "name"},
			{Label: v.Tr("admin.family")},
			{Label: v.Tr("admin.categories"), Sort: "categories", Align: "num"},
			{Label: v.Tr("admin.questions"), Sort: "questions", Align: "num"},
			{Label: v.Tr("admin.players"), Sort: "players", Align: "num"},
			{Label: v.Tr("admin.status")},
		},
		Empty: v.Tr("admin.no_domains"),
		Page:  page, Pages: pageCount(total, per),
	}
	setRange(&d, total, per)
	for _, r := range rows {
		status := v.Tr("admin.hidden")
		cls := "badge-outline"
		if r.IsPublished {
			status, cls = v.Tr("admin.live"), "badge-live"
		}
		name := r.Name
		if r.IsFeatured {
			name += " ★"
		}
		d.Rows = append(d.Rows, adminpages.TableRow{
			ID: fmt.Sprint(r.ID), State: string(r.Family),
			Cells: []adminpages.Cell{
				{Text: name, Sub: r.Slug, Icon: r.Icon, Color: r.Color,
					Href: "/admin/domains/" + fmt.Sprint(r.ID), Drawer: true},
				{Badge: v.Tr("admin.family_" + string(r.Family)), BadgeClass: familyBadge(string(r.Family))},
				{Text: adminNumber(int(r.Categories)), Align: "num"},
				{Text: adminNumber(int(r.Questions)), Align: "num"},
				{Text: adminNumber(int(r.Players)), Align: "num"},
				{Badge: status, BadgeClass: cls, Dot: true},
			},
			Actions: []adminpages.Action{
				{Label: v.Tr("admin.edit"), Icon: "edit", Href: "/admin/domains/" + fmt.Sprint(r.ID), Drawer: true},
			},
			Menu: []adminpages.Action{
				{Label: v.Tr("admin.open_categories"), Icon: "layers",
					Href: "/admin/categories?domain=" + fmt.Sprint(r.ID)},
				{Label: v.Tr("admin.open_questions"), Icon: "question",
					Href: "/admin/questions?domain=" + fmt.Sprint(r.ID)},
				{Label: publishLabel(v, r.IsPublished), Icon: "eye",
					Href: "/admin/domains/" + fmt.Sprint(r.ID), Drawer: true},
			},
		})
	}
	render.Page(c, adminpages.Table(v, w.shell(c, v.Tr("admin.domains"), "domains"), d))
}

// familyBadge keeps the Islamic family in its own colour, the way the design marks
// it everywhere else (BR-03).
func familyBadge(family string) string {
	if family == "islamic" {
		return "badge-live"
	}
	return "badge-neutral"
}

// publishLabel names what the drawer will do, not what the domain is.
func publishLabel(v *view.Ctx, published bool) string {
	if published {
		return v.Tr("admin.hide")
	}
	return v.Tr("admin.publish")
}

func (w *Web) domainForm(c *gin.Context) {
	v := view.From(c)
	d := adminpages.DomainFormData{
		Action: "/admin/domains", Icons: iconChoices, Motifs: motifChoices,
		Colors: colorChoices, Names: map[string]string{}, Descriptions: map[string]string{},
		IsPublished: true,
	}
	if raw := c.Param("id"); raw != "" && raw != "new" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		row, err := w.svc.db.Q.GetAdminDomain(c.Request.Context(), id)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		d.ID = row.ID
		d.Slug, d.Family, d.Icon, d.Color, d.Motif = row.Slug, string(row.Family), row.Icon, row.Color, row.Motif
		d.Position, d.IsFeatured, d.IsPublished = row.Position, row.IsFeatured, row.IsPublished
		if row.IsNewUntil.Valid {
			d.NewUntil = row.IsNewUntil.Time.Format("2006-01-02")
		}
		texts, err := w.svc.db.Q.DomainTexts(c.Request.Context(), id)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, t := range texts {
			d.Names[t.Lang] = t.Name
			d.Descriptions[t.Lang] = t.Description
		}
	}
	if render.IsHTMX(c) {
		render.Page(c, adminpages.DomainDrawer(v, d))
		return
	}
	render.Page(c, adminpages.DomainForm(v, w.shell(c, v.Tr("admin.domains"), "domains"), d))
}

func (w *Web) saveDomain(c *gin.Context) {
	v := view.From(c)
	id, _ := strconv.ParseInt(c.PostForm("id"), 10, 64)
	pos, _ := strconv.Atoi(c.PostForm("position"))
	in := DomainInput{
		ID: id, Slug: c.PostForm("slug"), Family: c.PostForm("family"),
		Icon: c.PostForm("icon"), Color: c.PostForm("color"), Motif: c.PostForm("motif"),
		Position: int32(pos), IsFeatured: c.PostForm("is_featured") != "",
		IsPublished: c.PostForm("is_published") != "", NewUntil: c.PostForm("is_new_until"),
		Names: map[string]string{}, Descriptions: map[string]string{},
	}
	for _, lang := range i18n.Langs {
		in.Names[lang] = c.PostForm("name_" + lang)
		in.Descriptions[lang] = c.PostForm("description_" + lang)
	}
	if _, err := w.svc.SaveDomain(c.Request.Context(), w.actor(c), in); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.afterSave(c, "/admin/domains")
}

// ---------------------------------------------------------------- A05 categories

func (w *Web) categories(c *gin.Context) {
	v := view.From(c)
	domainID, _ := strconv.ParseInt(c.Query("domain"), 10, 64)
	page, per := apiutil.PageParams(c, 25, 100)
	f := CategoryFilter{
		Lang: v.Lang, DomainID: domainID, Live: c.Query("live"), Q: c.Query("q"),
		Sort: c.Query("sort"), Asc: c.Query("dir") != "desc",
		Page: page, PerPage: per,
	}
	rows, total, err := w.svc.Categories(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	counts, err := w.svc.CategoryCounts(c.Request.Context(), domainID)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	domains, _, err := w.svc.Domains(c.Request.Context(), DomainFilter{Lang: v.Lang})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	options := []adminpages.Option{{Value: "0", Label: v.Tr("admin.any_domain")}}
	for _, dom := range domains {
		options = append(options, adminpages.Option{Value: fmt.Sprint(dom.ID), Label: dom.Name})
	}
	d := adminpages.TableData{
		Title: v.Tr("admin.categories"), Sub: v.Tr("admin.categories_sub"),
		BasePath: "/admin/categories", Query: c.Request.URL.RawQuery,
		NewLabel: v.Tr("admin.new_category"), NewHref: "/admin/categories/new", Drawer: true,
		ExportPath: "/admin/categories/export.csv", TemplatePath: "/admin/categories/template.csv",
		ImportPath: "/admin/categories/import",
		SearchName: "q", Search: f.Q, SearchLabel: v.Tr("admin.search_categories"),
		Sort: f.sort(), Asc: f.Asc,
		SegmentKey: "live", Segment: f.Live,
		Segments: []adminpages.Segment{
			{Value: "", Label: v.Tr("admin.all"), Count: counts.All},
			{Value: "live", Label: v.Tr("admin.live"), Count: counts.Live},
			{Value: "retired", Label: v.Tr("admin.retired"), Count: counts.Retired},
		},
		Filters: []adminpages.Filter{{Name: "domain", Label: v.Tr("admin.domain"), Value: c.Query("domain"), Options: options}},
		Columns: []adminpages.Column{
			{Label: v.Tr("admin.category"), Sort: "name"},
			{Label: v.Tr("admin.domain")},
			{Label: v.Tr("admin.questions"), Sort: "questions", Align: "num"},
			{Label: v.Tr("admin.order"), Sort: "order", Align: "num"},
			{Label: v.Tr("admin.status")},
		},
		Empty: v.Tr("admin.no_categories"),
		Page:  page, Pages: pageCount(total, per),
	}
	setRange(&d, total, per)
	for _, r := range rows {
		status, cls := v.Tr("admin.retired"), "badge-retired"
		state := "retired"
		if r.IsPublished {
			status, cls, state = v.Tr("admin.live"), "badge-live", "live"
		}
		d.Rows = append(d.Rows, adminpages.TableRow{
			ID: fmt.Sprint(r.ID), State: state,
			Cells: []adminpages.Cell{
				{Text: r.Name, Sub: r.Slug, Icon: r.Icon, Color: r.Color,
					Href: "/admin/categories/" + fmt.Sprint(r.ID), Drawer: true},
				{Text: r.DomainName, Color: r.DomainColor, Dot: true},
				{Text: fmt.Sprintf("%d / %d", r.QuestionCount, r.QuestionsTotal), Align: "num"},
				{Text: fmt.Sprint(r.Position), Align: "num"},
				{Badge: status, BadgeClass: cls, Dot: true},
			},
			Actions: []adminpages.Action{
				{Label: v.Tr("admin.edit"), Icon: "edit", Href: "/admin/categories/" + fmt.Sprint(r.ID), Drawer: true},
			},
			Menu: []adminpages.Action{
				{Label: v.Tr("admin.open_questions"), Icon: "question",
					Href: "/admin/questions?category=" + fmt.Sprint(r.ID)},
				{Label: retireLabel(v, r.IsPublished), Icon: "archive", Danger: r.IsPublished,
					Href: "/admin/categories/" + fmt.Sprint(r.ID), Drawer: true,
					Confirm: retireConfirm(v, r.IsPublished, r.Name)},
			},
		})
	}
	render.Page(c, adminpages.Table(v, w.shell(c, v.Tr("admin.categories"), "categories"), d))
}

// retireLabel and retireConfirm are A05's "Retire" and the modal behind it: a
// retired category keeps its questions and its history, it only stops being
// offered — which is what the confirmation has to say.
func retireLabel(v *view.Ctx, published bool) string {
	if published {
		return v.Tr("admin.retire")
	}
	return v.Tr("admin.bring_back")
}

func retireConfirm(v *view.Ctx, published bool, name string) string {
	if !published {
		return ""
	}
	return v.Tr("admin.retire_confirm", "name", name)
}

func (w *Web) categoryForm(c *gin.Context) {
	v := view.From(c)
	domains, _, err := w.svc.Domains(c.Request.Context(), DomainFilter{Lang: v.Lang})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := adminpages.CategoryFormData{
		Action: "/admin/categories", Icons: iconChoices, Colors: colorChoices,
		Names: map[string]string{}, IsPublished: true,
	}
	for _, dom := range domains {
		d.Domains = append(d.Domains, adminpages.Option{Value: fmt.Sprint(dom.ID), Label: dom.Name})
	}
	if raw := c.Param("id"); raw != "" && raw != "new" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		row, err := w.svc.db.Q.GetAdminCategory(c.Request.Context(), id)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		d.ID, d.DomainID, d.Slug = row.ID, row.DomainID, row.Slug
		d.Icon, d.Color, d.Position, d.IsPublished = row.Icon, row.Color, row.Position, row.IsPublished
		texts, err := w.svc.db.Q.CategoryTexts(c.Request.Context(), id)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, t := range texts {
			d.Names[t.Lang] = t.Name
		}
	}
	if render.IsHTMX(c) {
		render.Page(c, adminpages.CategoryDrawer(v, d))
		return
	}
	render.Page(c, adminpages.CategoryForm(v, w.shell(c, v.Tr("admin.categories"), "categories"), d))
}

func (w *Web) saveCategory(c *gin.Context) {
	v := view.From(c)
	id, _ := strconv.ParseInt(c.PostForm("id"), 10, 64)
	domainID, _ := strconv.ParseInt(c.PostForm("domain_id"), 10, 64)
	pos, _ := strconv.Atoi(c.PostForm("position"))
	in := CategoryInput{
		ID: id, DomainID: domainID, Slug: c.PostForm("slug"), Icon: c.PostForm("icon"),
		Color: c.PostForm("color"), Position: int32(pos),
		IsPublished: c.PostForm("is_published") != "", Names: map[string]string{},
	}
	for _, lang := range i18n.Langs {
		in.Names[lang] = c.PostForm("name_" + lang)
	}
	if _, err := w.svc.SaveCategory(c.Request.Context(), w.actor(c), in); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.afterSave(c, "/admin/categories")
}

// iconChoices, motifChoices and colorChoices are the palettes of SPEC §2; an admin
// picks from them so a new domain always looks like the rest of the app.
var iconChoices = []string{"mosque", "pen", "ball", "map", "briefcase", "atom", "book", "star", "globe", "flame", "users", "bolt"}

var motifChoices = []string{"mosque", "feather", "ball", "globe", "chart", "atom", "book", "star"}

var colorChoices = []string{
	"#0b7a5c", "#a61e4d", "#c2410c", "#0b7285", "#364fc7", "#862e9c",
	"#0d9488", "#7c3aed", "#2563eb", "#db2777", "#0f766e", "#4f46e5",
	"#059669", "#b45309", "#be123c", "#2b8a3e", "#a16207", "#1864ab",
	"#c92a2a", "#5f3dc4", "#2f5f9e", "#be185d",
}

// whenLabel turns the service's ISO stamp into what a reader wants: the time today,
// the date before that.
func whenLabel(iso string) string {
	t, err := time.Parse("2006-01-02T15:04:05Z", iso)
	if err != nil {
		return iso
	}
	local := t.Local()
	if time.Since(local) < 24*time.Hour {
		return local.Format("15:04")
	}
	return local.Format("2 Jan 15:04")
}

package admin

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/http/middleware"
	"siraj/internal/http/render"
	"siraj/internal/modules/nodes"
	"siraj/internal/modules/support"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	adminpages "siraj/web/templates/admin"
	"siraj/web/templates/pages"
)

// Web mounts the eleven admin screens under /admin. Every route needs a staff session
// (RequireStaff); the handlers that write check the role they need (RequireRole).
type Web struct {
	svc     *Service
	support *support.Service
	// nodes is the universal hierarchy. The console reads the tree through it, not
	// through the v1 domain and category tables.
	nodes *nodes.Service
}

func NewWeb(s *Service, sup *support.Service, tree *nodes.Service) *Web {
	return &Web{svc: s, support: sup, nodes: tree}
}

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	g := private.Group("/admin", middleware.RequireStaff())
	g.GET("", w.dashboard)
	// The hierarchy (HIER-3): the only screen that can create a node below the two
	// levels A04 and A05 still edit.
	g.GET("/tree", w.tree)
	g.POST("/tree", w.createNode)
	g.POST("/tree/:id/status", w.setNodeStatus)
	g.POST("/tree/:id/archive", w.archiveNode)
	g.POST("/tree/:id/move", w.moveNode)
	g.GET("/users", w.users)
	// A02b: one account's configuration — its role, the player's own settings and
	// the domains their app shows (over the list with htmx, as a page without).
	g.GET("/users/:id", w.userDrawer)
	g.POST("/users/:id", middleware.RequireRole("moderator", "admin"), w.saveUser)
	g.POST("/users/:id/role", middleware.RequireRole("admin"), w.setRole)
	g.POST("/users/:id/sanction", middleware.RequireRole("moderator", "admin"), w.sanction)
	g.POST("/users/:id/lift", middleware.RequireRole("moderator", "admin"), w.lift)
	g.GET("/support", w.supportInbox)
	g.GET("/support/:id", w.supportThread)
	g.POST("/support/:id/reply", w.supportReply)
	g.POST("/support/:id/status", w.supportStatus)
	g.GET("/domains", middleware.RequireRole("admin"), w.domains)
	g.GET("/domains/new", middleware.RequireRole("admin"), w.domainForm)
	g.GET("/domains/:id", middleware.RequireRole("admin"), w.domainForm)
	g.POST("/domains", middleware.RequireRole("admin"), w.saveDomain)
	g.GET("/categories", w.categories)
	g.GET("/categories/new", middleware.RequireRole("admin"), w.categoryForm)
	g.GET("/categories/:id", w.categoryForm)
	g.POST("/categories", w.saveCategory)
	g.GET("/questions", w.questions)
	g.POST("/questions/bulk", middleware.RequireRole("moderator", "admin"), w.bulkQuestions)
	g.GET("/questions/new", w.questionForm)
	g.GET("/questions/:id", w.questionForm)
	g.POST("/questions", w.saveQuestion)
	g.GET("/review", middleware.RequireRole("moderator", "admin"), w.review)
	g.POST("/review/:id", middleware.RequireRole("moderator", "admin"), w.decide)
	// The toast's Undo: a decision made with one keystroke needs one to take back.
	g.POST("/review/:id/undo", middleware.RequireRole("moderator", "admin"), w.undoReview)
	g.GET("/comments", middleware.RequireRole("moderator", "admin"), w.comments)
	g.POST("/comments/:id", middleware.RequireRole("moderator", "admin"), w.moderateComment)
	g.GET("/rated", middleware.RequireRole("moderator", "admin"), w.rated)
	g.GET("/health", middleware.RequireRole("moderator", "admin"), w.health)
	g.POST("/health/duplicates", middleware.RequireRole("moderator", "admin"), w.duplicate)
	g.POST("/health/reports/:id", middleware.RequireRole("moderator", "admin"), w.resolveReport)
	g.GET("/audit", middleware.RequireRole("admin"), w.audit)
	// Export CSV on A11: the same filters, as a file rather than a page.
	g.GET("/audit.csv", middleware.RequireRole("admin"), w.auditCSV)
	g.GET("/search", w.search)
	// The category select of the question drawer follows its domain.
	g.GET("/questions/categories", w.questionCategories)
	// CSV: every content list goes out as a file and comes back as one. The export
	// of the account list is admin-only; the content ones follow their screens.
	g.GET("/users/export.csv", middleware.RequireRole("admin"), w.exportCSVKind("users"))
	for _, kind := range []string{"domains", "categories", "questions"} {
		g.GET("/"+kind+"/export.csv", w.exportCSVKind(kind))
		g.GET("/"+kind+"/template.csv", w.templateCSVKind(kind))
		g.GET("/"+kind+"/import", middleware.RequireRole("moderator", "admin"), w.importDrawerKind(kind))
		g.POST("/"+kind+"/import", middleware.RequireRole("moderator", "admin"), w.runImportKind(kind))
	}
}

// ---------------------------------------------------------------- the shell

func (w *Web) shell(c *gin.Context, title, crumb string) adminpages.Shell {
	v := view.From(c)
	ctx := c.Request.Context()
	counts, _ := w.svc.QuestionCounts(ctx)
	tickets, _ := w.support.CountsByStatus(ctx)
	_, comments, _ := w.svc.Comments(ctx, CommentFilter{Tab: "needs", Page: 1, PerPage: 1})
	s := adminpages.Shell{
		Title: title, Crumb: crumb, Section: v.Tr(sectionKey(crumb)),
		Name: v.DisplayName, Role: v.User.Role, Initials: v.Initials(),
		Groups: []adminpages.NavGroup{
			{Label: v.Tr("admin.group.overview"), Items: []adminpages.NavItem{
				{Label: v.Tr("admin.dashboard"), Href: "/admin", Icon: "dashboard"},
			}},
			{Label: v.Tr("admin.group.people"), Items: []adminpages.NavItem{
				{Label: v.Tr("admin.users"), Href: "/admin/users", Icon: "users"},
				{Label: v.Tr("admin.support"), Href: "/admin/support", Icon: "support",
					Count: tickets["open"] + tickets["waiting"], Hot: tickets["open"] > 0},
			}},
			{Label: v.Tr("admin.group.content"), Items: []adminpages.NavItem{
				{Label: v.Tr("admin.hierarchy"), Href: "/admin/tree", Icon: "layers"},
				{Label: v.Tr("admin.domains"), Href: "/admin/domains", Icon: "globe"},
				{Label: v.Tr("admin.categories"), Href: "/admin/categories", Icon: "layers"},
				{Label: v.Tr("admin.questions"), Href: "/admin/questions", Icon: "question"},
			}},
			{Label: v.Tr("admin.group.moderation"), Items: []adminpages.NavItem{
				{Label: v.Tr("admin.review"), Href: "/admin/review", Icon: "review",
					Count: counts["review"], Hot: counts["review"] > 0},
				{Label: v.Tr("admin.comments"), Href: "/admin/comments", Icon: "comment", Count: comments},
				{Label: v.Tr("admin.rated"), Href: "/admin/rated", Icon: "thumbs-down"},
			}},
			{Label: v.Tr("admin.group.system"), Items: []adminpages.NavItem{
				{Label: v.Tr("admin.health"), Href: "/admin/health", Icon: "shield-check"},
				{Label: v.Tr("admin.audit"), Href: "/admin/audit", Icon: "history"},
			}},
		},
	}
	return s
}

func (w *Web) actor(c *gin.Context) Actor {
	return Actor{ID: view.From(c).User.ID, IP: c.ClientIP()}
}

// ---------------------------------------------------------------- A01

func (w *Web) dashboard(c *gin.Context) {
	v := view.From(c)
	days := int32(7)
	if n, err := strconv.Atoi(c.Query("days")); err == nil && (n == 14 || n == 30) {
		days = int32(n)
	}
	d, err := w.svc.Dashboard(c.Request.Context(), v.Lang, days)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := adminpages.DashboardData{
		Greeting: adminGreeting(v),
		Today:    time.Now().Format("Monday 2 January"),
		Days:     days,
		KPIs: []adminpages.KPI{
			{Label: v.Tr("admin.kpi.players_week"), Icon: "users", Value: fmt.Sprint(d.KPIs.PlayersWeek), Delta: d.PlayersDelta, DeltaNote: v.Tr("admin.vs_last_week")},
			{Label: v.Tr("admin.kpi.games_today"), Icon: "play", Value: fmt.Sprint(d.KPIs.GamesToday), Delta: d.GamesDelta, DeltaNote: v.Tr("admin.vs_yesterday")},
			{Label: v.Tr("admin.kpi.accuracy"), Icon: "target", Value: fmt.Sprintf("%d%%", d.Accuracy)},
			{Label: v.Tr("admin.kpi.review"), Icon: "review", Value: fmt.Sprint(d.KPIs.QuestionsReview), Attention: d.KPIs.QuestionsReview > 0},
		},
	}
	maxGames := 1
	for _, g := range d.GamesPerDay {
		if int(g.Games) > maxGames {
			maxGames = int(g.Games)
		}
	}
	for _, g := range d.GamesPerDay {
		data.Games = append(data.Games, adminpages.Bar{Label: g.Day.Time.Format("2 Jan"), Value: int(g.Games), Max: maxGames})
	}
	maxPlayers := 1
	for _, p := range d.PlayersByDomain {
		if int(p.Players) > maxPlayers {
			maxPlayers = int(p.Players)
		}
	}
	for _, p := range d.PlayersByDomain {
		data.ByDomain = append(data.ByDomain, adminpages.Bar{Label: p.Name, Value: int(p.Players), Max: maxPlayers, Color: p.Color})
	}
	// "Players by language" is a share of everyone, the way the design reads it.
	totalLang := 0
	for _, l := range d.ByLanguage {
		totalLang += int(l.Players)
	}
	for _, l := range d.ByLanguage {
		share := 0
		if totalLang > 0 {
			share = int(l.Players) * 100 / totalLang
		}
		data.ByLanguage = append(data.ByLanguage, adminpages.Bar{Label: v.Tr("lang." + l.Lang), Value: share, Max: 100})
	}
	for _, cat := range d.Categories {
		data.Categories = append(data.Categories, adminpages.CatRow{
			Name: cat.Name, Icon: cat.Icon, Color: cat.Color,
			Plays: cat.Answered, Accuracy: cat.Accuracy,
		})
	}
	data.IslamicOnlyPercent = d.IslamicOnlyPercent

	// "Needs attention" is one row per thing a moderator can act on, newest rule first.
	if d.KPIs.QuestionsReview > 0 {
		data.Attention = append(data.Attention, adminpages.Row{
			Icon: "review", Tone: "c-lamp", Text: v.Tr("admin.attention.review", "count", d.KPIs.QuestionsReview),
			Action: v.Tr("admin.review_action"), Href: "/admin/review",
		})
	}
	if d.KPIs.TicketsOpen > 0 {
		data.Attention = append(data.Attention, adminpages.Row{
			Icon: "support", Tone: "c-info", Text: v.Tr("admin.attention.tickets", "count", d.KPIs.TicketsOpen),
			Action: v.Tr("admin.open_inbox"), Href: "/admin/support",
		})
	}
	if d.KPIs.ReportsOpen > 0 {
		data.Attention = append(data.Attention, adminpages.Row{
			Icon: "flag", Tone: "c-danger", Text: v.Tr("admin.attention.reports", "count", d.KPIs.ReportsOpen),
			Action: v.Tr("admin.inspect"), Href: "/admin/rated",
		})
	}
	if d.KPIs.CommentReportsOpen > 0 {
		data.Attention = append(data.Attention, adminpages.Row{
			Icon: "comment", Tone: "c-danger", Text: v.Tr("admin.attention.comments", "count", d.KPIs.CommentReportsOpen),
			Action: v.Tr("admin.inspect"), Href: "/admin/comments",
		})
	}
	if d.KPIs.DuplicatesOpen > 0 {
		data.Attention = append(data.Attention, adminpages.Row{
			Icon: "copy", Tone: "c-lamp", Text: v.Tr("admin.attention.duplicates", "count", d.KPIs.DuplicatesOpen),
			Action: v.Tr("admin.compare"), Href: "/admin/health#duplicates",
		})
	}
	for _, r := range d.Recent {
		who := v.Tr("admin.system")
		if r.ActorName.Valid {
			who = r.ActorName.String
		}
		when := ""
		if r.CreatedAt.Valid {
			when = r.CreatedAt.Time.Local().Format("2 Jan 15:04")
		}
		data.Recent = append(data.Recent, adminpages.Row{
			Icon: auditIcon(r.Action), Tone: auditTone(r.Action),
			Text: who + " · " + r.Action, Meta: when,
		})
	}
	render.Page(c, adminpages.Dashboard(v, w.shell(c, v.Tr("admin.dashboard"), "dashboard"), data))
}

// ---------------------------------------------------------------- A02 users

func (w *Web) users(c *gin.Context) {
	v := view.From(c)
	page, per := apiutil.PageParams(c, 25, 100)
	f := UserFilter{
		Kind: c.Query("kind"), Role: c.Query("role"), Lang: c.Query("lang"),
		Status: c.Query("status"), Q: c.Query("q"),
		Sort: c.Query("sort"), Asc: c.Query("dir") != "desc",
		Page: page, PerPage: per,
	}
	rows, total, err := w.svc.Users(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	counts, err := w.svc.UserCounts(c.Request.Context())
	if err != nil {
		w.fail(c, v, err)
		return
	}

	d := adminpages.TableData{
		Title: v.Tr("admin.users"), Sub: v.Tr("admin.users_sub"),
		BasePath: "/admin/users", Query: c.Request.URL.RawQuery, Drawer: true,
		ExportPath: exportIfAdmin(v, "/admin/users/export.csv"),
		NewLabel:   v.Tr("admin.invite_staff"), NewHref: inviteIfAdmin(v),
		SearchName: "q", Search: f.Q, SearchLabel: v.Tr("admin.search_users"),
		Sort: f.sort(), Asc: f.Asc,
		Columns: []adminpages.Column{
			{Label: v.Tr("admin.user"), Sort: "user"},
			{Label: v.Tr("admin.role"), Sort: "role"},
			{Label: v.Tr("admin.status")},
			{Label: v.Tr("admin.language")},
			{Label: v.Tr("admin.games"), Sort: "games", Align: "num"},
			{Label: v.Tr("admin.accuracy"), Sort: "accuracy"},
			{Label: v.Tr("admin.last_seen"), Sort: "last_seen"},
		},
		Page: page, Pages: pageCount(total, per),
		// The design puts who to look at in a segmented control, and the narrower
		// questions — status, language — in the selects beside it.
		SegmentKey: "role", Segment: f.Role,
		Segments: []adminpages.Segment{
			{Value: "", Label: v.Tr("admin.everyone"), Count: counts.Everyone},
			{Value: "player", Label: v.Tr("admin.players"), Count: counts.Players},
			{Value: "staff", Label: v.Tr("admin.staff"), Count: counts.Staff},
		},
		Filters: []adminpages.Filter{
			{Name: "status", Label: v.Tr("admin.status"), Value: f.Status, Options: []adminpages.Option{
				{Value: "", Label: v.Tr("admin.any_status")},
				{Value: "active", Label: v.Tr("admin.status_active")},
				{Value: "suspended", Label: v.Tr("admin.status_suspended")},
				{Value: "banned", Label: v.Tr("admin.status_banned")},
				{Value: "invited", Label: v.Tr("admin.status_invited")},
			}},
			{Name: "lang", Label: v.Tr("admin.language"), Value: f.Lang, Options: []adminpages.Option{
				{Value: "", Label: v.Tr("admin.any_language")}, {Value: "ar", Label: v.Tr("lang.ar")},
				{Value: "en", Label: v.Tr("lang.en")}, {Value: "fr", Label: v.Tr("lang.fr")},
			}},
		},
		Empty: v.Tr("admin.no_users"),
	}
	if v.User.HasRole("moderator") {
		d.BulkPath = "/admin/users/bulk"
		d.Bulk = []adminpages.BulkAction{
			{Label: v.Tr("admin.message"), Value: "message", Icon: "mail"},
			{Label: v.Tr("admin.suspend"), Value: "suspend", Icon: "clock"},
			{Label: v.Tr("admin.csv_export"), Value: "export", Icon: "download"},
		}
	}
	setRange(&d, total, per)
	for _, r := range rows {
		d.Rows = append(d.Rows, w.userRow(v, r))
	}
	render.Page(c, adminpages.Table(v, w.shell(c, v.Tr("admin.users"), "users"), d))
}

// userRow is one line of A02: who, what they may do, how they are, and the two ways
// in — the eye that opens the drawer and the "⋯" with the rest.
func (w *Web) userRow(v *view.Ctx, r sqlc.AdminUsersRow) adminpages.TableRow {
	id := r.ID.String()
	sub := ""
	if r.Username.Valid {
		sub = "@" + r.Username.String
	}
	accuracy := adminpages.Cell{Text: "—", Align: "num"}
	if r.Accuracy >= 0 {
		accuracy = adminpages.Cell{
			Text: fmt.Sprintf("%d%%", r.Accuracy), Meter: int(r.Accuracy),
			MeterClass: accuracyTone(int(r.Accuracy)),
		}
	}
	row := adminpages.TableRow{
		ID:    id,
		State: r.Status,
		Cells: []adminpages.Cell{
			{Text: r.DisplayName, Sub: sub, Avatar: adminInitials(r.DisplayName),
				Color: adminColor(id), Href: "/admin/users/" + id, Drawer: true},
			{Badge: v.Tr("role." + string(r.Role)), BadgeClass: roleBadge(string(r.Role))},
			{Badge: v.Tr("admin.status_" + r.Status), BadgeClass: statusBadge(r.Status), Dot: true},
			{Badge: v.Tr("lang." + r.Lang), BadgeClass: "badge-soft"},
			{Text: adminNumber(int(r.Games)), Align: "num"},
			accuracy,
			{Text: lastSeen(v, r.LastSeenAt)},
		},
		Actions: []adminpages.Action{
			{Label: v.Tr("admin.view"), Icon: "eye", Href: "/admin/users/" + id, Drawer: true},
		},
	}
	if !v.User.HasRole("moderator") {
		return row
	}
	row.Menu = []adminpages.Action{
		{Label: v.Tr("admin.change_role"), Icon: "key", Href: "/admin/users/" + id, Drawer: true},
		{Label: v.Tr("admin.send_password_reset"), Icon: "mail",
			Href: "/admin/users/" + id + "/reset", Post: true},
		{Label: v.Tr("admin.suspend_ellipsis"), Icon: "clock",
			Href: "/admin/users/" + id + "/sanction?kind=suspension", Post: true},
		{Label: v.Tr("admin.ban"), Icon: "ban", Danger: true,
			Href: "/admin/users/" + id + "/sanction?kind=ban", Post: true,
			Confirm: v.Tr("admin.ban_confirm", "name", r.DisplayName)},
	}
	return row
}

// inviteIfAdmin hides "Invite staff" from everyone who could not use it.
func inviteIfAdmin(v *view.Ctx) string {
	if v.User.HasRole("admin") {
		return "/admin/users/invite"
	}
	return ""
}

// accuracyTone colours the bar the way the design does: a weak score is not a
// failure, but it is worth seeing.
func accuracyTone(pct int) string {
	switch {
	case pct < 40:
		return "danger"
	case pct < 60:
		return "warn"
	}
	return ""
}

// lastSeen is the design's "Online now / 12 min ago / Yesterday / Never". Presence is
// the only thing it says differently from `since`: a player seen four minutes ago is
// here, while a question submitted four minutes ago is simply recent.
func lastSeen(v *view.Ctx, at pgtype.Timestamptz) string {
	if !at.Valid {
		return v.Tr("admin.never")
	}
	if time.Since(at.Time) < 5*time.Minute {
		return v.Tr("admin.online_now")
	}
	return since(v, at.Time)
}

// since is how long ago something happened, in the reader's language: the queue's
// "2 days ago" and the user table's "12 min ago" come off this one ladder.
func since(v *view.Ctx, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := time.Since(at)
	switch {
	case d < time.Hour:
		n := int(d.Minutes())
		if n < 1 {
			n = 1
		}
		return v.Tr("admin.minutes_ago", "n", n)
	case d < 24*time.Hour:
		return v.Tr("admin.hours_ago", "n", int(d.Hours()))
	case d < 48*time.Hour:
		return v.Tr("admin.yesterday")
	case d < 30*24*time.Hour:
		return v.Tr("admin.days_ago", "n", int(d.Hours()/24))
	}
	return v.Tr("admin.weeks_ago", "n", int(d.Hours()/24/7))
}

// statusBadge is the colour the design gives each of the four words.
func statusBadge(status string) string {
	switch status {
	case "banned":
		return "badge-danger"
	case "suspended":
		return "badge-warn"
	case "invited":
		return "badge-info"
	}
	return "badge-ok"
}

// adminNumber groups thousands the way the table shows them (1,240).
func adminNumber(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	out := ""
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(r)
	}
	return out
}

func (w *Web) setRole(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.SetRole(c.Request.Context(), w.actor(c), id, c.PostForm("role")); err != nil {
		w.fail(c, v, err)
		return
	}
	w.back(c, "/admin/users")
}

func (w *Web) sanction(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	kind := c.DefaultQuery("kind", c.PostForm("kind"))
	days, _ := strconv.Atoi(c.PostForm("days"))
	if err := w.svc.Sanction(c.Request.Context(), w.actor(c), id, kind, c.PostForm("reason"), days); err != nil {
		w.fail(c, v, err)
		return
	}
	w.back(c, "/admin/users")
}

func (w *Web) lift(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.LiftSanctions(c.Request.Context(), w.actor(c), id); err != nil {
		w.fail(c, v, err)
		return
	}
	w.back(c, "/admin/users")
}

// ---------------------------------------------------------------- helpers

func pageCount(total, per int) int {
	n := (total + per - 1) / per
	if n < 1 {
		return 1
	}
	return n
}

func (w *Web) back(c *gin.Context, fallback string) {
	back := c.GetHeader("Referer")
	if back == "" {
		back = fallback
	}
	render.Redirect(c, back)
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
	if e, ok := apperr.As(err); ok && e.Status < 500 {
		render.Toast(c, e.Message)
		w.back(c, "/admin")
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

var _ = uuid.Nil
var _ = sqlc.UserRolePlayer

// adminGreeting is the dashboard's "Good morning, Malek" (A01).
func adminGreeting(v *view.Ctx) string {
	key := "admin.greeting.evening"
	switch h := time.Now().Hour(); {
	case h < 12:
		key = "admin.greeting.morning"
	case h < 18:
		key = "admin.greeting.afternoon"
	}
	return v.Tr(key, "name", v.Name())
}

// auditIcon and auditTone colour the activity feed by what happened (A01, A11).
func auditIcon(action string) string {
	switch {
	case strings.Contains(action, "approve"):
		return "check"
	case strings.Contains(action, "reject"), strings.Contains(action, "ban"), strings.Contains(action, "suspend"):
		return "ban"
	case strings.Contains(action, "retire"), strings.Contains(action, "delete"):
		return "trash"
	case strings.Contains(action, "import"):
		return "upload"
	case strings.Contains(action, "update"), strings.Contains(action, "edit"):
		return "pen"
	}
	return "plus"
}

func auditTone(action string) string {
	switch {
	case strings.Contains(action, "approve"):
		return "c-brand"
	case strings.Contains(action, "reject"), strings.Contains(action, "ban"), strings.Contains(action, "suspend"), strings.Contains(action, "delete"):
		return "c-danger"
	case strings.Contains(action, "retire"):
		return "c-lamp"
	}
	return ""
}

// from and to are the "Showing 1–10 of 2,318" line of every admin table.
func from(page, per, total int) int {
	if total == 0 {
		return 0
	}
	return (page-1)*per + 1
}

func to(page, per, total int) int {
	if n := page * per; n < total {
		return n
	}
	return total
}

// roleBadge gives staff roles the console's colours (A02).
func roleBadge(role string) string {
	switch role {
	case "admin":
		return "badge-lamp"
	case "moderator", "support":
		return "badge-info"
	case "translator":
		return "badge-soft"
	}
	return ""
}

var adminPalette = []string{
	"#0b7a5c", "#a61e4d", "#c2410c", "#0b7285", "#364fc7", "#862e9c",
	"#2563eb", "#db2777", "#b45309", "#7c3aed", "#059669", "#be123c",
}

// exportIfAdmin hides the download from staff who may read the list but not take
// it away: the account file carries every player's email (AD-02).
func exportIfAdmin(v *view.Ctx, path string) string {
	if v.User != nil && string(v.User.Role) == "admin" {
		return path
	}
	return ""
}

func adminColor(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return adminPalette[int(h.Sum32())%len(adminPalette)]
}

func adminInitials(name string) string {
	out := make([]rune, 0, 2)
	space := true
	for _, r := range name {
		if r == ' ' {
			space = true
			continue
		}
		if space {
			out = append(out, r)
			space = false
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return strings.ToUpper(string(out))
}

// setRange fills the "Showing 1–10 of 2,318" line of an admin table.
func setRange(d *adminpages.TableData, total, per int) {
	d.Total = total
	d.From = from(d.Page, per, total)
	d.To = to(d.Page, per, total)
}

// sectionKey is the middle breadcrumb: which group of the sidebar a screen sits in.
func sectionKey(crumb string) string {
	switch crumb {
	case "users", "support":
		return "admin.group.people"
	case "domains", "categories", "questions":
		return "admin.group.content"
	case "review", "comments", "rated":
		return "admin.group.moderation"
	case "health", "audit":
		return "admin.group.system"
	}
	return ""
}

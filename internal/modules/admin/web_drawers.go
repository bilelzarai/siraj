package admin

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	adminpages "siraj/web/templates/admin"
)

// The console edits in drawers (A02b, A04b, A05b, A06b) and moves content as CSV.
// Every handler here answers twice: the drawer for a screen that asked with htmx,
// and the same form as a page for one that did not (NF-02).

// ---------------------------------------------------------------- categories of a domain

// questionCategories swaps the category select of the question drawer when its
// domain changes, so a question is never filed under another domain's topic.
func (w *Web) questionCategories(c *gin.Context) {
	v := view.From(c)
	domainID, _ := strconv.ParseInt(c.Query("domain_id"), 10, 64)
	rows, _, err := w.svc.Categories(c.Request.Context(), CategoryFilter{Lang: v.Lang, DomainID: domainID})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	options := make([]adminpages.Option, 0, len(rows))
	for _, cat := range rows {
		options = append(options, adminpages.Option{Value: fmt.Sprint(cat.ID), Label: cat.Name})
	}
	render.Page(c, adminpages.CategoryOptions(v, options, 0))
}

// ---------------------------------------------------------------- A02b users

func (w *Web) userDrawer(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.BadRequest("bad_id", "that is not a user id"))
		return
	}
	d, err := w.userData(c, id, "")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if render.IsHTMX(c) {
		render.Page(c, adminpages.UserDrawer(v, d))
		return
	}
	render.Page(c, adminpages.UserPage(v, w.shell(c, v.Tr("admin.users"), "users"), d))
}

// userData reads one account for the drawer: the row, the numbers over it, the
// domains it follows and what moderation has done to it.
func (w *Web) userData(c *gin.Context, id uuid.UUID, problem string) (adminpages.UserFormData, error) {
	v := view.From(c)
	detail, err := w.svc.UserDetail(c.Request.Context(), id, v.Lang)
	if err != nil {
		return adminpages.UserFormData{}, err
	}
	row := detail.Row
	d := adminpages.UserFormData{
		ID: row.ID.String(), Name: row.DisplayName, Kind: string(row.Kind),
		Role: string(row.Role), Lang: row.Lang, Theme: row.Theme,
		DailyGoal: row.DailyGoal, IslamicOnly: row.IslamicOnly,
		Level: row.Level, XP: row.Xp, Games: int(row.Games), Accuracy: detail.Accuracy,
		Streak: detail.Streak, Providers: row.Providers,
		Action:  "/admin/users/" + row.ID.String(),
		CanRole: v.User != nil && string(v.User.Role) == "admin",
		Error:   problem,
	}
	d.Initials, d.Color = adminInitials(row.DisplayName), adminColor(row.ID.String())
	if row.Username.Valid {
		d.Username = row.Username.String
	}
	if row.Email.Valid {
		d.Email = row.Email.String
	}
	if row.CreatedAt.Valid {
		d.Joined = row.CreatedAt.Time.Format("2 January 2006")
	}
	d.LastSeen = v.Tr("admin.never")
	if row.LastSeenAt.Valid {
		d.LastSeen = row.LastSeenAt.Time.Format("2 Jan 2006, 15:04")
	}
	if d.CanRole {
		d.SanctionURL = "/admin/users?q=" + d.Username
	}

	follows := map[int64]bool{}
	for _, dom := range detail.Domains {
		follows[dom.ID] = true
	}
	// Every published domain is offered; the ones they follow come first, ticked.
	for _, dom := range detail.Catalog {
		chip := adminpages.UserChip{
			ID: dom.ID, Name: dom.Name, Color: dom.Color, Icon: dom.Icon,
			Islamic: string(dom.Family) == "islamic", Selected: follows[dom.ID],
		}
		d.Chips = append(d.Chips, chip)
	}
	sortChips(d.Chips)

	for _, top := range detail.Top {
		d.Top = append(d.Top, adminpages.UserMastery{
			Name: top.Name, Icon: top.Icon, Color: top.Color,
			Percent: int(top.Mastery), Warn: top.Mastery < 60,
		})
	}
	for _, s := range detail.Sanctions {
		line := adminpages.UserSanctionRow{
			Kind: string(s.Kind), Reason: s.Reason, Lifted: s.LiftedAt.Valid,
			By: v.Tr("admin.by_staff"),
		}
		if s.ByName.Valid {
			line.By = s.ByName.String
		}
		if s.CreatedAt.Valid {
			line.When = s.CreatedAt.Time.Format("2 Jan 2006")
		}
		d.Sanctions = append(d.Sanctions, line)
	}
	return d, nil
}

// saveUser writes the drawer: the role, the player's settings and their domains, in
// one transaction with its audit row (AD-03).
func (w *Web) saveUser(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.BadRequest("bad_id", "that is not a user id"))
		return
	}
	goal, _ := strconv.Atoi(c.PostForm("daily_goal"))
	in := UserConfig{
		UserID: id, Role: c.PostForm("role"), Lang: c.PostForm("lang"),
		Theme: c.PostForm("theme"), DailyGoal: int32(goal),
		IslamicOnly: c.PostForm("islamic_only") != "",
	}
	// A role select is disabled for staff who may not change it, and a disabled
	// select posts nothing: keep what the account already has.
	if in.Role == "" {
		if current, err := w.svc.db.Q.GetUser(c.Request.Context(), id); err == nil {
			in.Role = string(current.Role)
		}
	}
	for _, raw := range c.PostFormArray("domain_id") {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			in.DomainIDs = append(in.DomainIDs, n)
		}
	}
	if err := w.svc.SaveUserConfig(c.Request.Context(), w.actor(c), in); err != nil {
		if render.IsHTMX(c) {
			d, readErr := w.userData(c, id, message(err))
			if readErr != nil {
				w.fail(c, v, readErr)
				return
			}
			render.Page(c, adminpages.UserDrawer(v, d))
			return
		}
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.afterSave(c, "/admin/users")
}

// ---------------------------------------------------------------- CSV

// exportCSV streams one of the console's lists as a file. Who may download which
// list is the same rule as who may see the screen it comes from.
func (w *Web) exportCSV(c *gin.Context) {
	v := view.From(c)
	kind := c.Param("kind")
	var buf bytes.Buffer
	var err error
	ctx := c.Request.Context()
	switch kind {
	case "domains":
		err = w.svc.ExportDomains(ctx, &buf)
	case "categories":
		domainID, _ := strconv.ParseInt(c.Query("domain"), 10, 64)
		err = w.svc.ExportCategories(ctx, &buf, domainID)
	case "questions":
		categoryID, _ := strconv.ParseInt(c.Query("category"), 10, 64)
		err = w.svc.ExportQuestions(ctx, &buf, categoryID, c.Query("status"))
	case "users":
		err = w.svc.ExportUsers(ctx, &buf)
	default:
		w.fail(c, v, apperr.NotFound("export"))
		return
	}
	if err != nil {
		w.fail(c, v, err)
		return
	}
	// The download is a staff action like any other: it leaves a trail (AD-03).
	_ = w.svc.Write(ctx, w.actor(c), "export."+kind, kind, "", gin.H{"rows": strings.Count(buf.String(), "\n") - 1})
	sendCSV(c, FileName(kind), buf.Bytes())
}

// templateCSV is the empty file with one filled line, so a first import is an edit
// rather than a guess.
func (w *Web) templateCSV(c *gin.Context) {
	kind := c.Param("kind")
	var buf bytes.Buffer
	Template(&buf, kind)
	if buf.Len() == 0 {
		w.fail(c, view.From(c), apperr.NotFound("template"))
		return
	}
	sendCSV(c, FileName(kind+"-template"), buf.Bytes())
}

// csvBOM is what makes Excel open a UTF-8 file with Arabic in it correctly.
const csvBOM = "\ufeff"

func sendCSV(c *gin.Context, name string, body []byte) {
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Cache-Control", "no-store")
	// The BOM is what makes Excel open UTF-8 Arabic correctly.
	c.Data(http.StatusOK, "text/csv; charset=utf-8", append([]byte(csvBOM), body...))
}

// importDrawer opens the upload panel.
func (w *Web) importDrawer(c *gin.Context) {
	v := view.From(c)
	kind := c.Param("kind")
	d, ok := w.importData(v, kind)
	if !ok {
		w.fail(c, v, apperr.NotFound("import"))
		return
	}
	if render.IsHTMX(c) {
		render.Page(c, adminpages.ImportDrawer(v, d))
		return
	}
	render.Page(c, adminpages.ImportPage(v, w.shell(c, v.Tr("admin."+kind), kind), d))
}

func (w *Web) importData(v *view.Ctx, kind string) (adminpages.ImportData, bool) {
	d := adminpages.ImportData{
		Kind: kind, Action: "/admin/" + kind + "/import",
		TemplatePath: "/admin/" + kind + "/template.csv",
		ExportPath:   "/admin/" + kind + "/export.csv",
		Title:        v.Tr("admin.csv_title", "what", v.Tr("admin."+kind)),
	}
	switch kind {
	case "domains":
		d.Columns = DomainColumns
	case "categories":
		d.Columns = CategoryColumns
	case "questions":
		d.Columns = QuestionColumns
	default:
		return d, false
	}
	return d, true
}

// runImport reads the file. "check" says what would happen, "write" does it; the
// same parse decides both, so the preview never disagrees with the result.
func (w *Web) runImport(c *gin.Context) {
	v := view.From(c)
	kind := c.Param("kind")
	d, ok := w.importData(v, kind)
	if !ok {
		w.fail(c, v, apperr.NotFound("import"))
		return
	}
	write := c.PostForm("mode") == "write"

	file, err := c.FormFile("file")
	if err != nil {
		d.Error = v.Tr("admin.csv_no_file")
		w.renderImport(c, v, d, kind)
		return
	}
	if file.Size > MaxImportBytes {
		d.Error = v.Tr("admin.csv_too_big", "n", MaxImportMB)
		w.renderImport(c, v, d, kind)
		return
	}
	src, err := file.Open()
	if err != nil {
		w.fail(c, v, err)
		return
	}
	defer src.Close()

	var report ImportReport
	switch kind {
	case "domains":
		report, err = w.svc.ImportDomains(c.Request.Context(), w.actor(c), src, !write)
	case "categories":
		report, err = w.svc.ImportCategories(c.Request.Context(), w.actor(c), src, !write)
	case "questions":
		report, err = w.svc.ImportQuestions(c.Request.Context(), w.actor(c), src, !write)
	}
	if err != nil {
		d.Error = message(err)
		w.renderImport(c, v, d, kind)
		return
	}
	d.Checked = true
	d.Rows, d.Created, d.Updated = report.Rows, report.Created, report.Updated
	for _, p := range report.Problems {
		d.Problems = append(d.Problems, adminpages.ImportProblem{Line: p.Line, Column: p.Column, Why: p.Why})
	}
	d.Written = write && report.OK()
	if write && !report.OK() {
		d.Error = v.Tr("admin.csv_not_written")
	}
	w.renderImport(c, v, d, kind)
}

func (w *Web) renderImport(c *gin.Context, v *view.Ctx, d adminpages.ImportData, kind string) {
	if render.IsHTMX(c) {
		render.Page(c, adminpages.ImportDrawer(v, d))
		return
	}
	render.Page(c, adminpages.ImportPage(v, w.shell(c, v.Tr("admin."+kind), kind), d))
}

// afterSave closes the drawer and puts the list back in front of the player.
func (w *Web) afterSave(c *gin.Context, list string) {
	if render.IsHTMX(c) {
		c.Header("X-Siraj-Close", "1")
	}
	render.Redirect(c, list)
}

// message is what an apperr says to a person; anything else is a plain failure.
func message(err error) string {
	if e, ok := apperr.As(err); ok {
		if len(e.Fields) > 0 {
			parts := make([]string, 0, len(e.Fields))
			for field, why := range e.Fields {
				parts = append(parts, field+": "+why)
			}
			return strings.Join(parts, " · ")
		}
		return e.Message
	}
	return err.Error()
}

func sortChips(chips []adminpages.UserChip) {
	for i := 1; i < len(chips); i++ {
		for j := i; j > 0 && rank(chips[j]) < rank(chips[j-1]); j-- {
			chips[j], chips[j-1] = chips[j-1], chips[j]
		}
	}
}

func rank(c adminpages.UserChip) int {
	if c.Selected {
		return 0
	}
	return 1
}

// The CSV handlers are mounted once per list, so each one knows its own kind
// without a path parameter fighting the ":id" routes beside it.
func (w *Web) exportCSVKind(kind string) gin.HandlerFunc {
	return func(c *gin.Context) { c.Params = append(c.Params, gin.Param{Key: "kind", Value: kind}); w.exportCSV(c) }
}

func (w *Web) templateCSVKind(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "kind", Value: kind})
		w.templateCSV(c)
	}
}

func (w *Web) importDrawerKind(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "kind", Value: kind})
		w.importDrawer(c)
	}
}

func (w *Web) runImportKind(kind string) gin.HandlerFunc {
	return func(c *gin.Context) { c.Params = append(c.Params, gin.Param{Key: "kind", Value: kind}); w.runImport(c) }
}

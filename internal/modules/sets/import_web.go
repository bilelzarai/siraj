package sets

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/view"
	"siraj/web/templates/pages"
)

// RegisterImport adds the two things My questions needs to take a spreadsheet: the
// template to fill in, and the upload itself. There is no screen in between — the
// file is checked and imported in one step, and anything wrong with it comes back on
// the screen the player was already on (MQ-05).
func (w *Web) RegisterImport(private *gin.RouterGroup) {
	private.GET("/my-questions/import/template.csv", w.importTemplate)
	private.GET("/my-questions/:id/export.csv", w.exportSet)
	private.POST("/my-questions/import", w.importUpload)
	private.POST("/my-questions/:id/import", w.importIntoSet)
}

// importTemplate hands over the file a spreadsheet can be filled in from.
func (w *Web) importTemplate(c *gin.Context) {
	c.Header("Content-Disposition", `attachment; filename="siraj-questions.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(Template()))
}

// exportSet hands back everything in one set, in the shape the importer reads: the
// template a player gets from inside a set is the set itself (MQ-05).
func (w *Web) exportSet(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	set, questions, err := w.svc.Get(c.Request.Context(), v.User, id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	_, cats := w.topics(c, v)
	names := map[int64]string{}
	for _, list := range cats {
		for _, cat := range list {
			names[cat.ID] = cat.Name
		}
	}
	c.Header("Content-Disposition", `attachment; filename="`+fileNameOf(set.Name)+`.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8",
		[]byte(Export(set, questions, func(id int64) string { return names[id] })))
}

// fileNameOf keeps a set's name usable as a file name.
func fileNameOf(name string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		case r == ' ' || r == '_':
			return '-'
		}
		return -1
	}, name)
	if clean == "" {
		return "siraj-set"
	}
	return strings.ToLower(clean)
}

// importUpload reads the file and makes a set of it. A file with anything wrong
// writes nothing and comes back as a list of lines to fix.
func (w *Web) importUpload(c *gin.Context) {
	v := view.From(c)
	report, problem := w.readUpload(c, v)
	if problem.Rows == 0 && len(problem.Problems) > 0 {
		w.listWithProblems(c, v, problem)
		return
	}
	if !report.OK() {
		w.listWithProblems(c, v, viewReport(report))
		return
	}
	set, n, err := w.svc.ImportNew(c.Request.Context(), v.User, v.Tr("sets.imported_set"), v.Lang, report)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("sets.imported", "n", n))
	render.Redirect(c, "/my-questions/"+set.ID.String())
}

// importIntoSet adds a file's questions on top of a set that already exists.
func (w *Web) importIntoSet(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	report, problem := w.readUpload(c, v)
	if problem.Rows == 0 && len(problem.Problems) > 0 {
		w.editorWithProblems(c, v, id, problem)
		return
	}
	if !report.OK() {
		w.editorWithProblems(c, v, id, viewReport(report))
		return
	}
	n, err := w.svc.ImportInto(c.Request.Context(), v.User, id, report)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("sets.imported", "n", n))
	render.Redirect(c, "/my-questions/"+id.String())
}

// listWithProblems draws My questions again with the reasons the file was refused.
func (w *Web) listWithProblems(c *gin.Context, v *view.Ctx, report pages.ImportReport) {
	data, err := w.listData(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data.Report = report
	render.Status(c, http.StatusUnprocessableEntity, pages.MyQuestions(v, data))
}

// editorWithProblems does the same on the set the file was dropped onto.
func (w *Web) editorWithProblems(c *gin.Context, v *view.Ctx, id uuid.UUID, report pages.ImportReport) {
	data, _, ok := w.loadID(c, v, id)
	if !ok {
		return
	}
	data.Report = report
	render.Status(c, http.StatusUnprocessableEntity, pages.SetEditor(v, data))
}

// readUpload takes the file off the request and checks it against the categories this
// player may write for. The second value carries the problems a file has before it is
// even read: none chosen, or too big to look at.
func (w *Web) readUpload(c *gin.Context, v *view.Ctx) (ImportReport, pages.ImportReport) {
	file, err := c.FormFile("file")
	if err != nil || file == nil {
		return ImportReport{}, pages.ImportReport{
			Problems: []pages.ImportProblem{{Why: v.Tr("sets.import_no_file")}},
		}
	}
	if file.Size > MaxImportBytes {
		return ImportReport{}, pages.ImportReport{
			Problems: []pages.ImportProblem{{Why: v.Tr("sets.import_too_big", "n", MaxImportMB)}},
		}
	}
	src, err := file.Open()
	if err != nil {
		return ImportReport{}, pages.ImportReport{
			Problems: []pages.ImportProblem{{Why: v.Tr("sets.import_no_file")}},
		}
	}
	defer src.Close()

	// The names in the file are matched against the player's own categories, so an
	// import can never reach outside what they may write for (MQ-05, DM-03).
	lookup, fallback := w.categoryLookup(c, v)
	return ReadCSV(src, v.Lang, lookup, fallback), pages.ImportReport{}
}

// categoryLookup matches a name in the file to one of the player's categories, and
// gives the one the whole file falls back to when the column is left out.
func (w *Web) categoryLookup(c *gin.Context, v *view.Ctx) (CategoryLookup, int64) {
	_, cats := w.topics(c, v)
	byName := map[string]int64{}
	var first int64
	for _, list := range cats {
		for _, cat := range list {
			byName[strings.ToLower(strings.TrimSpace(cat.Name))] = cat.ID
			if first == 0 {
				first = cat.ID
			}
		}
	}
	fallback := first
	if raw := c.PostForm("category_id"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			fallback = id
		}
	}
	return func(name string) (int64, bool) {
		id, ok := byName[strings.ToLower(strings.TrimSpace(name))]
		return id, ok
	}, fallback
}

// viewReport maps the service's report onto what the page draws.
func viewReport(r ImportReport) pages.ImportReport {
	out := pages.ImportReport{Rows: r.Rows, Ready: r.OK()}
	for _, p := range r.Problems {
		out.Problems = append(out.Problems, pages.ImportProblem{
			Line: p.Line, Column: p.Column, Why: p.Why,
		})
	}
	return out
}

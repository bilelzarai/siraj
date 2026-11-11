package sets

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/modules/catalog"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/components"
	"siraj/web/templates/pages"
)

// Web is P28 My questions and P28b the set editor, plus overlay O04: the question
// editor opens as a drawer over the screen that asked for it, and as its own page
// when there is no JavaScript (NF-02).
type Web struct {
	svc     *Service
	catalog *catalog.Service
}

func NewWeb(s *Service, cat *catalog.Service) *Web { return &Web{svc: s, catalog: cat} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/my-questions", w.list)
	private.POST("/my-questions", w.create)
	private.GET("/my-questions/:id", w.editor)
	private.POST("/my-questions/:id", w.rename)
	private.POST("/my-questions/:id/delete", w.remove)
	private.POST("/my-questions/:id/suggest", w.suggest)
	private.GET("/my-questions/:id/q/new", w.newQuestion)
	private.POST("/my-questions/:id/q/new", w.addQuestion)
	private.GET("/my-questions/:id/q/:qid", w.editQuestion)
	private.POST("/my-questions/:id/q/:qid", w.saveQuestion)
	private.POST("/my-questions/:id/q/:qid/duplicate", w.duplicate)
	private.POST("/my-questions/:id/q/:qid/delete", w.deleteQuestion)
	w.RegisterImport(private)
}

func (w *Web) list(c *gin.Context) {
	v := view.From(c)
	if v.IsGuest() {
		render.Page(c, pages.LockedForGuest(v, "my-questions"))
		return
	}
	data, err := w.listData(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Page(c, pages.MyQuestions(v, data))
}

// listData is My questions, which the importer also draws when it has to say why a
// file was refused.
func (w *Web) listData(c *gin.Context, v *view.Ctx) (pages.MyQuestionsData, error) {
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	rows, total, err := w.svc.List(c.Request.Context(), v.User, page, SetsPerPage)
	if err != nil {
		return pages.MyQuestionsData{}, err
	}
	data := pages.MyQuestionsData{Min: MinQuestions, Page: page, Total: total}
	data.Pages = (total + SetsPerPage - 1) / SetsPerPage
	if data.Pages < 1 {
		data.Pages = 1
	}
	for _, s := range rows {
		data.Sets = append(data.Sets, card(s))
	}
	return data, nil
}

// SetsPerPage is how many sets one screen of My questions holds (MQ-01).
const SetsPerPage = 12

func card(s Set) pages.SetCard {
	return pages.SetCard{
		ID: s.ID.String(), Name: s.Name, MainLang: s.MainLang,
		Questions: s.Questions, Plays: s.Plays, Ready: s.Ready,
		Complete: s.Complete, Missing: s.Missing, Updated: s.UpdatedAt[:10],
	}
}

func (w *Web) create(c *gin.Context) {
	v := view.From(c)
	set, err := w.svc.Create(c.Request.Context(), v.User, c.PostForm("name"), v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/my-questions/"+set.ID.String())
}

func (w *Web) load(c *gin.Context, v *view.Ctx) (pages.SetEditorData, uuid.UUID, bool) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return pages.SetEditorData{}, uuid.Nil, false
	}
	return w.loadID(c, v, id)
}

func (w *Web) loadID(c *gin.Context, v *view.Ctx, id uuid.UUID) (pages.SetEditorData, uuid.UUID, bool) {
	set, questions, err := w.svc.Get(c.Request.Context(), v.User, id)
	if err != nil {
		w.fail(c, v, err)
		return pages.SetEditorData{}, uuid.Nil, false
	}
	data := pages.SetEditorData{Set: card(set), Min: MinQuestions, Back: "/my-questions/" + id.String()}
	if back := c.Query("then"); strings.HasPrefix(back, "/") {
		data.Back = back
	}
	data.Domains, data.Categories = w.topics(c, v)
	data.Page, data.Pages, questions = pageOfQuestions(c, questions)
	names := map[int64]components.Category{}
	for _, list := range data.Categories {
		for _, cat := range list {
			names[cat.ID] = cat
		}
	}
	for _, q := range questions {
		row := pages.SetQuestionRow{
			ID: q.ID, Position: q.Position, Correct: q.CorrectIndex,
			Lang: q.MainLang, CategoryID: q.CategoryID, Difficulty: q.Difficulty,
		}
		if t, ok := q.Text[q.MainLang]; ok {
			row.Body, row.Answers = t.Body, t.Answers
			if int(q.CorrectIndex) < len(t.Answers) {
				row.CorrectText = t.Answers[q.CorrectIndex]
			}
		}
		if cat, ok := names[q.CategoryID]; ok {
			row.CategoryName, row.CategoryIcon, row.Color = cat.Name, cat.Icon, cat.Color
		}
		for _, lang := range i18n.Langs {
			if t, ok := q.Text[lang]; ok && t.Body != "" {
				row.Langs = append(row.Langs, strings.ToUpper(lang))
			}
		}
		data.Questions = append(data.Questions, row)
	}
	return data, id, true
}

// topics are the domains the player owns and their categories: the two selects at the
// top of the question editor stay inside Your domains (DM-03).
func (w *Web) topics(c *gin.Context, v *view.Ctx) ([]view.Domain, map[int64][]components.Category) {
	ids := make([]int64, 0, len(v.MyDomains))
	for _, d := range v.MyDomains {
		ids = append(ids, d.ID)
	}
	byDomain, err := w.catalog.CategoriesOf(c.Request.Context(), v.Lang, ids)
	if err != nil {
		_ = c.Error(err)
		return v.MyDomains, map[int64][]components.Category{}
	}
	return v.MyDomains, byDomain
}

// editorData opens one question in the editor, or a blank one at the end of the set.
func (w *Web) editorData(c *gin.Context, v *view.Ctx, data pages.SetEditorData, action string, q *Question) pages.QuestionEditorData {
	out := pages.QuestionEditorData{
		Set: data, Action: action, New: q == nil,
		Bodies: map[string]string{}, Answers: map[string][]string{}, Explain: map[string]string{},
	}
	if q == nil {
		out.Position = len(data.Questions) + 1
		out.Question = pages.SetQuestionRow{Lang: data.Set.MainLang, Difficulty: 2}
		// A new question starts on the topic of the last one: most people write a
		// run of questions about the same thing.
		if n := len(data.Questions); n > 0 {
			out.Question.CategoryID = data.Questions[n-1].CategoryID
			out.Question.Difficulty = data.Questions[n-1].Difficulty
		}
	} else {
		out.Position = int(q.Position)
		out.Question = pages.SetQuestionRow{
			ID: q.ID, Position: q.Position, Correct: q.CorrectIndex, Lang: q.MainLang,
			CategoryID: q.CategoryID, Difficulty: q.Difficulty,
		}
		for lang, t := range q.Text {
			out.Bodies[lang], out.Answers[lang], out.Explain[lang] = t.Body, t.Answers, t.Explanation
		}
	}
	out.DomainID = w.domainOf(data, out.Question.CategoryID, v)
	return out
}

// domainOf is which domain select to open on: the one holding the question's category.
func (w *Web) domainOf(data pages.SetEditorData, categoryID int64, v *view.Ctx) int64 {
	for id, list := range data.Categories {
		for _, cat := range list {
			if cat.ID == categoryID {
				return id
			}
		}
	}
	if len(v.MyDomains) > 0 {
		return v.MyDomains[0].ID
	}
	return 0
}

// editor renders O04 as a drawer over the screen that asked for it, or as its own
// page when the request did not come from htmx.
func (w *Web) renderEditor(c *gin.Context, v *view.Ctx, d pages.QuestionEditorData) {
	if render.IsHTMX(c) {
		render.Page(c, pages.SetQuestionDrawer(v, d))
		return
	}
	render.Page(c, pages.SetQuestionEditor(v, d))
}

// QuestionsPerPage is how many of a set's questions one screen holds (MQ-05).
const QuestionsPerPage = 10

// pageOfQuestions cuts a long set into screenfuls, keeping the numbering of the whole
// set so row 11 still reads as 11.
func pageOfQuestions(c *gin.Context, all []Question) (page, pages int, out []Question) {
	pages = (len(all) + QuestionsPerPage - 1) / QuestionsPerPage
	if pages < 1 {
		pages = 1
	}
	page, _ = strconv.Atoi(c.Query("qpage"))
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	from := (page - 1) * QuestionsPerPage
	to := from + QuestionsPerPage
	if from > len(all) {
		from = len(all)
	}
	if to > len(all) {
		to = len(all)
	}
	return page, pages, all[from:to]
}

// backTo is where saving returns to: the screen that opened the editor.
func backTo(c *gin.Context, fallback string) string {
	for _, raw := range []string{c.PostForm("then"), c.Query("then")} {
		if strings.HasPrefix(raw, "/") {
			return raw
		}
	}
	return fallback
}

func (w *Web) editor(c *gin.Context) {
	v := view.From(c)
	data, _, ok := w.load(c, v)
	if !ok {
		return
	}
	render.Page(c, pages.SetEditor(v, data))
}

func (w *Web) rename(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Rename(c.Request.Context(), v.User, id, c.PostForm("name"), c.PostForm("main_lang")); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("sets.saved"))
	render.Redirect(c, "/my-questions/"+id.String())
}

func (w *Web) remove(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Delete(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("sets.deleted"))
	render.Redirect(c, "/my-questions")
}

func (w *Web) suggest(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Suggest(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("sets.suggested"))
	render.Redirect(c, "/my-questions/"+id.String())
}

func (w *Web) newQuestion(c *gin.Context) {
	v := view.From(c)
	data, id, ok := w.load(c, v)
	if !ok {
		return
	}
	action := pages.WithBack("/my-questions/"+id.String()+"/q/new", data.Back)
	w.renderEditor(c, v, w.editorData(c, v, data, action, nil))
}

func (w *Web) editQuestion(c *gin.Context) {
	v := view.From(c)
	data, id, ok := w.load(c, v)
	if !ok {
		return
	}
	qid, _ := strconv.ParseInt(c.Param("qid"), 10, 64)
	_, questions, err := w.svc.Get(c.Request.Context(), v.User, id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	var found *Question
	for i := range questions {
		if questions[i].ID == qid {
			found = &questions[i]
			break
		}
	}
	if found == nil {
		render.Status(c, http.StatusNotFound, pages.NotFound(v))
		return
	}
	action := pages.WithBack("/my-questions/"+id.String()+"/q/"+c.Param("qid"), data.Back)
	w.renderEditor(c, v, w.editorData(c, v, data, action, found))
}

// readQuestion turns the editor form into a QuestionInput (three language tabs).
func readQuestion(c *gin.Context, v *view.Ctx) QuestionInput {
	in := QuestionInput{
		MainLang: c.PostForm("main_lang"),
		Text:     map[string]Text{},
	}
	if in.MainLang == "" && v != nil {
		in.MainLang = v.Lang
	}
	in.CategoryID, _ = strconv.ParseInt(c.PostForm("category_id"), 10, 64)
	d, _ := strconv.Atoi(c.PostForm("difficulty"))
	in.Difficulty = int32(d)
	if in.Difficulty < 1 || in.Difficulty > 3 {
		in.Difficulty = 1
	}
	ci, _ := strconv.Atoi(c.PostForm("correct_index"))
	in.CorrectIndex = int32(ci)
	for _, lang := range i18n.Langs {
		body := c.PostForm("body_" + lang)
		answers := make([]string, 4)
		for i := 0; i < 4; i++ {
			answers[i] = c.PostForm("answer_" + lang + "_" + strconv.Itoa(i))
		}
		in.Text[lang] = Text{Body: body, Answers: answers, Explanation: c.PostForm("explanation_" + lang)}
	}
	return in
}

func (w *Web) addQuestion(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if _, err := w.svc.AddQuestion(c.Request.Context(), v.User, id, readQuestion(c, v)); err != nil {
		w.questionFailed(c, v, err)
		return
	}
	back := backTo(c, "/my-questions/"+id.String())
	if c.PostForm("again") != "" {
		// "Save and add another" never leaves the drawer: the same editor comes back
		// empty, ready for the next question, with the set behind it brought up to
		// date out of band. A full navigation here would tear the screen down and
		// build it again, which is what made it flash (MQ-05).
		w.addAnother(c, v, id, back)
		return
	}
	render.Redirect(c, back)
}

// addAnother re-renders the editor for the next question, over the set it belongs to.
func (w *Web) addAnother(c *gin.Context, v *view.Ctx, id uuid.UUID, back string) {
	if !render.IsHTMX(c) {
		// Without htmx there is no drawer to keep: the plain page does the same job.
		render.Redirect(c, pages.WithBack("/my-questions/"+id.String()+"/q/new", back))
		return
	}
	data, _, ok := w.loadID(c, v, id)
	if !ok {
		return
	}
	action := pages.WithBack("/my-questions/"+id.String()+"/q/new", back)
	render.Page(c, pages.SetQuestionDrawerAgain(v, w.editorData(c, v, data, action, nil)))
}

// questionFailed puts the editor's own message back on the screen the player is on,
// instead of throwing them out of the drawer.
func (w *Web) questionFailed(c *gin.Context, v *view.Ctx, err error) {
	e, ok := apperr.As(err)
	if !ok || e.Status >= 500 {
		w.fail(c, v, err)
		return
	}
	// The editor says what is wrong beside the field it is wrong in, and keeps what
	// the player typed, rather than throwing the whole drawer away (MQ-03).
	if len(e.Fields) > 0 && render.IsHTMX(c) {
		id, idErr := apiutil.UUIDParam(c, "id")
		if idErr == nil {
			if data, _, loaded := w.loadID(c, v, id); loaded {
				d := w.editorData(c, v, data, c.Request.URL.Path, nil)
				d.Errors = fieldProblems(v, e.Fields)
				keepTyped(c, &d)
				render.Status(c, http.StatusUnprocessableEntity, pages.SetQuestionDrawer(v, d))
				return
			}
		}
	}
	render.Toast(c, e.Message)
	render.Redirect(c, backTo(c, c.Request.URL.Path))
}

// keepTyped puts what the player wrote back in the boxes, so a refused question is
// corrected rather than typed again (MQ-03).
func keepTyped(c *gin.Context, d *pages.QuestionEditorData) {
	in := readQuestion(c, nil)
	d.Question.Correct = in.CorrectIndex
	d.Question.Difficulty = in.Difficulty
	d.Question.CategoryID = in.CategoryID
	d.Question.Lang = in.MainLang
	for lang, t := range in.Text {
		d.Bodies[lang] = t.Body
		d.Answers[lang] = t.Answers
		d.Explain[lang] = t.Explanation
	}
}

// fieldProblems turns the service's field names into the words the editor shows.
func fieldProblems(v *view.Ctx, fields map[string]string) map[string]string {
	out := make(map[string]string, len(fields))
	for field, why := range fields {
		key := "sets.err_" + field
		if tr := v.Tr(key); tr != key {
			out[field] = tr
			continue
		}
		out[field] = why
	}
	return out
}

func (w *Web) saveQuestion(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	qid, _ := strconv.ParseInt(c.Param("qid"), 10, 64)
	if err := w.svc.UpdateQuestion(c.Request.Context(), v.User, id, qid, readQuestion(c, v)); err != nil {
		w.questionFailed(c, v, err)
		return
	}
	render.Redirect(c, backTo(c, "/my-questions/"+id.String()))
}

func (w *Web) duplicate(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	qid, _ := strconv.ParseInt(c.Param("qid"), 10, 64)
	if _, err := w.svc.Duplicate(c.Request.Context(), v.User, id, qid); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, backTo(c, "/my-questions/"+id.String()))
}

func (w *Web) deleteQuestion(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	qid, _ := strconv.ParseInt(c.Param("qid"), 10, 64)
	if err := w.svc.DeleteQuestion(c.Request.Context(), v.User, id, qid); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, backTo(c, "/my-questions/"+id.String()))
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
		render.Redirect(c, c.Request.URL.Path)
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

// SetCards lists the player's own sets for the "One of my sets" source of a round
// (P20, MQ-05). A set under five questions is listed but cannot be picked (MQ-03).
// DraftFor is the set a "write them now" round writes into: the one already named in
// the round's address, or a new one made on the spot (MQ-05).
func (w *Web) DraftFor(c *gin.Context, v *view.Ctx, raw string) (string, error) {
	if id, err := uuid.Parse(raw); err == nil {
		if _, _, err := w.svc.Get(c.Request.Context(), v.User, id); err == nil {
			return id.String(), nil
		}
	}
	set, err := w.svc.Create(c.Request.Context(), v.User, v.Tr("sets.written_for_round"), v.Lang)
	if err != nil {
		return "", err
	}
	return set.ID.String(), nil
}

// RoundQuestions lists a set's questions for the round screen that is writing them.
func (w *Web) RoundQuestions(c *gin.Context, v *view.Ctx, raw string) ([]pages.SetQuestionRow, int) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, 0
	}
	data, _, ok := w.loadID(c, v, id)
	if !ok {
		return nil, 0
	}
	return data.Questions, int(data.Set.Questions)
}

func (w *Web) SetCards(c *gin.Context, v *view.Ctx) []pages.RoundSet {
	rows, _, err := w.svc.List(c.Request.Context(), v.User, 1, 50)
	if err != nil {
		return nil
	}
	out := make([]pages.RoundSet, 0, len(rows))
	for _, s := range rows {
		out = append(out, pages.RoundSet{
			ID: s.ID.String(), Name: s.Name, Questions: s.Questions, Ready: s.Ready,
		})
	}
	return out
}

// NewQuestionDrawer is the editor opened over another screen — the round that is
// writing its own questions asks for it once its draft set exists, so the first
// question opens the same drawer as every one after it (MQ-05).
func (w *Web) NewQuestionDrawer(c *gin.Context, v *view.Ctx, raw, back string) (templ.Component, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, false
	}
	data, _, ok := w.loadID(c, v, id)
	if !ok {
		return nil, false
	}
	data.Back = back
	action := pages.WithBack("/my-questions/"+id.String()+"/q/new", back)
	return pages.SetQuestionDrawer(v, w.editorData(c, v, data, action, nil)), true
}

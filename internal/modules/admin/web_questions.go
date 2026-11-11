package admin

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	adminpages "siraj/web/templates/admin"
)

// ---------------------------------------------------------------- A06 questions

func (w *Web) questions(c *gin.Context) {
	v := view.From(c)
	page, per := apiutil.PageParams(c, 25, 100)
	domainID, _ := strconv.ParseInt(c.Query("domain"), 10, 64)
	categoryID, _ := strconv.ParseInt(c.Query("category"), 10, 64)
	difficulty, _ := strconv.Atoi(c.Query("level"))
	f := QuestionFilter{
		Lang: v.Lang, Status: c.Query("status"), DomainID: domainID, CategoryID: categoryID,
		Difficulty: int32(difficulty), Langs: c.Query("langs"), Q: c.Query("q"),
		Sort: c.Query("sort"), Asc: c.Query("dir") != "desc",
		Page: page, PerPage: per,
	}
	rows, total, err := w.svc.Questions(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	counts, _ := w.svc.QuestionCounts(c.Request.Context())
	domains, _, err := w.svc.Domains(c.Request.Context(), DomainFilter{Lang: v.Lang})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	domainOptions := []adminpages.Option{{Value: "0", Label: v.Tr("admin.any_domain")}}
	for _, dom := range domains {
		domainOptions = append(domainOptions, adminpages.Option{Value: fmt.Sprint(dom.ID), Label: dom.Name})
	}
	// The category list follows the domain above it, so the two filters agree.
	cats, _, err := w.svc.Categories(c.Request.Context(), CategoryFilter{Lang: v.Lang, DomainID: domainID})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	categoryOptions := []adminpages.Option{{Value: "0", Label: v.Tr("admin.any_category")}}
	for _, cat := range cats {
		categoryOptions = append(categoryOptions, adminpages.Option{Value: fmt.Sprint(cat.ID), Label: cat.Name})
	}
	d := adminpages.TableData{
		Title: v.Tr("admin.questions"), BasePath: "/admin/questions", Query: c.Request.URL.RawQuery,
		NewLabel: v.Tr("admin.new_question"), NewHref: "/admin/questions/new", Drawer: true,
		ExportPath: "/admin/questions/export.csv", TemplatePath: "/admin/questions/template.csv",
		ImportPath: "/admin/questions/import",
		SearchName: "q", Search: f.Q, SearchLabel: v.Tr("admin.search_questions"),
		Sort: f.sort(), Asc: f.Asc,
		SegmentKey: "status", Segment: f.Status,
		Segments: []adminpages.Segment{
			{Value: "", Label: v.Tr("admin.all"), Count: counts["published"] + counts["review"] + counts["draft"] + counts["archived"]},
			{Value: "published", Label: v.Tr("admin.live"), Count: counts["published"]},
			{Value: "review", Label: v.Tr("admin.review"), Count: counts["review"]},
			{Value: "draft", Label: v.Tr("sets.draft"), Count: counts["draft"]},
			{Value: "archived", Label: v.Tr("admin.retired"), Count: counts["archived"]},
		},
		Filters: []adminpages.Filter{
			{Name: "domain", Label: v.Tr("admin.domain"), Value: c.Query("domain"), Options: domainOptions},
			{Name: "category", Label: v.Tr("admin.category"), Value: c.Query("category"), Options: categoryOptions},
			{Name: "level", Label: v.Tr("admin.level"), Value: c.Query("level"), Options: []adminpages.Option{
				{Value: "0", Label: v.Tr("admin.any_level")},
				{Value: "1", Label: v.Tr("admin.level_easy")},
				{Value: "2", Label: v.Tr("admin.level_medium")},
				{Value: "3", Label: v.Tr("admin.level_hard")},
			}},
			{Name: "langs", Label: v.Tr("admin.languages"), Value: f.Langs, Options: []adminpages.Option{
				{Value: "", Label: v.Tr("admin.any_translations")},
				{Value: "all", Label: v.Tr("admin.langs_all")},
				{Value: "missing", Label: v.Tr("admin.langs_missing")},
			}},
		},
		Sub: v.Tr("admin.questions_sub"),
		Columns: []adminpages.Column{
			{Label: v.Tr("admin.question"), Sort: "body"},
			{Label: v.Tr("admin.level"), Sort: "difficulty"},
			{Label: v.Tr("admin.languages")},
			{Label: v.Tr("admin.accuracy"), Sort: "accuracy"},
			{Label: v.Tr("admin.plays"), Sort: "plays", Align: "num"},
			{Label: v.Tr("admin.stars"), Align: "num"},
			{Label: v.Tr("admin.status")},
		},
		Empty: v.Tr("admin.no_questions"),
		Page:  page, Pages: pageCount(total, per),
		BulkPath: "/admin/questions/bulk",
		Bulk: []adminpages.BulkAction{
			{Label: v.Tr("admin.publish"), Value: "published", Icon: "check-circle"},
			{Label: v.Tr("admin.send_to_review"), Value: "review", Icon: "review"},
			{Label: v.Tr("admin.retire"), Value: "archived", Icon: "archive", Danger: true},
		},
	}
	setRange(&d, total, per)
	for _, r := range rows {
		stars := "—"
		if r.Ratings > 0 {
			stars = fmt.Sprintf("%.1f (%d)", float64(r.Stars10)/10, r.Ratings)
		}
		body := r.Body
		if len([]rune(body)) > 90 {
			body = string([]rune(body)[:90]) + "…"
		}
		meta := fmt.Sprintf("#%d · %s · %d %s", r.ID, r.CategoryName, r.Points, v.Tr("admin.pts"))
		d.Rows = append(d.Rows, adminpages.TableRow{
			ID: fmt.Sprint(r.ID), State: string(r.Status),
			Cells: []adminpages.Cell{
				{
					Text: body, Sub: meta, Icon: r.CategoryIcon, Color: r.CategoryColor,
					Href: "/admin/questions/" + fmt.Sprint(r.ID), Drawer: true,
				},
				{Level: int(r.Difficulty), LevelLabel: levelName(v, int(r.Difficulty))},
				{Langs: langStrip(r.LangCodes)},
				questionAccuracy(r.Accuracy),
				{Text: adminNumber(int(r.Plays)), Align: "num"},
				{Text: stars, Align: "num"},
				{Badge: statusLabel(v, string(r.Status)), BadgeClass: statusClass(string(r.Status)), Dot: true},
			},
			Actions: []adminpages.Action{
				{Label: v.Tr("admin.edit"), Icon: "edit", Href: "/admin/questions/" + fmt.Sprint(r.ID), Drawer: true},
			},
			Menu: []adminpages.Action{
				{Label: v.Tr("admin.open_category"), Icon: "layers",
					Href: "/admin/questions?category=" + fmt.Sprint(r.CategoryID)},
				{Label: v.Tr("admin.publish"), Icon: "check-circle", Post: true,
					Href: "/admin/questions/bulk", Fields: map[string]string{"ids": fmt.Sprint(r.ID), "action": "published"}},
				{Label: v.Tr("admin.send_to_review"), Icon: "review", Post: true,
					Href: "/admin/questions/bulk", Fields: map[string]string{"ids": fmt.Sprint(r.ID), "action": "review"}},
				{Label: v.Tr("admin.retire"), Icon: "archive", Post: true, Danger: true,
					Href: "/admin/questions/bulk", Fields: map[string]string{"ids": fmt.Sprint(r.ID), "action": "archived"},
					Confirm: v.Tr("admin.retire_question_confirm")},
			},
		})
	}
	render.Page(c, adminpages.Table(v, w.shell(c, v.Tr("admin.questions"), "questions"), d))
}

// questionAccuracy is the design's bar: a question nobody has played yet has no
// score to show, and one almost everybody gets wrong is worth noticing.
func questionAccuracy(pct int32) adminpages.Cell {
	if pct < 0 {
		return adminpages.Cell{Text: "—", Align: "num"}
	}
	return adminpages.Cell{
		Text: fmt.Sprintf("%d%%", pct), Meter: int(pct), MeterClass: accuracyTone(int(pct)),
	}
}

func levelName(v *view.Ctx, d int) string {
	switch d {
	case 1:
		return v.Tr("setup.easy")
	case 2:
		return v.Tr("setup.medium")
	case 3:
		return v.Tr("setup.hard")
	}
	return "—"
}

func statusLabel(v *view.Ctx, s string) string {
	switch s {
	case "published":
		return v.Tr("admin.live")
	case "review":
		return v.Tr("admin.review")
	case "draft":
		return v.Tr("sets.draft")
	case "archived":
		return v.Tr("admin.retired")
	}
	return s
}

func statusClass(s string) string {
	switch s {
	case "published":
		return "badge-live"
	case "review":
		return "badge-pending"
	case "draft":
		return "badge-draft"
	case "archived":
		return "badge-retired"
	}
	return ""
}

func (w *Web) bulkQuestions(c *gin.Context) {
	v := view.From(c)
	raw := c.PostFormArray("ids")
	ids := make([]int64, 0, len(raw))
	for _, s := range raw {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		w.back(c, "/admin/questions")
		return
	}
	if _, err := w.svc.BulkStatus(c.Request.Context(), w.actor(c), ids, c.PostForm("action")); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.back(c, "/admin/questions")
}

func (w *Web) questionForm(c *gin.Context) {
	v := view.From(c)
	cats, _, err := w.svc.Categories(c.Request.Context(), CategoryFilter{Lang: v.Lang})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	domains, _, err := w.svc.Domains(c.Request.Context(), DomainFilter{Lang: v.Lang})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := adminpages.QuestionFormData{
		Action: "/admin/questions", Difficulty: 2, Points: 20, MainLang: v.Lang, Status: "draft",
		Bodies: map[string]string{}, Answers: map[string][]string{}, Explanations: map[string]string{},
	}
	for _, dom := range domains {
		d.Domains = append(d.Domains, adminpages.Option{Value: fmt.Sprint(dom.ID), Label: dom.Name})
	}
	if len(d.Domains) > 0 {
		d.DomainID, _ = strconv.ParseInt(d.Domains[0].Value, 10, 64)
	}
	for _, cat := range cats {
		d.Categories = append(d.Categories, adminpages.Option{
			Value: fmt.Sprint(cat.ID), Label: cat.DomainName + " · " + cat.Name,
		})
	}
	if raw := c.Param("id"); raw != "" && raw != "new" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		row, err := w.svc.db.Q.GetAdminQuestion(c.Request.Context(), id)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		d.ID, d.CategoryID = row.ID, row.CategoryID
		d.DomainID = domainOfCategory(cats, row.CategoryID)
		d.Difficulty, d.Points, d.CorrectIndex = int32(row.Difficulty), row.Points, int32(row.CorrectIndex)
		d.Source, d.MainLang, d.Status = row.Source, row.MainLang, string(row.Status)
		texts, err := w.svc.db.Q.QuestionTexts(c.Request.Context(), id)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		for _, t := range texts {
			d.Bodies[t.Lang] = t.Body
			d.Answers[t.Lang] = t.Answers
			d.Explanations[t.Lang] = t.Explanation
		}
	}
	if render.IsHTMX(c) {
		render.Page(c, adminpages.QuestionDrawer(v, d))
		return
	}
	render.Page(c, adminpages.QuestionForm(v, w.shell(c, v.Tr("admin.questions"), "questions"), d))
}

// domainOfCategory says which domain a question's category belongs to, so the
// drawer opens with the right pair selected.
func domainOfCategory(cats []sqlc.AdminCategoriesRow, categoryID int64) int64 {
	for _, cat := range cats {
		if cat.ID == categoryID {
			return cat.DomainID
		}
	}
	return 0
}

func (w *Web) saveQuestion(c *gin.Context) {
	v := view.From(c)
	id, _ := strconv.ParseInt(c.PostForm("id"), 10, 64)
	categoryID, _ := strconv.ParseInt(c.PostForm("category_id"), 10, 64)
	difficulty, _ := strconv.Atoi(c.PostForm("difficulty"))
	points, _ := strconv.Atoi(c.PostForm("points"))
	// The correct answer is one per question; the drawer posts it from whichever
	// language tab the writer picked it in (A06b).
	correct := 0
	for _, lang := range append([]string{c.PostForm("main_lang")}, i18n.Langs...) {
		if raw := c.PostForm("correct_" + lang); raw != "" {
			correct, _ = strconv.Atoi(raw)
			break
		}
	}
	if raw := c.PostForm("correct_index"); raw != "" {
		correct, _ = strconv.Atoi(raw)
	}
	in := QuestionInput{
		ID: id, CategoryID: categoryID, Difficulty: int32(difficulty), Points: int32(points),
		CorrectIndex: int32(correct), Source: c.PostForm("source"),
		MainLang: c.PostForm("main_lang"), Status: c.PostForm("status"),
		Bodies: map[string]string{}, Answers: map[string][]string{}, Explanations: map[string]string{},
	}
	for _, lang := range i18n.Langs {
		in.Bodies[lang] = c.PostForm("body_" + lang)
		answers := make([]string, 4)
		for i := 0; i < 4; i++ {
			answers[i] = c.PostForm(fmt.Sprintf("answer_%s_%d", lang, i))
		}
		in.Answers[lang] = answers
		in.Explanations[lang] = c.PostForm("explanation_" + lang)
	}
	if _, err := w.svc.SaveQuestion(c.Request.Context(), w.actor(c), in); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("admin.saved"))
	// "Save and add another" keeps the drawer open on an empty question, which is
	// how a session of writing goes (A06b).
	if c.PostForm("again") != "" && render.IsHTMX(c) {
		c.Request.URL.Path = "/admin/questions/new"
		w.questionForm(c)
		return
	}
	w.afterSave(c, "/admin/questions")
}

// ---------------------------------------------------------------- A07 review queue

func (w *Web) review(c *gin.Context) {
	v := view.From(c)
	sort := "oldest"
	if c.Query("sort") == "newest" {
		sort = "newest"
	}
	page, per := apiutil.PageParams(c, adminpages.ReviewPerPage, adminpages.ReviewShowAll)
	rows, total, err := w.svc.Questions(c.Request.Context(), QuestionFilter{
		Lang: v.Lang, Status: "review", Sort: "created", Asc: sort == "oldest",
		Page: page, PerPage: per,
	})
	if err != nil {
		w.fail(c, v, err)
		return
	}

	d := adminpages.ReviewData{Sort: sort, Total: total, Page: page, Goal: total}
	d.Pages = pageCount(total, per)
	if rest := total - page*per; rest > 0 {
		d.More = rest
	}
	if done, err := w.svc.ReviewedToday(c.Request.Context(), v.User.ID); err == nil {
		d.Done = done
		// The ring reads "4 of 12": what is left to do plus what is already done is
		// the day's work, which is what the design counts.
		d.Goal = total + done
	}

	// Which question the pane is showing: the one asked for, the one after a skip,
	// else the first of the list.
	open := int64(0)
	if raw, err := strconv.ParseInt(c.Query("id"), 10, 64); err == nil {
		open = raw
	}
	if raw, err := strconv.ParseInt(c.Query("skip"), 10, 64); err == nil {
		open = nextAfter(rows, raw)
	}
	if open == 0 && len(rows) > 0 {
		open = rows[0].ID
	}

	for _, r := range rows {
		d.Items = append(d.Items, adminpages.ReviewItem{
			ID: r.ID, Body: r.Body, Category: r.CategoryName,
			Icon: first(r.CategoryIcon, "book"), Color: r.CategoryColor,
			Author: "", When: since(v, r.CreatedAt.Time), Current: r.ID == open,
		})
	}
	if open > 0 {
		pane, err := w.reviewPane(c, v, rows, open)
		if err != nil {
			w.fail(c, v, err)
			return
		}
		d.Open = pane
		// The list says who sent each question; the pane has just read it for the
		// open one, and the rest come from the same call the list already made.
		for i := range d.Items {
			if d.Items[i].ID == open && pane.Author != "" {
				d.Items[i].Author = pane.Author
			}
		}
	}
	if what := c.Query("done"); what != "" {
		if id, err := strconv.ParseInt(c.Query("undo"), 10, 64); err == nil {
			d.Undo, d.UndoWhat = id, "admin.toast."+what
		}
	}
	render.Page(c, adminpages.Review(v, w.shell(c, v.Tr("admin.review"), "review"), d))
}

// reviewPane fills the right-hand side for one question.
func (w *Web) reviewPane(c *gin.Context, v *view.Ctx, rows []sqlc.AdminQuestionsRow, id int64) (*adminpages.ReviewOpen, error) {
	detail, err := w.svc.ReviewDetail(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	out := &adminpages.ReviewOpen{
		ID: id, Author: detail.Author, FirstName: detail.FirstName,
		Source: detail.Source, Submitted: since(v, detail.Submitted),
	}
	for _, r := range rows {
		if r.ID != id {
			continue
		}
		out.Category, out.Domain = r.CategoryName, r.DomainName
		out.Icon, out.Color = first(r.CategoryIcon, "book"), r.CategoryColor
		out.Level, out.Difficulty = levelName(v, int(r.Difficulty)), int(r.Difficulty)
		out.Correct = int(r.CorrectIndex)
		// "New question" is a question that has never been published: an edit of a
		// live one is a change, not a new arrival.
		out.IsNew = !r.PublishedAt.Valid
	}
	main := 0
	for _, t := range detail.Texts {
		if t.Lang == detail.MainLang {
			main = len([]rune(t.Explanation))
		}
	}
	for _, t := range detail.Texts {
		text := adminpages.ReviewText{
			Lang: t.Lang, Body: t.Body, Answers: t.Answers,
			Explanation: t.Explanation, Status: "complete",
		}
		switch got := len([]rune(t.Explanation)); {
		case got == 0:
			text.Status = "missing"
		case main > 0 && t.Lang != detail.MainLang && 100-got*100/main >= 40:
			text.Status = "short"
		}
		out.Texts = append(out.Texts, text)
	}
	for _, ch := range detail.Checks {
		out.Checks = append(out.Checks, adminpages.ReviewCheck{
			Level: ch.Level, Text: v.Tr(ch.Text, "n", ch.Arg),
		})
	}
	return out, nil
}

// nextAfter is what Skip lands on: the question after the one skipped, wrapping to
// the first so the last of a page does not leave an empty pane.
func nextAfter(rows []sqlc.AdminQuestionsRow, id int64) int64 {
	for i, r := range rows {
		if r.ID == id && i+1 < len(rows) {
			return rows[i+1].ID
		}
	}
	if len(rows) > 0 && rows[0].ID != id {
		return rows[0].ID
	}
	return 0
}

// undoReview is the toast's Undo: the question goes back in the queue.
func (w *Web) undoReview(c *gin.Context) {
	v := view.From(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Unreview(c.Request.Context(), w.actor(c), id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/admin/review?id="+strconv.FormatInt(id, 10))
}

func (w *Web) decide(c *gin.Context) {
	v := view.From(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	decision := c.PostForm("decision")
	// Read the author before the decision: "Sent back to Yasmine" needs the name, and
	// the row is the same either way.
	name := ""
	if detail, err := w.svc.ReviewDetail(c.Request.Context(), id); err == nil {
		name = detail.FirstName
	}
	if err := w.svc.Review(c.Request.Context(), w.actor(c), id, decision, c.PostForm("note")); err != nil {
		w.fail(c, v, err)
		return
	}
	// The screen says what it did and offers to take it back, which is what the three
	// one-key decisions need.
	what := map[string]string{"approved": "approved", "rejected": "rejected", "sent_back": "sent_back"}[decision]
	next := "/admin/review?sort=" + url.QueryEscape(c.PostForm("sort")) +
		"&done=" + what + "&undo=" + strconv.FormatInt(id, 10)
	if what == "sent_back" && name != "" {
		next += "&name=" + url.QueryEscape(name)
	}
	render.Redirect(c, next)
}

// ---------------------------------------------------------------- A08 comments

func (w *Web) comments(c *gin.Context) {
	v := view.From(c)
	page, per := apiutil.PageParams(c, adminpages.CommentPerPage, 100)
	f := CommentFilter{
		Tab: c.DefaultQuery("tab", "needs"), Q: strings.TrimSpace(c.Query("q")),
		Lang: c.Query("lang"), ReadAs: v.Lang, Page: page, PerPage: per,
	}
	rows, total, err := w.svc.Comments(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	counts, err := w.svc.CommentCounts(c.Request.Context())
	if err != nil {
		w.fail(c, v, err)
		return
	}

	d := adminpages.CommentsData{
		Tab: f.Tab, Query: f.Q, Lang: f.Lang,
		Counts: adminpages.CommentTabCounts{
			NeedsAction: counts.NeedsAction, Flagged: counts.Flagged,
			Resolved: counts.Resolved, Hidden: counts.Hidden,
		},
		Page: page, Pages: pageCount(total, per), Total: total,
		From: (page-1)*per + 1, To: (page-1)*per + len(rows),
	}
	if total == 0 {
		d.From = 0
	}
	for _, r := range rows {
		who := r.DisplayName.String
		if who == "" {
			who = r.Username.String
		}
		if who == "" {
			// A guest's comment still has an author, just not a name.
			who = v.Tr("admin.a_guest")
		}
		card := adminpages.CommentCard{
			ID: r.ID.String(), Author: who, Initials: adminInitials(who), Color: adminColor(who),
			Lang: r.Lang, Body: r.Body, QuestionID: r.QuestionID,
			Question: r.QuestionBody, Reports: int(r.Reports),
			Hidden: r.Status == "hidden",
			// The tab already says which set this is; a card in Resolved is settled.
			Resolved: f.Tab == "resolved",
		}
		if r.CreatedAt.Valid {
			card.When = since(v, r.CreatedAt.Time)
		}
		d.Cards = append(d.Cards, card)
	}
	render.Page(c, adminpages.Comments(v, w.shell(c, v.Tr("admin.comments"), "comments"), d))
}

// moderateComment is every decision a card offers: hide, show, keep, resolve, reopen
// or suspend. One handler, because they are one shape — a decision about one comment,
// recorded in the audit log by the service.
func (w *Web) moderateComment(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("comment"))
		return
	}
	if err := w.svc.ModerateComment(c.Request.Context(), w.actor(c), id, c.PostForm("action")); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("admin.saved"))
	w.back(c, "/admin/comments")
}

// ---------------------------------------------------------------- A09 rated poorly

func (w *Web) rated(c *gin.Context) {
	v := view.From(c)
	page, per := apiutil.PageParams(c, adminpages.RatedPerPage, 100)
	days := 30
	if n, err := strconv.Atoi(c.Query("days")); err == nil && (n == 7 || n == 30 || n == 90) {
		days = n
	}
	f := RatedFilter{
		Lang: v.Lang, Days: days, Reason: c.Query("reason"), Page: page, PerPage: per,
	}

	summary, err := w.svc.RatingOverview(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	rows, err := w.svc.RatedDetailed(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}

	d := adminpages.RatedData{
		PositivePercent: summary.PositivePercent, Ratings: summary.Ratings,
		BelowThreshold: summary.BelowThreshold,
		TopReason:      summary.TopReason, TopReasonCount: summary.TopReasonCount,
		ReasonsGiven: summary.ReasonsGiven,
		Days:         days, Reason: f.Reason,
		Page: page, Pages: pageCount(summary.BelowThreshold, per), Total: summary.BelowThreshold,
		From: (page-1)*per + 1, To: (page-1)*per + len(rows),
	}
	if summary.BelowThreshold == 0 {
		d.From = 0
	}
	if reasons, err := w.svc.RatingReasons(c.Request.Context()); err == nil {
		for _, r := range reasons {
			d.Reasons = append(d.Reasons, adminpages.RatedReason{Label: r})
		}
	}
	for _, r := range rows {
		row := adminpages.RatedRow{
			ID: r.ID, Body: r.Body, Category: r.Category, Color: r.Color, Icon: r.Icon,
			Ratings: r.Ratings, Up: r.Up, Down: r.Down, DownPercent: r.DownPercent,
		}
		// Most-picked first, so the chip that explains the row comes first.
		for label, n := range r.Reasons {
			row.Reasons = append(row.Reasons, adminpages.RatedReason{Label: label, Count: n})
		}
		sort.Slice(row.Reasons, func(i, j int) bool {
			if row.Reasons[i].Count != row.Reasons[j].Count {
				return row.Reasons[i].Count > row.Reasons[j].Count
			}
			return row.Reasons[i].Label < row.Reasons[j].Label
		})
		d.Rows = append(d.Rows, row)
	}
	render.Page(c, adminpages.Rated(v, w.shell(c, v.Tr("admin.rated"), "rated"), d))
}

// ---------------------------------------------------------------- A10 content health

func (w *Web) health(c *gin.Context) {
	v := view.From(c)
	// The two lists page independently: clearing duplicates should not move the
	// reader's place in the reports.
	dupPage, _ := strconv.Atoi(c.Query("dup"))
	repPage, _ := strconv.Atoi(c.Query("rep"))
	h, err := w.svc.Health(c.Request.Context(), v.Lang, dupPage, repPage)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := adminpages.HealthData{
		Counts:      h.Counts,
		DupPage:     max(dupPage, 1),
		DupPages:    pageCount(h.DuplicateTotal, HealthPerPage),
		DupTotal:    h.DuplicateTotal,
		ReportPage:  max(repPage, 1),
		ReportPages: pageCount(h.ReportTotal, HealthPerPage),
		ReportTotal: h.ReportTotal,
	}
	for _, cov := range h.Coverage {
		percent := 0
		if cov.Total > 0 {
			percent = int(cov.Translated) * 100 / int(cov.Total)
		}
		d.Coverage = append(d.Coverage, adminpages.Bar{
			Label: strings.ToUpper(cov.Code), Value: percent, Max: 100,
		})
	}
	for _, dup := range h.Duplicates {
		d.Duplicates = append(d.Duplicates, adminpages.Duplicate{
			A: dup.QuestionA, B: dup.QuestionB,
			Similarity: int(dup.Similarity * 100),
			BodyA:      dup.BodyA, BodyB: dup.BodyB,
		})
	}
	for _, r := range h.Reports {
		who := "—"
		if r.DisplayName.Valid {
			who = r.DisplayName.String
		}
		d.Reports = append(d.Reports, adminpages.Report{
			ID: r.ID.String(), QuestionID: r.QuestionID, Reason: r.Reason,
			Note: r.Note, By: who, Body: r.Body,
		})
	}
	render.Page(c, adminpages.Health(v, w.shell(c, v.Tr("admin.health"), "health"), d))
}

func (w *Web) duplicate(c *gin.Context) {
	v := view.From(c)
	a, _ := strconv.ParseInt(c.PostForm("a"), 10, 64)
	b, _ := strconv.ParseInt(c.PostForm("b"), 10, 64)
	if err := w.svc.ResolveDuplicate(c.Request.Context(), w.actor(c), a, b, c.PostForm("status")); err != nil {
		w.fail(c, v, err)
		return
	}
	w.back(c, "/admin/health")
}

func (w *Web) resolveReport(c *gin.Context) {
	v := view.From(c)
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.ResolveReport(c.Request.Context(), w.actor(c), id, c.PostForm("status")); err != nil {
		w.fail(c, v, err)
		return
	}
	w.back(c, "/admin/health")
}

// ---------------------------------------------------------------- A11 audit log

func (w *Web) audit(c *gin.Context) {
	v := view.From(c)
	f := w.auditFilter(c)
	rows, total, err := w.svc.Audit(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := adminpages.AuditData{
		Q: f.Q, Actor: f.Actor, Area: f.Area, Days: f.Days,
		Page: f.Page, Pages: pageCount(total, f.PerPag), Total: total,
		From: (f.Page-1)*f.PerPag + 1, To: (f.Page-1)*f.PerPag + len(rows),
	}
	if total == 0 {
		d.From = 0
	}
	d.Actors = []adminpages.AuditOption{{Value: "", Label: v.Tr("admin.anyone")}}
	if actors, err := w.svc.AuditActors(c.Request.Context()); err == nil {
		for _, a := range actors {
			d.Actors = append(d.Actors, adminpages.AuditOption{Value: a.Username, Label: a.Name})
		}
	}
	d.Areas = []adminpages.AuditOption{{Value: "", Label: v.Tr("admin.all_areas")}}
	if areas, err := w.svc.AuditAreas(c.Request.Context()); err == nil {
		for _, a := range areas {
			d.Areas = append(d.Areas, adminpages.AuditOption{Value: a, Label: areaName(v, a)})
		}
	}
	d.Entries = w.auditEntries(v, rows, w.svc.AuditNames(c.Request.Context(), v.Lang, rows))
	d.Groups = auditDays(d.Entries)
	render.Page(c, adminpages.Audit(v, w.shell(c, v.Tr("admin.audit"), "audit"), d))
}

// auditFilter reads A11's toolbar. The range defaults to the design's 7 days, so the
// screen opens on a week rather than on the whole history.
func (w *Web) auditFilter(c *gin.Context) AuditFilter {
	page, _ := apiutil.PageParams(c, adminpages.AuditPerPage, adminpages.AuditPerPage)
	days := 7
	if raw := c.Query("days"); raw != "" {
		switch raw {
		case "1", "7", "30":
			days, _ = strconv.Atoi(raw)
		case "0", "all":
			days = 0
		}
	}
	return AuditFilter{
		Q: strings.TrimSpace(c.Query("q")), Actor: c.Query("actor"),
		Action: c.Query("action"), Area: c.Query("area"), Days: days,
		Page: page, PerPag: adminpages.AuditPerPage,
	}
}

// auditEntries turns the rows into the screen's own shape: the day heading goes on the
// first row of each day, and the recorded diff becomes the lines "Show change" opens.
func (w *Web) auditEntries(v *view.Ctx, rows []sqlc.AuditLogRow, names map[string]string) []adminpages.AuditEntry {
	out := make([]adminpages.AuditEntry, 0, len(rows))
	today := time.Now().Local().Format("2006-01-02")
	yesterday := time.Now().Local().AddDate(0, 0, -1).Format("2006-01-02")
	lastDay := ""
	for _, r := range rows {
		e := adminpages.AuditEntry{
			Action: actionName(v, r.Action),
			Icon:   actionIcon(r.Action),
			Area:   areaName(v, auditArea(r.Action, r.Entity)),
		}
		if r.CreatedAt.Valid {
			at := r.CreatedAt.Time.Local()
			e.Time = at.Format("15:04")
			switch day := at.Format("2006-01-02"); {
			case day == today:
				e.Day = v.Tr("admin.today")
			case day == yesterday:
				e.Day = v.Tr("admin.yesterday")
			default:
				e.Day = at.Format("2 January 2006")
			}
			if e.Day == lastDay {
				e.Day = ""
			} else {
				lastDay = e.Day
			}
		}
		switch {
		case r.ActorName.Valid && r.ActorName.String != "":
			e.Actor = r.ActorName.String
		case r.ActorUsername.Valid && r.ActorUsername.String != "":
			e.Actor = r.ActorUsername.String
		default:
			// The worker writes with no actor; the design draws it as "System".
			e.Actor, e.System = v.Tr("admin.system"), true
		}
		e.Initials, e.Color = adminInitials(e.Actor), adminColor(e.Actor)
		if e.System {
			e.Color = "#4b5563"
		}
		if r.EntityID.Valid && r.EntityID.String != "" {
			e.Target = auditTarget(r.Entity, r.EntityID.String, names)
		}
		e.Changes = auditChanges(r.Diff)
		out = append(out, e)
	}
	return out
}

// auditDays folds the rows into the days they happened on, keeping the order they
// arrived in. The entries already carry the day heading on the first row of each day,
// which is where the grouping comes from.
func auditDays(entries []adminpages.AuditEntry) []adminpages.AuditDay {
	var out []adminpages.AuditDay
	for _, e := range entries {
		if e.Day != "" || len(out) == 0 {
			label := e.Day
			if label == "" {
				label = "—"
			}
			out = append(out, adminpages.AuditDay{Label: label})
		}
		out[len(out)-1].Entries = append(out[len(out)-1].Entries, e)
	}
	return out
}

// auditChanges reads the diff the write recorded. It is a free-form object, so only
// what can be shown as one line per field is shown, and the rest is left alone.
func auditChanges(raw []byte) []adminpages.AuditChange {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]adminpages.AuditChange, 0, len(keys))
	for _, k := range keys {
		change := adminpages.AuditChange{Field: k}
		// {"field": {"from": …, "to": …}} is shown as "was → now"; anything else is
		// shown as the value it is.
		if pair, ok := fields[k].(map[string]any); ok {
			_, hasFrom := pair["from"]
			_, hasTo := pair["to"]
			if hasFrom || hasTo {
				change.From, change.To = scalar(pair["from"]), scalar(pair["to"])
				if change.From == "" && change.To == "" {
					continue
				}
				out = append(out, change)
				continue
			}
		}
		change.To = scalar(fields[k])
		if change.To == "" && change.From == "" {
			continue
		}
		out = append(out, change)
	}
	return out
}

// scalar prints a JSON value the way a reader expects it, and gives up on anything
// that is not one line long.
func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

// actionName turns "question.publish" into the words the design shows, falling back to
// the recorded action so a new one is still readable before it has a translation.
func actionName(v *view.Ctx, action string) string {
	if key := "admin.action." + action; v.Has(key) {
		return v.Tr(key)
	}
	return action
}

// actionIcon is the mark in front of the action, by what the action does.
func actionIcon(action string) string {
	_, verb, _ := strings.Cut(action, ".")
	switch verb {
	case "publish", "approve", "approved", "resolve", "resolved":
		return "check"
	case "reject", "rejected", "ban", "suspend", "suspension":
		return "ban"
	case "import":
		return "upload"
	case "archive", "retire":
		return "archive"
	case "role":
		return "key"
	case "sent_back", "undo":
		return "reply"
	case "reply":
		return "reply"
	case "check", "checks":
		return "shield-check"
	case "delete":
		return "trash"
	default:
		return "edit"
	}
}

// auditArea is which part of the console an entry belongs to. It is the entity for
// almost everything, except a review decision: approving a question is work done in
// the review queue, which is where the design files it.
func auditArea(action, entity string) string {
	switch action {
	case "question.approved", "question.rejected", "question.sent_back", "question.undo":
		return "review"
	}
	return entity
}

// auditTarget names the row the entry touched: a question by its number, a player by
// their handle, a category or domain by its name. Anything the page could not resolve
// keeps the id it was recorded with rather than showing nothing.
func auditTarget(entity, id string, names map[string]string) string {
	if name, ok := names[entity+":"+id]; ok {
		return name
	}
	switch entity {
	case "question", "questions":
		return "#" + id
	case "ticket":
		return "T-" + id
	}
	return id
}

// areaName is the badge at the end of the row: which part of the console the entry
// belongs to.
func areaName(v *view.Ctx, entity string) string {
	if key := "admin.area." + entity; v.Has(key) {
		return v.Tr(key)
	}
	return entity
}

// auditCSV is the Export CSV button: the entries the filters show, as a file.
func (w *Web) auditCSV(c *gin.Context) {
	v := view.From(c)
	f := w.auditFilter(c)
	// A download is not a page of a list: it carries everything the filters match.
	f.Page, f.PerPag = 1, MaxImportRows
	rows, _, err := w.svc.Audit(c.Request.Context(), f)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="audit-log.csv"`)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	out := csv.NewWriter(c.Writer)
	_ = out.Write([]string{"when", "who", "action", "target", "area", "change"})
	for _, r := range rows {
		when := ""
		if r.CreatedAt.Valid {
			when = r.CreatedAt.Time.Local().Format(time.RFC3339)
		}
		who := "system"
		if r.ActorName.Valid && r.ActorName.String != "" {
			who = r.ActorName.String
		}
		target := ""
		if r.EntityID.Valid {
			target = r.EntityID.String
		}
		changes := make([]string, 0, 4)
		for _, ch := range auditChanges(r.Diff) {
			if ch.From != "" {
				changes = append(changes, ch.Field+": "+ch.From+" → "+ch.To)
				continue
			}
			changes = append(changes, ch.Field+": "+ch.To)
		}
		_ = out.Write([]string{when, who, r.Action, target, r.Entity, strings.Join(changes, "; ")})
	}
	out.Flush()
	if err := out.Error(); err != nil {
		_ = c.Error(err)
	}
}

// search is the Ctrl K box: it sends staff to the list that matches best.
func (w *Web) search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		render.Redirect(c, "/admin")
		return
	}
	if _, err := strconv.Atoi(q); err == nil {
		render.Redirect(c, "/admin/questions?q="+q)
		return
	}
	if strings.HasPrefix(strings.ToUpper(q), "T-") {
		render.Redirect(c, "/admin/support?q="+q)
		return
	}
	render.Redirect(c, "/admin/users?q="+q)
}

// langStrip is A06's AR / EN / FR mark: a language the question is missing shows
// outlined, so a gap in the three translations is visible at a glance (LG-05).
func langStrip(have []string) []adminpages.Lang {
	present := make(map[string]bool, len(have))
	for _, code := range have {
		present[strings.ToLower(code)] = true
	}
	out := make([]adminpages.Lang, 0, 3)
	for _, code := range []string{"ar", "en", "fr"} {
		out = append(out, adminpages.Lang{Code: strings.ToUpper(code), Has: present[code]})
	}
	return out
}

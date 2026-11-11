package questions

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// Web is the question feedback of the website: the inline star rater, the comments
// drawer and the report drawer (GP-05, RS-03, RS-04), plus the question of the day.
type Web struct{ svc *Service }

func NewWeb(s *Service) *Web { return &Web{svc: s} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.PUT("/questions/:id/rating", w.rate)
	private.GET("/questions/:id/comments", w.comments)
	private.POST("/questions/:id/comments", w.addComment)
	private.GET("/questions/:id/report", w.reportDrawer)
	private.POST("/questions/:id/report", w.report)
	public.POST("/daily-question/answer", w.answerDaily)
}

func (w *Web) rate(c *gin.Context) {
	v := view.From(c)
	id, ok := questionID(c)
	if !ok {
		return
	}
	stars, _ := strconv.Atoi(c.PostForm("stars"))
	if stars == 0 {
		stars, _ = strconv.Atoi(c.Query("stars"))
	}
	if err := w.svc.Rate(c.Request.Context(), v.User, id, int32(stars), c.PostForm("reason")); err != nil {
		fail(c, err)
		return
	}
	render.Toast(c, v.Tr("rate.thanks"))
	c.Status(http.StatusNoContent)
}

func (w *Web) comments(c *gin.Context) {
	v := view.From(c)
	id, ok := questionID(c)
	if !ok {
		return
	}
	rows, err := w.svc.Comments(c.Request.Context(), id, 30)
	if err != nil {
		fail(c, err)
		return
	}
	render.Page(c, pages.CommentsDrawer(v, id, w.questionBody(c, id, v.Lang), viewComments(rows)))
}

func (w *Web) addComment(c *gin.Context) {
	v := view.From(c)
	id, ok := questionID(c)
	if !ok {
		return
	}
	if _, err := w.svc.AddComment(c.Request.Context(), v.User, id, c.PostForm("body")); err != nil {
		fail(c, err)
		return
	}
	rows, err := w.svc.Comments(c.Request.Context(), id, 30)
	if err != nil {
		fail(c, err)
		return
	}
	render.Page(c, pages.CommentsDrawer(v, id, w.questionBody(c, id, v.Lang), viewComments(rows)))
}

func (w *Web) reportDrawer(c *gin.Context) {
	v := view.From(c)
	id, ok := questionID(c)
	if !ok {
		return
	}
	render.Page(c, pages.ReportDrawer(v, id, w.questionBody(c, id, v.Lang), ReportReasons, false))
}

func (w *Web) report(c *gin.Context) {
	v := view.From(c)
	id, ok := questionID(c)
	if !ok {
		return
	}
	if err := w.svc.Report(c.Request.Context(), v.User, id, c.PostForm("reason"), c.PostForm("note"), v.Lang); err != nil {
		if e, ok := apperr.As(err); ok && e.Status == http.StatusUnprocessableEntity {
			render.Page(c, pages.ReportDrawer(v, id, w.questionBody(c, id, v.Lang), ReportReasons, false))
			return
		}
		fail(c, err)
		return
	}
	render.Page(c, pages.ReportDrawer(v, id, "", ReportReasons, true))
}

// answerDaily is the one try a day from the Home or Welcome card (23).
func (w *Web) answerDaily(c *gin.Context) {
	v := view.From(c)
	if !v.SignedIn() {
		render.Redirect(c, "/guest?then=/home")
		return
	}
	choice, err := strconv.Atoi(c.Query("choice"))
	if err != nil {
		choice, _ = strconv.Atoi(c.PostForm("choice"))
	}
	daily, err := w.svc.AnswerToday(c.Request.Context(), v.User, int32(choice), v.Lang)
	if err != nil {
		fail(c, err)
		return
	}
	if daily == nil {
		c.Status(http.StatusNoContent)
		return
	}
	card := PageCard(daily, v.Lang)
	id := c.Query("card")
	if id == "" {
		id = "h-qotd"
	}
	// The welcome screen shows the compact card; Home shows the full one.
	render.Page(c, pages.QotdCard(v, card, id, id == "w-qotd"))
}

// PageCard maps the service's question of the day onto the page model.
func PageCard(d *Daily, lang string) *pages.QOTD {
	if d == nil {
		return nil
	}
	return &pages.QOTD{
		QuestionID: d.QuestionID, Day: d.Day.Format("2006-01-02"),
		Domain: view.Domain{
			ID: d.DomainID, Slug: d.DomainSlug, Name: d.DomainName, Color: d.DomainColor,
			Icon: d.DomainIcon, Motif: d.DomainMotif, Family: d.DomainFamily,
		},
		CategoryName: d.CategoryName, Difficulty: d.Difficulty, Points: d.Points,
		Body: d.Body, Answers: d.Answers, Correct: d.CorrectIndex,
		Explanation: d.Explanation, Source: d.Source, PercentRight: d.PercentRight,
		Answered: d.Answered, Picked: d.Choice, Lang: lang,
	}
}

func viewComments(rows []Comment) []pages.Comment {
	out := make([]pages.Comment, 0, len(rows))
	for _, r := range rows {
		author := r.Author
		if author == "" {
			author = "—"
		}
		out = append(out, pages.Comment{
			Author: author, Initials: avatar.Initials(author), Color: avatar.Color(author),
			Body: r.Body, When: r.CreatedAt.Local().Format("2 Jan 15:04"),
		})
	}
	return out
}

func questionID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(http.StatusNotFound)
		return 0, false
	}
	return id, true
}

func fail(c *gin.Context, err error) {
	if e, ok := apperr.As(err); ok {
		render.Toast(c, e.Message)
		c.Status(e.Status)
		return
	}
	_ = c.Error(err)
	c.Status(http.StatusInternalServerError)
}

// questionBody echoes the question inside the report drawer, so the player can see
// what they are reporting (RS-04).
func (w *Web) questionBody(c *gin.Context, id int64, lang string) string {
	row, err := w.svc.db.Q.QuestionText(c.Request.Context(), sqlc.QuestionTextParams{ID: id, Lang: lang})
	if err != nil {
		return ""
	}
	return row
}

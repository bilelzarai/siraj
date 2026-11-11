package questions

import (
	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/platform/authctx"
)

// API is the question feedback group (32–35) and the question of the day (22, 23).
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	public.GET("/daily-question", a.daily)             // #22
	private.POST("/daily-question/answer", a.answer)   // #23
	private.PUT("/questions/:id/rating", a.rate)       // #32
	public.GET("/questions/:id/comments", a.comments)  // #33
	private.POST("/questions/:id/comments", a.comment) // #34
	private.POST("/questions/:id/reports", a.report)   // #35
}

// DailyDTO never carries the right answer before the player has answered.
type DailyDTO struct {
	QuestionID   int64    `json:"question_id"`
	Day          string   `json:"day"`
	DomainID     int64    `json:"domain_id"`
	DomainName   string   `json:"domain_name"`
	DomainColor  string   `json:"domain_color"`
	CategoryName string   `json:"category_name"`
	Difficulty   int32    `json:"difficulty"`
	Points       int32    `json:"points"`
	Body         string   `json:"body"`
	Answers      []string `json:"answers"`
	Answered     bool     `json:"answered"`
	Choice       *int32   `json:"choice,omitempty"`
	Correct      *bool    `json:"correct,omitempty"`
	CorrectIndex *int32   `json:"correct_index,omitempty"`
	Explanation  string   `json:"explanation,omitempty"`
	Source       string   `json:"source,omitempty"`
	PercentRight int32    `json:"percent_right,omitempty"`
}

func dailyDTO(d *Daily) *DailyDTO {
	if d == nil {
		return nil
	}
	out := &DailyDTO{
		QuestionID: d.QuestionID, Day: d.Day.Format("2006-01-02"), DomainID: d.DomainID,
		DomainName: d.DomainName, DomainColor: d.DomainColor, CategoryName: d.CategoryName,
		Difficulty: d.Difficulty, Points: d.Points, Body: d.Body, Answers: d.Answers,
		Answered: d.Answered,
	}
	if d.Answered {
		choice, correct, index := d.Choice, d.Correct, d.CorrectIndex
		out.Choice, out.Correct, out.CorrectIndex = &choice, &correct, &index
		out.Explanation, out.Source, out.PercentRight = d.Explanation, d.Source, d.PercentRight
	}
	return out
}

func (a *API) daily(c *gin.Context) {
	d, err := a.svc.Today(c.Request.Context(), authctx.Get(c), apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dailyDTO(d))
}

type answerBody struct {
	Choice int32 `json:"choice"`
}

func (a *API) answer(c *gin.Context) {
	var body answerBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	d, err := a.svc.AnswerToday(c.Request.Context(), authctx.Get(c), body.Choice, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dailyDTO(d))
}

type rateBody struct {
	Stars  int32  `json:"stars"`
	Reason string `json:"reason"`
}

func (a *API) rate(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body rateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.Rate(c.Request.Context(), authctx.Get(c), id, body.Stars, body.Reason); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) comments(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	rows, err := a.svc.Comments(c.Request.Context(), id, 50)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{"id": r.ID, "body": r.Body, "author": r.Author, "created_at": r.CreatedAt})
	}
	apiutil.OK(c, out)
}

type commentBody struct {
	Body string `json:"body"`
}

func (a *API) comment(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body commentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	comment, err := a.svc.AddComment(c.Request.Context(), authctx.Get(c), id, body.Body)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{"id": comment.ID, "body": comment.Body, "created_at": comment.CreatedAt})
}

type reportBody struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

func (a *API) report(c *gin.Context) {
	id, err := apiutil.IDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body reportBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.Report(c.Request.Context(), authctx.Get(c), id, body.Reason, body.Note, apiutil.Lang(c)); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

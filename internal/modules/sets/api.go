package sets

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// API is endpoints 96–105.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/sets", a.list)                                 // 96
	private.POST("/sets", a.create)                              // 97
	private.GET("/sets/:id", a.get)                              // 98
	private.PATCH("/sets/:id", a.patch)                          // 99
	private.DELETE("/sets/:id", a.remove)                        // 100
	private.POST("/sets/:id/questions", a.addQuestion)           // 101
	private.PATCH("/sets/:id/questions/:qid", a.editQuestion)    // 102
	private.DELETE("/sets/:id/questions/:qid", a.deleteQuestion) // 103
	private.POST("/sets/:id/questions/:qid/duplicate", a.dupe)   // 104
	private.POST("/sets/:id/suggest", a.suggest)                 // 105
}

type questionBody struct {
	CategoryID   int64           `json:"category_id"`
	Difficulty   int32           `json:"difficulty"`
	Points       int32           `json:"points"`
	CorrectIndex int32           `json:"correct_index"`
	MainLang     string          `json:"main_lang"`
	Text         map[string]Text `json:"text"`
}

func (b questionBody) input() QuestionInput {
	return QuestionInput{
		CategoryID: b.CategoryID, Difficulty: b.Difficulty, Points: b.Points,
		CorrectIndex: b.CorrectIndex, MainLang: b.MainLang, Text: b.Text,
	}
}

func (a *API) list(c *gin.Context) {
	page, per := apiutil.PageParams(c, 20, 50)
	rows, total, err := a.svc.List(c.Request.Context(), authctx.Get(c), page, per)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Paged(c, rows, apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

func (a *API) create(c *gin.Context) {
	var body struct {
		Name     string `json:"name"`
		MainLang string `json:"main_lang"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	set, err := a.svc.Create(c.Request.Context(), authctx.Get(c), body.Name, body.MainLang)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, set)
}

func (a *API) get(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	set, questions, err := a.svc.Get(c.Request.Context(), authctx.Get(c), id)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"set": set, "questions": questions})
}

func (a *API) patch(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body struct {
		Name     string `json:"name"`
		MainLang string `json:"main_lang"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.Rename(c.Request.Context(), authctx.Get(c), id, body.Name, body.MainLang); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) remove(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Delete(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) addQuestion(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body questionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	qid, err := a.svc.AddQuestion(c.Request.Context(), authctx.Get(c), id, body.input())
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{"id": qid})
}

func questionID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("qid"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.BadRequest("bad_id", "qid must be a number")
	}
	return id, nil
}

func (a *API) editQuestion(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	qid, err := questionID(c)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	var body questionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.UpdateQuestion(c.Request.Context(), authctx.Get(c), id, qid, body.input()); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) deleteQuestion(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	qid, err := questionID(c)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.DeleteQuestion(c.Request.Context(), authctx.Get(c), id, qid); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) dupe(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	qid, err := questionID(c)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	newID, err := a.svc.Duplicate(c.Request.Context(), authctx.Get(c), id, qid)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{"id": newID})
}

func (a *API) suggest(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Suggest(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

package games

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/platform/authctx"
)

// API is the games group of the mobile API: endpoints 24–31. Same service as the web.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.POST("/games", a.start)              // #24
	private.GET("/games/:id", a.state)           // #25
	private.POST("/games/:id/answers", a.answer) // #26
	private.POST("/games/:id/fifty", a.fifty)    // #27
	private.POST("/games/:id/skip", a.skip)      // #28
	private.POST("/games/:id/finish", a.finish)  // #29
	private.GET("/games/:id/results", a.results) // #30
	private.GET("/me/games", a.history)          // #31
}

// QuestionDTO never carries the correct answer before it is revealed.
type QuestionDTO struct {
	Position     int32    `json:"position"`
	ID           int64    `json:"id"`
	Body         string   `json:"body"`
	Answers      []string `json:"answers"`
	DomainID     int64    `json:"domain_id"`
	DomainName   string   `json:"domain_name"`
	DomainColor  string   `json:"domain_color"`
	CategoryID   int64    `json:"category_id"`
	CategoryName string   `json:"category_name"`
	Difficulty   int32    `json:"difficulty"`
	Points       int32    `json:"points"`
	InLanguage   bool     `json:"in_language"`
}

func questionDTO(q Question) QuestionDTO {
	return QuestionDTO{
		Position: q.Position, ID: q.ID, Body: q.Body, Answers: q.Answers,
		DomainID: q.DomainID, DomainName: q.DomainName, DomainColor: q.DomainColor,
		CategoryID: q.CategoryID, CategoryName: q.CategoryName,
		Difficulty: q.Difficulty, Points: q.Points, InLanguage: q.InLanguage,
	}
}

type GameDTO struct {
	ID            string       `json:"id"`
	Mode          string       `json:"mode"`
	Status        string       `json:"status"`
	QuestionCount int32        `json:"question_count"`
	Seconds       int32        `json:"seconds_per_question"`
	Position      int32        `json:"position"`
	Score         int32        `json:"score"`
	Correct       int32        `json:"correct"`
	FiftyUsed     bool         `json:"fifty_used"`
	SkipUsed      bool         `json:"skip_used"`
	Done          bool         `json:"done"`
	Question      *QuestionDTO `json:"question,omitempty"`
}

type startBody struct {
	DomainID    int64   `json:"domain_id"`
	CategoryIDs []int64 `json:"category_ids"`
	// A node of the universal hierarchy to play from, at any depth, and how much of
	// the branch counts: direct | subtree | both (§10.2).
	NodeID     int64  `json:"node_id"`
	NodeScope  string `json:"node_scope"`
	AllMine    bool   `json:"all_mine"`
	Kind       string `json:"kind"` // quick | custom | mix
	Difficulty int32  `json:"difficulty"`
	Count      int32  `json:"question_count"`
	Seconds    int32  `json:"seconds_per_question"`
}

func (a *API) start(c *gin.Context) {
	var body startBody
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			apiutil.Fail(c, apiutil.BadJSON(err))
			return
		}
	}
	if body.Count == 0 {
		body.Count = QuickGameQuestions
	}
	if body.Seconds == 0 && body.Kind != "no_timer" {
		body.Seconds = 20
	}
	game, err := a.svc.Start(c.Request.Context(), StartInput{
		Who:  authctx.Get(c),
		Mode: sqlc.GameModeSolo,
		Scope: Scope{
			DomainID: body.DomainID, CategoryIDs: body.CategoryIDs,
			NodeID: body.NodeID, NodeScope: body.NodeScope,
			AllMine: body.AllMine, Kind: body.Kind,
		},
		Difficulty: body.Difficulty, Count: body.Count, Seconds: body.Seconds,
		Lang: apiutil.Lang(c),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.respond(c, game, true)
}

func (a *API) state(c *gin.Context) {
	game, ok := a.load(c)
	if !ok {
		return
	}
	a.respond(c, game, false)
}

type answerBody struct {
	Position int32  `json:"position"`
	Choice   *int32 `json:"choice"`
	TimeMS   int32  `json:"time_ms"`
}

func (a *API) answer(c *gin.Context) {
	game, ok := a.load(c)
	if !ok {
		return
	}
	var body answerBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	result, err := a.svc.Answer(c.Request.Context(), game, AnswerInput{
		Position: body.Position, Choice: body.Choice, TimeMS: body.TimeMS,
	}, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{
		"correct":       result.Correct,
		"timed_out":     result.TimedOut,
		"points":        result.Points,
		"correct_index": result.Question.CorrectIndex,
		"explanation":   result.Question.Explanation,
		"source":        result.Question.Source,
		"score":         result.TotalScore,
		"last":          result.Last,
	})
}

func (a *API) fifty(c *gin.Context) {
	game, ok := a.load(c)
	if !ok {
		return
	}
	state, err := a.svc.State(c.Request.Context(), game)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	keep, err := a.svc.Fifty(c.Request.Context(), game, state.Position, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"keep": keep})
}

func (a *API) skip(c *gin.Context) {
	game, ok := a.load(c)
	if !ok {
		return
	}
	state, err := a.svc.State(c.Request.Context(), game)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Skip(c.Request.Context(), game, state.Position); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) finish(c *gin.Context) {
	game, ok := a.load(c)
	if !ok {
		return
	}
	abandon := c.Query("abandon") == "1"
	result, err := a.svc.Finish(c.Request.Context(), game, abandon)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"score": result.Score, "correct": result.Correct, "xp": result.XP})
}

func (a *API) results(c *gin.Context) {
	game, ok := a.load(c)
	if !ok {
		return
	}
	summary, err := a.svc.Results(c.Request.Context(), game, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	review := make([]gin.H, 0, len(summary.Review))
	for _, item := range summary.Review {
		review = append(review, gin.H{
			"question":      questionDTO(item.Question),
			"correct_index": item.Question.CorrectIndex,
			"explanation":   item.Question.Explanation,
			"source":        item.Question.Source,
			"choice":        item.Choice,
			"correct":       item.Correct,
			"points":        item.Points,
			"time_ms":       item.TimeMS,
			"skipped":       item.Skipped,
		})
	}
	byDomain := make([]gin.H, 0, len(summary.ByDomain))
	for _, d := range summary.ByDomain {
		byDomain = append(byDomain, gin.H{
			"domain_id": d.DomainID, "domain_name": d.DomainName,
			"answered": d.Answered, "correct": d.Correct,
		})
	}
	apiutil.OK(c, gin.H{
		"score": summary.Score, "correct": summary.Correct, "total": summary.Total,
		"xp": summary.XP, "accuracy": summary.Accuracy, "average_ms": summary.AverageMS,
		"by_domain": byDomain, "review": review,
	})
}

func (a *API) history(c *gin.Context) {
	page, per := apiutil.PageParams(c, 20, 50)
	rows, total, err := a.svc.History(c.Request.Context(), authctx.Get(c), apiutil.Lang(c), c.Query("mode"), page, per)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{
			"game_id": r.GameID, "mode": r.Mode, "question_count": r.Questions,
			"score": r.Score, "correct": r.Correct, "finished_at": r.FinishedAt,
		})
	}
	apiutil.Paged(c, out, apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

func (a *API) load(c *gin.Context) (Game, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return Game{}, false
	}
	game, err := a.svc.Load(c.Request.Context(), authctx.Get(c), id)
	if err != nil {
		apiutil.Fail(c, err)
		return Game{}, false
	}
	return game, true
}

func (a *API) respond(c *gin.Context, game Game, created bool) {
	state, err := a.svc.State(c.Request.Context(), game)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	dto := GameDTO{
		ID: game.Row.ID.String(), Mode: string(game.Row.Mode), Status: string(game.Row.Status),
		QuestionCount: int32(game.Row.QuestionCount), Seconds: int32(game.Row.SecondsPerQuestion),
		Position: state.Position, Score: state.Score, Correct: state.Correct,
		FiftyUsed: state.FiftyUsed, SkipUsed: state.SkipUsed, Done: state.Done,
	}
	if !state.Done {
		question, err := a.svc.QuestionAt(c.Request.Context(), game.Row.ID, state.Position, apiutil.Lang(c))
		if err != nil {
			apiutil.Fail(c, err)
			return
		}
		// Handing the question over starts its clock on the server (NF-05).
		if _, err := a.svc.Serve(c.Request.Context(), game.Row.ID, state.Position); err != nil {
			apiutil.Fail(c, err)
			return
		}
		q := questionDTO(question)
		dto.Question = &q
	}
	if created {
		apiutil.Created(c, dto)
		return
	}
	apiutil.OK(c, dto)
}

package duel

import (
	"github.com/google/uuid"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// API is endpoints 47–50: the queue, the duel and the player's ratings. The website
// (P17, P18) and the app call the same service (NF-04).
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.POST("/duels/queue", a.search)         // 47
	private.DELETE("/duels/queue", a.cancel)       // 48
	private.GET("/duels/queue", a.state)           // 47, read side: am I still searching?
	private.GET("/duels/:id", a.detail)            // 49
	private.GET("/me/duel-ratings", a.myRatings)   // 50
	private.GET("/duels/:id/opponent", a.opponent) // 49, the score bar on its own
}

func stateDTO(s State) gin.H {
	out := gin.H{
		"searching": s.Searching,
		"rating":    s.Rating,
		"wins":      s.Wins,
		"losses":    s.Losses,
		"questions": Questions,
		"seconds":   Seconds,
		"game_id":   nil,
	}
	if s.GameID != uuid.Nil {
		out["game_id"] = s.GameID
	}
	return out
}

// search is endpoint 47: join the queue, and come back matched when somebody was
// already waiting.
func (a *API) search(c *gin.Context) {
	var body struct {
		DomainID int64 `json:"domain_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && c.Request.ContentLength > 0 {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	state, err := a.svc.Search(c.Request.Context(), authctx.Get(c), body.DomainID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, stateDTO(state))
}

// cancel is endpoint 48.
func (a *API) cancel(c *gin.Context) {
	if err := a.svc.Cancel(c.Request.Context(), authctx.Get(c)); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

func (a *API) state(c *gin.Context) {
	state, err := a.svc.State(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, stateDTO(state))
}

// detail is endpoint 49: the duel as it stands — my side comes from the game
// endpoints (#25, #30), the other side from here, so a score is never invented on
// the client.
func (a *API) detail(c *gin.Context) {
	gameID, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	who := authctx.Get(c)
	opponent, err := a.svc.Opponent(c.Request.Context(), who, gameID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if !opponent.Found {
		apiutil.Fail(c, apperr.NotFound("duel"))
		return
	}
	state, err := a.svc.State(c.Request.Context(), who)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	record, err := a.svc.Record(c.Request.Context(), who, opponent.UserID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{
		"game_id":  gameID,
		"me":       gin.H{"rating": state.Rating, "wins": state.Wins, "losses": state.Losses},
		"opponent": opponentDTO(opponent),
		"head_to_head": gin.H{
			"wins": record.Wins, "losses": record.Losses, "draws": record.Draws,
		},
	})
}

func (a *API) opponent(c *gin.Context) {
	gameID, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	opponent, err := a.svc.Opponent(c.Request.Context(), authctx.Get(c), gameID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if !opponent.Found {
		apiutil.Fail(c, apperr.NotFound("duel"))
		return
	}
	apiutil.OK(c, opponentDTO(opponent))
}

func opponentDTO(o Opponent) gin.H {
	results := o.Results
	if results == nil {
		results = []bool{}
	}
	return gin.H{
		"user_id": o.UserID, "name": o.Name, "level": o.Level,
		"score": o.Score, "done": o.Done, "answered": o.Answered,
		"rating": o.Rating, "wins": o.Wins, "losses": o.Losses,
		"results": results,
	}
}

// myRatings is endpoint 50.
func (a *API) myRatings(c *gin.Context) {
	rows, err := a.svc.MyRatings(c.Request.Context(), authctx.Get(c), apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = ratingDTO(r)
	}
	apiutil.OK(c, out)
}

func ratingDTO(r sqlc.MyDuelRatingsRow) gin.H {
	return gin.H{
		"domain_id": r.DomainID, "domain_slug": r.DomainSlug, "domain_name": r.DomainName,
		"color": r.Color, "icon": r.Icon,
		"rating": r.Rating, "wins": r.Wins, "losses": r.Losses, "draws": r.Draws,
	}
}

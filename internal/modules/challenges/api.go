package challenges

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/apiutil"
	"siraj/internal/modules/games"
	"siraj/internal/platform/authctx"
)

// API is endpoints 51–62 and 118. Every read goes through the same Card / Detail
// builders as the website, which is where CH-09 is enforced: a score that belongs to
// somebody else never leaves the server while the challenge is open.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/challenges", a.list)                       // 51
	private.GET("/challenges/stats", a.stats)                // 52
	private.POST("/challenges", a.create)                    // 53
	private.GET("/challenges/:id", a.detail)                 // 54
	private.POST("/challenges/:id/accept", a.accept)         // 55
	private.POST("/challenges/:id/decline", a.decline)       // 56
	private.DELETE("/challenges/:id", a.cancel)              // 57
	private.POST("/challenges/:id/remind", a.remind)         // 58
	private.POST("/challenges/:id/rematch", a.rematch)       // 59
	private.POST("/challenges/:id/close", a.close)           // 118
	private.POST("/challenges/:id/link", a.makeLink)         // 60, the sharing half
	private.GET("/challenge-links/:code", a.openLink)        // 60
	private.POST("/challenges/random-opponent", a.random)    // 61
	private.POST("/local-rounds", a.localRound)              // 62
	private.GET("/local-rounds/:id", a.localState)           // 62, read side
	private.GET("/local-rounds/:id/results", a.localResults) // 62, the podium of P21
}

func cardDTO(c Card) gin.H {
	players := make([]gin.H, len(c.Players))
	for i, p := range c.Players {
		row := gin.H{
			"user_id": p.UserID, "name": p.Name, "level": p.Level,
			"is_me": p.IsMe, "status": p.Status, "rank": p.Rank, "result": p.Result,
			// nil while the challenge is open and the line is not the viewer's (CH-09).
			"score": p.Score,
		}
		players[i] = row
	}
	out := gin.H{
		"id": c.ID, "creator": c.Creator, "creator_id": c.CreatorID, "mine": c.Mine,
		"status": c.Status, "my_status": c.MyStatus, "my_result": c.MyResult,
		"questions": c.Questions, "difficulty": c.Difficulty, "message": c.Message,
		"category": c.Category, "icon": c.Icon, "color": c.Color,
		"players": players, "played": c.Played, "total": c.Total, "closed": c.Closed(),
		"deadline_at": nil,
	}
	if c.Deadline != nil {
		out["deadline_at"] = c.Deadline.UTC().Format(time.RFC3339)
	}
	return out
}

// list is endpoint 51: ?box=received|sent|finished.
func (a *API) list(c *gin.Context) {
	page, per := apiutil.PageParams(c, 20, 50)
	box := c.DefaultQuery("box", c.DefaultQuery("tab", "received"))
	cards, total, err := a.svc.Box(c.Request.Context(), authctx.Get(c), box, apiutil.Lang(c), page, per)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(cards))
	for i, card := range cards {
		out[i] = cardDTO(card)
	}
	apiutil.Paged(c, out, apiutil.PageInfo{Page: page, PerPage: per, Total: total})
}

// stats is endpoint 52: how many are waiting in each box.
func (a *API) stats(c *gin.Context) {
	received, sent, finished, err := a.svc.Counts(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"received": received, "sent": sent, "finished": finished})
}

// createBody is what P20 collects, as JSON.
type createBody struct {
	FriendIDs   []string `json:"friend_ids"`
	Locals      []string `json:"locals"`
	Source      string   `json:"source"` // bank | set | written
	SetID       string   `json:"set_id"`
	DomainID    int64    `json:"domain_id"`
	CategoryIDs []int64  `json:"category_ids"`
	// A node of the universal tree to play from, at any depth, and how much of the
	// branch counts: direct | subtree | both (§10.2). Omitted means the whole branch.
	NodeID     int64  `json:"node_id"`
	NodeScope  string `json:"node_scope"`
	Difficulty int32  `json:"difficulty"`
	Count      int32  `json:"question_count"`
	Seconds    int32  `json:"seconds"`
	Message    string `json:"message"`
	Days       int32  `json:"days"`
	Teams      bool   `json:"teams"`
}

func (b createBody) input(c *gin.Context) (CreateInput, error) {
	in := CreateInput{
		Who:    authctx.Get(c),
		Source: b.Source,
		Scope: games.Scope{
			Kind: "challenge", DomainID: b.DomainID, CategoryIDs: b.CategoryIDs,
			// A branch of the universal tree, and how much of it counts. An unknown
			// or absent scope reads as the whole branch (games.Scope.nodeScope).
			NodeID: b.NodeID, NodeScope: b.NodeScope,
		},
		Difficulty: b.Difficulty, Count: b.Count, Seconds: b.Seconds,
		Message: b.Message, Days: b.Days, Lang: apiutil.Lang(c), Locals: b.Locals,
	}
	if in.Source == "" {
		in.Source = "bank"
	}
	if b.SetID != "" {
		id, err := uuid.Parse(b.SetID)
		if err != nil {
			return in, apiutil.BadJSON(err)
		}
		in.SetID = id
	}
	for _, raw := range b.FriendIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return in, apiutil.BadJSON(err)
		}
		in.FriendIDs = append(in.FriendIDs, id)
	}
	return in, nil
}

// create is endpoint 53. CH-03 — the creator plays first — is the client's call on
// the API: the game is started with #55 right after.
func (a *API) create(c *gin.Context) {
	var body createBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	in, err := body.input(c)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	challenge, err := a.svc.Create(c.Request.Context(), in)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	card, err := a.svc.One(c.Request.Context(), in.Who, challenge.ID, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, cardDTO(card))
}

// detail is endpoint 54: waiting while it is open, the standings once it closes.
func (a *API) detail(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	d, err := a.svc.Detail(c.Request.Context(), authctx.Get(c), id, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := cardDTO(d.Card)
	removed := make([]gin.H, len(d.Removed))
	for i, p := range d.Removed {
		removed[i] = gin.H{"user_id": p.UserID, "name": p.Name, "status": p.Status}
	}
	ranked := make([]gin.H, len(d.Ranked))
	for i, p := range d.Ranked {
		ranked[i] = gin.H{
			"user_id": p.UserID, "name": p.Name, "rank": p.Rank,
			"result": p.Result, "score": p.Score,
		}
	}
	out["is_host"] = d.IsHost
	out["playing"] = d.Playing
	out["not_started"] = d.NotStart
	out["removed"] = removed
	out["ranked"] = ranked
	out["can_close"] = d.CanClose
	out["blocker"] = d.Blocker
	out["will_remove"] = d.WillRemove
	out["my_rank"] = d.MyRank
	out["won"] = d.Won
	out["tied"] = d.Tied
	// "mine" already means "I created this challenge" on the card; the viewer's own
	// line is "me", so one field never changes type between two reads of the same
	// challenge.
	if d.HasMine {
		out["me"] = gin.H{
			"status": d.Mine.Status, "score": d.Mine.Score,
			"rank": d.Mine.Rank, "result": d.Mine.Result,
		}
	}
	if d.Closed() {
		out["winner"] = gin.H{"user_id": d.Winner.UserID, "name": d.Winner.Name, "score": d.Winner.Score}
	}
	apiutil.OK(c, out)
}

// accept is endpoint 55: it deals this player's copy of the frozen questions and
// hands back the game to play with #25 and #26.
func (a *API) accept(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	game, err := a.svc.Accept(c.Request.Context(), authctx.Get(c), id, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{
		"game_id": game.Row.ID, "question_count": game.Row.QuestionCount,
		"seconds": game.Row.SecondsPerQuestion,
	})
}

// decline is endpoint 56.
func (a *API) decline(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Decline(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// cancel is endpoint 57.
func (a *API) cancel(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Cancel(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// remind is endpoint 58: it answers how many players were nudged.
func (a *API) remind(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	sent, err := a.svc.Remind(c.Request.Context(), authctx.Get(c), id)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"reminded": sent})
}

// rematch is endpoint 59: the same players, the same settings, new questions.
func (a *API) rematch(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	who := authctx.Get(c)
	challenge, err := a.svc.Rematch(c.Request.Context(), who, id, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	card, err := a.svc.One(c.Request.Context(), who, challenge.ID, apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, cardDTO(card))
}

// close is endpoint 118: the host closing early (CH-07, D-03).
func (a *API) close(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.CloseEarly(c.Request.Context(), authctx.Get(c), id); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.detail(c)
}

// makeLink is the sharing half of endpoint 60 (CH-10): a link that lasts 24 hours.
func (a *API) makeLink(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	link, err := a.svc.ShareLink(c.Request.Context(), authctx.Get(c), id)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, gin.H{
		"code": link.Code, "challenge_id": link.ChallengeID,
		"expires_at": link.ExpiresAt.Time.UTC().Format(time.RFC3339),
		"path":       "/challenge-links/" + link.Code,
	})
}

// openLink is endpoint 60: opening a shared link joins the challenge.
func (a *API) openLink(c *gin.Context) {
	card, err := a.svc.OpenLink(c.Request.Context(), authctx.Get(c), c.Param("code"), apiutil.Lang(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, cardDTO(card))
}

// random is endpoint 61: a real player to challenge, never an invented one.
func (a *API) random(c *gin.Context) {
	opponent, err := a.svc.RandomOpponent(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{
		"user_id": opponent.UserID, "name": opponent.Name,
		"username": opponent.Username, "level": opponent.Level,
	})
}

// localRound is endpoint 62: a pass-the-device round saved on the server, so every
// turn is scored there and nothing is kept in the browser (DV-02…DV-05, NF-05).
func (a *API) localRound(c *gin.Context) {
	var body createBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	in, err := body.input(c)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	gameID, err := a.svc.StartDeviceRound(c.Request.Context(), in, body.Teams)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	state, err := a.svc.DeviceState(c.Request.Context(), gameID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, deviceStateDTO(state))
}

// localState is where a round has got to: whose turn it is, and what the table may
// see of this question (DV-04).
func (a *API) localState(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	state, err := a.svc.DeviceState(c.Request.Context(), id)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, deviceStateDTO(state))
}

// localResults is the podium of P21.
func (a *API) localResults(c *gin.Context) {
	id, err := apiutil.UUIDParam(c, "id")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	res, err := a.svc.DeviceResults(c.Request.Context(), id)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{
		"questions": res.Questions, "teams": res.Teams, "totals": res.Totals,
		"winner": res.Winner, "diff": res.Diff, "players": devicePlayerDTOs(res.Players),
	})
}

func deviceStateDTO(s DeviceState) gin.H {
	out := gin.H{
		"game_id": s.GameID, "position": s.Position, "questions": s.Questions,
		"revealed": s.Revealed, "done": s.Done, "fastest": s.Fastest,
		"teams": s.Teams, "totals": s.Totals, "gains": s.Gains,
		"players": devicePlayerDTOs(s.Players), "turn": nil,
	}
	if s.Turn != nil {
		out["turn"] = gin.H{"player_id": s.Turn.ID, "name": s.Turn.Name}
	}
	return out
}

func devicePlayerDTOs(players []DevicePlayer) []gin.H {
	out := make([]gin.H, len(players))
	for i, p := range players {
		out[i] = gin.H{
			"player_id": p.ID, "name": p.Name, "score": p.Score, "correct": p.Correct,
			"team": p.Team, "answered": p.Answered, "right": p.Right, "gain": p.Gain,
			"time_ms": p.TimeMS, "now": p.Now, "choice": p.Choice,
		}
	}
	return out
}

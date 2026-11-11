package rooms

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// API is endpoints 36–46 and 119: a live room over JSON, on the same service the
// lobby and the live board use (NF-04). Nothing is kept in memory: every answer is
// read from the room, its members and its game.
type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.POST("/rooms", a.create)                       // 36
	private.GET("/rooms", a.open)                          // 36, read side: rooms I can join
	private.GET("/rooms/:code", a.get)                     // 37
	private.POST("/rooms/:code/join", a.join)              // 38
	private.POST("/rooms/:code/leave", a.leave)            // 39
	private.PATCH("/rooms/:code", a.patch)                 // 40
	private.POST("/rooms/:code/ready", a.ready)            // 41
	private.DELETE("/rooms/:code/players/:userId", a.kick) // 42
	private.POST("/rooms/:code/start", a.start)            // 43
	private.GET("/rooms/:code/messages", a.messages)       // 44
	private.POST("/rooms/:code/messages", a.say)           // 45
	private.GET("/rooms/:code/podium", a.podium)           // 46
	private.POST("/rooms/:code/teams/shuffle", a.shuffle)  // 119
	private.POST("/rooms/:code/teams/move", a.move)        // 119 (the other half of RM-05)
	private.GET("/rooms/:code/standings", a.standings)     // the live board of P15
	private.POST("/rooms/:code/again", a.again)            // "play again", as the podium offers
}

// room reads the room of the request and checks the player may see it.
func (a *API) room(c *gin.Context) (sqlc.Room, bool) {
	code := strings.ToUpper(strings.TrimSpace(c.Param("code")))
	room, err := a.svc.ByCode(c.Request.Context(), code)
	if err != nil {
		apiutil.Fail(c, err)
		return sqlc.Room{}, false
	}
	return room, true
}

func (a *API) roomDTO(c *gin.Context, room sqlc.Room) (gin.H, error) {
	ctx := c.Request.Context()
	who := authctx.Get(c)
	members, err := a.svc.Members(ctx, room)
	if err != nil {
		return nil, err
	}
	set := a.svc.Settings(room)
	players := make([]gin.H, len(members))
	ready := 0
	for i, m := range members {
		if m.IsReady {
			ready++
		}
		players[i] = gin.H{
			"user_id": m.UserID, "name": m.DisplayName, "level": m.Level,
			"is_host": m.IsHost, "is_ready": m.IsReady, "is_guest": m.Kind == sqlc.UserKindGuest,
			"is_me": who != nil && m.UserID == who.ID,
		}
	}
	out := gin.H{
		"code": strings.TrimSpace(room.Code), "status": room.Status,
		"host_id": room.HostID, "is_host": who != nil && room.HostID == who.ID,
		"max_players": room.MaxPlayers, "chat_enabled": room.ChatEnabled,
		"is_guest_room": room.IsGuestRoom,
		"settings": gin.H{
			"domain_id": set.Scope.DomainID, "category_ids": set.Scope.CategoryIDs,
			"difficulty": set.Difficulty, "question_count": set.Count, "seconds": set.Seconds,
			"mode": a.svc.Mode(set, len(members)), "allow_guests": !set.GuestsOff,
		},
		"players": players, "ready_count": ready, "player_count": len(members),
	}
	if sides, err := a.svc.Teams(ctx, room); err == nil && a.svc.Mode(set, len(members)) == ModeTeams {
		out["teams"] = [][]uuid.UUID{sides[0], sides[1]}
	}
	if game, ok := a.svc.Game(ctx, room); ok {
		out["game_id"] = game.ID
	}
	return out, nil
}

// create is endpoint 36.
func (a *API) create(c *gin.Context) {
	var body struct {
		DomainID    int64   `json:"domain_id"`
		CategoryIDs []int64 `json:"category_ids"`
		Difficulty  int32   `json:"difficulty"`
		Count       int32   `json:"question_count"`
		Seconds     int32   `json:"seconds"`
		MaxPlayers  int32   `json:"max_players"`
		Chat        *bool   `json:"chat_enabled"`
		Mode        string  `json:"mode"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	chat := true
	if body.Chat != nil {
		chat = *body.Chat
	}
	room, err := a.svc.Host(c.Request.Context(), HostInput{
		Who:        authctx.Get(c),
		Scope:      games.Scope{Kind: "live", DomainID: body.DomainID, CategoryIDs: body.CategoryIDs},
		Difficulty: body.Difficulty, Count: body.Count, Seconds: body.Seconds,
		MaxPlayers: body.MaxPlayers, Chat: chat, Mode: body.Mode, Lang: apiutil.Lang(c),
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	dto, err := a.roomDTO(c, room)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.Created(c, dto)
}

// open lists the rooms this player can walk into: their friends' open rooms, the
// same list P13 shows under the code box.
func (a *API) open(c *gin.Context) {
	rows, err := a.svc.OpenRooms(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = gin.H{
			"code": strings.TrimSpace(r.Code), "host_id": r.HostID, "host": r.HostName,
			"players": r.Players, "max_players": r.Max, "teams": r.Teams,
			"domain_id": r.DomainID,
		}
	}
	apiutil.OK(c, out)
}

// get is endpoint 37.
func (a *API) get(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	dto, err := a.roomDTO(c, room)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dto)
}

// join is endpoint 38.
func (a *API) join(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	if err := a.svc.Join(c.Request.Context(), authctx.Get(c), room); err != nil {
		apiutil.Fail(c, err)
		return
	}
	dto, err := a.roomDTO(c, room)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dto)
}

// leave is endpoint 39.
func (a *API) leave(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	if err := a.svc.Leave(c.Request.Context(), authctx.Get(c), room); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// patch is endpoint 40: the host's settings panel, and the room mode with it.
func (a *API) patch(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	var body struct {
		Count       *int32  `json:"question_count"`
		Seconds     *int32  `json:"seconds"`
		AllowGuests *bool   `json:"allow_guests"`
		Mode        *string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	ctx, who := c.Request.Context(), authctx.Get(c)
	set := a.svc.Settings(room)
	if body.Count != nil || body.Seconds != nil || body.AllowGuests != nil {
		in := SettingsInput{Count: set.Count, Seconds: set.Seconds, AllowGuests: !set.GuestsOff}
		if body.Count != nil {
			in.Count = *body.Count
		}
		if body.Seconds != nil {
			in.Seconds = *body.Seconds
		}
		if body.AllowGuests != nil {
			in.AllowGuests = *body.AllowGuests
		}
		if err := a.svc.UpdateSettings(ctx, who, room, in); err != nil {
			apiutil.Fail(c, err)
			return
		}
	}
	if body.Mode != nil {
		if err := a.svc.SetMode(ctx, who, room, *body.Mode); err != nil {
			apiutil.Fail(c, err)
			return
		}
	}
	// Read the room again: the settings were written onto it.
	room, err := a.svc.Room(ctx, room.ID)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	dto, err := a.roomDTO(c, room)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dto)
}

// ready is endpoint 41.
func (a *API) ready(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	var body struct {
		Ready *bool `json:"ready"`
	}
	_ = c.ShouldBindJSON(&body)
	ready := true
	if body.Ready != nil {
		ready = *body.Ready
	}
	if err := a.svc.Ready(c.Request.Context(), authctx.Get(c), room, ready); err != nil {
		apiutil.Fail(c, err)
		return
	}
	dto, err := a.roomDTO(c, room)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dto)
}

// kick is endpoint 42.
func (a *API) kick(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	userID, err := apiutil.UUIDParam(c, "userId")
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	if err := a.svc.Kick(c.Request.Context(), authctx.Get(c), room, userID); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// start is endpoint 43: one game for the whole room.
func (a *API) start(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	game, err := a.svc.Start(c.Request.Context(), authctx.Get(c), room)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{"game_id": game.ID, "question_count": game.QuestionCount, "seconds": game.SecondsPerQuestion})
}

// messages is endpoint 44.
func (a *API) messages(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	who := authctx.Get(c)
	if member, err := a.member(c, room, who); err != nil || !member {
		apiutil.Fail(c, apperr.Forbidden("Join the room first"))
		return
	}
	lines, err := a.svc.Chat(c.Request.Context(), room)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := make([]gin.H, len(lines))
	for i, l := range lines {
		out[i] = gin.H{
			"author": l.Author, "body": l.Body, "kind": l.Kind, "system": l.System,
			"file_name": l.FileName, "file_size": l.FileSize, "file_url": l.FileURL,
			"duration": l.Duration, "at": l.When,
		}
	}
	apiutil.OK(c, out)
}

// say is endpoint 45.
func (a *API) say(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	var body struct {
		Body     string `json:"body"`
		UploadID string `json:"upload_id"`
		Kind     string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	upload := uuid.Nil
	if body.UploadID != "" {
		id, err := uuid.Parse(body.UploadID)
		if err != nil {
			apiutil.Fail(c, apperr.BadRequest("bad_id", "upload_id must be a UUID"))
			return
		}
		upload = id
	}
	if err := a.svc.SayWith(c.Request.Context(), authctx.Get(c), room, body.Body, upload, body.Kind); err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.NoContent(c)
}

// podium is endpoint 46: the final ranking, with the two sides when the room played
// in teams (RM-05, RM-07).
func (a *API) podium(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	game, running := a.svc.Game(c.Request.Context(), room)
	if !running {
		apiutil.Fail(c, apperr.Conflict("not_started", "This room has not played yet"))
		return
	}
	rows, err := a.svc.Standings(c.Request.Context(), authctx.Get(c), game.ID, -1)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	out := gin.H{
		"code": strings.TrimSpace(room.Code), "game_id": game.ID,
		"questions": game.QuestionCount, "rows": standingDTOs(rows), "teams": HasTeams(rows),
	}
	if HasTeams(rows) {
		totals, _, _ := TeamTotals(rows)
		winner := -1
		switch {
		case totals[0] > totals[1]:
			winner = 0
		case totals[1] > totals[0]:
			winner = 1
		}
		out["team_totals"] = totals
		out["winner"] = winner
	}
	apiutil.OK(c, out)
}

// standings is the live board of P15: where everyone stands on the question the
// caller names (?position=), or on the game as a whole when they do not.
func (a *API) standings(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	game, running := a.svc.Game(c.Request.Context(), room)
	if !running {
		apiutil.Fail(c, apperr.Conflict("not_started", "This room has not played yet"))
		return
	}
	position := int32(-1)
	if n, err := strconv.Atoi(c.Query("position")); err == nil {
		position = int32(n)
	}
	rows, err := a.svc.Standings(c.Request.Context(), authctx.Get(c), game.ID, position)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	done, total, err := a.svc.Answered(c.Request.Context(), game.ID, position)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, gin.H{
		"game_id": game.ID, "position": position, "answered": done, "players": total,
		"rows": standingDTOs(rows),
	})
}

// shuffle is endpoint 119: the host dealing the two sides again.
func (a *API) shuffle(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	if err := a.svc.Shuffle(c.Request.Context(), authctx.Get(c), room); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.get(c)
}

// move is a player crossing to the other side (RM-05).
func (a *API) move(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	var body struct {
		Side int `json:"side"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	if err := a.svc.Move(c.Request.Context(), authctx.Get(c), room, body.Side); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.get(c)
}

// again reopens the room for another round, which is what the podium's button does.
func (a *API) again(c *gin.Context) {
	room, ok := a.room(c)
	if !ok {
		return
	}
	if err := a.svc.Reopen(c.Request.Context(), authctx.Get(c), room); err != nil {
		apiutil.Fail(c, err)
		return
	}
	a.get(c)
}

func (a *API) member(c *gin.Context, room sqlc.Room, who *authctx.Principal) (bool, error) {
	if who == nil {
		return false, nil
	}
	members, err := a.svc.Members(c.Request.Context(), room)
	if err != nil {
		return false, err
	}
	for _, m := range members {
		if m.UserID == who.ID {
			return true, nil
		}
	}
	return false, nil
}

func standingDTOs(rows []Standing) []gin.H {
	out := make([]gin.H, len(rows))
	for i, r := range rows {
		out[i] = gin.H{
			"rank": i + 1, "user_id": r.UserID, "name": r.Name, "score": r.Score,
			"correct": r.Correct, "answered": r.Answered, "done": r.Done, "is_me": r.IsMe,
			"is_guest": r.Guest, "team": r.Team, "streak": r.Streak, "xp": r.XP,
			"avg_ms": r.AvgMs, "best_ms": r.BestMs,
			"answered_this": r.AnsweredThis, "correct_this": r.CorrectThis, "gain": r.Gain,
		}
	}
	return out
}

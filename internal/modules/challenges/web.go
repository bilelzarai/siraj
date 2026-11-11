package challenges

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/render"
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// Setup is what the challenges screens borrow from the game setup screen: the topic
// picker of P09 is the same control on P20 (FLOWS §5).
type Setup interface {
	ReadSetup(c *gin.Context, v *view.Ctx) (pages.SetupData, games.Scope, int32, error)
}

// Friends is what P20 needs from the social module to list who can be challenged.
type Friends interface {
	FriendCards(c *gin.Context, v *view.Ctx) []pages.RoundFriend
}

// Sets is what P20 needs from the My questions module (MQ-05): the sets it can play,
// and — when the round writes its own questions — the set they go into.
type Sets interface {
	SetCards(c *gin.Context, v *view.Ctx) []pages.RoundSet
	DraftFor(c *gin.Context, v *view.Ctx, setID string) (string, error)
	// NewQuestionDrawer is the editor over this screen, so writing the first
	// question of a round never navigates away from it (MQ-05).
	NewQuestionDrawer(c *gin.Context, v *view.Ctx, setID, back string) (templ.Component, bool)
	RoundQuestions(c *gin.Context, v *view.Ctx, setID string) ([]pages.SetQuestionRow, int)
}

type Web struct {
	svc     *Service
	games   *games.Service
	setup   Setup
	friends Friends
	sets    Sets
}

func NewWeb(s *Service, g *games.Service, setup Setup, f Friends, sets Sets) *Web {
	return &Web{svc: s, games: g, setup: setup, friends: f, sets: sets}
}

const perPage = 12

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/challenges", w.list)
	private.GET("/challenges/new", w.newRound)
	private.POST("/challenges/new/add-player", w.addPlayer)
	private.POST("/challenges/new/write", w.writeQuestion)
	private.POST("/challenges", w.create)
	private.POST("/challenges/random", w.random)
	private.POST("/challenges/:id/accept", w.accept)
	private.POST("/challenges/:id/decline", w.decline)
	private.POST("/challenges/:id/cancel", w.cancel)
	private.POST("/challenges/:id/remind", w.remind)
	// The challenge's own page: waiting while it is open, results once it closes
	// (CH-04, CH-08), and the two things that end it (CH-07, CH-08).
	private.GET("/challenges/:id", w.detail)
	private.POST("/challenges/:id/close", w.closeEarly)
	private.POST("/challenges/:id/rematch", w.rematch)
	private.GET("/challenges/:id/accept", w.accept)
	private.GET("/round/device", w.deviceEntry)
	private.GET("/round/device/:id", w.device)
	private.GET("/round/device/:id/reveal", w.deviceReveal)
	private.POST("/round/device/:id/turn", w.deviceTurn)
	private.POST("/round/device/:id/next", w.deviceNext)
	private.POST("/round/device/:id/end", w.deviceEnd)
	private.GET("/round/device/:id/results", w.deviceResults)
	private.POST("/round/device/again", w.deviceAgain)
}

// list is P19: received, sent and finished (CH-02).
func (w *Web) list(c *gin.Context) {
	v := view.From(c)
	box := c.DefaultQuery("box", "received")
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	cards, total, err := w.svc.Box(c.Request.Context(), v.User, box, v.Lang, page, perPage)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	received, sent, finished, err := w.svc.Counts(c.Request.Context(), v.User)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := pages.ChallengesData{
		Box: box, Received: received, Sent: sent, Finished: finished,
		Page: page, Pages: (total + perPage - 1) / perPage,
	}
	if d.Pages < 1 {
		d.Pages = 1
	}
	for _, card := range cards {
		d.Cards = append(d.Cards, w.card(v, card))
	}
	render.Page(c, pages.Challenges(v, d))
}

// card turns the service's hidden-score Card into what the page draws. It never
// invents a score: an empty string is "still waiting" (CH-09).
func (w *Web) card(v *view.Ctx, c Card) pages.ChallengeCard {
	out := pages.ChallengeCard{
		ID: c.ID.String(), Icon: iconOr(c.Icon, "swords"), Color: colorOr(c.Color),
		Message: c.Message, Mine: c.Mine, Closed: c.Closed(), MyResult: c.MyResult,
		Questions: c.Questions,
		// Only a challenge somebody else sent you is yours to accept or decline; the
		// one you sent is simply waiting for them (CH-09).
		Pending: !c.Mine && c.MyStatus == string(sqlc.ParticipantStatusPending) && !c.Closed(),
		Sent:    c.Mine && !c.Closed(),
		// CH-03 has the creator play at once, but a round they left half-finished
		// must not become a dead end: their own turn stays reachable.
		PlayMine: c.Mine && !c.Closed() &&
			c.MyStatus != string(sqlc.ParticipantStatusPlayed) &&
			c.MyStatus != string(sqlc.ParticipantStatusDeclined),
	}
	parts := []string{}
	if c.Category != "" {
		parts = append(parts, c.Category)
	}
	parts = append(parts, difficultyName(v, c.Difficulty))
	parts = append(parts, v.Tr("domain.questions_count", "count", c.Questions))
	out.Title = strings.Join(parts, " · ")
	if c.Deadline != nil && !c.Closed() {
		out.Expires = expiresIn(v, *c.Deadline)
	}
	// A challenge you received reads "Omar K. vs You"; one you sent reads
	// "You vs Omar K." — you are always on the side that acts next.
	ordered := make([]Player, 0, len(c.Players))
	for _, p := range c.Players {
		if p.IsMe == c.Mine {
			ordered = append(ordered, p)
		}
	}
	for _, p := range c.Players {
		if p.IsMe != c.Mine {
			ordered = append(ordered, p)
		}
	}
	for _, p := range ordered {
		row := pages.ChallengePlayer{
			Name: p.Name, Initials: avatar.Initials(p.Name), Color: avatar.Color(p.UserID.String()),
			IsMe: p.IsMe, Result: p.Result,
			Played: p.Status == string(sqlc.ParticipantStatusPlayed),
		}
		if p.Score != nil {
			row.Score = fmt.Sprint(*p.Score)
		}
		out.Players = append(out.Players, row)
	}
	if !c.Closed() && c.Played < c.Total {
		out.Waiting = v.Tr("challenges.waiting", "done", c.Played, "total", c.Total)
	}
	return out
}

// newRound is P20 / P20b: who plays, the source, the topic picker and the settings.
func (w *Web) newRound(c *gin.Context) {
	v := view.From(c)
	d, err := w.round(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Page(c, pages.NewRound(v, d))
}

// addPlayer adds a name to a pass-the-device round and redraws the screen (RM-06).
func (w *Web) addPlayer(c *gin.Context) {
	v := view.From(c)
	locals := append(c.PostFormArray("local"), strings.TrimSpace(c.PostForm("new_local")))
	render.Redirect(c, w.roundURL(c, "device", locals))
	_ = v
}

func (w *Web) roundURL(c *gin.Context, who string, locals []string) string {
	q := c.Request.URL.Query()
	q.Set("who", who)
	q.Del("local")
	for _, name := range locals {
		if name = strings.TrimSpace(name); name != "" {
			q.Add("local", name)
		}
	}
	for _, key := range []string{"source", "all_mine", "domain", "view", "difficulty", "count", "seconds", "set_id"} {
		if val := c.PostForm(key); val != "" {
			q.Set(key, val)
		}
	}
	for _, id := range c.PostFormArray("category") {
		q.Add("category", id)
	}
	return "/challenges/new?" + q.Encode()
}

// round reads the whole screen's state out of the query string, so every control is
// an ordinary link and the page works without JavaScript.
func (w *Web) round(c *gin.Context, v *view.Ctx) (pages.RoundData, error) {
	setup, _, _, err := w.setup.ReadSetup(c, v)
	if err != nil {
		return pages.RoundData{}, err
	}
	setup.Room = false
	setup.Path = "/challenges/new"
	// Every control on this screen is a link; Extra carries the ones the topic picker
	// does not own, so changing a topic never drops the friends or the source.
	setup.Extra = roundExtra(c)
	d := pages.RoundData{
		Who: c.DefaultQuery("who", "friends"), Source: c.DefaultQuery("source", "bank"),
		Setup: setup, Picked: map[string]bool{}, SetID: c.Query("set_id"),
		Message: c.Query("message"),
	}
	switch d.Who {
	case "room", "device":
	default:
		d.Who = "friends"
	}
	switch d.Source {
	case "set", "written":
	default:
		d.Source = "bank"
	}
	d.Days = 3
	if n, err := strconv.Atoi(c.Query("days")); err == nil && (n == 1 || n == 3 || n == 7) {
		d.Days = int32(n)
	}
	for _, name := range c.QueryArray("local") {
		if name = strings.TrimSpace(name); name != "" && len(d.Locals) < MaxPlayers-1 {
			d.Locals = append(d.Locals, name)
		}
	}
	if drop, err := strconv.Atoi(c.Query("drop")); err == nil && drop >= 0 && drop < len(d.Locals) {
		d.Locals = append(d.Locals[:drop], d.Locals[drop+1:]...)
	}
	for _, id := range c.QueryArray("friend") {
		d.Picked[id] = true
	}
	if w.friends != nil && d.Who == "friends" {
		d.Friends = w.friends.FriendCards(c, v)
		for i := range d.Friends {
			d.Friends[i].Picked = d.Picked[d.Friends[i].ID]
		}
	}
	d.Min = MinSetQuestions
	d.BackHref = roundHref(c)
	if w.sets != nil && d.Source == "set" {
		for _, set := range w.sets.SetCards(c, v) {
			if set.Ready {
				d.Sets = append(d.Sets, set)
			} else {
				d.Drafts++
			}
		}
		// A set that is no longer playable must not stay picked.
		if d.SetID != "" {
			found := false
			for _, set := range d.Sets {
				found = found || set.ID == d.SetID
			}
			if !found {
				d.SetID = ""
			}
		}
	}
	if w.sets != nil && d.Source == "written" && d.SetID != "" {
		d.WrittenSetID = d.SetID
		d.Written, d.WrittenCount = w.sets.RoundQuestions(c, v, d.SetID)
	}

	d.Players = 1
	switch d.Who {
	case "device":
		d.Players += len(d.Locals)
		if d.Players < 2 {
			d.Problem = v.Tr("round.need_a_name")
		}
	case "friends":
		for _, f := range d.Friends {
			if f.Picked {
				d.Players++
			}
		}
		if d.Players < 2 {
			d.Problem, d.NeedFriend = v.Tr("round.need_a_friend"), true
		}
	default:
		d.Problem = v.Tr("round.room_first")
	}
	// "One of my sets" needs a set picked; "Write them now" makes its own as soon as
	// the first question is written, so it asks for questions instead (MQ-05).
	if d.Source == "set" && d.SetID == "" {
		d.Problem = v.Tr("round.need_a_set")
	}
	// A round deals the same questions to everybody, so the bank has to hold the whole
	// count before the button opens (CH-01, GP-02).
	if d.Problem == "" && d.Source == "bank" && d.Setup.Available < int(d.Setup.Count) {
		d.Problem = v.Tr("setup.fewer_questions", "available", d.Setup.Available, "count", d.Setup.Count)
	}
	// Questions written on the spot follow the same five-question rule (MQ-02).
	if d.Problem == "" && d.Source == "written" && d.WrittenCount < MinSetQuestions {
		d.Problem = v.Tr("sets.add_more", "n", MinSetQuestions-d.WrittenCount, "min", MinSetQuestions)
	}
	return d, nil
}

// roundHref is this screen's own address, kept so the question editor can come back
// to it with everything the player had already chosen.
func roundHref(c *gin.Context) string {
	if q := c.Request.URL.RawQuery; q != "" {
		return "/challenges/new?" + q
	}
	return "/challenges/new"
}

// writeQuestion opens the question editor for a "write them now" round, making the
// round's own set the first time it is asked for (MQ-05).
func (w *Web) writeQuestion(c *gin.Context) {
	v := view.From(c)
	if w.sets == nil {
		w.fail(c, v, apperr.NotFound("set"))
		return
	}
	setID, err := w.sets.DraftFor(c, v, c.PostForm("set_id"))
	if err != nil {
		w.fail(c, v, err)
		return
	}
	back := backToRound(c)
	back = pages.WithQuery(back, "set_id", setID)
	// The first question of a round opens the same drawer as every one after it: the
	// set behind the scenes is made on the way, not on a screen of its own (MQ-05).
	if render.IsHTMX(c) {
		if drawer, ok := w.sets.NewQuestionDrawer(c, v, setID, back); ok {
			render.Page(c, drawer)
			return
		}
	}
	render.Redirect(c, pages.WithBack("/my-questions/"+setID+"/q/new", back))
}

// create sends the challenge, or starts the pass-the-device round straight away.
func (w *Web) create(c *gin.Context) {
	v := view.From(c)
	data, scope, difficulty, err := w.setup.ReadSetup(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	count := int32(10)
	if n, err := strconv.Atoi(c.PostForm("count")); err == nil {
		count = int32(n)
	}
	// The screen already locks the button; a posted form that asks for more questions
	// than the choice holds goes back to the picker with the reason (CH-01).
	if c.DefaultPostForm("source", "bank") == "bank" && data.Available < int(count) {
		render.Toast(c, v.Tr("setup.fewer_questions", "available", data.Available, "count", count))
		render.Redirect(c, backToRound(c))
		return
	}
	seconds := int32(20)
	if n, err := strconv.Atoi(c.PostForm("seconds")); err == nil && n > 0 {
		seconds = int32(n)
	}
	in := CreateInput{
		Who: v.User, Source: c.DefaultPostForm("source", "bank"), Scope: scope,
		Difficulty: difficulty, Count: count, Seconds: seconds,
		Message: strings.TrimSpace(c.PostForm("message")), Lang: v.Lang,
	}
	if id, err := uuid.Parse(c.PostForm("set_id")); err == nil {
		in.SetID = id
	}
	if n, err := strconv.Atoi(c.PostForm("days")); err == nil {
		in.Days = int32(n)
	}

	if c.PostForm("who") == "device" {
		w.startDevice(c, v, in)
		return
	}
	for _, raw := range c.PostFormArray("friend") {
		if id, err := uuid.Parse(raw); err == nil {
			in.FriendIDs = append(in.FriendIDs, id)
		}
	}
	challenge, err := w.svc.Create(c.Request.Context(), in)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("challenges.sent_toast", "count", len(in.FriendIDs)))
	// CH-03: the creator plays first. Their round starts here, so the Sent box only
	// ever holds challenges they have already answered.
	game, err := w.svc.Accept(c.Request.Context(), v.User, challenge.ID, v.Lang)
	if err != nil {
		render.Redirect(c, "/challenges?box=sent")
		return
	}
	render.Redirect(c, "/games/"+game.Row.ID.String())
}

// startDevice deals one game for the whole phone: the host plus every name added,
// each answering the same questions in turn (RM-06).
func (w *Web) startDevice(c *gin.Context, v *view.Ctx, in CreateInput) {
	for _, name := range c.PostFormArray("local") {
		if name = strings.TrimSpace(name); name != "" {
			in.Locals = append(in.Locals, name)
		}
	}
	// Two to ten players named once each, the questions frozen, and the two sides
	// dealt when the setup screen asked for them: all of it in the service, so the
	// API builds the very same round (DV-01, NF-04).
	gameID, err := w.svc.StartDeviceRound(c.Request.Context(), in, c.PostForm("mode") == "teams")
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/round/device/"+gameID.String())
}

// deviceEntry sends someone who typed the route straight to the setup screen.
func (w *Web) deviceEntry(c *gin.Context) {
	render.Redirect(c, "/challenges/new?who=device")
}

// device is P21a: the pass screen between two answers of the same question. A round
// on one device goes question by question, so this names whoever holds the phone next
// and shows nothing of what the others picked (DV-02, DV-03).
func (w *Web) device(c *gin.Context) {
	v := view.From(c)
	state, ok := w.deviceState(c, v)
	if !ok {
		return
	}
	if state.Done {
		render.Redirect(c, "/round/device/"+state.GameID.String()+"/results")
		return
	}
	if state.Turn == nil {
		// Everyone has answered: the table turns the question over (DV-04).
		render.Redirect(c, deviceRevealURL(state.GameID, state.Position))
		return
	}
	// The answer that completed a question comes back here; the table sees that
	// question turned over before the next one is passed around (DV-02, DV-04).
	if n, err := strconv.Atoi(c.Query("answered")); err == nil {
		done, err := w.svc.DeviceStateAt(c.Request.Context(), state.GameID, int32(n))
		if err == nil && done.Revealed {
			render.Redirect(c, deviceRevealURL(state.GameID, int32(n)))
			return
		}
	}
	render.Page(c, pages.PassTheDevice(v, w.deviceTurnData(v, state)))
}

// deviceReveal is DV-04: the right answer, where everybody's pick landed, who was
// quickest, and the standings before the next question.
func (w *Web) deviceReveal(c *gin.Context) {
	v := view.From(c)
	state, ok := w.deviceState(c, v)
	if !ok {
		return
	}
	// The question being turned over is the one asked for, or the last one everyone
	// answered. A reload lands on the same screen.
	at := state.Position
	if n, err := strconv.Atoi(c.Query("at")); err == nil {
		at = int32(n)
	} else if state.Turn != nil && at > 0 {
		at = at - 1
	}
	if at < 0 {
		render.Redirect(c, "/round/device/"+state.GameID.String())
		return
	}
	shown, err := w.svc.DeviceStateAt(c.Request.Context(), state.GameID, at)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if !shown.Revealed {
		render.Redirect(c, "/round/device/"+state.GameID.String())
		return
	}
	d := w.deviceTurnData(v, shown)
	question, err := w.games.QuestionAt(c.Request.Context(), shown.GameID, at, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d.Question = pages.GameQuestion{
		ID: question.ID, Position: at, Body: question.Body, Answers: question.Answers,
		CorrectIndex: question.CorrectIndex, Explanation: question.Explanation,
		Source: question.Source,
	}
	d.Picks = make([][]pages.DevicePerson, len(question.Answers))
	for i := range d.Picks {
		for _, p := range shown.Choices(int32(i)) {
			d.Picks[i] = append(d.Picks[i], w.devicePerson(v, p))
		}
	}
	d.Revealed = true
	d.Last = int(at) == shown.Questions-1
	// The question's own domain colours the reveal, as it colours the game (BR-02).
	render.Page(c, pages.DeviceReveal(v.WithFocus(deviceFocus(question)), d))
}

// deviceTurn hands the phone over: the clock restarts and the player whose turn it is
// answers the question on the table.
func (w *Web) deviceTurn(c *gin.Context) {
	v := view.From(c)
	state, ok := w.deviceState(c, v)
	if !ok {
		return
	}
	if state.Turn == nil {
		render.Redirect(c, deviceRevealURL(state.GameID, state.Position))
		return
	}
	// Each player answers on their own clock, so the stamp starts again for them.
	if err := w.games.ResetClock(c.Request.Context(), state.GameID); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, fmt.Sprintf("/games/%s?player=%s&at=%d",
		state.GameID, state.Turn.ID, state.Position))
}

// deviceNext leaves the reveal: the next question, or the results after the last one.
func (w *Web) deviceNext(c *gin.Context) {
	v := view.From(c)
	state, ok := w.deviceState(c, v)
	if !ok {
		return
	}
	if state.Done {
		render.Redirect(c, "/round/device/"+state.GameID.String()+"/results")
		return
	}
	render.Redirect(c, "/round/device/"+state.GameID.String())
}

// deviceResults is DV-05: the podium of a round played on one phone — the same two
// shapes as a live room, with how many each of them got right.
func (w *Web) deviceResults(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("game"))
		return
	}
	// Everyone's place is settled before the podium reads it (DV-05).
	if err := w.svc.FinishDeviceRound(c.Request.Context(), id); err != nil {
		w.fail(c, v, err)
		return
	}
	result, err := w.svc.DeviceResults(c.Request.Context(), id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := pages.RoomResult{
		Device: true, GameID: id.String(), Questions: result.Questions,
		Teams: result.Teams, Totals: result.Totals, Winner: result.Winner,
		Diff: result.Diff, MyTeam: -1,
	}
	for i, p := range result.Players {
		person := w.devicePerson(v, p)
		row := pages.RoomStanding{
			Name: person.Name, Initials: person.Initials, Color: person.Color,
			Score: p.Score, Correct: p.Correct, Rank: i + 1, Team: p.Team,
			IsMe: person.Me, Done: true,
		}
		d.Rows = append(d.Rows, row)
		if row.Team == 0 || row.Team == 1 {
			d.Sides[row.Team] = append(d.Sides[row.Team], row)
		}
		if row.IsMe {
			d.Mine, d.HasMine, d.MyTeam = row, true, row.Team
		}
	}
	if len(d.Rows) > 0 {
		d.MVP = d.Rows[0].Name
	}
	render.Page(c, pages.RoomPodium(v, d))
}

// deviceAgain plays the same round again with the same people: a new game on the same
// choice, so the names and the sides carry over (DV-05).
func (w *Web) deviceAgain(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.PostForm("game"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("game"))
		return
	}
	game, err := w.svc.RepeatDeviceRound(c.Request.Context(), v.User, id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/round/device/"+game.String())
}

// deviceEnd stops the round early and shows the ranking so far.
func (w *Web) deviceEnd(c *gin.Context) {
	v := view.From(c)
	state, ok := w.deviceState(c, v)
	if !ok {
		return
	}
	render.Redirect(c, "/games/"+state.GameID.String()+"/results")
}

// deviceState reads the round behind the id in the address, or answers the error.
func (w *Web) deviceState(c *gin.Context, v *view.Ctx) (DeviceState, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("game"))
		return DeviceState{}, false
	}
	state, err := w.svc.DeviceState(c.Request.Context(), id)
	if err != nil {
		w.fail(c, v, err)
		return DeviceState{}, false
	}
	return state, true
}

// deviceTurnData maps the round onto what the two screens draw.
func (w *Web) deviceTurnData(v *view.Ctx, state DeviceState) pages.DeviceTurn {
	d := pages.DeviceTurn{
		GameID: state.GameID.String(), Position: state.Position,
		Questions: state.Questions, Answered: state.Answered(),
		Teams: state.Teams, Totals: state.Totals, Gains: state.Gains,
		ShareA: state.ShareA(), Fastest: state.Fastest,
	}
	for _, p := range state.Players {
		d.Players = append(d.Players, w.devicePerson(v, p))
	}
	for _, p := range state.Standings() {
		d.Standings = append(d.Standings, w.devicePerson(v, p))
	}
	if state.Turn != nil {
		turn := w.devicePerson(v, *state.Turn)
		d.Name, d.Initials, d.Color, d.Me = turn.Name, turn.Initials, turn.Color, turn.Me
	}
	return d
}

// devicePerson gives a local player the face and the name the screens use. The owner
// of the round is the signed-in player, so they are named as themselves (DV-06).
func (w *Web) devicePerson(v *view.Ctx, p DevicePlayer) pages.DevicePerson {
	name, me := p.Name, false
	if name == "" {
		name, me = v.Name(), true
	}
	return pages.DevicePerson{
		Name: name, Initials: avatar.Initials(name), Color: avatar.Color(p.ID.String()),
		Score: p.Score, Gain: p.Gain, Team: p.Team, Answered: p.Answered,
		Right: p.Right, Now: p.Now, Me: me,
	}
}

// deviceFocus is the domain the question belongs to, so the reveal wears the same
// colour the question did.
func deviceFocus(q games.Question) *view.Domain {
	return &view.Domain{
		ID: q.DomainID, Slug: q.DomainSlug, Name: q.DomainName, Color: q.DomainColor,
		Icon: q.DomainIcon, Motif: q.DomainMotif, Family: q.DomainFamily,
	}
}

func deviceRevealURL(gameID uuid.UUID, position int32) string {
	return fmt.Sprintf("/round/device/%s/reveal?at=%d", gameID, position)
}

// detail is the challenge's own page (CH-04, CH-08).
func (w *Web) detail(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("challenge"))
		return
	}
	d, err := w.svc.Detail(c.Request.Context(), v.User, id, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Page(c, pages.ChallengeView(v, w.detailData(v, d)))
}

// detailData maps the challenge onto what its page draws. Every score it carries has
// already been through the one DTO that decides whether it may be seen (CH-04).
func (w *Web) detailData(v *view.Ctx, d Detail) pages.ChallengeDetail {
	out := pages.ChallengeDetail{
		ID: d.ID.String(), Topic: d.Category, Questions: int(d.Questions),
		Played: d.Played, Total: d.Total, Playing: d.Playing, NotStart: d.NotStart,
		Message: d.Message, From: d.Creator, IsHost: d.IsHost, Closed: d.Closed(),
		CanClose: d.CanClose, Blocker: d.Blocker, MyRank: d.MyRank,
		WillGo: strings.Join(d.WillRemove, ", "),
	}
	if d.Deadline != nil {
		out.Deadline = deadlineLabel(v, *d.Deadline)
	}
	if d.HasMine {
		out.HasMine = d.Mine.Score != nil
		if d.Mine.Score != nil {
			out.MyScore = *d.Mine.Score
		}
		out.CanPlay = d.MyStatus == string(sqlc.ParticipantStatusPending)
	}
	top := int32(0)
	for _, p := range d.Players {
		if p.Score != nil && *p.Score > top {
			top = *p.Score
		}
	}
	for _, p := range d.Players {
		out.Players = append(out.Players, w.detailPlayer(p, top))
	}
	for _, p := range d.Removed {
		out.Removed = append(out.Removed, w.detailPlayer(p, top))
	}
	out.Title = w.detailTitle(v, d)
	if d.Closed() {
		out.WinnerName = d.Winner.Name
		out.WinnerInit, out.WinnerCol = avatar.Initials(d.Winner.Name), avatar.Color(d.Winner.UserID.String())
		out.Headline, out.Subline = w.resultLines(v, d)
		out.Confetti = d.Won || d.Tied
	}
	return out
}

// detailPlayer is one line of the players list.
func (w *Web) detailPlayer(p Player, top int32) pages.ChallengePlayer {
	row := pages.ChallengePlayer{
		Name: p.Name, Initials: avatar.Initials(p.Name), Color: avatar.Color(p.UserID.String()),
		IsMe: p.IsMe, Status: p.Status, Rank: p.Rank,
	}
	if p.Score != nil {
		row.Points = *p.Score
		if top > *p.Score {
			row.Gap = top - *p.Score
		}
	}
	return row
}

// detailTitle is the heading: who challenged whom while it is open (CH-04).
func (w *Web) detailTitle(v *view.Ctx, d Detail) string {
	switch {
	case d.Closed():
		return v.Tr("challenges.results_title")
	case d.MyStatus == string(sqlc.ParticipantStatusPending) && d.Total > 2:
		return v.Tr("challenges.group_of", "name", d.Creator)
	case d.MyStatus == string(sqlc.ParticipantStatusPending):
		return v.Tr("challenges.name_challenged_you", "name", d.Creator)
	}
	return v.Tr("challenges.waiting_for_others")
}

// resultLines are the two lines over a finished challenge (CH-08).
func (w *Web) resultLines(v *view.Ctx, d Detail) (headline, sub string) {
	switch {
	case d.Tied:
		headline = v.Tr("challenges.tied_first")
	case d.Won:
		headline = v.Tr("challenges.you_won")
	default:
		headline = v.Tr("challenges.name_wins", "name", d.Winner.Name)
	}
	played := len(d.Ranked)
	if removed := len(d.Removed); removed > 0 {
		return headline, v.Tr("challenges.played_and_removed", "played", played, "total", d.Total, "removed", removed)
	}
	return headline, v.Tr("challenges.n_played", "n", played)
}

// closeEarly is endpoint 118 (CH-07).
func (w *Web) closeEarly(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("challenge"))
		return
	}
	if err := w.svc.CloseEarly(c.Request.Context(), v.User, id); err != nil {
		if e, ok := apperr.As(err); ok && e.Status < 500 {
			render.Toast(c, e.Message)
			render.Redirect(c, "/challenges/"+id.String())
			return
		}
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("challenges.results_in"))
	render.Redirect(c, "/challenges/"+id.String())
}

// rematch sends the same challenge to the same players again (CH-08).
func (w *Web) rematch(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("challenge"))
		return
	}
	fresh, err := w.svc.Rematch(c.Request.Context(), v.User, id, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/challenges/"+fresh.ID.String())
}

func (w *Web) accept(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("challenge"))
		return
	}
	game, err := w.svc.Accept(c.Request.Context(), v.User, id, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/games/"+game.Row.ID.String())
}

func (w *Web) decline(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("challenge"))
		return
	}
	if err := w.svc.Decline(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/challenges")
}

// cancel calls off a challenge nobody else has started (CH-10).
func (w *Web) cancel(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("challenge"))
		return
	}
	if err := w.svc.Cancel(c.Request.Context(), v.User, id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("challenges.cancelled_toast"))
	render.Redirect(c, "/challenges?box=sent")
}

// remind nudges the players who have not started yet (CH-10).
func (w *Web) remind(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		w.fail(c, v, apperr.NotFound("challenge"))
		return
	}
	n, err := w.svc.Remind(c.Request.Context(), v.User, id)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Toast(c, v.Tr("challenges.reminded_toast", "count", n))
	render.Redirect(c, "/challenges?box=sent")
}

// random is CH-10: the same round against whoever is looking for one.
// random is CH-10's "random opponent": a real player — a friend first, otherwise
// somebody this player has already shared a game with — picked on the spot and added
// to the round being set up. With nobody to pick, it says so rather than inventing a
// name (endpoint 61).
func (w *Web) random(c *gin.Context) {
	v := view.From(c)
	opponent, err := w.svc.RandomOpponent(c.Request.Context(), v.User)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Code == "not_found" {
			render.Toast(c, v.Tr("challenges.random_none"))
			render.Redirect(c, w.roundURL(c, "friends", nil))
			return
		}
		w.fail(c, v, err)
		return
	}
	url := w.roundURL(c, "friends", nil)
	if strings.Contains(url, "?") {
		url += "&friend=" + opponent.UserID.String()
	}
	render.Toast(c, v.Tr("challenges.random_picked", "name", opponent.Name))
	render.Redirect(c, url)
}

// roundExtra is P20's own state: who is playing, where the questions come from, the
// friends picked, the names on this device, the message and the deadline. The picker
// keeps its own keys, so those are left out.
func roundExtra(c *gin.Context) url.Values {
	picker := map[string]bool{
		"all_mine": true, "domain": true, "category": true, "view": true,
		"difficulty": true, "count": true, "seconds": true, "q": true,
		"drop": true, "page": true, "then": true,
	}
	out := url.Values{}
	for key, values := range c.Request.URL.Query() {
		if picker[key] {
			continue
		}
		for _, value := range values {
			out.Add(key, value)
		}
	}
	return out
}

// backToRound rebuilds P20's address out of the posted form: every control on that
// screen lives in the query string, so a refused round reopens exactly as it was.
func backToRound(c *gin.Context) string {
	_ = c.Request.ParseForm()
	q := url.Values{}
	for key, values := range c.Request.PostForm {
		if key == "csrf_token" {
			continue
		}
		q[key] = values
	}
	return "/challenges/new?" + q.Encode()
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok {
		if e.Status == http.StatusNotFound {
			render.Status(c, http.StatusNotFound, pages.NotFound(v))
			return
		}
		withFlash := v.Clone()
		withFlash.Flash = e.Message
		render.Status(c, e.Status, pages.ServerError(withFlash))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

func difficultyName(v *view.Ctx, difficulty int32) string {
	switch difficulty {
	case 1:
		return v.Tr("setup.easy")
	case 2:
		return v.Tr("setup.medium")
	case 3:
		return v.Tr("setup.hard")
	}
	return v.Tr("setup.mixed")
}

func expiresIn(v *view.Ctx, when time.Time) string {
	left := time.Until(when)
	if left <= 0 {
		return v.Tr("challenges.expired")
	}
	if days := int(left.Hours() / 24); days >= 1 {
		return v.Tr("challenges.expires_days", "n", days)
	}
	return v.Tr("challenges.expires_hours", "n", int(left.Hours())+1)
}

func iconOr(icon, fallback string) string {
	if icon == "" {
		return fallback
	}
	return icon
}

func colorOr(color string) string {
	if color == "" {
		return "var(--brand)"
	}
	return color
}

// deadlineLabel is the time left on a challenge, in the words the card uses: hours
// while it is close, days before that (CH-09).
func deadlineLabel(v *view.Ctx, at time.Time) string {
	left := time.Until(at)
	switch {
	case left <= 0:
		return v.Tr("challenges.ended")
	case left < time.Hour:
		return v.Tr("challenges.ends_in_minutes", "n", int(left.Minutes()))
	case left < 48*time.Hour:
		return v.Tr("challenges.ends_in_hours", "n", int(left.Hours()))
	}
	return v.Tr("challenges.ends_in_days", "n", int(left.Hours()/24))
}

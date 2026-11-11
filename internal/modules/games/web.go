package games

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/render"
	"siraj/internal/modules/catalog"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/components"
	"siraj/web/templates/pages"
)

// Web is the solo game on the website: setup, the game itself and the results
// (prototype screens 9, 10 and 11). The same service answers the mobile API.
type Web struct {
	svc      *Service
	catalog  *catalog.Service
	progress ProgressReader
	rooms    Rooms
}

// ProgressReader is the daily goal shown on the results screen; it arrives with the
// progress module (T-24) and is optional until then.
type ProgressReader interface {
	TodayGoal(c *gin.Context) (done int32, goal int32)
}

func NewWeb(s *Service, cat *catalog.Service, progress ProgressReader) *Web {
	return &Web{svc: s, catalog: cat, progress: progress}
}

// SetProgress wires the progress module in after both are built: each needs the other
// (results need the daily goal, the profile needs recent games).
func (w *Web) SetProgress(p ProgressReader) { w.progress = p }

// Rooms lets a live game show the room's board beside the question (P15).
type Rooms interface {
	RoomCode(roomID uuid.UUID) string
}

func (w *Web) SetRooms(r Rooms) { w.rooms = r }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/setup", w.setup)
	private.POST("/games", w.start)
	private.GET("/play/quick", w.quick)
	private.GET("/games/:id", w.game)
	private.POST("/games/:id/answers", w.answer)
	private.POST("/games/:id/next", w.next)
	private.POST("/games/:id/fifty", w.fifty)
	private.POST("/games/:id/skip", w.skip)
	private.POST("/games/:id/finish", w.finish)
	private.GET("/games/:id/results", w.results)
	private.GET("/history", w.history)
}

// modeIcon is the circle in front of a history row, one per game mode.
var modeIcon = map[string]string{
	"solo": "bolt", "live": "radio", "duel": "swords", "challenge": "send", "device": "phone",
}

// history is P26: the player's finished games, filtered by mode (LB-03).
func (w *Web) history(c *gin.Context) {
	v := view.From(c)
	mode := c.Query("mode")
	if !ValidMode(mode) {
		mode = ""
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	const perPage = 20
	rows, total, err := w.svc.History(c.Request.Context(), v.User, v.Lang, mode, page, perPage)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	stats, err := w.svc.HistoryStats(c.Request.Context(), v.User)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := pages.HistoryData{
		Mode: mode, Games: stats.Games, ThisWeek: stats.ThisWeek,
		BestScore: stats.BestScore, Accuracy: stats.Accuracy,
		Page: page, Pages: (total + perPage - 1) / perPage,
	}
	if data.Pages < 1 {
		data.Pages = 1
	}
	for _, r := range rows {
		data.Rows = append(data.Rows, w.historyRow(v, r))
	}
	render.Page(c, pages.History(v, data))
}

// Recent implements progress.RecentGames: the short list on the profile.
func (w *Web) Recent(c *gin.Context, v *view.Ctx, limit int) []pages.RecentGame {
	rows, _, err := w.svc.History(c.Request.Context(), v.User, v.Lang, "", 1, limit)
	if err != nil {
		_ = c.Error(err)
		return nil
	}
	out := make([]pages.RecentGame, 0, len(rows))
	for _, r := range rows {
		out = append(out, w.historyRow(v, r))
	}
	return out
}

func (w *Web) historyRow(v *view.Ctx, r HistoryRow) pages.RecentGame {
	icon := modeIcon[r.Mode]
	if icon == "" {
		icon = "bolt"
	}
	color := r.DomainColor
	if color == "" {
		color = "var(--brand)"
	}
	title := r.DomainName
	if title == "" {
		title = v.Tr("play.mode." + r.Mode)
	}
	when := ""
	if !r.FinishedAt.IsZero() {
		when = r.FinishedAt.Local().Format("2 Jan 15:04")
	}
	return pages.RecentGame{
		ID: r.GameID.String(), Title: title,
		Subtitle: v.Tr("play.mode."+r.Mode) + " · " + when,
		Icon:     icon, Color: color, Mode: r.Mode,
		Score:  fmt.Sprintf("%d / %d", r.Correct, r.Questions),
		Points: r.Score,
	}
}

// setup is the game setup screen; every change re-renders it so the summary, the number
// of questions available and the maximum score stay true (GP-02).
func (w *Web) setup(c *gin.Context) {
	v := view.From(c)
	data, _, _, err := w.readSetup(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Page(c, pages.Setup(v.WithFocus(data.Domain), data))
}

// ServedAt is when the question at a position went on screen; a live room reads it to
// know when its clock has run out (RM-03).
func (w *Web) ServedAt(c *gin.Context, gameID uuid.UUID, position int32) (time.Time, error) {
	return w.svc.Serve(c.Request.Context(), gameID, position)
}

// ReadSetup is readSetup for the screens that share the topic picker: P12 hosts a
// room on it and P20 builds a challenge round on it (FLOWS §5).
func (w *Web) ReadSetup(c *gin.Context, v *view.Ctx) (pages.SetupData, Scope, int32, error) {
	return w.readSetup(c, v)
}

// readSetup turns the query (or the posted form) into the setup screen's state.
//
// The picker keeps "the domain in view" apart from "the domain the game runs on", so
// a player can look at another domain's categories without losing their choice (GP-02).
func (w *Web) readSetup(c *gin.Context, v *view.Ctx) (pages.SetupData, Scope, int32, error) {
	value := func(name string) string {
		if val, ok := c.GetPostForm(name); ok {
			return val
		}
		return c.Query(name)
	}
	values := func(name string) []string {
		if vals, ok := c.GetPostFormArray(name); ok && len(vals) > 0 {
			return vals
		}
		return c.QueryArray(name)
	}
	find := func(id int64) *view.Domain {
		for i := range v.MyDomains {
			if v.MyDomains[i].ID == id {
				return &v.MyDomains[i]
			}
		}
		return nil
	}

	data := pages.SetupData{Picked: map[int64]bool{}, Count: 10, Seconds: 20, Difficulty: 0}
	if n, err := strconv.Atoi(value("count")); err == nil && allowedCounts[int32(n)] {
		data.Count = int32(n)
	}
	if n, err := strconv.Atoi(value("seconds")); err == nil && allowedSeconds[int32(n)] {
		data.Seconds = int32(n)
	}
	if n, err := strconv.Atoi(value("difficulty")); err == nil && n >= 0 && n <= 3 {
		data.Difficulty = int32(n)
	}
	data.AllMine = value("all_mine") != ""
	data.Query = strings.TrimSpace(value("q"))

	// Everything the picker needs about the player's own domains and categories.
	ids := make([]int64, 0, len(v.MyDomains))
	data.DomainNames = make(map[int64]string, len(v.MyDomains))
	for _, d := range v.MyDomains {
		ids = append(ids, d.ID)
		data.DomainNames[d.ID] = d.Name
		data.MineQuestions += d.QuestionCount
	}
	byDomain, err := w.catalog.CategoriesOf(c.Request.Context(), v.Lang, ids)
	if err != nil {
		return data, Scope{}, data.Difficulty, err
	}
	for _, list := range byDomain {
		data.MyCategoryCount += len(list)
	}

	scope := Scope{Kind: "custom"}
	// A node chosen on the shared topic picker (P09, P12, P20). It coexists with the
	// v1 domain and category choices while the cut-over runs; whichever is given wins
	// for that game.
	if id, err := strconv.ParseInt(value("node"), 10, 64); err == nil && id > 0 {
		scope.NodeID, scope.NodeScope = id, value("node_scope")
	}
	if data.AllMine {
		scope.AllMine = true
	} else if id, err := strconv.ParseInt(value("domain"), 10, 64); err == nil {
		data.Domain = find(id)
	}
	if !data.AllMine && data.Domain == nil && len(v.MyDomains) > 0 {
		data.Domain = &v.MyDomains[0]
	}

	// The tab in view: the one asked for, else the picked domain, else the first.
	if id, err := strconv.ParseInt(value("view"), 10, 64); err == nil {
		data.View = find(id)
	}
	if data.View == nil {
		if data.Domain != nil {
			data.View = data.Domain
		} else if len(v.MyDomains) > 0 {
			data.View = &v.MyDomains[0]
		}
	}
	if data.View != nil {
		data.Categories = byDomain[data.View.ID]
		data.ViewQuestions = data.View.QuestionCount
	}

	if data.Domain != nil {
		scope.DomainID = data.Domain.ID
		for _, raw := range values("category") {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || data.Picked[id] {
				continue
			}
			for _, cat := range byDomain[data.Domain.ID] {
				if cat.ID == id {
					data.Picked[id] = true
					data.PickedList = append(data.PickedList, cat)
					scope.CategoryIDs = append(scope.CategoryIDs, id)
				}
			}
		}
	}

	data.TopicLabel = scopeLabel(v, data)
	if data.Query != "" {
		data.ResultDomains, data.ResultCats = searchTopics(v, byDomain, data.Query)
	}

	// How many questions the scope really holds: the summary says it, and the Start
	// button stays locked while the bank has fewer than the game asks for (GP-02).
	available, err := w.svc.Available(c.Request.Context(), v.User, scope, data.Difficulty)
	if err != nil {
		return data, scope, data.Difficulty, err
	}
	data.Available = available
	base := data.Difficulty
	if base == 0 {
		base = mixedDifficulty
	}
	data.MaxPoints = MaxPoints(base, plannedCount(available, data.Count), data.Seconds)
	return data, scope, data.Difficulty, nil
}

// scopeLabel is the "Topics" line of the summary (GP-02).
func scopeLabel(v *view.Ctx, d pages.SetupData) string {
	switch {
	case d.AllMine || d.Domain == nil:
		return v.Tr("setup.all_my_domains")
	case len(d.PickedList) == 0:
		return v.Tr("setup.all_of_domain", "name", d.Domain.Name)
	case len(d.PickedList) == 1:
		return d.PickedList[0].Name
	}
	names := make([]string, 0, len(d.PickedList))
	for _, c := range d.PickedList {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

// searchTopics filters the player's own domains and categories (DM-03: never outside
// Your domains) for the picker's search box.
func searchTopics(v *view.Ctx, byDomain map[int64][]components.Category, q string) ([]view.Domain, []components.Category) {
	needle := strings.ToLower(q)
	var domains []view.Domain
	for _, d := range v.MyDomains {
		if len(domains) == 6 {
			break
		}
		if strings.Contains(strings.ToLower(d.Name), needle) {
			domains = append(domains, d)
		}
	}
	var cats []components.Category
	for _, d := range v.MyDomains {
		for _, c := range byDomain[d.ID] {
			if len(cats) == 12 {
				return domains, cats
			}
			if strings.Contains(strings.ToLower(c.Name), needle) {
				cats = append(cats, c)
			}
		}
	}
	return domains, cats
}

// quick starts the 5-question quick game at once (HM-04).
func (w *Web) quick(c *gin.Context) {
	v := view.From(c)
	scope := Scope{Kind: "quick"}
	if id, err := strconv.ParseInt(c.Query("domain"), 10, 64); err == nil {
		scope.DomainID = id
	}
	w.startGame(c, v, StartInput{
		Who: v.User, Mode: sqlc.GameModeSolo, Scope: scope,
		Count: QuickGameQuestions, Seconds: 20, Lang: v.Lang,
	})
}

func (w *Web) start(c *gin.Context) {
	v := view.From(c)
	data, scope, difficulty, err := w.readSetup(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if c.PostForm("mix") != "" || c.Query("mix") != "" {
		scope.Kind = "mix"
	}
	w.startGame(c, v, StartInput{
		Who: v.User, Mode: sqlc.GameModeSolo, Scope: scope,
		Difficulty: difficulty, Count: data.Count, Seconds: data.Seconds, Lang: v.Lang,
	})
}

func (w *Web) startGame(c *gin.Context, v *view.Ctx, in StartInput) {
	game, err := w.svc.Start(c.Request.Context(), in)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Code == "no_questions" {
			render.Toast(c, v.Tr("setup.no_questions"))
			render.Redirect(c, "/setup")
			return
		}
		if e, ok := apperr.As(err); ok && e.Code == "not_enough_questions" {
			available, _ := w.svc.Available(c.Request.Context(), v.User, in.Scope, in.Difficulty)
			render.Toast(c, v.Tr("setup.fewer_questions", "available", available, "count", in.Count))
			render.Redirect(c, setupBack(in))
			return
		}
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/games/"+game.Row.ID.String())
}

func (w *Web) game(c *gin.Context) {
	v := view.From(c)
	game, state, err := w.load(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if state.Done {
		render.Redirect(c, "/games/"+game.Row.ID.String()+"/results")
		return
	}
	// The question on screen is not always the next one: after answering, the player
	// reads the feedback before pressing Next. "at" says which one they are looking
	// at, so a reload — changing the language is one — comes back to the same
	// question with the same panel, and never skips it (LG-01, GP-03).
	at := state.Position
	if n, err := strconv.Atoi(c.Query("at")); err == nil && n >= 0 && int32(n) <= state.Position && n < int(game.Row.QuestionCount) {
		at = int32(n)
	}
	question, err := w.svc.QuestionAt(c.Request.Context(), game.Row.ID, at, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	var feedback *pages.GameFeedback
	var picked *int32
	if given, ok := w.svc.AnswerFor(c.Request.Context(), game, at); ok {
		feedback = &pages.GameFeedback{Correct: given.Correct, TimedOut: given.TimedOut, Points: given.Points}
		picked = given.Choice
	}
	served := time.Time{}
	if feedback == nil {
		// Only a question still open starts its clock, and the clock started the
		// first time it was served: a reload reads what is left, never a fresh 20 s.
		served, err = w.svc.Serve(c.Request.Context(), game.Row.ID, at)
		if err != nil {
			w.fail(c, v, err)
			return
		}
	}
	data := w.build(v, game, state, question, feedback, picked)
	data.Seconds = secondsLeft(int32(game.Row.SecondsPerQuestion), served)
	v.Back = gameHref(game.Row.ID.String(), data.Player, at)
	render.Page(c, pages.Game(v.WithFocus(focusOf(question)), data))
}

// secondsLeft is what is left of a question's time. A page that comes back to a
// question already on the clock (a language change) shows the remaining seconds, and
// never less than one, so the player can still answer (NF-05).
func secondsLeft(total int32, served time.Time) int32 {
	if total <= 0 || served.IsZero() {
		return total
	}
	left := total - int32(time.Since(served).Seconds())
	if left < 1 {
		return 1
	}
	if left > total {
		return total
	}
	return left
}

// gameHref is this screen with the question it is showing, so a page-reloading
// control (the language menu) returns to exactly where the player was.
func gameHref(gameID, player string, at int32) string {
	href := fmt.Sprintf("/games/%s?at=%d", gameID, at)
	if player != "" {
		href += "&player=" + player
	}
	return href
}

func (w *Web) answer(c *gin.Context) {
	v := view.From(c)
	game, _, err := w.load(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	position, _ := strconv.Atoi(c.PostForm("position"))
	timeMS, _ := strconv.Atoi(c.PostForm("time_ms"))
	in := AnswerInput{Position: int32(position), TimeMS: int32(timeMS)}
	if raw := c.PostForm("choice"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			choice := int32(n)
			in.Choice = &choice
		}
	}
	answered, err := w.svc.Answer(c.Request.Context(), game, in, v.Lang)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Code == "already_answered" {
			w.renderCurrent(c, v, game)
			return
		}
		w.fail(c, v, err)
		return
	}
	state, err := w.svc.State(c.Request.Context(), game)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	// A round on one phone never shows the answer to the person who just gave it:
	// the pick stays hidden until the whole table has answered, and the device screen
	// takes over from here (DV-02).
	if game.Row.Mode == sqlc.GameModeDevice {
		// The position just answered rides along, so the device screen knows whether
		// this answer was the one that completes the question (DV-02, DV-04).
		render.Redirect(c, fmt.Sprintf("/round/device/%s?answered=%d", game.Row.ID, in.Position))
		return
	}
	feedback := &pages.GameFeedback{Correct: answered.Correct, TimedOut: answered.TimedOut, Points: answered.Points}
	data := w.build(v, game, state, answered.Question, feedback, answered.Choice)
	data.Last = answered.Last
	if render.IsHTMX(c) {
		data.Swap = true
		render.Page(c, pages.GameCard(v, data))
		return
	}
	render.Page(c, pages.Game(v.WithFocus(focusOf(answered.Question)), data))
}

// next moves to the question after the one that was just answered, or to the results.
func (w *Web) next(c *gin.Context) {
	v := view.From(c)
	game, state, err := w.load(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if state.Done {
		if _, err := w.svc.Finish(c.Request.Context(), game, false); err != nil {
			w.fail(c, v, err)
			return
		}
		render.Redirect(c, "/games/"+game.Row.ID.String()+"/results")
		return
	}
	w.renderCurrent(c, v, game)
}

func (w *Web) fifty(c *gin.Context) {
	v := view.From(c)
	game, state, err := w.load(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	keep, err := w.svc.Fifty(c.Request.Context(), game, state.Position, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	game.Scope.Fifty = true
	state.FiftyUsed = true
	question, err := w.svc.QuestionAt(c.Request.Context(), game.Row.ID, state.Position, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := w.build(v, game, state, question, nil, nil)
	for i := range question.Answers {
		if !contains(keep, int32(i)) {
			data.Hidden = append(data.Hidden, int32(i))
		}
	}
	if render.IsHTMX(c) {
		data.Swap = true
		render.Page(c, pages.GameCard(v, data))
		return
	}
	render.Page(c, pages.Game(v.WithFocus(focusOf(question)), data))
}

func (w *Web) skip(c *gin.Context) {
	v := view.From(c)
	game, state, err := w.load(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.Skip(c.Request.Context(), game, state.Position); err != nil {
		w.fail(c, v, err)
		return
	}
	game.Scope.Skip = true
	w.renderCurrent(c, v, game)
}

func (w *Web) finish(c *gin.Context) {
	v := view.From(c)
	game, _, err := w.load(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	abandon := c.PostForm("abandon") != ""
	if _, err := w.svc.Finish(c.Request.Context(), game, abandon); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/games/"+game.Row.ID.String()+"/results")
}

func (w *Web) results(c *gin.Context) {
	v := view.From(c)
	game, state, err := w.load(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if !game.Row.FinishedAt.Valid && state.Done {
		if _, err := w.svc.Finish(c.Request.Context(), game, false); err != nil {
			w.fail(c, v, err)
			return
		}
		game, _, _ = w.load(c, v)
	}
	summary, err := w.svc.Results(c.Request.Context(), game, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := pages.ResultsData{
		GameID: game.Row.ID.String(), Title: resultsTitle(v, game), Score: summary.Score,
		Correct: summary.Correct, Total: summary.Total, XP: summary.XP,
		Accuracy: summary.Accuracy, AverageMS: summary.AverageMS, Again: againURL(game),
	}
	for _, row := range summary.ByDomain {
		data.ByDomain = append(data.ByDomain, pages.DomainResult{
			DomainID: row.DomainID, DomainName: row.DomainName, Color: row.Color,
			Answered: row.Answered, Correct: row.Correct,
		})
	}
	review := make([]pages.ReviewItem, 0, len(summary.Review))
	for _, item := range summary.Review {
		review = append(review, pages.ReviewItem{
			Question: pageQuestion(item.Question), Choice: item.Choice, Correct: item.Correct,
			Points: item.Points, TimeMS: item.TimeMS, Skipped: item.Skipped,
		})
	}
	data.Review, data.ReviewPage, data.ReviewPages, data.ReviewPer = reviewPage(c, review)
	if w.progress != nil {
		data.DoneToday, data.Goal = w.progress.TodayGoal(c)
	}
	focus := w.focusFromReview(v, summary)
	render.Page(c, pages.Results(v.WithFocus(focus), data))
}

// reviewPage cuts "Review your answers" into pages: rpp says how many lines a page
// holds (one of pages.ReviewPerPage) and rp which page to show (GP-05).
func reviewPage(c *gin.Context, all []pages.ReviewItem) (items []pages.ReviewItem, page, pages_, per int) {
	per = pages.ReviewPerPage[0]
	if n, err := strconv.Atoi(c.Query("rpp")); err == nil && slices.Contains(pages.ReviewPerPage, n) {
		per = n
	}
	pages_ = (len(all) + per - 1) / per
	if pages_ < 1 {
		pages_ = 1
	}
	page = 1
	if n, err := strconv.Atoi(c.Query("rp")); err == nil && n > 1 {
		page = n
	}
	if page > pages_ {
		page = pages_
	}
	from := (page - 1) * per
	if from > len(all) {
		from = len(all)
	}
	to := min(from+per, len(all))
	return all[from:to], page, pages_, per
}

// renderCurrent draws the question the player is on now.
func (w *Web) renderCurrent(c *gin.Context, v *view.Ctx, game Game) {
	state, err := w.svc.State(c.Request.Context(), game)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if state.Done {
		if _, err := w.svc.Finish(c.Request.Context(), game, false); err != nil {
			w.fail(c, v, err)
			return
		}
		render.Redirect(c, "/games/"+game.Row.ID.String()+"/results")
		return
	}
	question, err := w.svc.QuestionAt(c.Request.Context(), game.Row.ID, state.Position, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if _, err := w.svc.Serve(c.Request.Context(), game.Row.ID, state.Position); err != nil {
		w.fail(c, v, err)
		return
	}
	data := w.build(v, game, state, question, nil, nil)
	if render.IsHTMX(c) {
		data.Swap = true
		render.Page(c, pages.GameCard(v, data))
		return
	}
	render.Page(c, pages.Game(v.WithFocus(focusOf(question)), data))
}

// resultsTitle is the badge at the top of the results card: how the game was played.
func resultsTitle(v *view.Ctx, game Game) string {
	switch game.Scope.Kind {
	case "quick":
		return v.Tr("play.quick")
	case "mix":
		return v.Tr("play.daily_mix")
	}
	return v.Tr("play.mode." + string(game.Row.Mode))
}

func (w *Web) build(v *view.Ctx, game Game, state State, question Question, feedback *pages.GameFeedback, choice *int32) pages.GameData {
	title := question.DomainName
	if game.Scope.Kind == "quick" {
		title = v.Tr("play.quick")
	}
	data := pages.GameData{
		GameID:   game.Row.ID.String(),
		Title:    title,
		Question: pageQuestion(question),
		State: pages.GameState{
			QuestionCount: int32(game.Row.QuestionCount), Score: state.Score,
			Results: state.Results, FiftyUsed: state.FiftyUsed, SkipUsed: state.SkipUsed,
		},
		Feedback: feedback,
		Picked:   choice,
		Seconds:  int32(game.Row.SecondsPerQuestion),
		Last:     state.Answered >= int32(game.Row.QuestionCount),
	}
	if game.Row.Mode == sqlc.GameModeDevice {
		data.Player = game.PlayerID.String()
	}
	data.Mode = string(game.Row.Mode)
	if w.rooms != nil && game.Row.RoomID.Valid {
		data.RoomCode = w.rooms.RoomCode(game.Row.RoomID.UUID)
	}
	return data
}

// focus is the domain of the question on screen: it re-colours the game (BR-02) and
// decides whether the geometric pattern shows (BR-03).
func focusOf(q Question) *view.Domain {
	return &view.Domain{
		ID: q.DomainID, Slug: q.DomainSlug, Name: q.DomainName, Color: q.DomainColor,
		Icon: q.DomainIcon, Motif: q.DomainMotif, Family: q.DomainFamily,
	}
}

func (w *Web) focusFromReview(v *view.Ctx, summary Summary) *view.Domain {
	if len(summary.ByDomain) > 1 || len(summary.Review) == 0 {
		return nil // a mixed game keeps the neutral shell (BR-02)
	}
	q := summary.Review[0].Question
	return &view.Domain{ID: q.DomainID, Slug: q.DomainSlug, Name: q.DomainName, Color: q.DomainColor, Family: q.DomainFamily, Icon: q.DomainIcon}
}

func pageQuestion(q Question) pages.GameQuestion {
	return pages.GameQuestion{
		Position: q.Position, ID: q.ID, CategoryName: q.CategoryName, CategoryIcon: q.CategoryIcon,
		CategoryColor: q.CategoryColor, DomainName: q.DomainName, DomainIcon: q.DomainIcon,
		DomainColor: q.DomainColor, Difficulty: q.Difficulty,
		Points: q.Points, CorrectIndex: q.CorrectIndex, Source: q.Source, Body: q.Body,
		Answers: q.Answers, Explanation: q.Explanation, InLanguage: q.InLanguage,
	}
}

func (w *Web) load(c *gin.Context, v *view.Ctx) (Game, State, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return Game{}, State{}, apperr.NotFound("game")
	}
	// A pass-the-device round has no account per player, so the turn says who is
	// answering; every other game is loaded as the signed-in player (RM-06).
	var game Game
	if raw := playerParam(c); raw != uuid.Nil {
		game, err = w.svc.LoadAs(c.Request.Context(), v.User, id, raw)
	} else {
		game, err = w.svc.Load(c.Request.Context(), v.User, id)
	}
	// Without this the next line overwrote err: a game that is missing, finished or
	// somebody else's answered "not found" and then had its answer thrown away, and
	// State ran on a zero Game — which reads no rows, returns no error and leaves the
	// screen to act on a game nobody owns.
	if err != nil {
		return Game{}, State{}, err
	}
	state, err := w.svc.State(c.Request.Context(), game)
	return game, state, err
}

// playerParam is the turn of a device round, carried on every link and form of the
// game screen so a reload keeps the same player.
func playerParam(c *gin.Context) uuid.UUID {
	raw := c.PostForm("player")
	if raw == "" {
		raw = c.Query("player")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok {
		if e.Status == http.StatusNotFound {
			render.Status(c, http.StatusNotFound, pages.NotFound(v))
			return
		}
		render.Toast(c, e.Message)
		withFlash := v.Clone()
		withFlash.Flash = e.Message
		render.Status(c, e.Status, pages.ServerError(withFlash))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

func againURL(g Game) string {
	if g.Scope.DomainID > 0 {
		return "/play/quick?domain=" + strconv.FormatInt(g.Scope.DomainID, 10)
	}
	return "/play/quick"
}

// mixedDifficulty is what a mixed game is costed at: the medium one, so the summary's
// "up to N points" stays honest (GP-02).
const mixedDifficulty = 2

// setupBack sends a refused start back to the screen that can fix it, carrying the
// scope it was asked for, so the player lands on the locked summary and not on an
// empty setup screen.
func setupBack(in StartInput) string {
	q := url.Values{}
	if in.Scope.DomainID > 0 {
		q.Set("domain", strconv.FormatInt(in.Scope.DomainID, 10))
		for _, id := range in.Scope.CategoryIDs {
			q.Add("category", strconv.FormatInt(id, 10))
		}
	} else {
		q.Set("all_mine", "1")
	}
	q.Set("difficulty", strconv.Itoa(int(in.Difficulty)))
	q.Set("count", strconv.Itoa(int(in.Count)))
	q.Set("seconds", strconv.Itoa(int(in.Seconds)))
	return "/setup?" + q.Encode()
}

func plannedCount(available int, asked int32) int32 {
	if available < int(asked) {
		return int32(available)
	}
	return asked
}

func contains(list []int32, v int32) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

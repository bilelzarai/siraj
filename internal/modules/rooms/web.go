package rooms

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/render"
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"
	authpages "siraj/web/templates/pages/auth"

	"siraj/internal/platform/avatar"
)

// Setup is the topic picker of P09, which P12 reuses to choose a room's questions.
type Setup interface {
	ReadSetup(c *gin.Context, v *view.Ctx) (pages.SetupData, games.Scope, int32, error)
	// ServedAt is when a question went on screen, so the room knows when its clock
	// has run out (RM-03, NF-05).
	ServedAt(c *gin.Context, gameID uuid.UUID, position int32) (time.Time, error)
}

// Guests makes the player a room needs when a visitor arrives with a code: the
// design puts "Join a room" on the entry screen, before anybody has an account
// (screens 28–30). The auth module answers it; this module only asks.
type Guests interface {
	EnsureGuest(c *gin.Context, name string) error
	SuggestName() string
}

type Web struct {
	svc     *Service
	setup   Setup
	guests  Guests
	baseURL string
}

func NewWeb(s *Service, setup Setup, guests Guests, baseURL string) *Web {
	return &Web{svc: s, setup: setup, guests: guests, baseURL: baseURL}
}

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	// The short link the code card prints, so what the host reads out loud actually
	// opens the room (RM-01).
	public.GET("/r/:code", w.shortLinkIn)
	private.GET("/rooms/new", w.hostPage)
	private.POST("/rooms", w.host)
	// Joining with a code is open: a visitor who has one becomes a guest in the
	// act of using it, rather than being sent to the welcome screen first.
	public.GET("/rooms/join", w.joinPage)
	public.POST("/rooms/join", w.join)
	private.GET("/rooms/:code", w.lobby)
	private.POST("/rooms/:code/ready", w.ready)
	private.POST("/rooms/:code/leave", w.leave)
	private.POST("/rooms/:code/kick", w.kick)
	private.POST("/rooms/:code/settings", w.settings)
	// The panel carries the Start button and the ready count, so it refreshes with
	// the players beside it: a room that fills up unlocks Start without a reload.
	private.GET("/rooms/:code/settings-panel", w.settingsPanel)
	private.GET("/rooms/:code/players", w.players)
	private.POST("/rooms/:code/mode", w.mode)
	private.POST("/rooms/:code/teams/move", w.moveTeam)
	// Endpoint 119: the host dealing the two sides again (RM-05).
	private.POST("/rooms/:code/teams/shuffle", w.shuffleTeams)
	private.GET("/rooms/:code/chat", w.chat)
	private.POST("/rooms/:code/chat", w.say)
	private.GET("/rooms/:code/next", w.next)
	private.POST("/rooms/:code/start", w.start)
	private.GET("/rooms/:code/play", w.play)
	private.GET("/rooms/:code/board", w.board)
	private.GET("/rooms/:code/arena", w.arena)
	private.GET("/rooms/:code/podium", w.podium)
	private.POST("/rooms/:code/again", w.again)
}

// hostPage is P12: the same picker as a solo game plus the room's own options.
func (w *Web) hostPage(c *gin.Context) {
	v := view.From(c)
	d, _, _, err := w.setup.ReadSetup(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d.Room = true
	d.Path = "/rooms/new"
	d.MaxPlayers = 8
	if n, err := strconv.Atoi(c.Query("max_players")); err == nil {
		d.MaxPlayers = int32(n)
	}
	d.Chat = c.DefaultQuery("chat", "1") != "0"
	d.GuestRoom = v.IsGuest()
	d.Mode = c.Query("mode")
	render.Page(c, pages.Setup(v, d))
}

func (w *Web) host(c *gin.Context) {
	v := view.From(c)
	data, scope, difficulty, err := w.setup.ReadSetup(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	// The picker locks the button; a posted form asking for more questions than the
	// choice holds comes back to P12 with the reason (RM-03, GP-02).
	if data.Available < int(data.Count) {
		render.Toast(c, v.Tr("setup.fewer_questions", "available", data.Available, "count", data.Count))
		render.Redirect(c, backToHost(c))
		return
	}
	in := HostInput{
		Who: v.User, Scope: scope, Difficulty: difficulty,
		Count: data.Count, Seconds: data.Seconds, Lang: v.Lang,
		Chat: c.PostForm("chat") != "",
		Mode: c.PostForm("mode"),
	}
	if n, err := strconv.Atoi(c.PostForm("max_players")); err == nil {
		in.MaxPlayers = int32(n)
	}
	room, err := w.svc.Host(c.Request.Context(), in)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

// backToHost rebuilds P12's address out of the posted form, so a refused room reopens
// on the same choice.
func backToHost(c *gin.Context) string {
	_ = c.Request.ParseForm()
	q := url.Values{}
	for key, values := range c.Request.PostForm {
		if key == "csrf_token" {
			continue
		}
		q[key] = values
	}
	return "/rooms/new?" + q.Encode()
}

func (w *Web) joinPage(c *gin.Context) {
	v := view.From(c)
	// From the entry screen the code box is a popup over it (screen 28).
	if c.Query("ns") == "popup" {
		render.Page(c, authpages.JoinPopup(v, authpages.Entry{Name: w.nameFor(v)}, c.Query("code"), ""))
		return
	}
	render.Page(c, pages.RoomJoin(v, w.joinData(c, v, c.Query("code"), "")))
}

// joinData fills P13: the code typed so far, the name the player will wear, and the
// rooms their friends are waiting in (RM-02).
func (w *Web) joinData(c *gin.Context, v *view.Ctx, code, problem string) pages.JoinData {
	d := pages.JoinData{Code: code, Problem: problem, Name: v.Name(), Guest: v.IsGuest()}
	rooms, err := w.svc.OpenRooms(c.Request.Context(), v.User)
	if err != nil {
		return d
	}
	for _, r := range rooms {
		mode := v.Tr("rooms.mode_ffa")
		if r.Teams {
			mode = v.Tr("rooms.mode_teams")
		}
		card := pages.OpenRoomCard{
			Code: r.Code, HostName: r.HostName,
			Title:    v.Tr("rooms.friends_room_title", "name", r.HostName),
			Initials: avatar.Initials(r.HostName), Color: avatar.Color(r.HostID.String()),
			Meta: v.Tr("rooms.friends_room_meta", "domain", w.domainName(v, r.DomainID), "players", r.Players, "mode", mode),
		}
		d.Rooms = append(d.Rooms, card)
	}
	return d
}

// nameFor is the name the popup shows: the player's own, or the "Guest 4821" a
// visitor will be given if they go through with it.
func (w *Web) nameFor(v *view.Ctx) string {
	if v.SignedIn() {
		return v.Name()
	}
	if w.guests != nil {
		return w.guests.SuggestName()
	}
	return ""
}

// domainName reads a domain's name out of the view context, which already holds the
// player's domains in their own language (LG-05).
func (w *Web) domainName(v *view.Ctx, id int64) string {
	for _, dom := range v.MyDomains {
		if dom.ID == id {
			return dom.Name
		}
	}
	return v.Tr("setup.all_my_domains")
}

func (w *Web) join(c *gin.Context) {
	v := view.From(c)
	code := c.PostForm("code")
	// The entry screen shows the same form as a popup, and its refusals belong in
	// the popup rather than on a page behind it (screen 22).
	popup := c.PostForm("from") == "entry"
	typed := strings.TrimSpace(c.PostForm("name"))
	refuse := func(problem string) {
		name := typed
		if name == "" {
			name = w.nameFor(v)
		}
		if popup {
			render.Status(c, http.StatusUnprocessableEntity,
				authpages.JoinPopup(v, authpages.Entry{Name: name}, code, problem))
			return
		}
		render.Status(c, http.StatusUnprocessableEntity, pages.RoomJoin(v, w.joinData(c, v, code, problem)))
	}
	room, err := w.svc.ByCode(c.Request.Context(), code)
	if err != nil {
		refuse(v.Tr("rooms.no_such_code"))
		return
	}
	// The code is real, so the visitor who typed it becomes a guest and joins as
	// the name they chose. A wrong code never costs an account.
	if !v.SignedIn() {
		if w.guests == nil {
			refuse(v.Tr("rooms.cannot_join"))
			return
		}
		if err := w.guests.EnsureGuest(c, typed); err != nil {
			refuse(v.Tr("rooms.cannot_join"))
			return
		}
	}
	// "Your name in the room": a guest types the name everyone will see, which is
	// their account name, because a guest is a real user (RL-02).
	if typed != "" && v.IsGuest() {
		_ = w.svc.Rename(c.Request.Context(), v.User, typed)
	}
	if err := w.svc.Join(c.Request.Context(), v.User, room); err != nil {
		message := v.Tr("rooms.cannot_join")
		if e, ok := apperr.As(err); ok {
			message = e.Message
		}
		refuse(message)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

// lobby is P14. A visit also joins, so a shared link works in one step (RM-02).
func (w *Web) lobby(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	if err := w.svc.Join(c.Request.Context(), v.User, room); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Page(c, pages.RoomLobby(v, w.lobbyData(c, v, room)))
}

func (w *Web) lobbyData(c *gin.Context, v *view.Ctx, room sqlc.Room) pages.RoomData {
	members, _ := w.svc.Members(c.Request.Context(), room)
	code := strings.TrimSpace(room.Code)
	settings := w.svc.Settings(room)
	d := pages.RoomData{
		Code: code, Link: w.baseURL + "/rooms/" + code,
		Host: room.HostID == v.User.ID, Max: int32(room.MaxPlayers),
		Chat: room.ChatEnabled,
		Settings: v.Tr("rooms.settings_line",
			"count", settings.Count, "seconds", settings.Seconds),
		HostName: v.Tr("rooms.host"),
	}
	sides, _ := w.svc.Teams(c.Request.Context(), room)
	d.Mode = w.svc.Mode(settings, len(members))
	d.CanTeams = len(members) >= MinTeamPlayers
	if settings.Mode == ModeTeams {
		d.Mode = ModeTeams // the host's choice stays on screen even while the room is short
	}
	d.MyTeam = -1
	for _, m := range members {
		row := pages.RoomMember{
			ID: m.UserID.String(), Name: m.DisplayName,
			Initials: avatar.Initials(m.DisplayName), Color: avatar.Color(m.UserID.String()),
			Host: m.IsHost, Ready: m.IsReady, IsMe: m.UserID == v.User.ID,
			Guest: m.Kind == sqlc.UserKindGuest, Level: m.Level, Team: int32(sides.Of(m.UserID)),
		}
		if row.Team >= 0 {
			d.Sides[row.Team] = append(d.Sides[row.Team], row)
		}
		if row.IsMe {
			d.MyTeam = row.Team
		}
		if row.Ready {
			d.Ready++
		}
		if row.IsMe {
			d.MyReady = row.Ready
		}
		if row.Host {
			d.HostName = row.Name
		}
		d.Members = append(d.Members, row)
	}
	d.Count, d.Seconds = settings.Count, settings.Seconds
	d.AllowGuests, d.GuestRoom = !settings.GuestsOff, room.IsGuestRoom
	d.ShortLink = shortLink(w.baseURL, code)
	if svg := QR(d.Link); svg != "" {
		d.QR = render.Raw(svg)
	}
	d.DomainName = w.domainName(v, settings.Scope.DomainID)
	for _, dom := range v.MyDomains {
		if dom.ID == settings.Scope.DomainID {
			d.DomainColor = dom.Color
		}
	}
	if game, ok := w.svc.Game(c.Request.Context(), room); ok && game.Status == sqlc.GameStatusPlaying {
		d.Playing, d.GameID = true, game.ID.String()
	}
	d.Signature = lobbySignature(d)
	if d.Chat {
		d.Lines, d.ChatAt = w.chatLines(c, v, room)
	}
	return d
}

// shortLinkIn sends sirāj.app/r/CODE to the room itself. Signing in happens on the
// way, so a link shared with someone who has never played still lands in the lobby.
func (w *Web) shortLinkIn(c *gin.Context) {
	code := strings.ToUpper(strings.TrimSpace(c.Param("code")))
	render.Redirect(c, "/rooms/"+code)
}

// shortLink is the address the code card prints under the six characters: the host
// reads it out, so it drops the scheme the browser adds back anyway.
func shortLink(baseURL, code string) string {
	short := strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
	return strings.TrimSuffix(short, "/") + "/r/" + code
}

// settingsPanel answers the panel's own poll (RM-01, RM-02).
func (w *Web) settingsPanel(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	render.Page(c, pages.RoomSettings(v, w.lobbyData(c, v, room)))
}

// settings saves the Room settings panel (RM-01).
func (w *Web) settings(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	in := SettingsInput{AllowGuests: c.PostForm("allow_guests") != ""}
	if n, err := strconv.Atoi(c.PostForm("count")); err == nil {
		in.Count = int32(n)
	}
	if n, err := strconv.Atoi(c.PostForm("seconds")); err == nil {
		in.Seconds = int32(n)
	}
	if err := w.svc.UpdateSettings(c.Request.Context(), v.User, room, in); err != nil {
		w.toastOrFail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

func (w *Web) ready(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	if err := w.svc.Ready(c.Request.Context(), v.User, room, c.PostForm("ready") == "true"); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

func (w *Web) leave(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	_ = w.svc.Leave(c.Request.Context(), v.User, room)
	render.Redirect(c, "/play")
}

// lobbySignature is everything the live block shows, in one short string: who is in,
// who is ready, and whether the game has started.
func lobbySignature(d pages.RoomData) string {
	h := fnv.New64a()
	for _, m := range d.Members {
		_, _ = h.Write([]byte(m.ID))
		if m.Ready {
			_, _ = h.Write([]byte("r"))
		}
		if m.Host {
			_, _ = h.Write([]byte("h"))
		}
	}
	if d.Playing {
		_, _ = h.Write([]byte("p"))
	}
	_, _ = h.Write([]byte(d.Mode))
	for k, side := range d.Sides {
		for _, m := range side {
			_, _ = h.Write([]byte(fmt.Sprint(k) + m.ID))
		}
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// players answers the lobby's own poll: only the block that changes while people
// arrive. When the host has started, every waiting screen is sent into the game, so
// the room goes together without anyone watching a banner (RM-02, RM-03).
func (w *Web) players(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	data := w.lobbyData(c, v, room)
	if data.Playing {
		render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code)+"/play")
		return
	}
	// Nothing has changed since the screen drew this block: say so, and htmx leaves
	// the cards exactly where they are.
	if data.Signature == c.Query("at") {
		c.Status(http.StatusNoContent)
		return
	}
	render.Page(c, pages.RoomLive(v, data))
}

// chat answers the panel's own poll, and says nothing when nothing was said.
func (w *Web) chat(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	lines, at := w.chatLines(c, v, room)
	if at == c.Query("at") {
		c.Status(http.StatusNoContent)
		return
	}
	render.Page(c, pages.RoomChat(v, strings.TrimSpace(room.Code), lines, at))
}

// say posts one line and hands back the panel with it in.
func (w *Web) say(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	if err := w.svc.Say(c.Request.Context(), v.User, room, c.PostForm("body")); err != nil {
		if e, ok := apperr.As(err); ok && e.Status < 500 {
			render.Toast(c, e.Message)
		} else {
			w.fail(c, v, err)
			return
		}
	}
	if !render.IsHTMX(c) {
		render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
		return
	}
	lines, at := w.chatLines(c, v, room)
	render.Page(c, pages.RoomChat(v, strings.TrimSpace(room.Code), lines, at))
}

// chatLines reads the conversation and a short stamp of it, so the poll can answer
// "nothing new" without sending the panel again.
func (w *Web) chatLines(c *gin.Context, v *view.Ctx, room sqlc.Room) ([]pages.ChatLine, string) {
	rows, err := w.svc.Chat(c.Request.Context(), room)
	if err != nil {
		return nil, ""
	}
	out := make([]pages.ChatLine, 0, len(rows))
	h := fnv.New64a()
	for _, r := range rows {
		_, _ = h.Write([]byte(r.Author + r.Body + r.When.Format(time.RFC3339Nano)))
		out = append(out, pages.ChatLine{
			Author: r.Author, Body: r.Body, System: r.System,
			FileName: r.FileName, FileURL: r.FileURL,
			Me: !r.System && r.Author == v.Name(),
		})
	}
	return out, strconv.FormatUint(h.Sum64(), 36)
}

// next is the pause between two questions of a live room: it keeps the screen on the
// answer until everyone has answered or the clock runs out, then counts the room into
// the next question (RM-03).
func (w *Web) next(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	game, ok := w.svc.Game(c.Request.Context(), room)
	if !ok {
		c.Status(http.StatusNoContent)
		return
	}
	position, _ := strconv.Atoi(c.Query("at"))
	done, total, err := w.svc.Answered(c.Request.Context(), game.ID, int32(position))
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	ready := done >= total
	if !ready {
		// The question's own clock has the last word: nobody waits for a player who
		// walked away from their phone (NF-05).
		if served, err := w.setup.ServedAt(c, game.ID, int32(position)); err == nil && !served.IsZero() {
			ready = time.Since(served) > time.Duration(game.SecondsPerQuestion+2)*time.Second
		}
	}
	render.Page(c, pages.RoomNext(v, strings.TrimSpace(room.Code), game.ID.String(), int32(position), done, total, ready))
}

// kick is the × on a player card in the lobby: the host making room (RM-02).
func (w *Web) kick(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.PostForm("player"))
	if err != nil {
		w.fail(c, v, apperr.Invalid(map[string]string{"player": "unknown"}))
		return
	}
	if err := w.svc.Kick(c.Request.Context(), v.User, room, id); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

func (w *Web) start(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	if _, err := w.svc.Start(c.Request.Context(), v.User, room); err != nil {
		// The bank shrank (a question was unpublished, or Islamic-only hid one) since
		// the room was set up: say so in the lobby instead of the error page.
		if e, ok := apperr.As(err); ok && (e.Code == "not_enough_questions" || e.Code == "no_questions") {
			render.Toast(c, e.Message)
			render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
			return
		}
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code)+"/play")
}

// play is P15: the room's game, with the live board beside it.
func (w *Web) play(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	game, ok := w.svc.Game(c.Request.Context(), room)
	if !ok {
		render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
		return
	}
	if game.Status == sqlc.GameStatusFinished {
		render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code)+"/podium")
		return
	}
	render.Redirect(c, "/games/"+game.ID.String())
}

// board is the column beside the question, polled on its own: the ranking while
// everyone plays for themselves, and what this question gave each player in teams
// (RM-04, RM-05).
func (w *Web) board(c *gin.Context) {
	v := view.From(c)
	d, ok := w.arenaData(c, v)
	if !ok {
		return
	}
	render.Page(c, pages.LiveBoard(v, d))
}

// arena is the full-width bar over a team game: both totals and the tug between them.
// In everyone-for-themselves it answers an empty shell, so the game screen can ask
// for it without knowing which way the room plays (RM-05).
func (w *Web) arena(c *gin.Context) {
	v := view.From(c)
	d, ok := w.arenaData(c, v)
	if !ok {
		return
	}
	render.Page(c, pages.RoomArena(v, d))
}

// arenaData reads the room's live state once: the standings, the two sides, and how
// far the question on screen has got.
func (w *Web) arenaData(c *gin.Context, v *view.Ctx) (pages.ArenaData, bool) {
	room, ok := w.room(c, v)
	if !ok {
		return pages.ArenaData{}, false
	}
	game, ok := w.svc.Game(c.Request.Context(), room)
	if !ok {
		c.Status(http.StatusNoContent)
		return pages.ArenaData{}, false
	}
	code := strings.TrimSpace(room.Code)
	// The question on screen is the one the viewer is answering; the strip follows it
	// so "+points" only appear once this player has seen the answer too (RM-06).
	position := int32(-1)
	if n, err := strconv.Atoi(c.Query("at")); err == nil {
		position = int32(n)
	}
	rows, err := w.svc.Standings(c.Request.Context(), v.User, game.ID, position)
	if err != nil {
		c.Status(http.StatusNoContent)
		return pages.ArenaData{}, false
	}
	d := pages.ArenaData{Code: code, Teams: HasTeams(rows), Position: position + 1, Total: len(rows)}
	best := int32(1)
	for _, r := range rows {
		if r.Score > best {
			best = r.Score
		}
	}
	for i, r := range rows {
		row := w.standing(r, i+1)
		row.Share = int(r.Score * 100 / best)
		if position >= 0 {
			row.Answered = r.AnsweredThis
		} else {
			row.Answered = r.Done
		}
		// The room has seen the answer once everyone still playing has given one.
		if row.Answered {
			d.Answered++
		}
		d.Rows = append(d.Rows, row)
		if row.Team == 0 || row.Team == 1 {
			d.Sides[row.Team] = append(d.Sides[row.Team], row)
		}
	}
	d.Revealed = d.Total > 0 && d.Answered == d.Total
	for i := range d.Rows {
		d.Rows[i].Revealed = d.Revealed
	}
	for k := range d.Sides {
		for i := range d.Sides[k] {
			d.Sides[k][i].Revealed = d.Revealed
		}
	}
	totals, gains, _ := TeamTotals(rows)
	d.Totals, d.Gains = totals, gains
	sum := totals[0] + totals[1]
	d.ShareA = 50
	if sum > 0 {
		d.ShareA = int(totals[0] * 100 / sum)
	}
	d.Lead, d.Diff = -1, totals[0]-totals[1]
	if d.Diff < 0 {
		d.Lead, d.Diff = 1, -d.Diff
	} else if d.Diff > 0 {
		d.Lead = 0
	}
	return d, true
}

// podium is P16: the result of the room's game, in the shape the room played in.
func (w *Web) podium(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	game, ok := w.svc.Game(c.Request.Context(), room)
	if !ok {
		render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
		return
	}
	rows, err := w.svc.Standings(c.Request.Context(), v.User, game.ID, -1)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	d := pages.RoomResult{
		Code: strings.TrimSpace(room.Code), Host: room.HostID == v.User.ID,
		// Everyone in the room answered the same game, so "Review my answers" is the
		// results of that game read as this player (RM-04).
		GameID: game.ID.String(), Questions: int(game.QuestionCount),
		Teams: HasTeams(rows), MyTeam: -1, Winner: -1,
		CanAddFriends: !v.IsGuest(),
	}
	for i, r := range rows {
		row := w.standing(r, i+1)
		d.Rows = append(d.Rows, row)
		if row.Team == 0 || row.Team == 1 {
			d.Sides[row.Team] = append(d.Sides[row.Team], row)
		}
		if row.IsMe {
			d.Mine, d.HasMine, d.MyTeam = row, true, row.Team
			d.MyXP, d.MyBestMs = r.XP, r.BestMs
		}
	}
	d.DomainName = w.domainName(v, w.svc.Settings(room).Scope.DomainID)
	if len(rows) > 0 {
		d.MVP = rows[0].Name
	}
	if d.Teams {
		totals, _, _ := TeamTotals(rows)
		d.Totals = totals
		switch {
		case totals[0] > totals[1]:
			d.Winner, d.Diff = 0, totals[0]-totals[1]
		case totals[1] > totals[0]:
			d.Winner, d.Diff = 1, totals[1]-totals[0]
		}
		d.Awards = w.awards(v, rows)
	}
	render.Page(c, pages.RoomPodium(v, d))
}

// awards are the three cards the design closes a team result with: who scored most,
// who answered fastest, and who strung the longest run of right answers together
// (RM-07).
func (w *Web) awards(v *view.Ctx, rows []Standing) []pages.RoomAward {
	var most, fastest, streak *Standing
	for i := range rows {
		r := &rows[i]
		if most == nil || r.Score > most.Score {
			most = r
		}
		if r.Correct > 0 && r.AvgMs > 0 && (fastest == nil || r.AvgMs < fastest.AvgMs) {
			fastest = r
		}
		if r.Streak > 1 && (streak == nil || r.Streak > streak.Streak) {
			streak = r
		}
	}
	out := make([]pages.RoomAward, 0, 3)
	add := func(key string, r *Standing, detail string) {
		if r == nil || r.Name == "" {
			return
		}
		out = append(out, pages.RoomAward{
			Key: key, Name: r.Name, Initials: avatar.Initials(r.Name),
			Color: avatar.Color(r.UserID.String()), Detail: detail,
		})
	}
	if most != nil {
		add("most_points", most, v.Tr("rooms.award_points_detail", "n", most.Score))
	}
	if fastest != nil {
		add("fastest", fastest, v.Tr("rooms.award_fastest_detail", "s", pages.Seconds(fastest.AvgMs)))
	}
	if streak != nil {
		add("comeback", streak, v.Tr("rooms.award_streak_detail", "n", streak.Streak))
	}
	return out
}

func (w *Web) again(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	if err := w.svc.Reopen(c.Request.Context(), v.User, room); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

// standing turns one service row into the line the screens draw.
func (w *Web) standing(r Standing, place int) pages.RoomStanding {
	return pages.RoomStanding{
		ID: nonNil(r.UserID), Name: r.Name, Initials: avatar.Initials(r.Name),
		Color: avatar.Color(r.UserID.String()), Score: r.Score, Correct: r.Correct,
		Done: r.Done, IsMe: r.IsMe, Guest: r.Guest, Rank: place, Team: r.Team,
		CorrectThis: r.CorrectThis, Gain: r.Gain, AvgMs: r.AvgMs, Streak: r.Streak,
	}
}

// nonNil keeps the zero uuid of a pass-the-device player out of the markup, so the
// row shows no "add friend" button for someone who has no account.
func nonNil(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// mode is the host moving the room between everyone-for-themselves and teams (RM-05).
func (w *Web) mode(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	if err := w.svc.SetMode(c.Request.Context(), v.User, room, c.PostForm("mode")); err != nil {
		w.toastOrFail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

// moveTeam is "Move here": a player crossing to the other side.
func (w *Web) moveTeam(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	side, _ := strconv.Atoi(c.PostForm("team"))
	if err := w.svc.Move(c.Request.Context(), v.User, room, side); err != nil {
		w.toastOrFail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

// shuffleTeams is endpoint 119: the host dealing both sides again.
func (w *Web) shuffleTeams(c *gin.Context) {
	v := view.From(c)
	room, ok := w.room(c, v)
	if !ok {
		return
	}
	if err := w.svc.Shuffle(c.Request.Context(), v.User, room); err != nil {
		w.toastOrFail(c, v, err)
		return
	}
	render.Redirect(c, "/rooms/"+strings.TrimSpace(room.Code))
}

// toastOrFail keeps a refused change on the screen it came from: the lobby says why
// instead of walking the whole room onto an error page.
func (w *Web) toastOrFail(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok && e.Status < 500 {
		render.Toast(c, e.Message)
		render.Redirect(c, "/rooms/"+strings.TrimSpace(c.Param("code")))
		return
	}
	w.fail(c, v, err)
}

func (w *Web) room(c *gin.Context, v *view.Ctx) (sqlc.Room, bool) {
	room, err := w.svc.ByCode(c.Request.Context(), c.Param("code"))
	if err != nil {
		w.fail(c, v, err)
		return sqlc.Room{}, false
	}
	return room, true
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

var _ = fmt.Sprint

// RoomCode answers the games module when a live game wants the room's board (P15).
func (w *Web) RoomCode(roomID uuid.UUID) string {
	room, err := w.svc.Room(context.Background(), roomID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(room.Code)
}

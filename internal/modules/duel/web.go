package duel

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

type Web struct{ svc *Service }

func NewWeb(s *Service) *Web { return &Web{svc: s} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/duel", w.page)
	private.POST("/duel/search", w.search)
	private.POST("/duel/cancel", w.cancel)
	private.GET("/duel/:id/bar", w.bar)
}

// page is P17. While the player is searching it polls itself, so the moment someone
// else joins the queue both sides move on together (DU-02).
func (w *Web) page(c *gin.Context) {
	v := view.From(c)
	state, err := w.svc.State(c.Request.Context(), v.User)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if state.Searching {
		// Every poll is also a chance to match: whoever asks first pairs the two.
		if next, err := w.svc.Search(c.Request.Context(), v.User, domainOf(c, v)); err == nil {
			state = next
		}
	}
	domainID := domainOf(c, v)
	available, err := w.svc.QuestionsAvailable(c.Request.Context(), v.User, domainID)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data := pages.DuelData{
		Searching: state.Searching, GameID: gameString(state.GameID),
		Rating: state.Rating, Wins: state.Wins, Losses: state.Losses,
		DomainID: domainID, DomainName: domainName(v, domainID), Guest: v.IsGuest(),
		Available: available, Need: Questions, Seconds: Seconds,
		Me: pages.DuelPlayer{Level: v.Level, Rating: state.Rating},
	}
	// Once the pairing is done both faces go on the card, so the player sees who they
	// are about to race (DU-01).
	if state.GameID != uuid.Nil {
		if other, err := w.svc.Opponent(c.Request.Context(), v.User, state.GameID); err == nil && other.Found {
			data.Them = opponentCard(other)
			if rate, ok := WinRate(other.Wins, other.Losses); ok {
				data.Them.WinRate, data.Them.HasRate = rate, true
			}
			// How the two of them stand, and the duel they played last (DU-01).
			if rec, err := w.svc.Record(c.Request.Context(), v.User, other.UserID); err == nil {
				data.H2HWins, data.H2HLosses = rec.Wins, rec.Losses
				data.HasLast, data.LastWon = rec.HasLast, rec.Won()
				data.LastMine, data.LastThem = rec.LastMine, rec.LastThem
				data.LastDom = domainName(v, rec.LastDomn)
				data.LastWhen = whenAgo(v, rec.LastWhen)
			}
		}
	}
	if rate, ok := WinRate(state.Wins, state.Losses); ok {
		data.Me.WinRate, data.Me.HasRate = rate, true
	}
	render.Page(c, pages.Duel(v, data))
}

func (w *Web) search(c *gin.Context) {
	v := view.From(c)
	if _, err := w.svc.Search(c.Request.Context(), v.User, domainOf(c, v)); err != nil {
		if e, ok := apperr.As(err); ok && (e.Code == "not_enough_questions" || e.Code == "no_questions") {
			render.Toast(c, e.Message)
			render.Redirect(c, "/duel?domain="+strconv.FormatInt(domainOf(c, v), 10))
			return
		}
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/duel")
}

func (w *Web) cancel(c *gin.Context) {
	v := view.From(c)
	if err := w.svc.Cancel(c.Request.Context(), v.User); err != nil {
		w.fail(c, v, err)
		return
	}
	render.Redirect(c, "/duel")
}

// bar is the score strip of P18, polled beside the question card.
func (w *Web) bar(c *gin.Context) {
	v := view.From(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	game, err := w.svc.games.Load(c.Request.Context(), v.User, id)
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	state, err := w.svc.games.State(c.Request.Context(), game)
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	other, err := w.svc.Opponent(c.Request.Context(), v.User, id)
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	me := pages.DuelPlayer{
		Score: state.Score, Results: state.Results, Level: v.Level,
		Answered: int32(len(state.Results)) > state.Position,
	}
	render.Page(c, pages.DuelBar(v, id.String(), me, opponentCard(other)))
}

// opponentCard maps the other player onto what the screens show of them.
func opponentCard(o Opponent) pages.DuelPlayer {
	return pages.DuelPlayer{
		Name: o.Name, Initials: avatar.Initials(o.Name), Color: avatar.Color(o.Name),
		Level: o.Level, Rating: o.Rating, Score: o.Score, Done: o.Done, Results: o.Results,
		Answered: o.Answered,
	}
}

// whenAgo reads a moment the way the card does: today, yesterday, or a number of
// days. Anything older is simply the date the player's locale writes.
func whenAgo(v *view.Ctx, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	days := int(time.Since(at).Hours() / 24)
	switch {
	case days <= 0:
		return v.Tr("time.today")
	case days == 1:
		return v.Tr("time.yesterday")
	case days < 30:
		return v.Tr("time.days_ago", "n", days)
	}
	return at.Format("2006-01-02")
}

// domainName is the name of the domain the duel runs on, for the searching line.
func domainName(v *view.Ctx, id int64) string {
	for _, d := range v.MyDomains {
		if d.ID == id {
			return d.Name
		}
	}
	return ""
}

// domainOf is the domain in focus: the one asked for, else the player's first.
func domainOf(c *gin.Context, v *view.Ctx) int64 {
	raw := c.PostForm("domain")
	if raw == "" {
		raw = c.Query("domain")
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		return id
	}
	if len(v.MyDomains) > 0 {
		return v.MyDomains[0].ID
	}
	return 0
}

func gameString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok {
		withFlash := v.Clone()
		withFlash.Flash = e.Message
		render.Status(c, e.Status, pages.ServerError(withFlash))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

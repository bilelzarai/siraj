// Package live is the website's end of the event stream: one connection per player,
// and the little fragments the screens re-ask for when it tells them something
// changed (NF-02 — the server still renders everything).
package live

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/render"
	"siraj/internal/platform/live"
	"siraj/internal/view"
	"siraj/web/templates/layouts"
)

// keepalive keeps proxies from closing a quiet stream.
const keepalive = 25 * time.Second

type Web struct{ hub *live.Hub }

func NewWeb(hub *live.Hub) *Web { return &Web{hub: hub} }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/events", w.events)
	private.GET("/nav/badges", w.badges)
}

// events is the stream itself. It writes one line per signal and a comment every few
// seconds so nothing in between cuts it off.
func (w *Web) events(c *gin.Context) {
	v := view.From(c)
	if v.User == nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	stream := w.hub.Open(v.User.ID)
	defer stream.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("Connection", "keep-alive")
	// nginx and friends buffer by default, which would hold every signal back.
	c.Header("X-Accel-Buffering", "no")

	// Say hello at once: the headers only reach the browser on the first write, and a
	// stream that says nothing for half a minute looks like a stalled request.
	_, _ = io.WriteString(c.Writer, ": open\n\n")
	c.Writer.Flush()

	ticker := time.NewTicker(keepalive)
	defer ticker.Stop()
	done := c.Request.Context().Done()

	c.Stream(func(out io.Writer) bool {
		select {
		case topic, ok := <-stream.C:
			if !ok {
				return false
			}
			_, _ = fmt.Fprintf(out, "event: %s\ndata: 1\n\n", topic)
			return true
		case <-ticker.C:
			_, _ = io.WriteString(out, ": keepalive\n\n")
			return true
		case <-done:
			return false
		}
	})
}

// badges is the navigation's counts on their own, so a screen that hears "something
// changed" can repaint the three numbers and nothing else.
func (w *Web) badges(c *gin.Context) {
	render.Page(c, layouts.NavBadges(view.From(c)))
}

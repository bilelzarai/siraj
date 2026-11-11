package live

import (
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"

	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

// API is the app's end of the event stream. Endpoint 111 is written as a WebSocket
// in the architecture; until /ws exists this is the same stream over server-sent
// events, which every mobile HTTP client can read: one line per signal, no payload,
// the client re-reads whatever changed from its own endpoint.
type API struct{ hub *live.Hub }

func NewAPI(hub *live.Hub) *API { return &API{hub: hub} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/events", a.events)
}

func (a *API) events(c *gin.Context) {
	who := authctx.Get(c)
	stream := a.hub.Open(who.ID)
	defer stream.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
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

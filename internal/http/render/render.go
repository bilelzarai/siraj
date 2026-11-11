package render

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

// IsHTMX reports whether the request came from htmx, which means the handler should
// answer with the partial only, not the whole page (one handler pair per screen).
func IsHTMX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

// Boosted requests are htmx page swaps: they still want the full page body.
func Boosted(c *gin.Context) bool { return c.GetHeader("HX-Boosted") == "true" }

// Page writes a templ component with 200.
func Page(c *gin.Context, comp templ.Component) {
	Status(c, http.StatusOK, comp)
}

// Status writes a templ component with an explicit status code.
//
// Every page carries the viewer's own name, their CSRF token and their state, so none
// of it may be kept: no store, no back-forward cache of a signed-in screen, and no
// stale copy of a page the developer has just changed (NF-01).
func Status(c *gin.Context, code int, comp templ.Component) {
	c.Status(code)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store, must-revalidate")
	if err := comp.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
	}
}

// Redirect sends htmx and ordinary browsers to the same place.
func Redirect(c *gin.Context, to string) {
	if IsHTMX(c) {
		c.Header("HX-Redirect", to)
		c.Status(http.StatusNoContent)
		return
	}
	c.Redirect(http.StatusSeeOther, to)
}

// Refresh asks htmx to reload the current page (used after settings changes that
// repaint the shell, e.g. language or Islamic-only mode).
func Refresh(c *gin.Context) {
	if IsHTMX(c) {
		c.Header("HX-Refresh", "true")
		c.Status(http.StatusNoContent)
		return
	}
	c.Redirect(http.StatusSeeOther, c.Request.URL.Path)
}

// Toast asks the front-end to show a toast after this response.
func Toast(c *gin.Context, message string) {
	payload, err := json.Marshal(map[string]string{"sj:toast": message})
	if err != nil {
		return
	}
	c.Header("HX-Trigger", string(payload))
}

// Raw wraps markup the server built itself — the room's QR square, and nothing a
// player ever typed — so a page can print it without templ escaping it.
func Raw(html string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, html)
		return err
	})
}

package settings

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/middleware"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/platform/authctx"
	"siraj/internal/view"
)

// Web holds the website routes that change a setting from the shell: the theme menu
// and the language menu (BR-04, LG-01). The full Settings screen comes with T-45.
type Web struct{ svc *Service }

func NewWeb(s *Service) *Web { return &Web{svc: s} }

// clearSession drops the session cookie after the account is deleted.
func (w *Web) clearSession(c *gin.Context) {
	c.SetCookie(middleware.CookieName, "", -1, "/", "", false, true)
}

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	// Both work for visitors too (cookie only), so they live on the public group.
	public.POST("/settings/theme", w.setTheme)
	public.POST("/settings/language", w.setLanguage)
	w.registerOnboarding(public, private)
	w.registerSettings(public, private)
}

func (w *Web) setTheme(c *gin.Context) {
	theme := c.PostForm("theme")
	if theme == "" {
		theme = c.Query("theme")
	}
	switch theme {
	case "light", "dark", "system":
	default:
		c.Status(http.StatusUnprocessableEntity)
		return
	}
	// The cookie is what the next page render reads for guests and visitors; for members
	// the stored setting wins, so both are written.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(middleware.ThemeCookie, theme, 60*60*24*365, "/", "", c.Request.TLS != nil, false)
	if who := authctx.Get(c); who != nil {
		if err := w.svc.SetTheme(c.Request.Context(), who, theme); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func (w *Web) setLanguage(c *gin.Context) {
	lang := c.PostForm("lang")
	if !i18n.Known(lang) {
		c.Status(http.StatusUnprocessableEntity)
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(middleware.LangCookie, lang, 60*60*24*365, "/", "", c.Request.TLS != nil, false)
	if who := authctx.Get(c); who != nil {
		if err := w.svc.SetLang(c.Request.Context(), who, lang); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		who.Lang = lang
	}
	back := c.PostForm("back")
	if back == "" || back[0] != '/' {
		back = "/"
	}
	_ = view.From(c)
	render.Redirect(c, back)
}

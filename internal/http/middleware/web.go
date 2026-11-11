package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/view"
)

const (
	// CSRFCookie holds the token the page also prints in forms and in <meta name="csrf-token">.
	CSRFCookie = "sj_csrf"
	// CSRFHeader is what htmx sends (see web/assets/js/main.ts).
	CSRFHeader = "X-CSRF-Token"
	// ThemeCookie keeps the theme of guests and signed-out visitors (BR-04).
	ThemeCookie = "sj_theme"
	// LangCookie keeps the language of guests and signed-out visitors (LG-01).
	LangCookie = "sj_lang"
)

// LangWithCookie extends Lang with the cookie step asked for in T-05:
// users.lang (set in Auth) → cookie → Accept-Language → English.
func LangWithCookie() gin.HandlerFunc {
	base := Lang()
	return func(c *gin.Context) {
		if q := c.Query("lang"); !supported[q] {
			if v, err := c.Cookie(LangCookie); err == nil && supported[v] {
				c.Request.Header.Set("Accept-Language", v)
			}
		}
		base(c)
	}
}

// CSRF protects every unsafe request a browser can be tricked into sending: the website's
// own forms and htmx calls, and any /api/v1 call that authenticates with the session
// cookie. A mobile client signing in has no cookie and no Bearer token yet, so /api/v1
// requests without a session cookie are exempt — there is no ambient credential to abuse.
func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		// A token client has no cookie jar to protect, and an API answer that sets
		// cookies only confuses it: /api/ calls that carry no session are left alone.
		if tokenClient(c) {
			c.Next()
			return
		}
		token, err := c.Cookie(CSRFCookie)
		if err != nil || len(token) < 20 {
			token = randomToken()
			c.SetSameSite(http.SameSiteLaxMode)
			c.SetCookie(CSRFCookie, token, 60*60*24*365, "/", "", c.Request.TLS != nil, false)
		}
		c.Set("csrf", token)

		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if bearerToken(c) != "" { // mobile client: the token cannot be attached by a browser
			c.Next()
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			if _, err := c.Cookie(CookieName); err != nil {
				c.Next() // no ambient credential: nothing to protect
				return
			}
		}
		sent := c.GetHeader(CSRFHeader)
		if sent == "" {
			sent = c.PostForm("csrf_token")
		}
		if subtle.ConstantTimeCompare([]byte(sent), []byte(token)) != 1 {
			apiutil.Fail(c, apperr.Forbidden("Your page expired. Reload and try again."))
			return
		}
		c.Next()
	}
}

// tokenClient is a call on /api/ that does not ride on the session cookie: either it
// carries a Bearer token, or it is signed out. Neither can be forged by a browser
// holding somebody's session, which is the only thing CSRF defends against.
func tokenClient(c *gin.Context) bool {
	if !strings.HasPrefix(c.Request.URL.Path, "/api/") {
		return false
	}
	if bearerToken(c) != "" {
		return true
	}
	_, err := c.Cookie(CookieName)
	return err != nil
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func bearerToken(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return ""
}

// Shell loads what every web page needs around its own content: the viewer, their
// domains (DM-03), the Islamic-only switch (DM-09) and the navigation badges.
type Shell interface {
	Load(ctx context.Context, p *authctx.Principal, lang string) (ShellData, error)
}

// ShellData is the answer of the shell loader.
type ShellData struct {
	DisplayName string
	Username    string
	Level       int32
	XP          int32
	XPNext      int32
	Theme       string
	IslamicOnly bool
	MyDomains   []view.Domain
	Counts      view.Counts
}

// View builds the per-request view context for web pages.
func View(bundle *i18n.Bundle, assets *render.Assets, shell Shell, dev bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := apiutil.Lang(c)
		p := authctx.Get(c)
		v := &view.Ctx{
			T:      bundle.For(lang),
			Assets: assets,
			Lang:   lang,
			Dir:    i18n.Dir(lang),
			Theme:  themeFromCookie(c),
			CSRF:   c.GetString("csrf"),
			Path:   c.Request.URL.Path,
			Dev:    dev,
			User:   p,
		}
		if p != nil && shell != nil {
			if data, err := shell.Load(c.Request.Context(), p, lang); err == nil {
				v.DisplayName = data.DisplayName
				v.Username = data.Username
				v.Level = data.Level
				v.XP = data.XP
				v.XPNext = data.XPNext
				v.IslamicOnly = data.IslamicOnly
				v.MyDomains = data.MyDomains
				v.Counts = data.Counts
				if data.Theme != "" {
					v.Theme = data.Theme
				}
			} else {
				_ = c.Error(err)
			}
		}
		view.Set(c, v)
		c.Next()
	}
}

func themeFromCookie(c *gin.Context) string {
	if v, err := c.Cookie(ThemeCookie); err == nil {
		switch v {
		case "light", "dark", "system":
			return v
		}
	}
	return "system"
}

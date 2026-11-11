// Package http mounts every route group: the website (templ + htmx), /api/v1 (JSON)
// and later /ws. Web pages and the API call the same services (NF-04).
package http

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apispec "siraj/api"
	"siraj/internal/db"
	"siraj/internal/http/apiutil"
	"siraj/internal/http/middleware"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/view"
	"siraj/web"
	"siraj/web/templates/pages"
)

// Module is what a feature package exposes to the JSON API.
// public = no player needed (visitors and guests); private = a player is required.
type Module interface {
	Register(public, private *gin.RouterGroup)
}

// WebModule is what a feature package exposes to the website. Same split, same services.
type WebModule interface {
	RegisterWeb(public, private *gin.RouterGroup)
}

// Options is everything the router needs from the app wiring.
type Options struct {
	DB       *db.DB
	Resolver middleware.Resolver
	Shell    middleware.Shell
	Bundle   *i18n.Bundle
	Assets   *render.Assets
	Dev      bool
	// CORSOrigins are the origins /api/v1 answers cross-origin (CORS_ORIGINS).
	CORSOrigins []string
	Modules     []Module
	Web         []WebModule
}

func NewRouter(o Options) *gin.Engine {
	if !o.Dev {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	// A client that calls a real path with the wrong verb deserves 405 and an Allow
	// header, not the 404 that would send it looking for a typo.
	r.HandleMethodNotAllowed = true
	_ = r.SetTrustedProxies(nil) // set your reverse proxy's IP here in production

	// Built assets (Vite output, embedded in the binary).
	r.GET("/static/*filepath", staticHandler(o.Dev))

	// ---------------- JSON API ----------------
	// One CORS handler for the group and for the two fallbacks: a browser's preflight
	// lands on neither a route nor a verb, so gin would answer it 405 before the
	// group's middleware ever ran.
	cors := middleware.CORS(o.CORSOrigins)
	api := r.Group("/api/v1", cors, middleware.Lang(), middleware.Auth(o.Resolver), middleware.CSRF())
	api.GET("/health", health(o.DB)) // #113
	api.GET("/config", config(o.Bundle))
	api.GET("/openapi.yaml", openapiSpec) // the contract, from the server itself
	api.GET("/docs", openapiDocs)         // and the page a browser reads it on
	apiPrivate := api.Group("", middleware.RequireAuth())
	for _, m := range o.Modules {
		m.Register(api, apiPrivate)
	}

	// ---------------- website ----------------
	webGroup := r.Group("",
		middleware.LangWithCookie(),
		middleware.Auth(o.Resolver),
		middleware.CSRF(),
		middleware.View(o.Bundle, o.Assets, o.Shell, o.Dev),
	)
	webPrivate := webGroup.Group("", requirePlayerPage())
	for _, m := range o.Web {
		m.RegisterWeb(webGroup, webPrivate)
	}

	r.NoRoute(apiFallback(cors, notFound(o.Bundle, o.Assets, o.Dev)))
	r.NoMethod(apiFallback(cors, methodNotAllowed()))
	return r
}

// requirePlayerPage sends signed-out visitors to the welcome screen instead of a 401.
func requirePlayerPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if view.From(c).SignedIn() {
			c.Next()
			return
		}
		render.Redirect(c, "/welcome")
		c.Abort()
	}
}

// staticHandler serves the built assets. Production reads the copy embedded in the
// binary; development prefers web/dist on disk, so rebuilding the front-end alone is
// enough (the embedded copy is a snapshot taken when the binary was compiled).
func staticHandler(dev bool) gin.HandlerFunc {
	files := http.FileServer(http.FS(web.Dist()))
	if dev {
		if info, err := os.Stat(render.DistDir); err == nil && info.IsDir() {
			files = http.FileServer(http.Dir(render.DistDir))
		}
	}
	return func(c *gin.Context) {
		if !dev {
			// Vite puts a hash in every file name, so they can be cached for a year.
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Header("Cache-Control", "no-store")
		}
		http.StripPrefix("/static/", files).ServeHTTP(c.Writer, c.Request)
	}
}

func notFound(bundle *i18n.Bundle, assets *render.Assets, dev bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiutil.WantsJSON(c) {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found", "message": "No such route"}})
			return
		}
		lang := apiutil.Lang(c)
		if !i18n.Known(lang) {
			lang = i18n.Fallback
		}
		v := &view.Ctx{T: bundle.For(lang), Assets: assets, Lang: lang, Dir: i18n.Dir(lang), Theme: "system", Dev: dev, Path: c.Request.URL.Path}
		render.Status(c, http.StatusNotFound, pages.NotFound(v))
	}
}

// apiFallback runs the CORS handler before a fallback answers, so a cross-origin
// client reads the 404 or the 405 instead of a browser error — and so a preflight on
// an existing path (which gin sees as the wrong verb) is answered 204 by CORS itself.
func apiFallback(cors gin.HandlerFunc, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			cors(c)
			if c.IsAborted() {
				return
			}
		}
		next(c)
	}
}

// methodNotAllowed answers 405 with the verbs the route does take. Gin has already
// worked them out; it puts them in Allow.
func methodNotAllowed() gin.HandlerFunc {
	return func(c *gin.Context) {
		allow := c.Writer.Header().Get("Allow")
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": gin.H{
			"code":    "method_not_allowed",
			"message": "This endpoint does not take " + c.Request.Method + ". Allowed: " + allow,
		}})
	}
}

func health(d *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		dbOK := d.Pool.Ping(ctx) == nil
		status := http.StatusOK
		if !dbOK {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"data": gin.H{"ok": dbOK, "database": dbOK, "lang": apiutil.Lang(c)}})
	}
}

// openapiSpec serves api/openapi.yaml, which describes every endpoint below it.
func openapiSpec(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, apispec.SpecContentType, apispec.Spec)
}

// openapiDocs renders the document above for a person rather than a client.
func openapiDocs(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", apispec.Docs)
}

// config is endpoint 112: what a client needs before signing in.
func config(bundle *i18n.Bundle) gin.HandlerFunc {
	type language struct {
		Code string `json:"code"`
		Name string `json:"name"`
		Dir  string `json:"dir"`
	}
	langs := make([]language, 0, len(i18n.Langs))
	for _, code := range i18n.Langs {
		langs = append(langs, language{code, i18n.Native[code], i18n.Dir(code)})
	}
	return func(c *gin.Context) {
		apiutil.OK(c, gin.H{
			"languages":          langs,
			"default_language":   i18n.Fallback,
			"quick_game_length":  5,
			"min_set_questions":  5,
			"question_counts":    []int{5, 10, 15, 20},
			"timer_seconds":      []int{10, 20, 30, 0},
			"max_room_players":   12,
			"max_device_players": 10,
		})
	}
}

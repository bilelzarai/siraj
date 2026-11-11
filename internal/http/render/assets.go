// Package render turns templ components into Gin responses and resolves asset URLs.
package render

import (
	"encoding/json"
	"os"
	"sync"

	"siraj/web"
)

// Assets answers the two questions a layout asks: which JS and which CSS to load.
// In development with Vite running (VITE_DEV_SERVER set) it points at the dev server so
// changes hot-reload; otherwise it reads the hashed names from the embedded manifest.
type Assets struct {
	viteDev  string
	dev      bool
	mu       sync.RWMutex
	js       string
	css      []string
	adminJS  string
	adminCSS []string
}

// The player app and the admin console are two Vite entries: each carries its own
// stylesheet, so an admin page never loads the player's CSS and the other way round.
const (
	entry      = "assets/js/main.ts"
	adminEntry = "assets/js/admin.ts"
)

// DistDir is where Vite writes in development; the production binary uses the copy
// embedded by web/embed.go instead.
const DistDir = "web/dist"

// NewAssets reads the manifest once for production. In development it re-reads
// web/dist on every page, so `bun run build` is picked up without rebuilding Go.
func NewAssets(viteDevServer string, dev bool) *Assets {
	a := &Assets{viteDev: viteDevServer, dev: dev}
	if viteDevServer == "" {
		a.load()
	}
	return a
}

func (a *Assets) load() {
	var entries map[string]web.Asset
	if a.dev {
		if raw, err := os.ReadFile(DistDir + "/.vite/manifest.json"); err == nil {
			_ = json.Unmarshal(raw, &entries)
		}
	}
	if entries == nil {
		if m, err := web.Manifest(); err == nil {
			entries = m
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e, ok := entries[entry]; ok {
		a.js = "/static/" + e.File
		a.css = a.css[:0]
		for _, c := range e.CSS {
			a.css = append(a.css, "/static/"+c)
		}
	}
	if e, ok := entries[adminEntry]; ok {
		a.adminJS = "/static/" + e.File
		a.adminCSS = a.adminCSS[:0]
		for _, c := range e.CSS {
			a.adminCSS = append(a.adminCSS, "/static/"+c)
		}
	}
}

// refresh keeps development honest without touching production.
func (a *Assets) refresh() {
	if a.dev && a.viteDev == "" {
		a.load()
	}
}

// Dev reports whether the page should load the Vite client (hot reload).
func (a *Assets) Dev() bool { return a.viteDev != "" }

// ViteClient is the module that enables hot reload in development.
func (a *Assets) ViteClient() string { return a.viteDev + "/static/@vite/client" }

// JS is the application entry point.
func (a *Assets) JS() string {
	if a.viteDev != "" {
		return a.viteDev + "/static/" + entry
	}
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.js
}

// AdminJS is the admin console's entry point.
func (a *Assets) AdminJS() string {
	if a.viteDev != "" {
		return a.viteDev + "/static/" + adminEntry
	}
	a.refresh()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.adminJS
}

// AdminCSS is the admin stylesheet, empty while Vite injects it from the JS entry.
func (a *Assets) AdminCSS() []string {
	if a.viteDev != "" {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, len(a.adminCSS))
	copy(out, a.adminCSS)
	return out
}

// CSS is empty when Vite serves the assets: it injects the stylesheet from the JS entry.
func (a *Assets) CSS() []string {
	if a.viteDev != "" {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, len(a.css))
	copy(out, a.css)
	return out
}

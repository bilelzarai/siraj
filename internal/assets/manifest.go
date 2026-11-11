// Package assets resolves a source entry to the file the bundler built from it.
//
// A missing manifest is not an error — it is the dev server, or a tree nobody
// has built yet — so every lookup misses and the page renders without that
// asset rather than refusing to answer. The one-command paths build first, so
// nobody lands there by accident.
package assets

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
)

type Entry struct {
	File    string   `json:"file"`
	Src     string   `json:"src"`
	IsEntry bool     `json:"isEntry"`
	CSS     []string `json:"css"`
	Imports []string `json:"imports"`
}

type Manifest struct {
	entries map[string]Entry
	base    string
	from    string
}

// Both locations are tried rather than one pinned: current Vite writes the
// manifest under a dot-directory inside the output, older versions at the
// output root. An upgrade must not silently start serving unhashed URLs.
var candidates = []string{"dist/.vite/manifest.json", "dist/manifest.json"}

// Load reads the build manifest out of the same filesystem the assets are
// served from, so a binary and its manifest can never disagree.
func Load(staticFS fs.FS, base string) *Manifest {
	m := &Manifest{entries: map[string]Entry{}, base: base}
	for _, c := range candidates {
		raw, err := fs.ReadFile(staticFS, c)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(raw, &m.entries); err != nil {
			continue
		}
		m.from = c
		break
	}
	return m
}

// Loaded reports whether a build was found, and Source names which candidate
// matched — logged at boot, because "which manifest" is the first question
// when a page links the wrong file.
func (m *Manifest) Loaded() bool   { return len(m.entries) > 0 }
func (m *Manifest) Source() string { return m.from }

// Resolve returns the public URL of a built entry, and whether it was found.
func (m *Manifest) Resolve(entry string) (string, bool) {
	e, ok := m.entries[entry]
	if !ok || e.File == "" {
		return "", false
	}
	return path.Join(m.base, e.File), true
}

// Stylesheets is how the link tag finds its hashed filename: the stylesheet is
// imported from js/app.js, so the bundler records it under that entry.
func (m *Manifest) Stylesheets(entry string) []string {
	e, ok := m.entries[entry]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(e.CSS))
	for _, href := range e.CSS {
		out = append(out, path.Join(m.base, href))
	}
	return out
}

// Links is what a page asks for its assets. It answers from the dev server
// when one is configured and from the build otherwise, so the templates know
// about neither.
type Links struct {
	manifest *Manifest
	dev      string // the dev origin, empty everywhere but a developer's laptop
	version  string // the content hash appended to files the bundler did not name
	base     string
}

func NewLinks(m *Manifest, devOrigin, version, base string) Links {
	return Links{manifest: m, dev: strings.TrimRight(devOrigin, "/"), version: version, base: base}
}

// Script returns the URL for a bundler entry such as "js/app.js".
//
// The dev server serves under the same base the built files are served from —
// it is told the same string — so the origin alone is not the answer.
func (l Links) Script(entry string) (string, bool) {
	if l.dev != "" {
		return l.dev + path.Join(l.base, entry), true
	}
	if l.manifest != nil {
		return l.manifest.Resolve(entry)
	}
	return "", false
}

// Stylesheets returns the stylesheets belonging to an entry. The dev server
// injects them through the module graph, so there are none to link.
func (l Links) Stylesheets(entry string) []string {
	if l.dev != "" {
		return nil
	}
	if l.manifest == nil {
		return nil
	}
	return l.manifest.Stylesheets(entry)
}

// DevClient is the dev server's own module, which is what performs the
// replacement when a source file changes. Empty when no dev server is
// configured, which is every deployment.
func (l Links) DevClient() string {
	if l.dev == "" {
		return ""
	}
	return l.dev + path.Join(l.base, "@vite/client")
}

// Copied returns the URL of a file the bundler copies verbatim — boot.js,
// which must stay a classic blocking script. It carries the tree's content
// hash, since the bundler did not put one in its name.
func (l Links) Copied(name string) string {
	if l.dev != "" {
		return l.dev + path.Join(l.base, name)
	}
	url := path.Join(l.base, name)
	if l.version == "" {
		return url
	}
	return url + "?v=" + l.version
}

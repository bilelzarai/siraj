// Package web embeds the built front-end assets (Vite output) so the binary ships alone.
package web

import (
	"embed"
	"encoding/json"
	"io/fs"
)

// dist is written by `bun run build` (see vite.config.ts). all: keeps .vite/manifest.json.
//
//go:embed all:dist
var dist embed.FS

// Dist is the built asset tree, served under /static.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed above guarantees the directory exists
	}
	return sub
}

// Asset is one entry of Vite's manifest.
type Asset struct {
	File string   `json:"file"`
	CSS  []string `json:"css"`
}

// Manifest maps a source path ("assets/js/main.ts") to its hashed output.
func Manifest() (map[string]Asset, error) {
	raw, err := dist.ReadFile("dist/.vite/manifest.json")
	if err != nil {
		return nil, err
	}
	m := map[string]Asset{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Package app exists only to embed the web assets that live at the repository
// root. go:embed can only reach files under the embedding package's own
// directory, so the declaration has to sit here rather than in cmd/server.
package app

import "embed"

//go:embed all:static
var staticFS embed.FS

// StaticFS returns the embedded asset tree rooted at static/.
func StaticFS() embed.FS { return staticFS }

package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// assetBase is where the bundler's output is served from. It is the same
// string the bundler is told to write into its URLs; they are two halves of
// one agreement, which is why it is a named constant rather than a literal in
// three places.
const assetBase = "/static/dist/"

// assetVersion is a short content hash of every static file, appended to asset
// URLs as ?v=. Without it a browser happily serves a year-old stylesheet from
// its cache after a deploy, and the page renders with half its rules missing.
func assetVersion(staticFS fs.FS) string {
	sum := sha256.New()

	err := fs.WalkDir(staticFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		// Only the files a page actually links to need to move the hash.
		switch path.Ext(p) {
		case ".css", ".js", ".svg":
		default:
			return nil
		}
		f, err := staticFS.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()

		sum.Write([]byte(p))
		_, err = io.Copy(sum, f)
		return err
	})
	if err != nil {
		slog.Warn("could not hash static assets; falling back to an unversioned URL", "error", err)
		return "dev"
	}

	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))[:10]
}

// staticHandler serves the embedded assets. Because every URL carries a content
// hash, a hit can be cached indefinitely — but only in production: in
// development the files change under a running server, so nothing is cached.
func staticHandler(staticFS fs.FS, dev bool) http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))

	return http.StripPrefix("/static/", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if dev {
				w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			} else if r.URL.Query().Get("v") != "" {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				// An unversioned request cannot be assumed fresh.
				w.Header().Set("Cache-Control", "public, max-age=300")
			}

			// Never let a directory listing leak the asset tree.
			if strings.HasSuffix(r.URL.Path, "/") {
				http.NotFound(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		}))
}

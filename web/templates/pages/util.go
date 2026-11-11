package pages

import (
	"net/url"
	"strconv"
	"strings"
)

// first falls back when a node carries no icon or colour of its own: a row must still
// draw rather than leave an empty square.
func first(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// itoa keeps the templates free of strconv noise.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// WithQuery is withQuery for the handlers.
func WithQuery(path, key, value string) string { return withQuery(path, key, value) }

// withQuery appends one parameter to a path that may already carry some.
func withQuery(path, key, value string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + key + "=" + url.QueryEscape(value)
}

package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// allowedHeaders is what an external client may send: the Bearer token, the body's
// type, the language it wants, and the two headers the browser-based clients use.
const allowedHeaders = "Authorization, Content-Type, Accept, Accept-Language, X-CSRF-Token, Idempotency-Key"

// exposedHeaders is what it may read back.
const exposedHeaders = "Content-Length, Content-Language"

// CORS answers the browser's cross-origin rules for /api/v1. A mobile app never needs
// it — there is no origin to check — but a web client served from another host does,
// and without it the browser refuses the response even though the server answered.
//
// Only the origins named in CORS_ORIGINS are allowed. The list is matched exactly;
// "*" allows any origin and is meant for development. Credentials are never allowed
// cross-origin: an external client authenticates with a Bearer token, so the cookie
// session stays same-origin and CSRF keeps its meaning.
func CORS(origins []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(origins))
	any := false
	for _, o := range origins {
		if o == "*" {
			any = true
			continue
		}
		allowed[strings.TrimRight(o, "/")] = true
	}
	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		// The answer depends on the origin, so caches must not share it.
		c.Writer.Header().Add("Vary", "Origin")
		if origin != "" && (any || allowed[origin]) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Expose-Headers", exposedHeaders)
		}
		if c.Request.Method != http.MethodOptions {
			c.Next()
			return
		}
		// Preflight: answer it here, so an unknown route does not turn into a 404
		// the browser reports as a CORS failure.
		c.Writer.Header().Add("Vary", "Access-Control-Request-Method")
		c.Writer.Header().Add("Vary", "Access-Control-Request-Headers")
		if origin == "" || !(any || allowed[origin]) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", allowedHeaders)
		c.Header("Access-Control-Max-Age", "600")
		c.AbortWithStatus(http.StatusNoContent)
	}
}

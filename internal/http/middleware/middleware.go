// Package middleware: language, authentication (Bearer token or session cookie), access rules.
package middleware

import (
	"context"
	"github.com/google/uuid"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/apiutil"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

var supported = map[string]bool{"ar": true, "en": true, "fr": true}

// Lang picks ar | en | fr from ?lang=, then Accept-Language; English otherwise.
// A signed-in player's own language wins over the header (applied in Auth).
func Lang() gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := "en"
		if q := c.Query("lang"); supported[q] {
			lang = q
		} else {
			for _, part := range strings.Split(c.GetHeader("Accept-Language"), ",") {
				tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
				if base := strings.SplitN(tag, "-", 2)[0]; supported[base] {
					lang = base
					break
				}
			}
		}
		c.Set("lang", lang)
		c.Next()
	}
}

// Resolver turns a raw token into a principal (implemented by the auth module).
type Resolver interface {
	Resolve(ctx context.Context, token string) (*authctx.Principal, error)
	// Seen marks the player as here right now, which is what the online dot and the
	// "Online now" row read (FR-02).
	Seen(ctx context.Context, userID uuid.UUID)
}

const CookieName = "siraj_session"

// TokenFrom reads "Authorization: Bearer <token>" (mobile) or the session cookie (web).
func TokenFrom(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if v, err := c.Cookie(CookieName); err == nil {
		return v
	}
	return ""
}

// Auth attaches the principal when a valid token is present. It never rejects;
// RequireAuth does that for routes that need a player.
func Auth(r Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tok := TokenFrom(c); tok != "" {
			if p, err := r.Resolve(c.Request.Context(), tok); err == nil && p != nil {
				authctx.Set(c, p)
				// Being here is what "online now" means: the resolver stamps the
				// player, at most once a minute (FR-02).
				r.Seen(c.Request.Context(), p.ID)
				if c.Query("lang") == "" && c.GetHeader("Accept-Language") == "" && supported[p.Lang] {
					c.Set("lang", p.Lang)
				}
			}
		}
		c.Next()
	}
}

// RequireAuth rejects anonymous requests (guests are fine: they are real users).
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if authctx.Get(c) == nil {
			apiutil.Fail(c, apperr.Unauthorized("Sign in or start a guest session first"))
			return
		}
		c.Next()
	}
}

// RequireMember rejects guests (for features guests can't use: ranked duels, leaderboards, …).
func RequireMember() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := authctx.Get(c)
		if p == nil || p.IsGuest() {
			apiutil.Fail(c, apperr.Forbidden("Create a free account to use this"))
			return
		}
		c.Next()
	}
}

// RequireStaff guards every /admin route: translator, moderator, support or admin.
func RequireStaff() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authctx.Get(c).IsStaff() {
			apiutil.Fail(c, apperr.Forbidden("Staff only"))
			return
		}
		c.Next()
	}
}

// RequireRole guards one admin action, e.g. RequireRole("moderator") for the review queue.
// Admins always pass. See docs/architecture.md → Admin back office for the role matrix.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authctx.Get(c).HasRole(roles...) {
			apiutil.Fail(c, apperr.Forbidden("Your role can't do this"))
			return
		}
		c.Next()
	}
}

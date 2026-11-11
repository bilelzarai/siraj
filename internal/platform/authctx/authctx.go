// Package authctx carries the signed-in player (member or guest) through a request.
package authctx

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Principal struct {
	ID          uuid.UUID
	Kind        string // guest | member
	Role        string // player | translator | moderator | support | admin
	Lang        string
	IslamicOnly bool // user_settings.islamic_only: general domains are hidden everywhere
}

func (p *Principal) IsGuest() bool { return p != nil && p.Kind == "guest" }

const key = "siraj.principal"

func Set(c *gin.Context, p *Principal) { c.Set(key, p) }

// Get returns the principal, or nil when the request is anonymous.
func Get(c *gin.Context) *Principal {
	if v, ok := c.Get(key); ok {
		if p, ok := v.(*Principal); ok {
			return p
		}
	}
	return nil
}

// Staff roles can open /admin. Players (members or guests) cannot.
var staffRoles = map[string]bool{"translator": true, "moderator": true, "support": true, "admin": true}

func (p *Principal) IsStaff() bool { return p != nil && p.Kind == "member" && staffRoles[p.Role] }

// HasRole is true for any listed role; admin always passes.
func (p *Principal) HasRole(roles ...string) bool {
	if p == nil || p.Kind != "member" {
		return false
	}
	if p.Role == "admin" {
		return true
	}
	for _, r := range roles {
		if p.Role == r {
			return true
		}
	}
	return false
}

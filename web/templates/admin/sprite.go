// Package admin holds the admin console's layout and screens (A01–A11).
// The console is staff-only and web-only; every write also records audit_log.
package admin

import (
	_ "embed"

	"github.com/a-h/templ"
)

// iconSprite is the inline <symbol> set the admin markup references with <use href="#i-…">.
// It comes straight from the design kit's admin pages, so an icon name in the prototype
// is the same icon here.
//
//go:embed sprite.html
var iconSprite string

// Sprite renders the icon set once per page, hidden.
func Sprite() templ.Component { return templ.Raw(iconSprite) }

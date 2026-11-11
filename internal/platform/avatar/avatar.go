// Package avatar is the one place a person's colour and initials are decided.
//
// Eleven files used to carry their own copy of the palette and three slightly
// different copies of the initials rule, which is how the same player ended up with
// a "?" on one screen and an empty circle on another. A page may not import a module,
// so this lives in platform, beside csvlimit, and both sides read it.
package avatar

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// Palette is every colour a generated avatar can take. They are values, not theming:
// a person keeps one colour wherever they appear, so the list must not be reordered.
var Palette = []string{
	"#0b7a5c", "#a61e4d", "#c2410c", "#0b7285", "#364fc7", "#862e9c",
	"#2563eb", "#db2777", "#b45309", "#7c3aed", "#059669", "#be123c",
}

// Color picks the colour of a seed — a user id where one is known, a name where it is
// not. The same seed always gives the same colour, on the server and in any template.
func Color(seed string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return Palette[int(h.Sum32())%len(Palette)]
}

// Initials is the one or two letters drawn in the circle. It walks runes, not bytes,
// so an Arabic or French name gives its own first letters rather than half a
// character, and it answers "?" rather than nothing when there is no name to read —
// an empty circle looks like a bug.
func Initials(name string) string {
	out := make([]rune, 0, 2)
	start := true
	for _, r := range name {
		if unicode.IsSpace(r) {
			start = true
			continue
		}
		if start {
			out = append(out, r)
			start = false
			if len(out) == 2 {
				break
			}
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return strings.ToUpper(string(out))
}

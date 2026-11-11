package avatar

import "testing"

// A player's face must not depend on which screen is drawing it: the same seed gives
// the same colour here, in a handler and in a template, because they all call this.
func TestColorIsStableAndInsidePalette(t *testing.T) {
	for _, seed := range []string{"", "Amina", "7f3a1c20-0000-4000-8000-000000000000", "علي"} {
		got := Color(seed)
		if got != Color(seed) {
			t.Errorf("Color(%q) is not stable", seed)
		}
		found := false
		for _, c := range Palette {
			if c == got {
				found = true
			}
		}
		if !found {
			t.Errorf("Color(%q) = %q, which is not in the palette", seed, got)
		}
	}
}

func TestInitials(t *testing.T) {
	cases := map[string]string{
		"Amina Haddad":      "AH",
		"amina":             "A",
		"  Yusuf   Al Amin": "YA",
		"":                  "?",
		"   ":               "?",
		"\tJean\nPaul":      "JP", // any whitespace separates, not only a space
		"علي حسن":           "عح",
		"Élodie Martin":     "ÉM",
	}
	for in, want := range cases {
		if got := Initials(in); got != want {
			t.Errorf("Initials(%q) = %q, want %q", in, got, want)
		}
	}
}

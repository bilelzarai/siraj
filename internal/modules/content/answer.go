package content

import (
	"math"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Checking an answer (§8). Each kind of question says how its answer is compared,
// through `answer_kind` on its content type — so adding "ordering" or "matching"
// later is a row in the registry and one case here, never a change to the game
// engine, which only ever asks whether an answer was right.
//
// Nothing in this file reads the request: the correct answer is compared on the
// server and never travels to the client before submission (§10.1).

// Answer is what a player sent. Only one field is meaningful per kind, which is what
// lets one shape carry every kind of question.
type Answer struct {
	// Choice is the index a single-choice question was answered with.
	Choice int
	// Choices are the indexes of a multiple-choice answer.
	Choices []int
	// Yes is a true/false answer.
	Yes bool
	// Text is a typed word or a number, as the player wrote it.
	Text string
}

// Correct compares an answer against what the item records as right.
//
// `want` is the item's `answer` column: {"index": 2} · {"indexes": [0,3]} ·
// {"yes": true} · {"text": ["zakat","zakah"]} · {"number": 5, "tolerance": 0.5}.
func Correct(kind string, want map[string]any, got Answer) bool {
	switch kind {
	case "single":
		return got.Choice == intOf(want["index"], -1)

	case "multi":
		// Every right answer and no wrong one: picking three of four when three are
		// right is not the same as knowing which three.
		wanted := intsOf(want["indexes"])
		if len(wanted) == 0 || len(wanted) != len(unique(got.Choices)) {
			return false
		}
		set := map[int]bool{}
		for _, i := range wanted {
			set[i] = true
		}
		for _, i := range got.Choices {
			if !set[i] {
				return false
			}
		}
		return true

	case "boolean":
		yes, ok := want["yes"].(bool)
		return ok && yes == got.Yes

	case "text":
		// Several spellings may be right, and a player should not lose a point to a
		// capital letter, a stray space or an Arabic diacritic.
		for _, accepted := range stringsOf(want["text"]) {
			if sameText(accepted, got.Text) {
				return true
			}
		}
		return false

	case "number":
		value, err := strconv.ParseFloat(strings.TrimSpace(asciiDigits(got.Text)), 64)
		if err != nil {
			return false
		}
		target, ok := floatOf(want["number"])
		if !ok {
			return false
		}
		// A tolerance lets "about 1,400 years" be right without demanding the exact
		// figure; with none, the number has to match.
		tolerance, _ := floatOf(want["tolerance"])
		return math.Abs(value-target) <= tolerance

	default:
		// A kind nobody taught the checker is never silently correct.
		return false
	}
}

// sameText compares two answers the way a reader would: case, surrounding space and
// Arabic diacritics do not change what a word is.
func sameText(a, b string) bool {
	return foldText(a) == foldText(b) && foldText(a) != ""
}

func foldText(s string) string {
	s = norm.NFKD.String(strings.TrimSpace(strings.ToLower(s)))
	var b strings.Builder
	for _, r := range s {
		switch {
		// Arabic combining marks: fatha through sukun, plus the superscript alef.
		case r >= 0x064B && r <= 0x0652, r == 0x0670:
			continue
		// The tatweel is decoration, never part of a word.
		case r == 0x0640:
			continue
		case r == ' ':
			// Runs of space are one space.
			if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") {
				b.WriteByte(' ')
			}
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// asciiDigits lets a number typed in Arabic-Indic digits be read as a number.
func asciiDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '٠' && r <= '٩': // Arabic-Indic
			b.WriteRune('0' + (r - '٠'))
		case r >= '۰' && r <= '۹': // Extended Arabic-Indic
			b.WriteRune('0' + (r - '۰'))
		case r == '٫': // Arabic decimal separator
			b.WriteByte('.')
		case r == ',' || r == '٬': // thousands separators
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func intOf(v any, fallback int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return fallback
}

func floatOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func intsOf(v any) []int {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, item := range raw {
		out = append(out, intOf(item, -1))
	}
	return out
}

func stringsOf(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func unique(v []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(v))
	for _, i := range v {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	return out
}

package service

import (
	"strings"
	"unicode"

	"github.com/bilelzarai/siraj/internal/models"
)

// maxDiffWords caps what the word diff will line up. Lining two texts up costs
// O(n·m), and past a few hundred words the difference between two prompts is
// not what anyone is squinting at anyway — so the panel shows both sides whole
// rather than spending the time.
const maxDiffWords = 400

// sameText reports whether two strings say the same thing. Spacing and case are
// set aside: "114 Surahs" against "114  surahs" is not a difference worth
// putting in front of an admin, while a missing question mark is, so
// punctuation is left alone.
func sameText(a, b string) bool { return normalizeText(a) == normalizeText(b) }

func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// diffWords lines two strings up word by word and returns each side cut into
// runs the other side also has and runs it does not.
//
// Word level rather than character level on purpose. "Badr" and "Uhud" share
// no letters, but "Badr" and "Bakr" share three — and a character diff paints
// that as a single letter flickering inside a word, when the thing the reader
// needs to see is that the word changed at all.
//
// Both sides come back empty when there is nothing to line up: one side blank,
// or a text too long to be worth the table.
func diffWords(a, b string) (leftSpans, rightSpans []models.DiffSpan) {
	left, right := splitWords(a), splitWords(b)
	if len(left) == 0 || len(right) == 0 ||
		len(left) > maxDiffWords || len(right) > maxDiffWords {
		return nil, nil
	}

	leftKeep, rightKeep := commonWords(foldWords(left), foldWords(right))
	return spansOf(left, leftKeep), spansOf(right, rightKeep)
}

// splitWords cuts a string into words, each carrying the whitespace that
// followed it. Joining the result back gives the original string exactly, so
// the panel shows the admin's text and not a respaced copy of it.
func splitWords(s string) []string {
	runes := []rune(s)
	var out []string
	for i := 0; i < len(runes); {
		start := i
		for i < len(runes) && !unicode.IsSpace(runes[i]) {
			i++
		}
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		out = append(out, string(runes[start:i]))
	}
	return out
}

// foldWords is the form words are matched on: trimmed of the spacing and the
// punctuation hanging off either end, and case-folded. A sentence that gained a
// comma should not read as every word after it having changed.
func foldWords(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = strings.ToLower(strings.TrimFunc(w, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
		}))
	}
	return out
}

// commonWords marks, on each side, the words belonging to a longest common
// subsequence of the two — which is to say everything that did not change.
func commonWords(a, b []string) (aKeep, bKeep []bool) {
	n, m := len(a), len(b)

	// best[i][j] is the length of the longest common subsequence of a[i:] and
	// b[j:]. Filled from the end so the walk below can go forwards, which is the
	// order the spans have to come out in.
	best := make([][]int, n+1)
	for i := range best {
		best[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				best[i][j] = best[i+1][j+1] + 1
			case best[i+1][j] >= best[i][j+1]:
				best[i][j] = best[i+1][j]
			default:
				best[i][j] = best[i][j+1]
			}
		}
	}

	aKeep, bKeep = make([]bool, n), make([]bool, m)
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			aKeep[i], bKeep[j] = true, true
			i, j = i+1, j+1
		case best[i+1][j] >= best[i][j+1]:
			i++
		default:
			j++
		}
	}
	return aKeep, bKeep
}

// spansOf glues neighbouring words that share a verdict into one span, so the
// highlight is drawn over a phrase rather than over each word separately.
func spansOf(words []string, keep []bool) []models.DiffSpan {
	var out []models.DiffSpan
	for i, w := range words {
		if len(out) > 0 && out[len(out)-1].Same == keep[i] {
			out[len(out)-1].Text += w
			continue
		}
		out = append(out, models.DiffSpan{Text: w, Same: keep[i]})
	}
	return out
}

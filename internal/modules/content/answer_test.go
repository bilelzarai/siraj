package content_test

import (
	"testing"

	"siraj/internal/modules/content"
)

// §8 — every question type is checked by the rule its kind declares. A player must
// not lose a point to a capital letter, a stray space, an Arabic diacritic or the
// digits their keyboard produces.

func TestSingleChoice(t *testing.T) {
	want := map[string]any{"index": float64(2)}
	if !content.Correct("single", want, content.Answer{Choice: 2}) {
		t.Error("the right choice was marked wrong")
	}
	if content.Correct("single", want, content.Answer{Choice: 1}) {
		t.Error("a wrong choice was marked right")
	}
}

func TestMultipleChoiceNeedsExactlyTheRightSet(t *testing.T) {
	want := map[string]any{"indexes": []any{float64(0), float64(3)}}
	for _, c := range []struct {
		name string
		got  []int
		ok   bool
	}{
		{"both right answers", []int{0, 3}, true},
		{"the same, in the other order", []int{3, 0}, true},
		{"only one of the two", []int{0}, false},
		{"both plus a wrong one", []int{0, 3, 1}, false},
		{"a duplicate does not stand in for the second", []int{0, 0}, false},
		{"nothing at all", nil, false},
	} {
		if got := content.Correct("multi", want, content.Answer{Choices: c.got}); got != c.ok {
			t.Errorf("%s: got %v, want %v", c.name, got, c.ok)
		}
	}
}

func TestTrueFalse(t *testing.T) {
	want := map[string]any{"yes": true}
	if !content.Correct("boolean", want, content.Answer{Yes: true}) {
		t.Error("true was marked wrong")
	}
	if content.Correct("boolean", want, content.Answer{Yes: false}) {
		t.Error("false was marked right")
	}
}

func TestTypedAnswerForgivesWhatIsNotTheAnswer(t *testing.T) {
	want := map[string]any{"text": []any{"zakat", "zakah"}}
	for _, c := range []struct {
		name string
		got  string
		ok   bool
	}{
		{"exactly", "zakat", true},
		{"the other accepted spelling", "zakah", true},
		{"capitals", "Zakat", true},
		{"surrounding space", "  zakat ", true},
		{"a different word", "salat", false},
		{"nothing", "", false},
	} {
		if got := content.Correct("text", want, content.Answer{Text: c.got}); got != c.ok {
			t.Errorf("%s: %q got %v, want %v", c.name, c.got, got, c.ok)
		}
	}
}

func TestArabicAnswersIgnoreDiacritics(t *testing.T) {
	// The same word, written with and without vowel marks, and with a tatweel.
	want := map[string]any{"text": []any{"الزكاة"}}
	for _, got := range []string{"الزكاة", "الزَّكاة", "الزكــاة", " الزكاة "} {
		if !content.Correct("text", want, content.Answer{Text: got}) {
			t.Errorf("%q was marked wrong: it is the same word", got)
		}
	}
	if content.Correct("text", want, content.Answer{Text: "الصلاة"}) {
		t.Error("a different word was marked right")
	}
}

func TestNumbersInEitherScript(t *testing.T) {
	want := map[string]any{"number": float64(5)}
	for _, got := range []string{"5", " 5 ", "٥"} {
		if !content.Correct("number", want, content.Answer{Text: got}) {
			t.Errorf("%q is five and was marked wrong", got)
		}
	}
	if content.Correct("number", want, content.Answer{Text: "6"}) {
		t.Error("six was accepted for five")
	}
	if content.Correct("number", want, content.Answer{Text: "not a number"}) {
		t.Error("a word was accepted as a number")
	}

	// A tolerance lets an approximate answer stand.
	about := map[string]any{"number": float64(1400), "tolerance": float64(50)}
	if !content.Correct("number", about, content.Answer{Text: "1,380"}) {
		t.Error("1,380 is within fifty of 1,400 and was marked wrong")
	}
	if content.Correct("number", about, content.Answer{Text: "1200"}) {
		t.Error("1,200 is not within fifty of 1,400")
	}
}

// A kind the checker has never been taught is never silently correct.
func TestUnknownKindIsNeverRight(t *testing.T) {
	if content.Correct("ordering", map[string]any{"any": 1}, content.Answer{Choice: 0}) {
		t.Fatal("an unimplemented kind marked an answer right")
	}
}

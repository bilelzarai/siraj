package service

import (
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// joinSpans puts a diffed side back together, so a test can assert that the
// panel shows the admin's text and not a respaced copy of it.
func joinSpans(spans []models.DiffSpan) string {
	var b strings.Builder
	for _, span := range spans {
		b.WriteString(span.Text)
	}
	return b.String()
}

// marked is what the highlight covers, which is the part of the comparison an
// admin actually reads.
func marked(spans []models.DiffSpan) []string {
	var out []string
	for _, span := range spans {
		if !span.Same {
			out = append(out, strings.TrimSpace(span.Text))
		}
	}
	return out
}

// The case the whole panel exists for: two prompts that differ by the one word
// that makes them different questions.
func TestDiffWordsMarksOnlyTheWordThatChanged(t *testing.T) {
	left, right := diffWords(
		"In which month did the Battle of Badr take place?",
		"In which month did the Battle of Uhud take place?")

	if got := marked(left); len(got) != 1 || got[0] != "Badr" {
		t.Errorf("left side highlighted %q, want just [Badr]", got)
	}
	if got := marked(right); len(got) != 1 || got[0] != "Uhud" {
		t.Errorf("right side highlighted %q, want just [Uhud]", got)
	}
}

func TestDiffWordsKeepsTheTextIntact(t *testing.T) {
	const a = "How many  surahs are in the Qur'an?"
	const b = "How many verses are in the Qur'an?"

	left, right := diffWords(a, b)
	if got := joinSpans(left); got != a {
		t.Errorf("left side reassembles to %q, want %q", got, a)
	}
	if got := joinSpans(right); got != b {
		t.Errorf("right side reassembles to %q, want %q", got, b)
	}
}

// A sentence that gained a comma has not had every word after it changed, and
// saying so would bury the differences that matter.
func TestDiffWordsIgnoresPunctuationAndCase(t *testing.T) {
	left, _ := diffWords("The battle of Badr, in Ramadan", "the Battle of Badr in ramadan")
	if got := marked(left); len(got) != 0 {
		t.Errorf("highlighted %q, want nothing", got)
	}
}

func TestDiffWordsDeclinesWhenThereIsNothingToLineUp(t *testing.T) {
	if left, right := diffWords("", "anything"); left != nil || right != nil {
		t.Errorf("an empty side produced spans: %v / %v", left, right)
	}
	long := strings.Repeat("word ", maxDiffWords+1)
	if left, right := diffWords(long, long+"more"); left != nil || right != nil {
		t.Error("a text past the cap was diffed; want both sides whole")
	}
}

func TestSameText(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"114 surahs", "114  Surahs", true},
		{" trimmed ", "trimmed", true},
		{"Is it so", "Is it so?", false},
		{"Badr", "Uhud", false},
	}
	for _, tc := range cases {
		if got := sameText(tc.a, tc.b); got != tc.want {
			t.Errorf("sameText(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func compareFixture() (importRecord, *models.QuestionDraft, models.ImportRow, map[int]string) {
	rec := importRecord{
		Line: 758, ExternalID: 1080300053, Category: "quran",
		Difficulty: 2, Correct: 2,
		Locales: map[string]models.TranslationDraft{
			"en": {Prompt: "How many surahs are in the Qur'an?",
				Choices:     []string{"110", "112", "114", "116"},
				Explanation: "The Qur'an contains 114 surahs."},
		},
	}
	bank := &models.QuestionDraft{
		ID: 1050100069, CategoryID: 1, Difficulty: 2, Points: 20, CorrectIndex: 2,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "How many surahs are in the Qur'an?",
				Choices:     []string{"110", "112", "114", "116"},
				Explanation: "The Qur'an contains 114 surahs."},
		},
	}
	row := models.ImportRow{Line: 758, SimilarID: 1050100069, Percent: 100}
	return rec, bank, row, map[int]string{1: "quran"}
}

// 100% similar and every field equal is the one case that is not a judgement
// call, and the panel has to say so outright.
func TestCompareRecordSpotsTheSameQuestion(t *testing.T) {
	rec, bank, row, slugs := compareFixture()

	cmp := compareRecord(rec, bank, row, slugs)
	if !cmp.Identical || cmp.Differences != 0 {
		t.Fatalf("identical questions reported %d differences", cmp.Differences)
	}
	if len(cmp.Locales) != 1 || !cmp.Locales[0].InLeft || !cmp.Locales[0].InRight {
		t.Fatalf("locales = %+v, want English on both sides", cmp.Locales)
	}
	// Points are blank in the file and derived from the difficulty on write, so
	// the panel has to compare what would be saved, not what was typed.
	if !cmp.Points.Same {
		t.Errorf("points read %q vs %q; the file's blank should resolve to 20",
			cmp.Points.Left, cmp.Points.Right)
	}
}

func TestCompareRecordCountsEachDifferenceOnce(t *testing.T) {
	rec, bank, row, slugs := compareFixture()
	rec.Correct = 1
	en := rec.Locales["en"]
	en.Prompt = "How many verses are in the Qur'an?"
	rec.Locales["en"] = en

	cmp := compareRecord(rec, bank, row, slugs)
	if cmp.Identical {
		t.Fatal("two differing questions were called identical")
	}
	if cmp.Differences != 2 {
		t.Errorf("differences = %d, want 2 (the prompt and the correct answer)", cmp.Differences)
	}
	if cmp.CorrectSame() {
		t.Error("the correct answer differs and was reported as the same")
	}
	if cmp.Locales[0].Prompt.Same || len(cmp.Locales[0].Prompt.LeftDiff) == 0 {
		t.Error("a differing prompt came back without a diff to show")
	}
}

// A language only one side has is worth one difference, not one per field:
// "the bank has not got this yet" is a single fact about the row, and counting
// it five times would say the two questions are further apart than they are.
func TestCompareRecordCountsAMissingLanguageOnce(t *testing.T) {
	rec, bank, row, slugs := compareFixture()
	rec.Locales["fr"] = models.TranslationDraft{
		Prompt:  "Combien de sourates compte le Coran ?",
		Choices: []string{"110", "112", "114", "116"},
	}

	cmp := compareRecord(rec, bank, row, slugs)
	if cmp.Differences != 1 {
		t.Errorf("differences = %d, want 1 for the added language", cmp.Differences)
	}

	var french *models.CompareLocale
	for i := range cmp.Locales {
		if cmp.Locales[i].Code == "fr" {
			french = &cmp.Locales[i]
		}
	}
	if french == nil {
		t.Fatal("the language the file adds is missing from the comparison")
	}
	if !french.InLeft || french.InRight {
		t.Errorf("French reads InLeft=%v InRight=%v, want true/false", french.InLeft, french.InRight)
	}
}

// The content-health sweep compares two questions the bank already has, and the
// pair it raises was raised by one language. The panel has to carry the others,
// because that is the evidence the list left out.
func TestCompareDraftsCarriesEveryLanguage(t *testing.T) {
	left := &models.QuestionDraft{
		ID: 1010100023, CategoryID: 1, Difficulty: 2, Points: 20, CorrectIndex: 1,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "Surah An-Nahl is named after which creature?",
				Choices: []string{"The ant", "The bee", "The spider", "The elephant"}},
			"ar": {Prompt: "سورة النحل سميت باسم أي مخلوق؟",
				Choices: []string{"النملة", "النحلة", "العنكبوت", "الفيل"}},
		},
	}
	right := &models.QuestionDraft{
		ID: 1010100024, CategoryID: 1, Difficulty: 2, Points: 20, CorrectIndex: 0,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "Surah An-Naml is named after which creature?",
				Choices: []string{"The ant", "The bee", "The spider", "The elephant"}},
			"ar": {Prompt: "سورة النمل سميت باسم أي مخلوق؟",
				Choices: []string{"النملة", "النحلة", "العنكبوت", "الفيل"}},
		},
	}

	cmp := compareDrafts(left, right, 88, map[int]string{1: "quran"})
	if cmp.LeftID != left.ID || cmp.RightID != right.ID {
		t.Fatalf("ids = %d/%d, want %d/%d", cmp.LeftID, cmp.RightID, left.ID, right.ID)
	}
	if len(cmp.Locales) != 2 {
		t.Fatalf("languages = %d, want both Arabic and English", len(cmp.Locales))
	}
	// An-Nahl against An-Naml: near-identical wording, and the answer that makes
	// them different questions is the bee against the ant.
	if cmp.CorrectSame() {
		t.Error("two different answers were reported as the same")
	}
	if cmp.Identical || cmp.Differences != 3 {
		t.Errorf("differences = %d, want 3 (both prompts and the answer)", cmp.Differences)
	}
	for _, loc := range cmp.Locales {
		if !loc.InLeft || !loc.InRight {
			t.Errorf("%s reads InLeft=%v InRight=%v, want both", loc.Code, loc.InLeft, loc.InRight)
		}
		if len(loc.Prompt.LeftDiff) == 0 {
			t.Errorf("%s prompt differs but came back without a diff", loc.Code)
		}
	}
}

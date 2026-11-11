package service

import (
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

const bom = "\xEF\xBB\xBF"

// localesFor builds the one-locale translation map most of these cases need.
func localesFor(locale, prompt string, choices []string) map[string]models.TranslationDraft {
	return map[string]models.TranslationDraft{
		locale: {Prompt: prompt, Choices: choices, Explanation: "because", Source: "import"},
	}
}

func recordWith(rec importRecord, prompt string, choices []string) importRecord {
	rec.Locales = localesFor("en", prompt, choices)
	return rec
}

// The CSV template the admin screen hands out starts with a byte-order mark so
// Excel opens Arabic correctly. Parsing has to tolerate it, or the importer
// refuses the one file it told the admin to use.
func TestParseAcceptsItsOwnTemplate(t *testing.T) {
	records, rejected, err := (&Importer{}).Parse(
		strings.NewReader(bom+ImportTemplateCSV()), "csv")
	if err != nil {
		t.Fatalf("the template was rejected: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("rows rejected: %+v", rejected)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want the template's single question", len(records))
	}

	rec := records[0]
	if rec.ExternalID != 9001 {
		t.Errorf("id = %d, want 9001", rec.ExternalID)
	}
	if rec.Category != "quran" {
		t.Errorf("category = %q, want quran", rec.Category)
	}
	if rec.Correct != 2 {
		t.Errorf("correct = %d, want 2", rec.Correct)
	}
	// One row per language, merged into a single question.
	for _, locale := range []string{"ar", "en", "fr"} {
		if _, ok := rec.Locales[locale]; !ok {
			t.Errorf("locale %s missing from the merged record", locale)
		}
	}
	if got := rec.Locales["en"].Choices; len(got) != 4 || got[2] != "114" {
		t.Errorf("en choices = %v, want four values with 114 third", got)
	}
}

func TestParseStripsBOMFromJSON(t *testing.T) {
	body := `[{"id":1,"category":"quran","difficulty":1,"correct":0,
	  "t":{"en":{"prompt":"How many chapters?","choices":["112","113","114","115"],"explanation":"x"}}}]`

	records, rejected, err := (&Importer{}).Parse(strings.NewReader(bom+body), "json")
	if err != nil {
		t.Fatalf("JSON with a BOM was rejected: %v", err)
	}
	if len(records) != 1 || len(rejected) != 0 {
		t.Fatalf("got %d records and %d rejections, want 1 and 0", len(records), len(rejected))
	}
}

func TestParseRejectsUnknownFormat(t *testing.T) {
	if _, _, err := (&Importer{}).Parse(strings.NewReader("x"), "xlsx"); err == nil {
		t.Error("an unsupported format should be refused")
	}
}

func TestParseRejectsEmptyFile(t *testing.T) {
	for _, format := range []string{"csv", "json"} {
		if _, _, err := (&Importer{}).Parse(strings.NewReader(bom+"   \n"), format); err == nil {
			t.Errorf("%s: a file holding only a BOM and whitespace should be refused", format)
		}
	}
}

// Rows sharing an id merge into one question, which is what makes the flat
// one-row-per-language CSV usable from a spreadsheet.
func TestParseCSVMergesRowsSharingAnID(t *testing.T) {
	csv := "id,category,difficulty,points,correct,locale,prompt,choice1,choice2,choice3,choice4,explanation\n" +
		"7,quran,1,10,0,en,Question?,a,b,c,d,why\n" +
		"7,quran,1,10,0,fr,Question ?,a,b,c,d,pourquoi\n"

	records, rejected, err := (&Importer{}).Parse(strings.NewReader(csv), "csv")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("rows rejected: %+v", rejected)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want the two rows merged into one", len(records))
	}
	if len(records[0].Locales) != 2 {
		t.Errorf("got %d locales, want 2", len(records[0].Locales))
	}
}

func TestParseCSVRejectsAMissingColumn(t *testing.T) {
	csv := "id,category,difficulty,points,correct,locale,prompt,choice1,choice2,choice3\n" +
		"7,quran,1,10,0,en,Question?,a,b,c\n"

	if _, _, err := (&Importer{}).Parse(strings.NewReader(csv), "csv"); err == nil {
		t.Error("a file missing choice4 should be refused outright")
	}
}

func TestParseCSVRejectsRowsIndividually(t *testing.T) {
	csv := "id,category,difficulty,points,correct,locale,prompt,choice1,choice2,choice3,choice4,explanation\n" +
		"not-a-number,quran,1,10,0,en,Question?,a,b,c,d,why\n" +
		"8,quran,1,10,0,klingon,Question?,a,b,c,d,why\n" +
		"9,quran,1,10,0,en,Good question?,a,b,c,d,why\n"

	records, rejected, err := (&Importer{}).Parse(strings.NewReader(csv), "csv")
	if err != nil {
		t.Fatalf("one bad row should not fail the upload: %v", err)
	}
	if len(records) != 1 || records[0].ExternalID != 9 {
		t.Errorf("got %+v, want only the well-formed row 9", records)
	}
	// The bad id, the bad locale, and the row left with no translations at all.
	if len(rejected) < 2 {
		t.Errorf("got %d rejections, want the bad id and the bad locale reported", len(rejected))
	}
}

// validate is the gate between an uploaded row and the question bank, so every
// rule it enforces gets a case here.
func TestValidate(t *testing.T) {
	base := importRecord{ExternalID: 5, Category: "quran", Difficulty: 2, Correct: 1}

	if got := validate(recordWith(base, "Prompt?", []string{"a", "b", "c", "d"})); got != "" {
		t.Fatalf("a well-formed record was refused: %s", got)
	}

	cases := []struct {
		name string
		rec  importRecord
		want string
	}{
		{"no category",
			recordWith(importRecord{Difficulty: 1}, "P?", []string{"a", "b", "c", "d"}), "category"},
		{"difficulty above range",
			recordWith(importRecord{Category: "quran", Difficulty: 4}, "P?", []string{"a", "b", "c", "d"}), "difficulty"},
		{"difficulty missing",
			recordWith(importRecord{Category: "quran"}, "P?", []string{"a", "b", "c", "d"}), "difficulty"},
		{"correct out of range",
			recordWith(importRecord{Category: "quran", Difficulty: 1, Correct: 4}, "P?", []string{"a", "b", "c", "d"}), "correct"},
		{"no translations",
			importRecord{Category: "quran", Difficulty: 1}, "no translations"},
		{"empty prompt",
			recordWith(base, "", []string{"a", "b", "c", "d"}), "prompt is empty"},
		{"three choices",
			recordWith(base, "P?", []string{"a", "b", "c"}), "4 choices"},
		{"blank choice",
			recordWith(base, "P?", []string{"a", "", "c", "d"}), "choice 2 is empty"},
		{"duplicate choice",
			recordWith(base, "P?", []string{"a", "b", "a", "d"}), "duplicate"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validate(tc.rec)
			if got == "" {
				t.Fatalf("record was accepted; want a reason mentioning %q", tc.want)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("reason = %q, want it to mention %q", got, tc.want)
			}
		})
	}
}

func TestValidateRejectsUnsupportedLocale(t *testing.T) {
	rec := importRecord{Category: "quran", Difficulty: 1}
	rec.Locales = localesFor("de", "Frage?", []string{"a", "b", "c", "d"})

	if got := validate(rec); !strings.Contains(got, "unsupported locale") {
		t.Errorf("reason = %q, want it to refuse the locale", got)
	}
}

// Two rows of one upload that say the same thing are a duplicate the importer
// has to raise. It only ever asked the bank, so both rows read as "add" in the
// preview; on apply the first was written and the second came back "pending" —
// a decision the admin was never offered, reported after the fact.
func TestFindClashesCatchesTheFileRepeatingItself(t *testing.T) {
	records := []importRecord{
		{Line: 2, ExternalID: 0, Category: "quran", Difficulty: 1, Correct: 0,
			Locales: localesFor("en", "How many surahs are in the Qur'an?", []string{"110", "112", "114", "116"})},
		{Line: 9, ExternalID: 0, Category: "quran", Difficulty: 1, Correct: 0,
			Locales: localesFor("en", "  how many   SURAHS are in the Qur'an?  ", []string{"110", "112", "114", "116"})},
		{Line: 12, ExternalID: 0, Category: "quran", Difficulty: 1, Correct: 0,
			Locales: localesFor("en", "Who was the first muezzin?", []string{"a", "b", "c", "d"})},
	}

	clashes := sameFileClashes(records)

	if _, ok := clashes[2]; ok {
		t.Error("the first of the pair was flagged; it is the one being repeated")
	}
	repeat, ok := clashes[9]
	if !ok {
		t.Fatal("the repeated row was not flagged")
	}
	if repeat.SameFileLine != 2 {
		t.Errorf("row 9 points at line %d, want 2", repeat.SameFileLine)
	}
	if _, ok := clashes[12]; ok {
		t.Error("an unrelated row was flagged")
	}
}

// The subject-area column is optional and it is a check, not a second way to
// file a question. A file written before the taxonomy had two levels parses
// exactly as it did; one that names a subject area is asserting where the
// category sits, and a spreadsheet gets that wrong silently.
func TestParseCSVReadsTheOptionalSubjectArea(t *testing.T) {
	const withoutIt = "id,category,difficulty,points,correct,locale,prompt,choice1,choice2,choice3,choice4,explanation\n" +
		"8,quran,1,10,0,en,Question?,a,b,c,d,why\n"
	const withIt = "id,category,difficulty,points,correct,locale,prompt,choice1,choice2,choice3,choice4,explanation,domain\n" +
		"9,quran,1,10,0,en,Question?,a,b,c,d,why,islamic\n"

	before, rejected, err := (&Importer{}).Parse(strings.NewReader(withoutIt), "csv")
	if err != nil || len(rejected) != 0 {
		t.Fatalf("a file without the column was not accepted: err=%v rejected=%+v", err, rejected)
	}
	if len(before) != 1 || before[0].Domain != "" {
		t.Fatalf("a missing column read as %q, want empty — an absent assertion is not a wrong one",
			before[0].Domain)
	}

	after, rejected, err := (&Importer{}).Parse(strings.NewReader(withIt), "csv")
	if err != nil || len(rejected) != 0 {
		t.Fatalf("a file with the column was not accepted: err=%v rejected=%+v", err, rejected)
	}
	if len(after) != 1 || after[0].Domain != "islamic" {
		t.Fatalf("the column read as %q, want islamic", after[0].Domain)
	}
}

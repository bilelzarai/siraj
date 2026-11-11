package sets

import (
	"strings"
	"testing"
)

func contains(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}

// anyCategory stands in for the player's catalog in these tests.
func anyCategory(name string) (int64, bool) {
	if strings.EqualFold(name, "Rivers") {
		return 7, true
	}
	return 0, false
}

// A good file reads cleanly and comes back as questions ready to write (MQ-05). The
// unsuffixed columns are read as the writer's own language.
func TestReadCSVAcceptsAGoodFile(t *testing.T) {
	file := `question,answer_1,answer_2,answer_3,answer_4,correct,category,difficulty,explanation
"Longest river?",Nile,Amazon,Congo,Volga,1,Rivers,easy,"The Nile."
"Deepest ocean?",Pacific,Atlantic,Indian,Arctic,1,Rivers,hard,
`
	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0)
	if !report.OK() {
		t.Fatalf("a good file was refused: %v", report.Problems)
	}
	if report.Rows != 2 || len(report.Questions) != 2 {
		t.Fatalf("read %d rows into %d questions", report.Rows, len(report.Questions))
	}
	first := report.Questions[0]
	if first.CorrectIndex != 0 {
		t.Errorf("correct 1 in the file is index 0, got %d", first.CorrectIndex)
	}
	if first.Difficulty != 1 || report.Questions[1].Difficulty != 3 {
		t.Errorf("easy and hard are 1 and 3, got %d and %d", first.Difficulty, report.Questions[1].Difficulty)
	}
	if first.CategoryID != 7 {
		t.Errorf("the category was not resolved: %d", first.CategoryID)
	}
	if got := first.Text["en"].Explanation; got != "The Nile." {
		t.Errorf("explanation %q", got)
	}
}

// A header with no question column anywhere, or no "correct", cannot be read at all.
func TestReadCSVRefusesAHeaderItCannotRead(t *testing.T) {
	report := ReadCSV(strings.NewReader("answer_1,answer_2\na,b\n"), "en", anyCategory, 0)
	if report.OK() {
		t.Fatal("a file with no question column was accepted")
	}
	missing := map[string]bool{}
	for _, p := range report.Problems {
		if p.Line != 1 {
			t.Errorf("a header problem is on line 1, got %d", p.Line)
		}
		missing[p.Column] = true
	}
	for _, want := range []string{"correct", "question_en"} {
		if !missing[want] {
			t.Errorf("%s was not reported as missing", want)
		}
	}
}

// A question may be written in one language, in two, or in all three; a language left
// out entirely is simply not there (MQ-03).
func TestReadCSVTakesAsManyLanguagesAsAreWritten(t *testing.T) {
	file := `correct,category,question_en,answer_1_en,answer_2_en,answer_3_en,answer_4_en,question_fr,answer_1_fr,answer_2_fr,answer_3_fr,answer_4_fr
1,Rivers,"Longest river?",Nile,Amazon,Congo,Volga,"Plus long fleuve ?",Nil,Amazone,Congo,Volga
2,Rivers,"English only?",a,b,c,d,,,,,
`
	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0)
	if !report.OK() {
		t.Fatalf("a part-translated file was refused: %v", report.Problems)
	}
	both := report.Questions[0].Text
	if len(both) != 2 || both["fr"].Body == "" {
		t.Errorf("both languages should come through: %v", both)
	}
	if _, has := both["ar"]; has {
		t.Error("a language nobody wrote should not be invented")
	}
	one := report.Questions[1].Text
	if len(one) != 1 || one["en"].Body != "English only?" {
		t.Errorf("the single-language row reads wrong: %v", one)
	}
}

// Half a language is a mistake worth naming; no language at all is refused outright.
func TestReadCSVRefusesHalfWrittenAndEmptyRows(t *testing.T) {
	file := `correct,category,question_en,answer_1_en,answer_2_en,answer_3_en,answer_4_en
1,Rivers,"Two answers only?",a,b,,
1,Rivers,,,,,
`
	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0)
	if report.OK() {
		t.Fatal("a half-written file was accepted")
	}
	byLine := map[int][]string{}
	for _, p := range report.Problems {
		byLine[p.Line] = append(byLine[p.Line], p.Column)
	}
	// Line 2 says both things that are true of it: the answers are short, and that
	// left the row with no complete language at all.
	if !contains(byLine[2], "answer_1_en") {
		t.Errorf("the half-written language should be named on line 2, got %v", byLine[2])
	}
	if !contains(byLine[3], "question_en") {
		t.Errorf("the empty row should be named on line 3, got %v", byLine[3])
	}
}

// Every bad line is named, with the column that is wrong, and no question survives it.
func TestReadCSVNamesTheLineAndTheColumn(t *testing.T) {
	file := `question,answer_1,answer_2,answer_3,answer_4,correct,category,difficulty
"Fine?",a,b,c,d,2,Rivers,hard
"Bad correct",a,b,c,d,9,Rivers,easy
"Unknown category",a,b,c,d,1,Nowhere,easy
"",a,b,c,d,1,Rivers,easy
`
	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0)
	if report.OK() {
		t.Fatal("a file with bad rows was accepted")
	}
	if report.Rows != 4 {
		t.Fatalf("four rows were read, got %d", report.Rows)
	}
	// Only the good row is kept, and it is never written while problems remain.
	if len(report.Questions) != 1 {
		t.Errorf("one row is sound, got %d", len(report.Questions))
	}
	byColumn := map[string]int{}
	for _, p := range report.Problems {
		byColumn[p.Column] = p.Line
	}
	if byColumn["correct"] != 3 {
		t.Errorf("the bad answer number is on line 3, got %d", byColumn["correct"])
	}
	if byColumn["category"] != 4 {
		t.Errorf("the unknown category is on line 4, got %d", byColumn["category"])
	}
	if byColumn["question_en"] != 5 {
		t.Errorf("the empty question is on line 5, got %d", byColumn["question_en"])
	}
}

// Blank lines between questions are skipped, not reported.
func TestReadCSVSkipsBlankLines(t *testing.T) {
	file := `question,answer_1,answer_2,answer_3,answer_4,correct,category
"One?",a,b,c,d,1,Rivers

"Two?",a,b,c,d,2,Rivers
`
	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0)
	if !report.OK() {
		t.Fatalf("blank lines were treated as problems: %v", report.Problems)
	}
	if report.Rows != 2 {
		t.Errorf("two questions, got %d", report.Rows)
	}
}

// A file with no category column leans on the one picked for the whole upload.
func TestReadCSVFallsBackToThePickedCategory(t *testing.T) {
	file := "question,answer_1,answer_2,answer_3,answer_4,correct\n\"One?\",a,b,c,d,1\n"
	if report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0); report.OK() {
		t.Error("with no category anywhere the file should be refused")
	}
	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 42)
	if !report.OK() {
		t.Fatalf("the picked category should carry the file: %v", report.Problems)
	}
	if report.Questions[0].CategoryID != 42 {
		t.Errorf("category %d", report.Questions[0].CategoryID)
	}
}

// The header may be spelled in any case, with spaces, and carry a spreadsheet's BOM.
func TestReadCSVForgivesTheHeadersSpreadsheetsWrite(t *testing.T) {
	file := "\ufeffQuestion,Answer 1,Answer 2,Answer 3,Answer 4,Correct,Category\n\"One?\",a,b,c,d,1,Rivers\n"
	if report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0); !report.OK() {
		t.Errorf("a spreadsheet's own header was refused: %v", report.Problems)
	}
}

// A set is only ready for the bank when every question is written in all three (MQ-06).
func TestCompletenessCountsUntranslatedQuestions(t *testing.T) {
	s := &Service{}
	full := Question{Text: map[string]Text{
		"en": {Body: "Q", Answers: []string{"a", "b", "c", "d"}},
		"ar": {Body: "س", Answers: []string{"أ", "ب", "ج", "د"}},
		"fr": {Body: "Q", Answers: []string{"a", "b", "c", "d"}},
	}}
	half := Question{Text: map[string]Text{
		"en": {Body: "Q", Answers: []string{"a", "b", "c", "d"}},
		"ar": {Body: "س", Answers: []string{"أ", "ب"}},
	}}
	if got := s.Completeness([]Question{full, full}); got != 0 {
		t.Errorf("two complete questions leave %d missing", got)
	}
	if got := s.Completeness([]Question{full, half}); got != 1 {
		t.Errorf("one short question should be counted once, got %d", got)
	}
}

// An exported set reads back as the same set: every language in the column that
// belongs to it, and the row ready to be imported again (MQ-05).
func TestExportRoundTripsThroughTheImporter(t *testing.T) {
	original := []Question{{
		CategoryID: 7, Difficulty: 3, CorrectIndex: 2,
		Text: map[string]Text{
			"en": {Body: "English body", Answers: []string{"a", "b", "c", "d"}, Explanation: "why"},
			"ar": {Body: "عربية", Answers: []string{"أ", "ب", "ج", "د"}},
		},
	}}
	file := Export(Set{Name: "Family quiz night", MainLang: "en"}, original,
		func(int64) string { return "Rivers" })

	// The header must line up with the columns, language by language.
	header := strings.Split(strings.SplitN(file, "\n", 2)[0], ",")
	if len(header) != len(Columns) {
		t.Fatalf("header has %d columns, want %d", len(header), len(Columns))
	}

	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 0)
	if !report.OK() {
		t.Fatalf("an exported set was refused by the importer: %v", report.Problems)
	}
	// The set comes back with its own name and language, not a generic one.
	if report.SetName != "Family quiz night" {
		t.Errorf("the set name did not survive the round trip: %q", report.SetName)
	}
	if report.MainLang != "en" {
		t.Errorf("the main language did not survive: %q", report.MainLang)
	}
	back := report.Questions[0]
	if back.CorrectIndex != 2 {
		t.Errorf("the right answer moved: %d", back.CorrectIndex)
	}
	if back.Difficulty != 3 {
		t.Errorf("the level moved: %d", back.Difficulty)
	}
	if got := back.Text["en"].Body; got != "English body" {
		t.Errorf("English landed as %q", got)
	}
	if got := back.Text["ar"].Body; got != "عربية" {
		t.Errorf("Arabic landed as %q", got)
	}
	if _, has := back.Text["fr"]; has {
		t.Error("a language nobody wrote came back from the export")
	}
	if got := back.Text["en"].Explanation; got != "why" {
		t.Errorf("the explanation landed as %q", got)
	}
}

// The template a player downloads to make a new set carries the set itself: its name
// and its main language, so the import does not land as "Imported questions" (MQ-01).
func TestTemplateCarriesTheSetItWouldMake(t *testing.T) {
	report := ReadCSV(strings.NewReader(Template()), "en", func(string) (int64, bool) {
		return 7, true
	}, 0)
	if !report.OK() {
		t.Fatalf("the template we hand out does not import: %v", report.Problems)
	}
	if report.SetName == "" {
		t.Error("the template should show where the set's name goes")
	}
	if report.MainLang != "en" {
		t.Errorf("the template should show the main language, got %q", report.MainLang)
	}
	if report.Rows != 2 {
		t.Errorf("the template shows two example rows, got %d", report.Rows)
	}
}

// A file that names no set still imports; the caller's fallback names it.
func TestImportWithoutASetNameFallsBack(t *testing.T) {
	file := "correct,question_en,answer_1_en,answer_2_en,answer_3_en,answer_4_en\n1,\"One?\",a,b,c,d\n"
	report := ReadCSV(strings.NewReader(file), "en", anyCategory, 7)
	if !report.OK() {
		t.Fatalf("a file with no set name was refused: %v", report.Problems)
	}
	if report.SetName != "" {
		t.Errorf("nothing named the set, so nothing should be reported: %q", report.SetName)
	}
}

// CSV import for My questions (MQ-05): a teacher with a spreadsheet of questions can
// bring the whole lot in at once, as a new set or on top of one they already have.
//
// Nothing is written until every row passes. A half-imported set is worse than none:
// the player would have to hunt for which lines made it in.
package sets

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"siraj/internal/i18n"

	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/csvlimit"
)

// Columns is the header the importer expects, in any order: which answer is right,
// then the question itself in as many of the three languages as the writer has. A
// language may be left out entirely; one of them has to be there in full (MQ-03).
//
// The unsuffixed spelling — question, answer_1 … — is read as the writer's own
// language, so a one-language file stays as short as it was.
var Columns = []string{
	"set_name", "main_lang", "correct", "category", "difficulty",
	"question_en", "answer_1_en", "answer_2_en", "answer_3_en", "answer_4_en", "explanation_en",
	"question_ar", "answer_1_ar", "answer_2_ar", "answer_3_ar", "answer_4_ar", "explanation_ar",
	"question_fr", "answer_1_fr", "answer_2_fr", "answer_3_fr", "answer_4_fr", "explanation_fr",
}

// ColumnLangs is the order the language columns stand in above. It is not the menu
// order of i18n.Langs, and an export that walked the wrong one would write every
// question into the wrong column.
var ColumnLangs = []string{"en", "ar", "fr"}

// required are the columns a file cannot be read without. Everything else is either
// optional or satisfied by one language being complete.
var required = []string{"correct"}

// MaxImportRows, MaxImportBytes and MaxImportMB are the project's one set of import
// limits, the same numbers the admin console's files are read to.
const (
	MaxImportRows  = csvlimit.Rows
	MaxImportBytes = csvlimit.Bytes
	MaxImportMB    = csvlimit.MB
)

// RowProblem is one thing wrong with one line, in the words the screen shows.
type RowProblem struct {
	Line   int    `json:"line"`
	Column string `json:"column,omitempty"`
	Why    string `json:"why"`
}

// ImportReport is what the upload screen draws after reading a file: what it found,
// and everything that would stop it going in.
type ImportReport struct {
	Rows     int          `json:"rows"`
	Problems []RowProblem `json:"problems"`
	// SetName and MainLang are the set the file describes, read from its own columns.
	// A file that names neither still imports; the set is simply named for it being
	// imported (MQ-01).
	SetName  string `json:"set_name,omitempty"`
	MainLang string `json:"main_lang,omitempty"`
	// Questions is what would be written, once nothing is wrong.
	Questions []QuestionInput `json:"-"`
}

// OK is true when the file can be imported as it stands.
func (r ImportReport) OK() bool { return len(r.Problems) == 0 && r.Rows > 0 }

// CategoryLookup resolves the names in the "category" column against the categories
// this player is allowed to write for. The web layer supplies it, so the importer
// never has to know how the catalog is arranged (NF-01).
type CategoryLookup func(name string) (int64, bool)

// ReadCSV checks a file and says exactly what is wrong with it, line by line. It
// never writes anything: the screen shows the report first and imports after.
func ReadCSV(r io.Reader, lang string, lookup CategoryLookup, fallback int64) ImportReport {
	out := ImportReport{}
	reader := csv.NewReader(io.LimitReader(r, MaxImportBytes))
	reader.FieldsPerRecord = -1 // ragged rows are reported, not fatal
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		out.Problems = append(out.Problems, RowProblem{Line: 1, Why: "the file is empty or not a CSV"})
		return out
	}
	at := map[string]int{}
	for i, name := range header {
		at[normalise(name)] = i
	}
	for _, name := range required {
		if _, ok := at[name]; !ok {
			out.Problems = append(out.Problems, RowProblem{
				Line: 1, Column: name, Why: "this column is missing from the header",
			})
		}
	}
	// A file that names no question at all in any language cannot be read.
	if !hasAnyQuestionColumn(at, lang) {
		out.Problems = append(out.Problems, RowProblem{
			Line: 1, Column: "question_" + lang,
			Why: "no question column: add question_en, question_ar or question_fr",
		})
	}
	if len(out.Problems) > 0 {
		return out
	}

	line := 1
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line++
		if err != nil {
			out.Problems = append(out.Problems, RowProblem{Line: line, Why: "this line could not be read"})
			continue
		}
		if blank(record) {
			continue
		}
		if out.Rows >= MaxImportRows {
			out.Problems = append(out.Problems, RowProblem{
				Line: line, Why: fmt.Sprintf("up to %d questions in one file", MaxImportRows),
			})
			break
		}
		out.Rows++
		// The set the file describes is read from the first row that names it: a
		// spreadsheet repeats the name down the column, or fills it in once.
		if out.SetName == "" {
			out.SetName = valueAt(record, at, "set_name")
		}
		if out.MainLang == "" {
			if code := normalise(valueAt(record, at, "main_lang")); i18n.Known(code) {
				out.MainLang = code
			}
		}
		in, problems := readRow(record, at, line, lang, lookup, fallback)
		out.Problems = append(out.Problems, problems...)
		if len(problems) == 0 {
			out.Questions = append(out.Questions, in)
		}
	}
	if out.Rows == 0 && len(out.Problems) == 0 {
		out.Problems = append(out.Problems, RowProblem{Line: 1, Why: "the file has a header but no questions"})
	}
	return out
}

// readRow turns one line into a question, or into the reasons it is not one. Each
// language is read on its own: an empty one is simply skipped, a half-written one is
// reported, and at least one has to come through whole (MQ-03).
func readRow(record []string, at map[string]int, line int, lang string, lookup CategoryLookup, fallback int64) (QuestionInput, []RowProblem) {
	var problems []RowProblem
	cell := func(name string) string {
		i, ok := at[name]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}
	add := func(column, why string) {
		problems = append(problems, RowProblem{Line: line, Column: column, Why: why})
	}

	correct, err := strconv.Atoi(cell("correct"))
	if err != nil || correct < 1 || correct > 4 {
		add("correct", "which answer is right, 1 to 4")
		correct = 1
	}
	difficulty := readDifficulty(cell("difficulty"))
	if difficulty == 0 {
		add("difficulty", "easy, medium or hard")
		difficulty = 2
	}
	categoryID := fallback
	if name := cell("category"); name != "" {
		id, ok := lookup(name)
		if !ok {
			add("category", "no category of yours is called that")
		} else {
			categoryID = id
		}
	} else if fallback == 0 {
		add("category", "this column is needed, or pick a category for the whole file")
	}

	texts := map[string]Text{}
	complete := ""
	for _, code := range i18n.Langs {
		// The unsuffixed columns belong to the writer's own language.
		suffix := "_" + code
		body := cell("question" + suffix)
		if body == "" && code == lang {
			body = cell("question")
		}
		answers := make([]string, 4)
		filled := 0
		for i := 0; i < 4; i++ {
			name := fmt.Sprintf("answer_%d%s", i+1, suffix)
			answers[i] = cell(name)
			if answers[i] == "" && code == lang {
				answers[i] = cell(fmt.Sprintf("answer_%d", i+1))
			}
			if answers[i] != "" {
				filled++
			}
		}
		explanation := cell("explanation" + suffix)
		if explanation == "" && code == lang {
			explanation = cell("explanation")
		}
		if body == "" && filled == 0 && explanation == "" {
			continue // this language was left out, which is allowed
		}
		if body == "" {
			add("question"+suffix, "answers without a question")
			continue
		}
		if len([]rune(body)) > 160 {
			add("question"+suffix, "up to 160 characters")
		}
		if filled < 4 {
			add("answer_1"+suffix, "a language that is written needs all four answers")
			continue
		}
		texts[code] = Text{Body: body, Answers: answers, Explanation: explanation}
		if complete == "" || code == lang {
			complete = code
		}
	}
	if complete == "" {
		add("question_"+lang, "write the question in at least one language")
		complete = lang
	}

	in := QuestionInput{
		CategoryID: categoryID, Difficulty: difficulty, CorrectIndex: int32(correct - 1),
		MainLang: complete, Text: texts,
	}
	return in, problems
}

// valueAt reads one cell by column name, or an empty string when the file has no
// such column.
func valueAt(record []string, at map[string]int, name string) string {
	i, ok := at[name]
	if !ok || i >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[i])
}

// hasAnyQuestionColumn says whether the header names a question in any language.
func hasAnyQuestionColumn(at map[string]int, lang string) bool {
	if _, ok := at["question"]; ok {
		return true
	}
	for _, code := range i18n.Langs {
		if _, ok := at["question_"+code]; ok {
			return true
		}
	}
	return false
}

// readDifficulty takes either the word or the number, in any of the three languages
// the app is written in.
func readDifficulty(raw string) int32 {
	switch normalise(raw) {
	case "1", "easy", "facile", "سهل":
		return 1
	case "", "2", "medium", "moyen", "متوسط":
		return 2 // a blank column is the middle, which is what the editor opens on
	case "3", "hard", "difficile", "صعب":
		return 3
	}
	return 0
}

func normalise(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "\ufeff") // a spreadsheet's byte order mark
	return strings.ReplaceAll(s, " ", "_")
}

func blank(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// Template is the file the button downloads: the header, one row written in all
// three languages, and one written in a single language — the two shapes a file is
// allowed to take (MQ-03, MQ-05).
func Template() string {
	rows := []string{
		strings.Join(Columns, ","),
		`"My new set",en,1,"Prophetic Biography",easy,` +
			`"Which city is the Prophet's mosque in?",Madinah,Makkah,Taif,Kufa,"It was built in Madinah.",` +
			`"في أي مدينة يقع مسجد النبي؟",المدينة,مكة,الطائف,الكوفة,"بُني في المدينة.",` +
			`"Dans quelle ville se trouve la mosquée du Prophète ?",Médine,La Mecque,Taïf,Koufa,"Elle a été bâtie à Médine."`,
		`"My new set",en,2,"Prophetic Biography",medium,` +
			`"In which year did the Hijrah take place?",622,610,630,570,"",` +
			`,,,,,,` +
			`,,,,,`,
	}
	return strings.Join(rows, "\n") + "\n"
}

// ImportInto writes a checked file into a set. It refuses a report that still has
// problems, so a caller cannot skip the check by accident.
func (s *Service) ImportInto(ctx context.Context, who *authctx.Principal, setID uuid.UUID, report ImportReport) (int, error) {
	if !report.OK() {
		return 0, apperr.Invalid(map[string]string{"file": "fix the lines below first"})
	}
	if _, _, err := s.Get(ctx, who, setID); err != nil {
		return 0, err
	}
	for _, in := range report.Questions {
		if _, err := s.AddQuestion(ctx, who, setID, in); err != nil {
			return 0, err
		}
	}
	return len(report.Questions), nil
}

// ImportNew makes a set out of a file in one step. The file names the set and picks
// its main language when it says so; the two arguments are what to fall back on
// (MQ-01, MQ-05).
func (s *Service) ImportNew(ctx context.Context, who *authctx.Principal, name, lang string, report ImportReport) (Set, int, error) {
	if !report.OK() {
		return Set{}, 0, apperr.Invalid(map[string]string{"file": "fix the lines below first"})
	}
	if report.SetName != "" {
		name = report.SetName
	}
	if report.MainLang != "" {
		lang = report.MainLang
	}
	set, err := s.Create(ctx, who, name, lang)
	if err != nil {
		return Set{}, 0, err
	}
	n, err := s.ImportInto(ctx, who, set.ID, report)
	if err != nil {
		return Set{}, 0, err
	}
	return set, n, nil
}

// Export writes a set back out in the same shape the importer reads, so a player can
// take what they have, edit it in a spreadsheet and send it back (MQ-05).
func Export(set Set, questions []Question, category func(int64) string) string {
	name, mainLang := set.Name, set.MainLang
	var out strings.Builder
	w := csv.NewWriter(&out)
	_ = w.Write(Columns)
	for _, q := range questions {
		row := make([]string, 0, len(Columns))
		row = append(row,
			name, mainLang,
			strconv.Itoa(int(q.CorrectIndex)+1),
			category(q.CategoryID),
			difficultyWord(q.Difficulty),
		)
		for _, code := range ColumnLangs {
			t := q.Text[code]
			answers := make([]string, 4)
			copy(answers, t.Answers)
			row = append(row, t.Body)
			row = append(row, answers...)
			row = append(row, t.Explanation)
		}
		_ = w.Write(row)
	}
	w.Flush()
	return out.String()
}

// difficultyWord is the word the importer reads back.
func difficultyWord(level int32) string {
	switch level {
	case 1:
		return "easy"
	case 3:
		return "hard"
	}
	return "medium"
}

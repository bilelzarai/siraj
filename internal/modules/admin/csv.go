// CSV for the console (A04, A05, A06, A02): every list screen downloads what it shows
// and, where the rows are content rather than history, reads the same file back.
//
// One shape serves both directions: the export writes the columns the import reads,
// so a file taken out, edited in a spreadsheet and brought back updates the rows it
// came from instead of doubling them. Nothing is written until every line passes —
// a half-imported bank is worse than none.
package admin

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/csvlimit"
)

// MaxImportRows and MaxImportBytes are the project's one pair of import limits,
// shared with a player's own set importer so both read the same size of file.
const (
	MaxImportRows  = csvlimit.Rows
	MaxImportBytes = csvlimit.Bytes
	MaxImportMB    = csvlimit.MB
)

// bom is the byte-order mark Excel writes in front of the first column name.
const bom = "\ufeff"

// langs is the column order of every file below. It is not i18n.Langs' menu order,
// and an export walking the wrong one would write each text into the wrong column.
var langs = []string{"en", "ar", "fr"}

// Columns of each file. The id column is written on export and read on import: a row
// that carries one updates that row, a row without one is created.
var (
	DomainColumns = []string{
		"id", "slug", "family", "icon", "color", "motif", "position", "is_featured", "is_published",
		"name_en", "name_ar", "name_fr", "description_en", "description_ar", "description_fr",
	}
	CategoryColumns = []string{
		"id", "domain_slug", "slug", "icon", "color", "position", "is_published",
		"name_en", "name_ar", "name_fr",
	}
	QuestionColumns = []string{
		"id", "domain_slug", "category_slug", "difficulty", "points", "correct", "status", "main_lang", "source",
		"question_en", "answer_1_en", "answer_2_en", "answer_3_en", "answer_4_en", "explanation_en",
		"question_ar", "answer_1_ar", "answer_2_ar", "answer_3_ar", "answer_4_ar", "explanation_ar",
		"question_fr", "answer_1_fr", "answer_2_fr", "answer_3_fr", "answer_4_fr", "explanation_fr",
	}
	UserColumns = []string{
		"id", "kind", "role", "username", "display_name", "email", "lang", "level", "xp",
		"islamic_only", "domains", "created_at", "last_seen_at",
	}
)

// ---------------------------------------------------------------- the files out

// ExportDomains writes A04 as CSV.
func (s *Service) ExportDomains(ctx context.Context, w io.Writer) error {
	rows, err := s.db.Q.ExportDomains(ctx)
	if err != nil {
		return err
	}
	out := csv.NewWriter(w)
	_ = out.Write(DomainColumns)
	for _, r := range rows {
		_ = out.Write([]string{
			itoa(r.ID), r.Slug, r.Family, r.Icon, r.Color, r.Motif, strconv.Itoa(int(r.Position)),
			yesNo(r.IsFeatured), yesNo(r.IsPublished),
			r.NameEn, r.NameAr, r.NameFr, r.DescriptionEn, r.DescriptionAr, r.DescriptionFr,
		})
	}
	out.Flush()
	return out.Error()
}

// ExportCategories writes A05; domainID 0 is every domain.
func (s *Service) ExportCategories(ctx context.Context, w io.Writer, domainID int64) error {
	rows, err := s.db.Q.ExportCategories(ctx, domainID)
	if err != nil {
		return err
	}
	out := csv.NewWriter(w)
	_ = out.Write(CategoryColumns)
	for _, r := range rows {
		_ = out.Write([]string{
			itoa(r.ID), r.DomainSlug, r.Slug, r.Icon, r.Color, strconv.Itoa(int(r.Position)),
			yesNo(r.IsPublished), r.NameEn, r.NameAr, r.NameFr,
		})
	}
	out.Flush()
	return out.Error()
}

// ExportQuestions writes A06, filtered the way the screen is.
func (s *Service) ExportQuestions(ctx context.Context, w io.Writer, categoryID int64, status string) error {
	rows, err := s.db.Q.ExportQuestions(ctx, sqlc.ExportQuestionsParams{CategoryID: categoryID, Status: status})
	if err != nil {
		return err
	}
	out := csv.NewWriter(w)
	_ = out.Write(QuestionColumns)
	for _, r := range rows {
		line := []string{
			itoa(r.ID), r.DomainSlug, r.CategorySlug,
			strconv.Itoa(int(r.Difficulty)), strconv.Itoa(int(r.Points)),
			// The file names the answer, not its index: a spreadsheet is read by people.
			letter(int(r.CorrectIndex)), r.Status, r.MainLang, r.Source,
		}
		for _, lang := range langs {
			body, answers, explanation := textsOf(r, lang)
			line = append(line, body)
			for i := 0; i < 4; i++ {
				line = append(line, answerAt(answers, i))
			}
			line = append(line, explanation)
		}
		_ = out.Write(line)
	}
	out.Flush()
	return out.Error()
}

// ExportUsers writes A02. It carries no secret: the same columns the table shows.
func (s *Service) ExportUsers(ctx context.Context, w io.Writer) error {
	rows, err := s.db.Q.ExportUsers(ctx)
	if err != nil {
		return err
	}
	out := csv.NewWriter(w)
	_ = out.Write(UserColumns)
	for _, r := range rows {
		created, seen := "", ""
		if r.CreatedAt.Valid {
			created = r.CreatedAt.Time.UTC().Format("2006-01-02")
		}
		if r.LastSeenAt.Valid {
			seen = r.LastSeenAt.Time.UTC().Format("2006-01-02")
		}
		_ = out.Write([]string{
			r.ID.String(), r.Kind, r.Role, r.Username, r.DisplayName, r.Email, r.Lang,
			strconv.Itoa(int(r.Level)), strconv.Itoa(int(r.Xp)),
			yesNo(r.IslamicOnly), strconv.Itoa(int(r.Domains)), created, seen,
		})
	}
	out.Flush()
	return out.Error()
}

// Template writes the header and one filled line, so the first import is a file
// somebody can actually edit rather than a guess at the columns.
func Template(w io.Writer, kind string) {
	out := csv.NewWriter(w)
	switch kind {
	case "domains":
		_ = out.Write(DomainColumns)
		_ = out.Write([]string{
			"", "aqeedah", "islamic", "mosque", "#0b7a5c", "mosque", "1", "no", "yes",
			"Creed", "العقيدة", "Croyance",
			"What a Muslim believes", "ما يعتقده المسلم", "Ce que croit un musulman",
		})
	case "categories":
		_ = out.Write(CategoryColumns)
		_ = out.Write([]string{"", "aqeedah", "tawhid", "book", "#0d9488", "1", "yes", "Tawhid", "التوحيد", "Tawhid"})
	case "questions":
		_ = out.Write(QuestionColumns)
		_ = out.Write([]string{
			"", "aqeedah", "tawhid", "1", "10", "B", "published", "en", "Qur'an 112",
			"Which surah is called the third of the Qur'an?", "Al-Falaq", "Al-Ikhlas", "An-Nas", "Al-Kawthar",
			"Al-Ikhlas equals a third of the Qur'an in reward.",
			"أي سورة تعدل ثلث القرآن؟", "الفلق", "الإخلاص", "الناس", "الكوثر", "الإخلاص تعدل ثلث القرآن.",
			"Quelle sourate équivaut au tiers du Coran ?", "Al-Falaq", "Al-Ikhlas", "An-Nas", "Al-Kawthar",
			"Al-Ikhlas équivaut au tiers du Coran.",
		})
	}
	out.Flush()
}

// ---------------------------------------------------------------- the files in

// RowProblem is one thing wrong with one line, in the words the screen shows.
type RowProblem struct {
	Line   int    `json:"line"`
	Column string `json:"column,omitempty"`
	Why    string `json:"why"`
}

// ImportReport is what the upload answers with: what the file held, what it would
// change, and everything wrong with it.
type ImportReport struct {
	Kind     string       `json:"kind"`
	Rows     int          `json:"rows"`
	Created  int          `json:"created"`
	Updated  int          `json:"updated"`
	Problems []RowProblem `json:"problems"`
}

// OK is true when the file can be written as it stands.
func (r ImportReport) OK() bool { return len(r.Problems) == 0 && r.Rows > 0 }

type record struct {
	line int
	cell map[string]string
}

func (r record) get(name string) string { return strings.TrimSpace(r.cell[name]) }

// catKey is the pair a question row names its category by: a slug is only unique
// inside its domain.
type catKey struct{ Domain, Category string }

// memo answers a slug lookup once from the database and then from memory. A file of
// 5 000 questions names the same handful of categories on every line, and without
// this the check before the write asks for each of them again line by line. Every
// lookup happens in that pass, before anything is written, so nothing it remembers
// can go stale underneath it.
func memo[K comparable](fetch func(K) (int64, error)) func(K) (int64, error) {
	seen := map[K]int64{}
	return func(key K) (int64, error) {
		if id, ok := seen[key]; ok {
			if id == 0 {
				return 0, pgx.ErrNoRows
			}
			return id, nil
		}
		id, err := fetch(key)
		switch {
		case err == nil:
			seen[key] = id
		case errors.Is(err, pgx.ErrNoRows):
			seen[key] = 0
		}
		return id, err
	}
}

// read turns a file into records, checking the header against the columns the kind
// expects. Unknown columns are ignored, so a spreadsheet with notes still imports.
func read(src io.Reader, want []string) ([]record, []RowProblem, error) {
	in := csv.NewReader(io.LimitReader(src, MaxImportBytes))
	in.FieldsPerRecord = -1
	in.TrimLeadingSpace = true

	header, err := in.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, apperr.Invalid(map[string]string{"file": "that file is empty"})
	}
	if err != nil {
		return nil, nil, apperr.Invalid(map[string]string{"file": "that file is not a CSV"})
	}
	index := map[int]string{}
	known := map[string]bool{}
	for _, name := range want {
		known[name] = true
	}
	for i, name := range header {
		name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, bom)))
		if known[name] {
			index[i] = name
		}
	}
	if len(index) == 0 {
		return nil, nil, apperr.Invalid(map[string]string{
			"file": "none of the columns match; download the template first",
		})
	}

	var out []record
	var problems []RowProblem
	line := 1
	for {
		row, err := in.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line++
		if err != nil {
			problems = append(problems, RowProblem{Line: line, Why: "this line could not be read"})
			continue
		}
		if blank(row) {
			continue
		}
		if len(out) >= MaxImportRows {
			problems = append(problems, RowProblem{Line: line, Why: fmt.Sprintf("more than %d rows; split the file", MaxImportRows)})
			break
		}
		rec := record{line: line, cell: map[string]string{}}
		for i, cell := range row {
			if name, ok := index[i]; ok {
				rec.cell[name] = cell
			}
		}
		out = append(out, rec)
	}
	return out, problems, nil
}

// ImportDomains reads A04's file. dryRun checks without writing, which is what the
// drawer's preview does.
func (s *Service) ImportDomains(ctx context.Context, a Actor, src io.Reader, dryRun bool) (ImportReport, error) {
	report := ImportReport{Kind: "domains"}
	records, problems, err := read(src, DomainColumns)
	if err != nil {
		return report, err
	}
	report.Rows, report.Problems = len(records), problems

	inputs := make([]DomainInput, 0, len(records))
	for _, rec := range records {
		in := DomainInput{
			Slug: rec.get("slug"), Family: rec.get("family"),
			Icon: first(rec.get("icon"), "mosque"), Color: first(rec.get("color"), "#0b7a5c"),
			Motif:    first(rec.get("motif"), "mosque"),
			Position: int32(number(rec.get("position"))), IsFeatured: truthy(rec.get("is_featured")),
			IsPublished: truthy(rec.get("is_published")),
			Names:       map[string]string{}, Descriptions: map[string]string{},
		}
		for _, lang := range langs {
			in.Names[lang] = rec.get("name_" + lang)
			in.Descriptions[lang] = rec.get("description_" + lang)
		}
		if in.Slug == "" {
			report.Problems = append(report.Problems, RowProblem{Line: rec.line, Column: "slug", Why: "a slug is required"})
		}
		if in.Family != "islamic" && in.Family != "general" {
			report.Problems = append(report.Problems, RowProblem{Line: rec.line, Column: "family", Why: "islamic or general"})
		}
		if in.Names["en"] == "" {
			report.Problems = append(report.Problems, RowProblem{Line: rec.line, Column: "name_en", Why: "the English name is required"})
		}
		if id := number64(rec.get("id")); id > 0 {
			in.ID = id
			report.Updated++
		} else if existing, err := s.db.Q.DomainIDBySlug(ctx, in.Slug); err == nil {
			in.ID = existing
			report.Updated++
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return report, err
		} else {
			report.Created++
		}
		inputs = append(inputs, in)
	}
	if dryRun || !report.OK() {
		return report, nil
	}
	for _, in := range inputs {
		if _, err := s.SaveDomain(ctx, a, in); err != nil {
			return report, err
		}
	}
	return report, s.Write(ctx, a, "domain.import", "domain", "", report)
}

// ImportCategories reads A05's file.
func (s *Service) ImportCategories(ctx context.Context, a Actor, src io.Reader, dryRun bool) (ImportReport, error) {
	report := ImportReport{Kind: "categories"}
	records, problems, err := read(src, CategoryColumns)
	if err != nil {
		return report, err
	}
	report.Rows, report.Problems = len(records), problems

	domainID := memo(func(slug string) (int64, error) { return s.db.Q.DomainIDBySlug(ctx, slug) })

	inputs := make([]CategoryInput, 0, len(records))
	for _, rec := range records {
		in := CategoryInput{
			Slug: rec.get("slug"), Icon: first(rec.get("icon"), "book"),
			Color: first(rec.get("color"), "#0d9488"), Position: int32(number(rec.get("position"))),
			IsPublished: truthy(rec.get("is_published")), Names: map[string]string{},
		}
		for _, lang := range langs {
			in.Names[lang] = rec.get("name_" + lang)
		}
		domainSlug := rec.get("domain_slug")
		switch id, err := domainID(domainSlug); {
		case err == nil:
			in.DomainID = id
		case errors.Is(err, pgx.ErrNoRows):
			report.Problems = append(report.Problems, RowProblem{
				Line: rec.line, Column: "domain_slug", Why: "no domain with that slug: create it first",
			})
		default:
			return report, err
		}
		if in.Slug == "" {
			report.Problems = append(report.Problems, RowProblem{Line: rec.line, Column: "slug", Why: "a slug is required"})
		}
		if in.Names["en"] == "" {
			report.Problems = append(report.Problems, RowProblem{Line: rec.line, Column: "name_en", Why: "the English name is required"})
		}
		if id := number64(rec.get("id")); id > 0 {
			in.ID, report.Updated = id, report.Updated+1
		} else if in.DomainID > 0 {
			if existing, err := s.db.Q.CategoryIDBySlug(ctx, sqlc.CategoryIDBySlugParams{
				Slug: in.Slug, DomainSlug: domainSlug,
			}); err == nil {
				in.ID, report.Updated = existing, report.Updated+1
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return report, err
			} else {
				report.Created++
			}
		}
		inputs = append(inputs, in)
	}
	if dryRun || !report.OK() {
		return report, nil
	}
	for _, in := range inputs {
		if _, err := s.SaveCategory(ctx, a, in); err != nil {
			return report, err
		}
	}
	return report, s.Write(ctx, a, "category.import", "category", "", report)
}

// ImportQuestions reads A06's file: the bank, in up to three languages per line.
func (s *Service) ImportQuestions(ctx context.Context, a Actor, src io.Reader, dryRun bool) (ImportReport, error) {
	report := ImportReport{Kind: "questions"}
	records, problems, err := read(src, QuestionColumns)
	if err != nil {
		return report, err
	}
	report.Rows, report.Problems = len(records), problems

	categoryID := memo(func(k catKey) (int64, error) {
		return s.db.Q.CategoryIDBySlug(ctx, sqlc.CategoryIDBySlugParams{Slug: k.Category, DomainSlug: k.Domain})
	})

	inputs := make([]QuestionInput, 0, len(records))
	for _, rec := range records {
		in := QuestionInput{
			Difficulty: int32(first2(number(rec.get("difficulty")), 1)),
			Points:     int32(number(rec.get("points"))),
			Status:     first(rec.get("status"), "draft"),
			MainLang:   first(rec.get("main_lang"), "en"),
			Source:     rec.get("source"),
			Bodies:     map[string]string{}, Answers: map[string][]string{}, Explanations: map[string]string{},
		}
		for _, lang := range langs {
			body := rec.get("question_" + lang)
			if body == "" {
				continue
			}
			in.Bodies[lang] = body
			answers := make([]string, 4)
			for i := 0; i < 4; i++ {
				answers[i] = rec.get(fmt.Sprintf("answer_%d_%s", i+1, lang))
			}
			in.Answers[lang] = answers
			in.Explanations[lang] = rec.get("explanation_" + lang)
		}
		correct, ok := correctIndex(rec.get("correct"))
		if !ok {
			report.Problems = append(report.Problems, RowProblem{
				Line: rec.line, Column: "correct", Why: "A, B, C, D or 1–4",
			})
		}
		in.CorrectIndex = correct
		if !i18n.Known(in.MainLang) {
			report.Problems = append(report.Problems, RowProblem{Line: rec.line, Column: "main_lang", Why: "ar, en or fr"})
		}
		if in.Bodies[in.MainLang] == "" {
			report.Problems = append(report.Problems, RowProblem{
				Line: rec.line, Column: "question_" + in.MainLang, Why: "the main language needs a question",
			})
		}
		for lang, answers := range in.Answers {
			for i, answer := range answers {
				if strings.TrimSpace(answer) == "" {
					report.Problems = append(report.Problems, RowProblem{
						Line: rec.line, Column: fmt.Sprintf("answer_%d_%s", i+1, lang), Why: "four answers are needed",
					})
					break
				}
			}
		}
		switch id, err := categoryID(catKey{Domain: rec.get("domain_slug"), Category: rec.get("category_slug")}); {
		case err == nil:
			in.CategoryID = id
		case errors.Is(err, pgx.ErrNoRows):
			report.Problems = append(report.Problems, RowProblem{
				Line: rec.line, Column: "category_slug", Why: "no such category in that domain",
			})
		default:
			return report, err
		}
		if id := number64(rec.get("id")); id > 0 {
			in.ID, report.Updated = id, report.Updated+1
		} else if in.CategoryID > 0 && in.Bodies[in.MainLang] != "" {
			if existing, err := s.db.Q.QuestionIDByBody(ctx, sqlc.QuestionIDByBodyParams{
				CategoryID: in.CategoryID, Body: in.Bodies[in.MainLang],
			}); err == nil {
				in.ID, report.Updated = existing, report.Updated+1
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return report, err
			} else {
				report.Created++
			}
		}
		inputs = append(inputs, in)
	}
	if dryRun || !report.OK() {
		return report, nil
	}
	for _, in := range inputs {
		if _, err := s.SaveQuestion(ctx, a, in); err != nil {
			return report, err
		}
	}
	return report, s.Write(ctx, a, "question.import", "question", "", report)
}

// ---------------------------------------------------------------- small helpers

func blank(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "yes", "y", "true", "live", "on":
		return true
	}
	return false
}

func number(v string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}

func number64(v string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n
}

func first(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func first2(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func letter(i int) string {
	if i < 0 || i > 3 {
		return "A"
	}
	return string(rune('A' + i))
}

// correctIndex reads "B", "b", "2" or "answer_2" as the second answer.
func correctIndex(v string) (int32, bool) {
	v = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "answer_")))
	if v == "" {
		return 0, false
	}
	if len(v) == 1 && v[0] >= 'A' && v[0] <= 'D' {
		return int32(v[0] - 'A'), true
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 4 {
		return int32(n - 1), true
	}
	return 0, false
}

func answerAt(answers []string, i int) string {
	if i < len(answers) {
		return answers[i]
	}
	return ""
}

func textsOf(r sqlc.ExportQuestionsRow, lang string) (string, []string, string) {
	switch lang {
	case "ar":
		return r.BodyAr, r.AnswersAr, r.ExplanationAr
	case "fr":
		return r.BodyFr, r.AnswersFr, r.ExplanationFr
	}
	return r.BodyEn, r.AnswersEn, r.ExplanationEn
}

// FileName is what the browser saves the download as.
func FileName(kind string) string { return "siraj-" + kind + ".csv" }

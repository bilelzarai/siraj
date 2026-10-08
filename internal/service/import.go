package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// MaxImportBytes bounds an uploaded file. 8 MB is roughly 20k questions.
const MaxImportBytes = 8 << 20

// How alike two prompts have to read before each screen reacts. Three numbers,
// named by what they cause rather than by where they are used, because they
// were chosen separately and nobody could see them together: a pair at 0.78
// used to be a hard stop on one screen, a shrug on another and invisible on a
// third.
//
// They are deliberately not equal. Blocking an import is the expensive
// mistake — three hundred rows held up by a false positive — so it asks for
// near-certainty. Warning a moderator costs one glance, so it is looser. The
// sweep is a report nobody is blocked on, so it is looser still.
const (
	// BlockAbove stops an imported row and asks the admin to decide.
	BlockAbove = 0.85
	// WarnAbove flags a near-match on the question form, once.
	WarnAbove = 0.70
	// ListAbove is the floor for the content-health sweep.
	ListAbove = 0.55
)

// DuplicateThreshold is the old name for BlockAbove, kept because the import
// report and its tests read better with it.
const DuplicateThreshold = BlockAbove

// What an admin can decide about a row the importer flagged. Two answers,
// because there are only two outcomes a row can have: it gets written or it
// does not.
//
// There was a third — "nothing" — which left the row out exactly as
// "duplicate" does. Two buttons that do the same thing is a choice the reader
// has to work out the difference between, and there was none to find.
//
// No decision at all leaves the row unwritten, so a file applied without
// looking can only ever import less than it would have, never more.
const (
	DecideNone   = ""          // not answered yet
	DecideDup    = "duplicate" // yes, we already have this — leave it out
	DecideImport = "anyway"    // not a duplicate, write it
)

// ValidDecision keeps a submitted choice to the two that exist.
func ValidDecision(v string) string {
	switch v {
	case DecideDup, DecideImport:
		return v
	}
	return DecideNone
}

// Importer turns an uploaded file into questions. Every import runs as a
// dry run first, so an admin sees exactly what would change before anything
// is written.
type Importer struct {
	repo *repository.Repo
}

func NewImporter(repo *repository.Repo) *Importer { return &Importer{repo: repo} }

// importRecord is the shape both the JSON and the CSV reader normalise to.
type importRecord struct {
	// Domain is what the row claims the category sits in, when it says. Empty
	// is the normal case and means exactly what it meant before the taxonomy
	// had two levels.
	Domain     string
	Line       int
	ExternalID int
	Category   string
	Difficulty int
	Points     int
	Correct    int
	Source     string
	Locales    map[string]models.TranslationDraft
}

// jsonQuestion mirrors the bundled seed format, so the file an operator
// exports is the same shape as the one they import.
type jsonQuestion struct {
	ID         int    `json:"id"`
	Category   string `json:"category"`
	Domain     string `json:"domain,omitempty"`
	Difficulty int    `json:"difficulty"`
	Points     int    `json:"points"`
	Correct    int    `json:"correct"`
	T          map[string]struct {
		Prompt      string   `json:"prompt"`
		Choices     []string `json:"choices"`
		Explanation string   `json:"explanation"`
	} `json:"t"`
}

// Parse reads a file into records, collecting per-row rejections rather than
// failing the whole upload on the first bad line.
func (im *Importer) Parse(r io.Reader, format string) ([]importRecord, []models.ImportRow, error) {
	limited := io.LimitReader(r, MaxImportBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, nil, fmt.Errorf("read upload: %w", err)
	}
	if len(data) > MaxImportBytes {
		return nil, nil, fmt.Errorf("file is larger than %d MB", MaxImportBytes>>20)
	}
	// Strip a UTF-8 byte-order mark. Excel writes one, and so does the CSV
	// template this importer hands out — which meant the header's first column
	// read as U+FEFF followed by "id", no "id" column was found, and the
	// importer rejected its own template file. strings.TrimSpace does not
	// remove U+FEFF, so it has to come off explicitly, before either decoder
	// sees the bytes.
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))

	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil, fmt.Errorf("the file is empty")
	}

	switch format {
	case "json":
		return parseJSON(data)
	case "csv":
		return parseCSV(data)
	default:
		return nil, nil, fmt.Errorf("unsupported format %q (use json or csv)", format)
	}
}

func parseJSON(data []byte) ([]importRecord, []models.ImportRow, error) {
	var raw []jsonQuestion
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON: %w", err)
	}

	var records []importRecord
	var rejected []models.ImportRow

	for i, q := range raw {
		line := i + 1
		rec := importRecord{
			Line:       line,
			ExternalID: q.ID,
			Category:   strings.TrimSpace(q.Category),
			Difficulty: q.Difficulty,
			Points:     q.Points,
			Correct:    q.Correct,
			Source:     "import",
			Locales:    map[string]models.TranslationDraft{},
		}
		for locale, t := range q.T {
			rec.Locales[strings.TrimSpace(locale)] = models.TranslationDraft{
				Prompt:      strings.TrimSpace(t.Prompt),
				Choices:     trimAll(t.Choices),
				Explanation: strings.TrimSpace(t.Explanation),
				Source:      "import",
			}
		}
		if problem := validate(rec); problem != "" {
			rejected = append(rejected, models.ImportRow{
				Line: line, Ref: refOf(rec), Outcome: "reject", Reason: problem,
			})
			continue
		}
		records = append(records, rec)
	}
	return records, rejected, nil
}

// csvColumns is the flat one-row-per-question layout, which is what a
// spreadsheet export looks like.
var csvColumns = []string{
	"id", "category", "difficulty", "points", "correct",
	"locale", "prompt", "choice1", "choice2", "choice3", "choice4", "explanation",
}

// optionalCSVColumns are read when present and never required. A file written
// before the taxonomy had two levels has to import exactly as it did, so
// "domain" cannot join the list above — and a file that does name a subject
// area is asserting which one the category belongs to, which is a check rather
// than a second way to file a question.
var optionalCSVColumns = []string{"domain"}

func parseCSV(data []byte) ([]importRecord, []models.ImportRow, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid CSV: %w", err)
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("the file has a header but no rows")
	}

	index := map[string]int{}
	for i, name := range rows[0] {
		index[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, required := range csvColumns {
		if _, ok := index[required]; !ok {
			return nil, nil, fmt.Errorf("missing required column %q", required)
		}
	}

	get := func(row []string, name string) string {
		i, ok := index[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	// One CSV row is one locale; rows sharing an id merge into one question.
	byID := map[int]*importRecord{}
	var order []int
	var rejected []models.ImportRow

	for n, row := range rows[1:] {
		line := n + 2 // 1-based, and the header is line 1

		id, err := strconv.Atoi(get(row, "id"))
		if err != nil || id <= 0 {
			rejected = append(rejected, models.ImportRow{
				Line: line, Outcome: "reject", Reason: "id must be a positive whole number",
			})
			continue
		}

		rec, ok := byID[id]
		if !ok {
			difficulty, _ := strconv.Atoi(get(row, "difficulty"))
			points, _ := strconv.Atoi(get(row, "points"))
			correct, err := strconv.Atoi(get(row, "correct"))
			if err != nil {
				rejected = append(rejected, models.ImportRow{
					Line: line, Ref: fmt.Sprint(id), Outcome: "reject",
					Reason: "correct must be 0, 1, 2 or 3",
				})
				continue
			}
			rec = &importRecord{
				Line: line, ExternalID: id,
				Category:   get(row, "category"),
				Domain:     get(row, "domain"),
				Difficulty: difficulty, Points: points, Correct: correct,
				Source:  "import",
				Locales: map[string]models.TranslationDraft{},
			}
			byID[id] = rec
			order = append(order, id)
		}

		locale := strings.ToLower(get(row, "locale"))
		if !i18n.IsSupported(locale) {
			rejected = append(rejected, models.ImportRow{
				Line: line, Ref: fmt.Sprint(id), Outcome: "reject",
				Reason: fmt.Sprintf("unsupported locale %q", locale),
			})
			continue
		}

		rec.Locales[locale] = models.TranslationDraft{
			Prompt: get(row, "prompt"),
			Choices: []string{
				get(row, "choice1"), get(row, "choice2"),
				get(row, "choice3"), get(row, "choice4"),
			},
			Explanation: get(row, "explanation"),
			Source:      "import",
		}
	}

	var records []importRecord
	for _, id := range order {
		rec := byID[id]
		if problem := validate(*rec); problem != "" {
			rejected = append(rejected, models.ImportRow{
				Line: rec.Line, Ref: refOf(*rec), Outcome: "reject", Reason: problem,
			})
			continue
		}
		records = append(records, *rec)
	}
	return records, rejected, nil
}

// validate returns a human-readable problem, or "" when the record is usable.
func validate(rec importRecord) string {
	if rec.Category == "" {
		return "category is required"
	}
	if rec.Difficulty < 1 || rec.Difficulty > 3 {
		return fmt.Sprintf("difficulty must be 1, 2 or 3 (got %d)", rec.Difficulty)
	}
	if rec.Correct < 0 || rec.Correct > 3 {
		return fmt.Sprintf("correct must be 0–3 (got %d)", rec.Correct)
	}
	if len(rec.Locales) == 0 {
		return "no translations supplied"
	}

	for locale, t := range rec.Locales {
		if !i18n.IsSupported(locale) {
			return fmt.Sprintf("unsupported locale %q", locale)
		}
		if t.Prompt == "" {
			return fmt.Sprintf("%s: prompt is empty", locale)
		}
		if len(t.Choices) != 4 {
			return fmt.Sprintf("%s: needs exactly 4 choices, got %d", locale, len(t.Choices))
		}
		seen := map[string]bool{}
		for i, c := range t.Choices {
			if strings.TrimSpace(c) == "" {
				return fmt.Sprintf("%s: choice %d is empty", locale, i+1)
			}
			if seen[c] {
				return fmt.Sprintf("%s: duplicate answer choice %q", locale, c)
			}
			seen[c] = true
		}
	}
	return ""
}

// Run plans an import, and applies it only when commit is true. The planning
// pass and the applying pass share one code path, so the preview an admin
// approves is exactly what gets written.
// decisions maps a file line to what the admin chose about the near-duplicate
// found on it. Absent means unanswered, which is treated as "leave it out".
func (im *Importer) Run(ctx context.Context, records []importRecord, rejected []models.ImportRow,
	actor *uuid.UUID, filename, format string, commit bool,
	decisions map[int]string) (*models.ImportRun, error) {

	run := &models.ImportRun{
		ActorID:   actor,
		Filename:  filename,
		Format:    format,
		Committed: commit,
		Total:     len(records) + len(rejected),
		Rejected:  len(rejected),
		Report:    append([]models.ImportRow{}, rejected...),
	}

	// The admin list, not the player's: the player's hides categories whose
	// domain is retired, and an import into a retired subject area is exactly
	// what retirement is for — filling it before anybody can reach it. Reading
	// the filtered list rejected those rows as "unknown category", which named
	// the wrong cause.
	categories, err := im.repo.AdminCategories(ctx, "en")
	if err != nil {
		return nil, err
	}
	bySlug := map[string]int{}
	// Where each category actually sits, for the rows that assert it.
	domainOf := map[string]string{}
	for _, c := range categories {
		bySlug[c.Slug] = c.ID
		domainOf[c.Slug] = c.DomainSlug
	}

	// Both of the questions the file asks about the bank — "is this id already
	// here?" and "does anything already read like this?" — are answered for
	// every row before the loop starts. Asking inside it meant a query per row
	// for the first and a query per row per language for the second, and the
	// whole lot again when the admin pressed apply.
	existing, err := im.existingIDs(ctx, records)
	if err != nil {
		return nil, err
	}
	clashes, err := im.findClashes(ctx, records)
	if err != nil {
		return nil, err
	}

	for _, rec := range records {
		// A row being added has nothing to link to yet — the report turns a
		// reference into a link, and following one for a row that is not in the
		// bank landed on "Page not found".
		refExists := rec.ExternalID > 0 && existing[rec.ExternalID]

		// A row that names a subject area is checked against where the category
		// actually sits. Getting this wrong in a spreadsheet is easy and
		// silent: the question lands in the bank under a name the author did
		// not intend, and the first anybody hears of it is a player being
		// asked about football in a round about fiqh.
		if rec.Domain != "" {
			if slug, known := domainOf[rec.Category]; known && !strings.EqualFold(slug, rec.Domain) {
				run.Rejected++
				run.Report = append(run.Report, models.ImportRow{
					Line: rec.Line, Ref: refOf(rec), Outcome: "reject",
					Reason: fmt.Sprintf("category %q is in %q, not %q",
						rec.Category, slug, rec.Domain),
					Prompt: promptOf(rec), RefExists: refExists,
				})
				continue
			}
		}

		categoryID, ok := bySlug[rec.Category]
		if !ok {
			run.Rejected++
			run.Report = append(run.Report, models.ImportRow{
				Line: rec.Line, Ref: refOf(rec), Outcome: "reject",
				Reason: fmt.Sprintf("unknown category %q", rec.Category),
				Prompt: promptOf(rec), RefExists: refExists,
			})
			continue
		}

		// A prompt that already exists almost verbatim is not the importer's
		// call to make. "In which month did the Battle of Badr take place?"
		// and the same sentence about Uhud score 84% alike and are different
		// questions — so the row is reported with what it resembles, and the
		// admin decides per row what happens to it.
		clash := clashes[rec.Line]
		if clash != nil && decisions[rec.Line] != DecideImport {
			// Not written either way, but the report says which it was:
			// "you have not looked at this yet" and "you looked and said it is
			// a duplicate" are different things to come back to, and only the
			// first is still asking for an answer.
			reason := fmt.Sprintf("looks like question %d (%d%% similar)",
				clash.RightID, clash.Percent())
			if clash.SameFileLine > 0 {
				reason = fmt.Sprintf("the same question as row %d of this file",
					clash.SameFileLine)
			}
			outcome := "pending"
			if decisions[rec.Line] == DecideDup {
				outcome = "duplicate"
			}
			run.Skipped++
			run.Report = append(run.Report, models.ImportRow{
				Line: rec.Line, Ref: refOf(rec), Outcome: outcome, Reason: reason,
				Prompt: promptOf(rec), RefExists: refExists,
				SimilarID: clash.RightID, SimilarPrompt: clash.RightPrompt,
				Percent: clash.Percent(),
			})
			continue
		}

		points := effectivePoints(rec)

		outcome := "add"
		if refExists {
			outcome = "update"
		}

		if commit {
			draft := &models.QuestionDraft{
				// The file's id is carried through on insert as well as on
				// update, so it means the same thing here as it does in the
				// bundled seed file and in the README: the row it names.
				ID:           rec.ExternalID,
				CategoryID:   categoryID,
				Difficulty:   rec.Difficulty,
				Points:       points,
				CorrectIndex: rec.Correct,
				Source:       rec.Source,
				IsActive:     true,
				CreatedBy:    actor,
				Translations: rec.Locales,
			}
			if _, err := im.repo.UpsertQuestion(ctx, draft); err != nil {
				run.Rejected++
				run.Report = append(run.Report, models.ImportRow{
					Line: rec.Line, Ref: refOf(rec), Outcome: "reject",
					Reason: err.Error(),
				})
				continue
			}
		}

		if outcome == "update" {
			run.Updated++
		} else {
			run.Added++
		}
		run.Report = append(run.Report, models.ImportRow{
			Line: rec.Line, Ref: refOf(rec), Outcome: outcome,
			Prompt: promptOf(rec),
			// A committed add exists from now on; a previewed one still does
			// not, and must not offer a link to itself.
			RefExists: refExists || (commit && outcome == "add"),
		})
	}

	if err := im.repo.RecordImport(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// existingIDs answers "which of these ids does the bank already hold?" for the
// whole file in one query.
func (im *Importer) existingIDs(ctx context.Context, records []importRecord) (map[int]bool, error) {
	ids := make([]int, 0, len(records))
	for _, rec := range records {
		if rec.ExternalID > 0 {
			ids = append(ids, rec.ExternalID)
		}
	}
	return im.repo.ExistingQuestionIDs(ctx, ids)
}

// findClashes looks for a near-duplicate of every row, in every language the
// file carries, and returns the strongest one found per line.
//
// One query per language rather than one per row per language. Strongest
// rather than first: the loop this replaces stopped at whichever language came
// out of a Go map first, so the same file could report a different question on
// two runs of the same upload.
func (im *Importer) findClashes(ctx context.Context, records []importRecord) (map[int]*models.DuplicatePair, error) {
	byLocale := map[string][]repository.PromptProbe{}
	for _, rec := range records {
		for locale, t := range rec.Locales {
			if strings.TrimSpace(t.Prompt) == "" {
				continue
			}
			byLocale[locale] = append(byLocale[locale], repository.PromptProbe{
				Key: rec.Line, Prompt: t.Prompt, ExcludeID: rec.ExternalID,
			})
		}
	}

	out := map[int]*models.DuplicatePair{}
	for locale, probes := range byLocale {
		found, err := im.repo.BestMatches(ctx, probes, locale, BlockAbove)
		if err != nil {
			return nil, err
		}
		for line, match := range found {
			if best, ok := out[line]; !ok || match.Score > best.Score {
				out[line] = match
			}
		}

	}

	// And the file against itself.
	for line, pair := range sameFileClashes(records) {
		if _, already := out[line]; !already {
			out[line] = pair
		}
	}
	return out, nil
}

// sameFileClashes finds rows of one upload that repeat each other.
//
// Every row was checked against the bank and never against the rows above it,
// so two identical rows in one file both read as "add" in the preview — and on
// apply the first was written and the second reported pending, a decision the
// admin was never offered and a reason that appeared only after the fact.
//
// Matched on the normalised prompt rather than by similarity: a file is not in
// the database to be searched, and "the same sentence, differently spaced" is
// what actually happens when a spreadsheet is edited by hand.
func sameFileClashes(records []importRecord) map[int]*models.DuplicatePair {
	out := map[int]*models.DuplicatePair{}
	seen := map[string]int{} // locale + normalised prompt -> the first line with it

	for _, rec := range records {
		for locale, t := range rec.Locales {
			normalised := normalizeText(t.Prompt)
			if normalised == "" {
				continue
			}
			key := locale + "\x00" + normalised
			first, ok := seen[key]
			if !ok {
				seen[key] = rec.Line
				continue
			}
			if _, already := out[rec.Line]; already {
				continue
			}
			out[rec.Line] = &models.DuplicatePair{
				Locale: locale, Score: 1,
				// No RightID: the row it repeats is in the file, not the bank,
				// so there is nothing to link to. The line identifies it.
				RightPrompt:  t.Prompt,
				SameFileLine: first,
			}
		}
	}
	return out
}

// ImportTemplateCSV is the downloadable example, so the expected columns are
// self-evident rather than documented somewhere else.
func ImportTemplateCSV() string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	// The optional column is in the template so an operator can see that it
	// exists; leaving it out of a file is still accepted, which is what
	// `optionalCSVColumns` is for.
	_ = w.Write(append(append([]string{}, csvColumns...), optionalCSVColumns...))
	_ = w.Write([]string{"9001", "quran", "1", "10", "2", "en",
		"How many surahs are in the Qur'an?", "110", "112", "114", "116",
		"The Qur'an contains 114 surahs.", "islamic"})
	_ = w.Write([]string{"9001", "quran", "1", "10", "2", "fr",
		"Combien de sourates compte le Coran ?", "110", "112", "114", "116",
		"Le Coran compte 114 sourates.", "islamic"})
	_ = w.Write([]string{"9001", "quran", "1", "10", "2", "ar",
		"كم عدد سور القرآن الكريم؟", "110", "112", "114", "116",
		"القرآن الكريم يتكوّن من 114 سورة.", "islamic"})
	w.Flush()
	return b.String()
}

func refOf(rec importRecord) string {
	if rec.ExternalID > 0 {
		return fmt.Sprint(rec.ExternalID)
	}
	for _, t := range rec.Locales {
		return clipRunes(t.Prompt, 40)
	}
	return ""
}

// promptOf is the row's question text, for a report that has to say which
// question it is talking about and not only which line.
func promptOf(rec importRecord) string {
	for _, locale := range []string{"en", "ar", "fr"} {
		if t, ok := rec.Locales[locale]; ok && t.Prompt != "" {
			return t.Prompt
		}
	}
	for _, t := range rec.Locales {
		return t.Prompt
	}
	return ""
}

func trimAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

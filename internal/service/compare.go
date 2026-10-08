package service

import (
	"context"
	"fmt"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// Comparer answers the question both duplicate screens raise and neither used
// to answer: are these two the same question?
//
// The import preview asks it about a row in a file and a question in the bank;
// the content-health sweep asks it about two questions the bank already has.
// The comparison is the same work either way — every field of both, in every
// language, with the wording lined up — so it is written once here.
type Comparer struct {
	repo *repository.Repo
}

func NewComparer(repo *repository.Repo) *Comparer { return &Comparer{repo: repo} }

// Import pairs every flagged row of a run with the question it resembles.
//
// The importer stops on a row whose wording is close to something already in
// the bank and asks the admin to decide. It used to ask on the strength of a
// percentage alone, which leaves the only way of checking as: memorise the
// incoming prompt, open the other question in a second tab, and read the two
// against each other by eye. With three hundred rows that is not a check anyone
// performs — it is a button people press.
//
// A row missing from the file, or a resembling question deleted since the
// sweep, is left out rather than failing the preview: one panel short is a
// smaller problem than no preview at all.
func (cm *Comparer) Import(ctx context.Context, records []importRecord,
	run *models.ImportRun) ([]*models.QuestionCompare, error) {

	if run == nil {
		return nil, nil
	}

	flagged := map[int]models.ImportRow{}
	for _, row := range run.Report {
		if row.SimilarID > 0 {
			flagged[row.Line] = row
		}
	}
	if len(flagged) == 0 {
		return nil, nil
	}

	slugByID, err := cm.categorySlugs(ctx)
	if err != nil {
		return nil, err
	}

	// Every resembling question in one read, for the same reason the sweep
	// does it: fifty flagged rows was a hundred queries, and a question that
	// two rows both resemble was read twice.
	ids := make([]int, 0, len(flagged))
	for _, row := range flagged {
		ids = append(ids, row.SimilarID)
	}
	drafts, err := cm.repo.QuestionDraftsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Walked in file order rather than map order: the panels sit under the
	// report and should run the same way down the page as the rows do.
	var out []*models.QuestionCompare
	for _, rec := range records {
		if len(out) >= models.MaxComparePanels {
			break
		}
		row, ok := flagged[rec.Line]
		if !ok {
			continue
		}
		existing := drafts[row.SimilarID]
		if existing == nil {
			// Deleted between the sweep and here. One panel short is a smaller
			// problem than no preview at all.
			continue
		}
		out = append(out, compareRecord(rec, existing, row, slugByID))
	}
	return out, nil
}

// Pairs builds the same comparison for near-duplicates already in the bank.
//
// The content-health sweep lists pairs one language at a time, because the same
// two questions surface once per language they share and a list that repeats
// itself is a list nobody finishes. That filtering is what makes this panel
// necessary rather than merely useful: the pair was raised by one language, and
// what happens to it has to be decided with the other two in view.
func (cm *Comparer) Pairs(ctx context.Context,
	pairs []*models.DuplicatePair) ([]*models.QuestionCompare, error) {

	if len(pairs) == 0 {
		return nil, nil
	}

	slugByID, err := cm.categorySlugs(ctx)
	if err != nil {
		return nil, err
	}

	// Read in one go rather than pair by pair: a question that resembles two
	// others appears in both pairs, and sixty pairs one side at a time is a
	// couple of hundred queries for one screen.
	if len(pairs) > models.MaxComparePanels {
		pairs = pairs[:models.MaxComparePanels]
	}
	ids := make([]int, 0, len(pairs)*2)
	for _, pair := range pairs {
		ids = append(ids, pair.LeftID, pair.RightID)
	}
	drafts, err := cm.repo.QuestionDraftsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	var out []*models.QuestionCompare
	for _, pair := range pairs {
		left, right := drafts[pair.LeftID], drafts[pair.RightID]
		if left == nil || right == nil {
			continue
		}
		out = append(out, compareDrafts(left, right, pair.Percent(), slugByID))
	}
	return out, nil
}

// Draft sets a question being written against the ones it resembles.
//
// The form's warning was a list of prompts and a percentage — the one screen
// of the three that still asked a moderator to judge a duplicate without
// showing them what it was judging. The panel already existed; this hands it
// the unsaved draft as the left side.
func (cm *Comparer) Draft(ctx context.Context, draft *models.QuestionDraft,
	matches []*models.DuplicatePair) ([]*models.QuestionCompare, error) {

	if draft == nil || len(matches) == 0 {
		return nil, nil
	}

	slugByID, err := cm.categorySlugs(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(matches))
	seen := map[int]bool{}
	for _, m := range matches {
		if !seen[m.RightID] {
			seen[m.RightID] = true
			ids = append(ids, m.RightID)
		}
	}
	drafts, err := cm.repo.QuestionDraftsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	var out []*models.QuestionCompare
	done := map[int]bool{}
	for _, m := range matches {
		right := drafts[m.RightID]
		if right == nil || done[m.RightID] {
			continue
		}
		done[m.RightID] = true
		cmp := compareDrafts(draft, right, m.Percent(), slugByID)
		// The draft has no id until it is saved, so the panel must not offer a
		// link to it. compareDrafts copies left.ID, which is 0 for a new one.
		out = append(out, cmp)
	}
	return out, nil
}

// categorySlugs maps a category id back to the slug a file would name it by, so
// both sides of a comparison speak the same language about categories.
func (cm *Comparer) categorySlugs(ctx context.Context) (map[int]string, error) {
	// The admin list: this panel is shown to a moderator comparing two bank
	// questions, and one of them may sit under a retired domain. The player's
	// filtered list would leave its category blank.
	categories, err := cm.repo.AdminCategories(ctx, "en")
	if err != nil {
		return nil, err
	}
	out := make(map[int]string, len(categories))
	for _, cat := range categories {
		out[cat.ID] = cat.Slug
	}
	return out, nil
}

// compareRecord sets a row of an uploaded file against a question in the bank.
func compareRecord(rec importRecord, bank *models.QuestionDraft,
	row models.ImportRow, slugByID map[int]string) *models.QuestionCompare {

	cmp := &models.QuestionCompare{
		Line:        rec.Line,
		Percent:     row.Percent,
		RightID:     row.SimilarID,
		Category:    plainPair(rec.Category, slugByID[bank.CategoryID]),
		Difficulty:  plainPair(fmt.Sprint(rec.Difficulty), fmt.Sprint(bank.Difficulty)),
		Points:      plainPair(fmt.Sprint(effectivePoints(rec)), fmt.Sprint(bank.Points)),
		LeftCorrect: rec.Correct,

		RightCorrect: bank.CorrectIndex,
	}
	fillLocales(cmp, rec.Locales, bank.Translations)
	return cmp
}

// compareDrafts sets two questions in the bank against each other.
func compareDrafts(left, right *models.QuestionDraft, percent int,
	slugByID map[int]string) *models.QuestionCompare {

	cmp := &models.QuestionCompare{
		LeftID:       left.ID,
		RightID:      right.ID,
		Percent:      percent,
		Category:     plainPair(slugByID[left.CategoryID], slugByID[right.CategoryID]),
		Difficulty:   plainPair(fmt.Sprint(left.Difficulty), fmt.Sprint(right.Difficulty)),
		Points:       plainPair(fmt.Sprint(left.Points), fmt.Sprint(right.Points)),
		LeftCorrect:  left.CorrectIndex,
		RightCorrect: right.CorrectIndex,
	}
	fillLocales(cmp, left.Translations, right.Translations)
	return cmp
}

// fillLocales walks every language either side has and counts what differs.
func fillLocales(cmp *models.QuestionCompare, left, right map[string]models.TranslationDraft) {
	for _, pair := range []models.ComparePair{cmp.Category, cmp.Difficulty, cmp.Points} {
		if !pair.Same {
			cmp.Differences++
		}
	}
	if !cmp.CorrectSame() {
		cmp.Differences++
	}

	// In the order the rest of the interface lists languages — including one
	// side's language the other has not got, because "this row brings a French
	// translation the bank lacks" is the difference that decides some of these.
	for _, loc := range i18n.Supported {
		leftT, inLeft := left[loc.Code]
		rightT, inRight := right[loc.Code]
		if !inLeft && !inRight {
			continue
		}

		block := models.CompareLocale{
			Code: loc.Code, Name: loc.Name, Dir: loc.Dir,
			InLeft: inLeft, InRight: inRight,
			Prompt:      textPair(leftT.Prompt, rightT.Prompt),
			Explanation: textPair(leftT.Explanation, rightT.Explanation),
		}
		for i := 0; i < len(leftT.Choices) || i < len(rightT.Choices); i++ {
			block.Choices = append(block.Choices,
				textPair(choiceAt(leftT.Choices, i), choiceAt(rightT.Choices, i)))
		}

		// A language only one side has counts once. Calling it four or five
		// differences would say the two questions are further apart than they
		// are, when the real reading is "this side does not have it yet".
		if !inLeft || !inRight {
			cmp.Differences++
		} else {
			for _, pair := range append([]models.ComparePair{block.Prompt, block.Explanation}, block.Choices...) {
				if !pair.Same {
					cmp.Differences++
				}
			}
		}
		cmp.Locales = append(cmp.Locales, block)
	}

	cmp.Identical = cmp.Differences == 0
}

// effectivePoints is what an imported row is worth once the file's blank is
// filled in — the same rule the import writes by, so the comparison shows what
// would be saved rather than what was typed.
func effectivePoints(rec importRecord) int {
	if rec.Points == 0 {
		return rec.Difficulty * 10
	}
	return rec.Points
}

func choiceAt(choices []string, i int) string {
	if i < len(choices) {
		return choices[i]
	}
	return ""
}

// textPair is a field worth lining up word by word.
func textPair(left, right string) models.ComparePair {
	pair := models.ComparePair{Left: left, Right: right, Same: sameText(left, right)}
	if !pair.Same {
		pair.LeftDiff, pair.RightDiff = diffWords(left, right)
	}
	return pair
}

// plainPair is a field that is a word or a number, where a highlight inside it
// would say nothing the two values side by side do not already say.
func plainPair(left, right string) models.ComparePair {
	return models.ComparePair{Left: left, Right: right, Same: left == right}
}

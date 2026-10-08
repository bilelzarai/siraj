package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// The question bank: listing, bulk actions, and the authoring form.
func (h *Handlers) AdminQuestions(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	paging := h.paging(w, r)
	filter := adminQuestionFilterFrom(r.URL.Query(), c.Locale)
	filter.Limit, filter.Offset = paging.Size, paging.Offset()

	questions, total, err := h.repo.AdminQuestions(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging = paging.withTotal(total)
	categories, _ := h.repo.Categories(r.Context(), c.Locale, 0)
	// The domain level above them, so the category select can be grouped and
	// narrowed. Retired domains included: a category filed under one is still
	// in the bank and still has to be findable in order to be moved out.
	domains, _ := h.repo.AdminDomains(r.Context(), c.Locale)

	h.render(w, r, http.StatusOK, views.AdminQuestions(c, chrome, views.AdminQuestionsData{
		Questions:  questions,
		Categories: categories,
		Domains:    domains,
		Total:      total,
		Pager:      pagerFor(paging),
		Query:      filter.Query,
		CategoryID: filter.CategoryID,
		DomainID:   filter.DomainID,
		Difficulty: filter.Difficulty,
		OnlyReview: filter.OnlyReview,
		OnlyActive: filter.OnlyActive,
		Retired:    filter.OnlyRetired,
		OnlyNoted:  filter.OnlyNoted,
		MinLocales: filter.MinLocales,
		ForceIDs:   forceIDs(r.URL.Query().Get("force")),
		Sorting: views.SortState{
			Sort: filter.Sort, Path: "/admin/questions", Query: r.URL.Query(),
		},
	}))
}

// adminQuestionFilterFrom reads the question browser's filter out of a set of
// submitted values.
//
// One reader for all three callers — the listing off the query string, a bulk
// action off its form, and the export — because "everything matching what the
// admin is looking at" is only true if all three build the same filter from the
// same names. They did not: the bulk path had its own copy, so a filter field
// added to the screen silently widened what a bulk delete selected.
func adminQuestionFilterFrom(v urlValues, locale string) repository.AdminQuestionFilter {
	f := repository.AdminQuestionFilter{
		Query:      clip(strings.TrimSpace(v.Get("q")), 200),
		CategoryID: valueInt(v, "category"),
		DomainID:   valueInt(v, "domain"),
		Difficulty: valueInt(v, "difficulty"),
		Locale:     locale,
		OnlyReview: v.Get("review") == "1",
	}
	// Live and retired are the two halves of one control, so "no state chosen"
	// has to mean both rather than defaulting to one of them.
	switch v.Get("state") {
	case "live":
		f.OnlyActive = true
	case "retired":
		f.OnlyRetired = true
	}
	// Translation coverage: anything short of every shipped language.
	if v.Get("coverage") == "partial" {
		f.MinLocales = models.ShippedLocales
	}
	f.Sort = sortFrom(v)
	f.OnlyNoted = v.Get("review") == "changes"
	if f.OnlyNoted {
		f.OnlyReview = false
	}
	return f
}

// urlValues is what both a query string and a parsed form satisfy, so the
// filter reader does not care which one it was handed.
type urlValues interface{ Get(string) string }

// values adapts url.Values to urlValues without naming the package at every
// call site.
func values(v url.Values) urlValues { return v }

func valueInt(v urlValues, name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v.Get(name)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// forceIDs reads the rows a refused delete wants unlocked. One id or a comma
// separated list, because a single row and a bulk selection come back through
// the same parameter.
func forceIDs(raw string) map[int]bool {
	out := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && id > 0 {
			out[id] = true
		}
	}
	return out
}

// AdminQuestionBulk applies one action to a selection of questions.
//
// Two ways to choose what it acts on. "selection" is the ids ticked on screen.
// "filter" is every question the current search matches, including the pages
// the admin cannot see — which is what makes clearing out a whole category
// possible without paging through it forty rows at a time. The filter is
// rebuilt here from the submitted search rather than trusted as a list of ids,
// so what gets deleted is what the screen was showing.
func (h *Handlers) AdminQuestionBulk(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	back := backTo(r, "/admin/questions")

	var ids []int
	if r.PostFormValue("scope") == "filter" {
		var err error
		ids, err = h.repo.AdminQuestionIDs(r.Context(),
			adminQuestionFilterFrom(values(r.PostForm), c.Locale))
		if err != nil {
			h.serverError(w, r, err)
			return
		}
	} else {
		for _, raw := range r.PostForm["ids"] {
			if id, err := strconv.Atoi(raw); err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
	}

	if len(ids) == 0 {
		h.flash(w, "warning", c.T("admin.questions.bulk.none"))
		redirect(w, r, back)
		return
	}

	switch r.PostFormValue("action") {
	case "activate", "deactivate":
		active := r.PostFormValue("action") == "activate"
		n, err := h.repo.SetQuestionsActive(r.Context(), ids, active)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		action := "question.bulk.deactivate"
		if active {
			action = "question.bulk.activate"
		}
		h.audit(r, action, "question", "", map[string]any{"count": n, "ids": ids})
		h.flash(w, "success", c.T("admin.questions.bulk.toggled", n))

	case "delete":
		// Same rule as deleting one, applied to the set: deletion cascades
		// into game_answers, so the first press is refused when any of the
		// selection has been played. The refusal carries the numbers and the
		// selection back, so confirming is one press and not a re-selection.
		played, answers, err := h.repo.QuestionsAnswerImpact(r.Context(), ids)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if played > 0 && r.PostFormValue("confirm") != "delete" {
			h.flash(w, "warning", c.T("admin.questions.bulk.hasHistory", played, answers))
			redirect(w, r, withForceIDs(back, ids))
			return
		}

		n, err := h.repo.DeleteQuestions(r.Context(), ids)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		h.audit(r, "question.bulk.delete", "question", "", map[string]any{
			"count": n, "ids": ids, "answers_destroyed": answers,
		})
		h.flash(w, "success", c.T("admin.questions.bulk.deleted", n))

	default:
		h.NotFound(w, r)
		return
	}

	redirect(w, r, back)
}

// withForceIDs puts the refused selection back on the URL so the reloaded page
// can re-tick exactly those rows. Without it the admin would have to find and
// re-select every one of them just to answer the warning.
//
// Set rather than appended: back is the page the admin came from, which after
// an earlier refusal already carries a force of its own, and the reader takes
// the first value — so appending would answer the warning about a stale
// selection.
func withForceIDs(back string, ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}

	u, err := url.Parse(back)
	if err != nil {
		return back
	}
	q := u.Query()
	q.Set("force", strings.Join(parts, ","))
	u.RawQuery = q.Encode()
	return u.String()
}

// AdminQuestionAction toggles activation or deletes a question.
func (h *Handlers) AdminQuestionAction(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}

	// Audit only what actually happened: recording the intent before the call
	// meant a failed action still left a line saying it had been taken.
	switch chi.URLParam(r, "action") {
	case "activate":
		if err = h.repo.SetQuestionActive(r.Context(), id, true); err == nil {
			h.auditChange(r, "question.activate", "question", chi.URLParam(r, "id"), "",
				map[string]any{"state": "retired"},
				map[string]any{"state": "live"})
		}
	case "deactivate":
		if err = h.repo.SetQuestionActive(r.Context(), id, false); err == nil {
			h.auditChange(r, "question.deactivate", "question", chi.URLParam(r, "id"), "",
				map[string]any{"state": "live"},
				map[string]any{"state": "retired"})
		}
	case "delete":
		// Deleting cascades to game_answers, which rewrites players' history.
		// Deactivation is almost always what the admin actually wants, so the
		// first attempt is refused and sends them back with this row unlocked.
		answers, _ := h.repo.QuestionAnswerCount(r.Context(), id)
		if answers > 0 && r.PostFormValue("confirm") != "delete" {
			h.flash(w, "warning", c.T("admin.questions.hasHistory", answers))
			redirect(w, r, fmt.Sprintf("/admin/questions?force=%d", id))
			return
		}
		if err = h.repo.DeleteQuestion(r.Context(), id); err == nil {
			h.audit(r, "question.delete", "question", chi.URLParam(r, "id"),
				map[string]any{"answers_destroyed": answers})
			h.flash(w, "success", c.T("admin.questions.deleted", id))
		}
	default:
		h.NotFound(w, r)
		return
	}

	if err != nil {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, backTo(r, "/admin/questions"))
}

// AdminQuestionForm opens the authoring form, blank for a new question or
// loaded for an existing one.
//
// Until this existed the only ways to add content were editing the bundled JSON
// file and restarting, or preparing a bulk import — so a moderator who spotted
// a wrong answer could deactivate the question but not fix it.
func (h *Handlers) AdminQuestionForm(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	d := views.AdminQuestionFormData{
		Difficulty: 1,
		Points:     0,
		IsActive:   true,
		Locales:    map[string]views.QuestionLocaleForm{},
	}

	if raw := chi.URLParam(r, "id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			h.NotFound(w, r)
			return
		}
		draft, err := h.repo.QuestionDraftByID(r.Context(), id)
		if err != nil {
			h.notFoundOrError(w, r, err)
			return
		}
		d = questionFormFromDraft(draft)
	}

	categories, err := h.categoryOptions(r.Context(), c.Locale, d.CategoryID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d.Categories = categories
	if d.CategoryID == 0 && len(categories) > 0 {
		d.CategoryID = categories[0].ID
	}

	h.render(w, r, http.StatusOK, views.AdminQuestionForm(c, chrome, d))
}

// AdminQuestionSave writes the form, creating or updating in one path so both
// go through the same validation.
func (h *Handlers) AdminQuestionSave(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	d := views.AdminQuestionFormData{
		ID:           intParam(r, "id", 0),
		CategoryID:   intParam(r, "category_id", 0),
		Difficulty:   intParam(r, "difficulty", 1),
		Points:       intParam(r, "points", 0),
		CorrectIndex: intParam(r, "correct_index", 0),
		IsActive:     r.PostFormValue("is_active") == "1",
		Locales:      map[string]views.QuestionLocaleForm{},
		Errors:       map[string]string{},
	}

	// Read every shipped locale; a locale left entirely blank is simply not
	// part of this submission, which is how a question gets written one
	// language at a time.
	for _, loc := range i18n.Supported {
		form := views.QuestionLocaleForm{
			Prompt:      strings.TrimSpace(r.PostFormValue("prompt_" + loc.Code)),
			Explanation: strings.TrimSpace(r.PostFormValue("explanation_" + loc.Code)),
			Choices:     make([]string, 4),
		}
		for i := range form.Choices {
			form.Choices[i] = strings.TrimSpace(
				r.PostFormValue(fmt.Sprintf("choice_%s_%d", loc.Code, i)))
		}
		d.Locales[loc.Code] = form
	}

	// The category this question is already filed under stays selectable even
	// if its domain has since been retired — see categoryOptions. Read from
	// what is stored rather than from what was posted, so the exception cannot
	// be used to aim a question at a category a player cannot reach.
	keep := 0
	if d.ID > 0 {
		if stored, err := h.repo.QuestionDraftByID(r.Context(), d.ID); err == nil {
			keep = stored.CategoryID
		}
	}
	categories, err := h.categoryOptions(r.Context(), c.Locale, keep)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d.Categories = categories

	translations := map[string]models.TranslationDraft{}
	for code, form := range d.Locales {
		if form.Blank() {
			continue
		}
		if problem := validateQuestionLocale(form); problem != "" {
			d.Errors[code] = c.T(problem)
			continue
		}
		// A human typed this, so it does not need reviewing — that flag exists
		// for machine output and bulk imports.
		translations[code] = models.TranslationDraft{
			Prompt:      form.Prompt,
			Choices:     form.Choices,
			Explanation: form.Explanation,
			Source:      "human",
			NeedsReview: false,
		}
	}

	if !validCategory(categories, d.CategoryID) {
		d.Error = c.T("admin.question.badCategory")
	}
	if d.Difficulty < 1 || d.Difficulty > 3 {
		d.Error = c.T("admin.question.badDifficulty")
	}
	if d.CorrectIndex < 0 || d.CorrectIndex > 3 {
		d.Error = c.T("admin.question.badCorrect")
	}
	if len(translations) == 0 && d.Error == "" && len(d.Errors) == 0 {
		d.Error = c.T("admin.question.noLocales")
	}

	if d.Error != "" || len(d.Errors) > 0 {
		h.render(w, r, http.StatusUnprocessableEntity,
			views.AdminQuestionForm(c, chrome, d))
		return
	}

	// Warn once about a near-duplicate rather than refusing: the moderator can
	// see the match and decide. Confirming resubmits with the same form.
	actor := c.User.ID
	draft := &models.QuestionDraft{
		ID:           d.ID,
		CategoryID:   d.CategoryID,
		Difficulty:   d.Difficulty,
		Points:       d.Points,
		CorrectIndex: d.CorrectIndex,
		Source:       "human",
		IsActive:     d.IsActive,
		CreatedBy:    &actor,
		Translations: translations,
	}

	// Warn once about a near-duplicate rather than refusing: the moderator can
	// read both questions and decide. Confirming resubmits the same form.
	if r.PostFormValue("allow_similar") != "1" {
		if similar := h.similarToDraft(r, d.ID, translations); len(similar) > 0 {
			d.Similar = similar
			if compare, err := h.comparer.Draft(r.Context(), draft, similar); err != nil {
				slogError(r, fmt.Errorf("draft comparison: %w", err))
			} else {
				d.Compare = compare
			}
			h.render(w, r, http.StatusUnprocessableEntity,
				views.AdminQuestionForm(c, chrome, d))
			return
		}
	}

	id, err := h.repo.UpsertQuestion(r.Context(), draft)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.duplicates.Invalidate()

	action, message := "question.create", "admin.question.created"
	if d.ID > 0 {
		action, message = "question.update", "admin.question.updated"
	}
	h.audit(r, action, "question", fmt.Sprint(id), map[string]any{
		"locales": len(translations),
	})
	h.flash(w, "success", c.T(message, id))
	redirect(w, r, "/admin/questions")
}

// similarToDraft looks for an existing question whose prompt reads almost the
// same, in any of the locales being written.
func (h *Handlers) similarToDraft(r *http.Request, excludeID int, translations map[string]models.TranslationDraft) []*models.DuplicatePair {
	var out []*models.DuplicatePair
	for locale, t := range translations {
		matches, err := h.repo.SimilarPrompts(r.Context(), t.Prompt, locale, excludeID, 3, service.WarnAbove)
		if err != nil {
			slogError(r, fmt.Errorf("similar prompts for %s: %w", locale, err))
			continue
		}
		// The query filters on the same number, so everything that comes back
		// is already over the line.
		out = append(out, matches...)
	}
	return out
}

func validateQuestionLocale(form views.QuestionLocaleForm) string {
	if form.Prompt == "" {
		return "admin.question.promptRequired"
	}
	seen := map[string]bool{}
	for _, choice := range form.Choices {
		if choice == "" {
			return "admin.question.choicesRequired"
		}
		if seen[choice] {
			return "admin.question.choicesDuplicate"
		}
		seen[choice] = true
	}
	return ""
}

// categoryOptions is what a question may be filed under: every category a
// player can reach, plus `keep` when the question already sits in one that is
// not reachable — a category whose domain has been retired.
//
// The exception is the whole point. Without it the select holds no option for
// the question's own category, the browser preselects the first one, and an
// edit that never touched the field moves the question into another subject
// area — back into players' draws under a name nobody chose.
func (h *Handlers) categoryOptions(ctx context.Context, locale string, keep int) ([]*models.Category, error) {
	categories, err := h.repo.Categories(ctx, locale, 0)
	if err != nil {
		return nil, err
	}
	if keep <= 0 {
		return categories, nil
	}
	for _, cat := range categories {
		if cat.ID == keep {
			return categories, nil
		}
	}

	all, err := h.repo.AdminCategories(ctx, locale)
	if err != nil {
		return nil, err
	}
	for _, cat := range all {
		if cat.ID == keep {
			unreachable := cat.Category
			categories = append(categories, &unreachable)
			break
		}
	}
	return categories, nil
}

func validCategory(categories []*models.Category, id int) bool {
	for _, c := range categories {
		if c.ID == id {
			return true
		}
	}
	return false
}

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// A player's own questions: written by them, played only by the friends they
// invite, never in the bank. The three ways they get here are writing them,
// keeping the ones from a match already played, and uploading a file.

// MyQuestions lists a player's collections, or the questions in one.
func (h *Handlers) MyQuestions(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	sets, err := h.repo.QuestionSets(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d := views.QuestionSetsData{Sets: sets}

	if idParam := chi.URLParam(r, "id"); idParam != "" {
		id, ok := parseUUID(idParam)
		if !ok {
			h.NotFound(w, r)
			return
		}
		set, err := h.repo.QuestionSet(r.Context(), id, c.User.ID)
		if err != nil {
			h.notFoundOrError(w, r, err)
			return
		}
		questions, err := h.repo.SetQuestions(r.Context(), id, c.User.ID, c.Locale)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		d.Active = set
		d.Questions = questions

		// One question opened for editing, named on the URL so the browser's
		// back button closes it.
		if editID, err := strconv.Atoi(r.URL.Query().Get("edit")); err == nil && editID > 0 {
			for _, q := range questions {
				if q.ID != editID {
					continue
				}
				played, _ := h.repo.SetQuestionPlayed(r.Context(), q.ID)
				d.EditingID = q.ID
				d.EditingPlayed = played
				d.Editing = views.DraftRow{
					Prompt:      q.Prompt,
					Choices:     q.Choices,
					Correct:     q.CorrectIndex,
					Explanation: q.Explanation,
				}
			}
		}
	}

	h.render(w, r, http.StatusOK, views.QuestionSets(c, d))
}

// CreateQuestionSet opens a new collection.
func (h *Handlers) CreateQuestionSet(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	if h.tooManyWrites(w, r, service.LimitComment) {
		return
	}

	// A set is created with its first questions. Asking only for a name left
	// people with a shelf of named nothings — and an empty collection is not a
	// collection, it is a label.
	written, _ := authoredRows(r)
	if len(written) == 0 {
		h.retryCreate(w, r, written, nil, c.T("sets.writeOneFirst"))
		return
	}
	bad := map[int]string{}
	for i, q := range written {
		if !q.Valid() {
			bad[i] = c.T("challenge.questionIncomplete")
		}
	}
	if len(bad) > 0 {
		h.retryCreate(w, r, written, bad, c.T("sets.fixRows", len(bad)))
		return
	}

	drafts, categories, difficulties, correct, err := draftsFrom(written)
	if err != nil {
		h.retryCreate(w, r, written, nil, c.T("challenge.questionIncomplete"))
		return
	}

	set, err := h.repo.CreateQuestionSet(r.Context(), c.User.ID,
		r.PostFormValue("name"), c.Locale)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrForbidden):
			h.retryCreate(w, r, written, nil, c.T("sets.needName"))
		case errors.Is(err, repository.ErrConflict):
			h.retryCreate(w, r, written, nil, c.T("sets.tooMany", repository.MaxSetsPerPlayer))
		default:
			h.serverError(w, r, err)
		}
		return
	}

	if _, err := h.repo.AddToQuestionSet(r.Context(), set.ID, c.User.ID, c.Locale,
		drafts, categories, difficulties, correct); err != nil {
		// The set exists but is empty, which is the state this whole change is
		// about. Take it back out rather than leaving one behind.
		_ = h.repo.DeleteQuestionSet(r.Context(), set.ID, c.User.ID)
		h.serverError(w, r, err)
		return
	}

	h.flash(w, "success", c.T("sets.added", len(drafts)))
	redirect(w, r, "/my/questions/"+set.ID.String())
}

// AddQuestionsToSet writes new questions into a collection.
func (h *Handlers) AddQuestionsToSet(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	if h.tooManyWrites(w, r, service.LimitComment) {
		return
	}

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}

	written, _ := authoredRows(r)
	if len(written) == 0 {
		h.retryAdd(w, r, id, written, c.T("sets.nothingWritten"))
		return
	}

	// Which rows are not questions yet. Reported per row and handed straight
	// back: refusing six because the fourth is short of an answer, and clearing
	// the form while doing it, is how somebody loses an evening's writing.
	bad := map[int]string{}
	for i, q := range written {
		if !q.Valid() {
			bad[i] = c.T("challenge.questionIncomplete")
		}
	}
	if len(bad) > 0 {
		h.retryAddWith(w, r, id, written, bad, c.T("sets.fixRows", len(bad)))
		return
	}

	drafts, categories, difficulties, correct, err := draftsFrom(written)
	if err != nil {
		h.retryAdd(w, r, id, written, c.T("challenge.questionIncomplete"))
		return
	}

	if _, err := h.repo.AddToQuestionSet(r.Context(), id, c.User.ID, c.Locale,
		drafts, categories, difficulties, correct); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			h.NotFound(w, r)
		case errors.Is(err, repository.ErrConflict):
			h.retryAdd(w, r, id, written, c.T("sets.full", repository.MaxSetQuestions))
		default:
			h.serverError(w, r, err)
		}
		return
	}

	h.flash(w, "success", c.T("sets.added", len(drafts)))
	redirect(w, r, "/my/questions/"+id.String())
}

// retryCreate re-renders the new-set form with what was being written on it.
func (h *Handlers) retryCreate(w http.ResponseWriter, r *http.Request,
	written []service.AuthoredQuestion, bad map[int]string, message string) {

	c := h.viewCtx(w, r)
	sets, err := h.repo.QuestionSets(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusUnprocessableEntity, views.QuestionSets(c, views.QuestionSetsData{
		Sets:    sets,
		NewName: r.PostFormValue("name"),
		Drafts:  draftRows(written, bad),
		Error:   message,
	}))
}

// draftRows carries what was typed back to the form, with the rows that are
// not questions yet marked.
func draftRows(written []service.AuthoredQuestion, bad map[int]string) []views.DraftRow {
	out := make([]views.DraftRow, 0, len(written))
	for i, q := range written {
		out = append(out, views.DraftRow{
			Prompt:      q.Prompt,
			Choices:     q.Choices,
			Correct:     q.Correct,
			Explanation: q.Explanation,
			Error:       bad[i],
		})
	}
	if len(out) == 0 {
		out = []views.DraftRow{{}}
	}
	return out
}

// retryAdd re-renders the set screen with what was being written still on it.
func (h *Handlers) retryAdd(w http.ResponseWriter, r *http.Request, setID uuid.UUID,
	written []service.AuthoredQuestion, message string) {
	h.retryAddWith(w, r, setID, written, nil, message)
}

func (h *Handlers) retryAddWith(w http.ResponseWriter, r *http.Request, setID uuid.UUID,
	written []service.AuthoredQuestion, bad map[int]string, message string) {

	c := h.viewCtx(w, r)

	sets, err := h.repo.QuestionSets(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	set, err := h.repo.QuestionSet(r.Context(), setID, c.User.ID)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	questions, err := h.repo.SetQuestions(r.Context(), setID, c.User.ID, c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	drafts := draftRows(written, bad)

	h.render(w, r, http.StatusUnprocessableEntity, views.QuestionSets(c, views.QuestionSetsData{
		Sets:      sets,
		Active:    set,
		Questions: questions,
		Drafts:    drafts,
		Error:     message,
	}))
}

// draftsFrom turns submitted questions into what the repository writes, and
// refuses the lot if any one of them is not a question.
func draftsFrom(written []service.AuthoredQuestion) ([]models.TranslationDraft, []int, []int, []int, error) {
	drafts := make([]models.TranslationDraft, 0, len(written))
	categories := make([]int, 0, len(written))
	difficulties := make([]int, 0, len(written))
	correct := make([]int, 0, len(written))

	for _, q := range written {
		if !q.Valid() {
			return nil, nil, nil, nil, service.ErrBadQuestion
		}
		choices := make([]string, len(q.Choices))
		for i, choice := range q.Choices {
			choices[i] = strings.TrimSpace(choice)
		}
		drafts = append(drafts, models.TranslationDraft{
			Prompt:      strings.TrimSpace(q.Prompt),
			Choices:     choices,
			Explanation: strings.TrimSpace(q.Explanation),
			Source:      "player",
		})
		categories = append(categories, 1)
		difficulties = append(difficulties, 1)
		correct = append(correct, q.Correct)
	}
	return drafts, categories, difficulties, correct, nil
}

// UpdateSetQuestion rewrites one question.
//
// A question somebody has already answered is not edited: the review screen
// reads its text by joining, so changing it would change what people are shown
// they were asked. Those are copied instead, leaving the played one alone.
func (h *Handlers) UpdateSetQuestion(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	questionID, err := strconv.Atoi(chi.URLParam(r, "question"))
	if err != nil {
		h.NotFound(w, r)
		return
	}

	written, _ := authoredRows(r)
	if len(written) != 1 || !written[0].Valid() {
		h.flash(w, "error", c.T("challenge.questionIncomplete"))
		redirect(w, r, fmt.Sprintf("/my/questions/%s?edit=%d", id, questionID))
		return
	}

	drafts, _, _, correct, err := draftsFrom(written)
	if err != nil {
		h.flash(w, "error", c.T("challenge.questionIncomplete"))
		redirect(w, r, fmt.Sprintf("/my/questions/%s?edit=%d", id, questionID))
		return
	}

	err = h.repo.UpdateSetQuestion(r.Context(), id, c.User.ID, questionID,
		c.Locale, drafts[0], correct[0])
	switch {
	case errors.Is(err, repository.ErrConflict):
		// Somebody has answered it since the form was opened.
		h.flash(w, "error", c.T("sets.playedNoEdit"))
	case errors.Is(err, repository.ErrNotFound):
		h.NotFound(w, r)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	default:
		h.flash(w, "success", c.T("sets.updated"))
	}
	redirect(w, r, "/my/questions/"+id.String())
}

// CopySetQuestion makes an editable copy of one that has been played.
func (h *Handlers) CopySetQuestion(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	questionID, err := strconv.Atoi(chi.URLParam(r, "question"))
	if err != nil {
		h.NotFound(w, r)
		return
	}

	questions, err := h.repo.SetQuestions(r.Context(), id, c.User.ID, c.Locale)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	var source *models.Question
	for _, q := range questions {
		if q.ID == questionID {
			source = q
		}
	}
	if source == nil {
		h.NotFound(w, r)
		return
	}

	ids, err := h.repo.AddToQuestionSet(r.Context(), id, c.User.ID, c.Locale,
		[]models.TranslationDraft{{
			Prompt:      source.Prompt,
			Choices:     source.Choices,
			Explanation: source.Explanation,
			Source:      "player",
		}}, []int{source.CategoryID}, []int{source.Difficulty}, []int{source.CorrectIndex})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			h.flash(w, "error", c.T("sets.full", repository.MaxSetQuestions))
			redirect(w, r, "/my/questions/"+id.String())
			return
		}
		h.serverError(w, r, err)
		return
	}

	h.flash(w, "success", c.T("sets.copied"))
	redirect(w, r, fmt.Sprintf("/my/questions/%s?edit=%d", id, ids[0]))
}

// RemoveFromSet takes one question out.
func (h *Handlers) RemoveFromSet(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	questionID, err := strconv.Atoi(chi.URLParam(r, "question"))
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if err := h.repo.RemoveFromQuestionSet(r.Context(), id, c.User.ID, questionID); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	redirect(w, r, "/my/questions/"+id.String())
}

// DeleteQuestionSet removes a whole collection.
func (h *Handlers) DeleteQuestionSet(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	if err := h.repo.DeleteQuestionSet(r.Context(), id, c.User.ID); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	h.flash(w, "success", c.T("sets.deleted"))
	redirect(w, r, "/my/questions")
}

// KeepMatchQuestions saves the questions written for one match into a
// collection, which is the path most people will actually use: write them once
// while opening a match, keep them when it turns out they were good.
func (h *Handlers) KeepMatchQuestions(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	challengeID, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}

	setID, ok := parseUUID(r.PostFormValue("set"))
	if !ok {
		// No collection chosen: make one named after the match's opponent list
		// rather than asking a second question.
		set, err := h.repo.CreateQuestionSet(r.Context(), c.User.ID,
			c.T("sets.defaultName"), c.Locale)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		setID = set.ID
	}

	added, err := h.repo.SaveMatchQuestionsToSet(r.Context(), challengeID, setID, c.User.ID)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	if added == 0 {
		h.flash(w, "info", c.T("sets.nothingToKeep"))
	} else {
		h.flash(w, "success", c.T("sets.kept", added))
	}
	redirect(w, r, "/my/questions/"+setID.String())
}

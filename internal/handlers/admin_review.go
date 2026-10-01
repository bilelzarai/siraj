package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// The translation review queue.
func (h *Handlers) AdminReview(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	questions, total, err := h.repo.AdminQuestions(r.Context(), repository.AdminQuestionFilter{
		OnlyReview: true,
		Locale:     c.Locale,
		Limit:      adminPageSize,
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// The queue is only half the job: a question can also be waiting on a
	// language nobody has written yet. Those are what the translate action
	// operates on, so they belong on the same screen.
	incomplete, incompleteTotal, err := h.repo.AdminQuestions(r.Context(), repository.AdminQuestionFilter{
		MinLocales: models.ShippedLocales,
		Locale:     c.Locale,
		Limit:      adminPageSize,
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminReview(c, chrome, views.AdminReviewData{
		Questions:       questions,
		Total:           total,
		Incomplete:      incomplete,
		IncompleteTotal: incompleteTotal,
		Translator:      h.translator.Provider(),
		Available:       h.translator.Available(),
	}))
}

func (h *Handlers) AdminReviewAction(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}
	locale := r.PostFormValue("locale")
	if !i18n.IsSupported(locale) {
		h.NotFound(w, r)
		return
	}

	switch chi.URLParam(r, "action") {
	case "approve":
		if err = h.repo.ApproveTranslation(r.Context(), id, locale, c.User.ID); err == nil {
			h.audit(r, "translation.approve", "question", chi.URLParam(r, "id"),
				map[string]any{"locale": locale})
			h.flash(w, "success", c.T("admin.review.approved", locale))
		}
	case "reject":
		// Rejecting deletes the translation outright, which is the point: a
		// machine rendering of scripture that a human has refused should not
		// sit in the table waiting to be approved by the next reviewer.
		if err = h.repo.RejectTranslation(r.Context(), id, locale); err == nil {
			h.audit(r, "translation.reject", "question", chi.URLParam(r, "id"),
				map[string]any{"locale": locale})
			h.flash(w, "info", c.T("admin.review.rejected", locale))
		}
	default:
		h.NotFound(w, r)
		return
	}

	if err != nil {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, backTo(r, "/admin/review"))
}

// AdminTranslate fills one missing locale of a question from the configured
// provider.
//
// The output is written with needs_review set, which every player-facing query
// filters on, so nothing reaches a player between this call and a human
// approving it in the queue above. That gate is the reason this feature is
// allowed to exist at all: a plausible-but-wrong rendering of a Qur'anic verse
// or a hadith is a real harm, not a cosmetic defect.
func (h *Handlers) AdminTranslate(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}
	target := r.PostFormValue("locale")
	if !i18n.IsSupported(target) {
		h.NotFound(w, r)
		return
	}

	if !h.translator.Available() {
		h.flash(w, "error", c.T("admin.review.translateUnavailable"))
		redirect(w, r, backTo(r, "/admin/review"))
		return
	}

	draft, err := h.repo.QuestionDraftByID(r.Context(), id)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	// Never paper over a translation a human has already signed off. Refilling
	// one that is still awaiting review is fine — that is a retry.
	if existing, ok := draft.Translations[target]; ok && !existing.NeedsReview {
		h.flash(w, "error", c.T("admin.review.translateApprovedExists", target))
		redirect(w, r, backTo(r, "/admin/review"))
		return
	}

	source, from := pickTranslationSource(draft, target)
	if source == nil {
		h.flash(w, "error", c.T("admin.review.translateNoSource"))
		redirect(w, r, backTo(r, "/admin/review"))
		return
	}

	// The provider is a network call with a stranger's latency on the other
	// end; do not let it sit on the request for as long as it likes.
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	out, err := service.TranslateQuestion(ctx, h.translator, service.QuestionText{
		Prompt:      source.Prompt,
		Choices:     source.Choices,
		Explanation: source.Explanation,
	}, from, target)
	if err != nil {
		slogError(r, fmt.Errorf("translate question %d into %s: %w", id, target, err))
		h.flash(w, "error", c.T("admin.review.translateFailed", err.Error()))
		redirect(w, r, backTo(r, "/admin/review"))
		return
	}

	draft.Translations = map[string]models.TranslationDraft{
		target: {
			Prompt:      out.Prompt,
			Choices:     out.Choices,
			Explanation: out.Explanation,
			Source:      "machine",
			NeedsReview: true,
		},
	}
	if _, err := h.repo.UpsertQuestion(r.Context(), draft); err != nil {
		h.serverError(w, r, err)
		return
	}

	h.audit(r, "translation.machine", "question", chi.URLParam(r, "id"), map[string]any{
		"from": from, "to": target, "provider": h.translator.Provider(),
	})
	h.flash(w, "success", c.T("admin.review.translateQueued", target))
	redirect(w, r, backTo(r, "/admin/review"))
}

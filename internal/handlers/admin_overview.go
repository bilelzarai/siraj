package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// The screens that report rather than change: dashboard, content health,
// poorly-rated questions, player remarks, and the audit trail.
func (h *Handlers) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	stats, err := h.repo.PlatformStats(r.Context(), c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	audit, _ := h.repo.AuditTrail(r.Context(), 8)

	h.render(w, r, http.StatusOK, views.AdminDashboard(c, chrome, views.AdminDashboardData{
		Stats:  stats,
		Recent: audit,
	}))
}

// AdminRatingsDismiss marks a flagged question as read, so the alert can empty.
func (h *Handlers) AdminRatingsDismiss(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}
	if err := h.repo.DismissRatings(r.Context(), id); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	h.audit(r, "question.ratings.dismiss", "question", chi.URLParam(r, "id"), nil)
	h.flash(w, "success", c.T("admin.rated.dismissed", id))
	redirect(w, r, backTo(r, "/admin/rated"))
}

func (h *Handlers) AdminIntegrity(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	issues, err := h.repo.IntegrityIssues(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// One language at a time, the interface's own unless the screen was asked
	// for another. `loc` rather than `lang`: `lang` switches the whole interface,
	// and reading Arabic duplicates should not mean reading an Arabic interface
	// to get at them.
	locale := c.Locale
	if q := strings.TrimSpace(r.URL.Query().Get("loc")); i18n.IsSupported(q) {
		locale = q
	}

	duplicates, sweptAt, err := h.duplicates.Pairs(r.Context(), locale, service.ListAbove, 60)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// Same as the import preview: a pair is raised by one language and decided
	// with all of them in view, so the comparison is built here rather than
	// left to a second screen.
	compare, err := h.comparer.Pairs(r.Context(), duplicates)
	if err != nil {
		slog.ErrorContext(r.Context(), "duplicate comparison failed", "error", err)
		compare = nil
	}

	h.render(w, r, http.StatusOK, views.AdminIntegrity(c, chrome, views.AdminIntegrityData{
		Issues:     issues,
		Duplicates: duplicates,
		Compare:    compare,
		Locale:     locale,
		SweptAt:    sweptAt,
	}))
}

// AdminComments is the moderation queue for player remarks on questions.
//
// Migration 0006 added `hidden` and `hidden_by` for exactly this, and the
// repository grew the two methods it needs, but nothing ever called them — so
// player-authored text about scripture questions was publishable and
// unmoderatable.
func (h *Handlers) AdminComments(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	// Hidden rows are kept for the trail, so a moderator has to be able to see
	// them in order to put one back.
	includeHidden := r.URL.Query().Get("hidden") != "0"

	total, err := h.repo.CountQuestionComments(r.Context(), includeHidden)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging := h.paging(w, r).withTotal(total)

	comments, err := h.repo.QuestionCommentsPage(r.Context(), includeHidden,
		paging.Size, paging.Offset())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminComments(c, chrome, views.AdminCommentsData{
		Comments:      comments,
		IncludeHidden: includeHidden,
		Pager:         pagerFor(paging),
	}))
}

// AdminCommentAction hides or restores one remark.
func (h *Handlers) AdminCommentAction(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}

	var hidden bool
	switch chi.URLParam(r, "action") {
	case "hide":
		hidden = true
	case "show":
		hidden = false
	default:
		h.NotFound(w, r)
		return
	}

	if err := h.repo.SetQuestionCommentHidden(r.Context(), id, hidden, c.User.ID); err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	action, message := "comment.show", "admin.comments.shown"
	if hidden {
		action, message = "comment.hide", "admin.comments.hidden"
	}
	h.audit(r, action, "comment", chi.URLParam(r, "id"), nil)
	h.flash(w, "success", c.T(message))
	redirect(w, r, backTo(r, "/admin/comments"))
}

func (h *Handlers) AdminAudit(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	total, err := h.repo.CountAuditEntries(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging := h.paging(w, r).withTotal(total)

	entries, err := h.repo.AuditPage(r.Context(), paging.Size, paging.Offset())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, views.AdminAudit(c, chrome, views.AdminAuditData{
		Entries: entries,
		Pager:   pagerFor(paging),
	}))
}

// AdminRated lists the questions players keep marking down — the red light
// that a rating is for. Reached from the nav, which carries the count as a
// badge so a bad question is noticed rather than waited for.
func (h *Handlers) AdminRated(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	total, err := h.repo.CountPoorlyRated(r.Context(),
		models.PoorRatingThreshold, models.MinRatingVotes)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging := h.paging(w, r).withTotal(total)

	questions, err := h.repo.PoorlyRatedQuestions(r.Context(), c.Locale,
		models.PoorRatingThreshold, models.MinRatingVotes, paging.Size, paging.Offset())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminRated(c, chrome, views.AdminRatedData{
		Questions: questions,
		Pager:     pagerFor(paging),
		Threshold: fmt.Sprintf("%.0f", models.PoorRatingThreshold),
		MinVotes:  models.MinRatingVotes,
	}))
}

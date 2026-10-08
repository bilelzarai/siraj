package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
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
	insight, err := h.repo.DashboardInsight(r.Context(), c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	audit, _ := h.repo.AuditTrail(r.Context(), 8)

	h.render(w, r, http.StatusOK, views.AdminDashboard(c, chrome, views.AdminDashboardData{
		Stats:     stats,
		Insight:   insight,
		Recent:    audit,
		Attention: h.attention(r, c, chrome, insight),
	}))
}

// attention assembles the "needs attention" list.
//
// Every entry is a number some other admin screen already reports. The list
// adds one thing to them: an order, so the first screen an admin opens in the
// morning is the one with the oldest problem on it rather than the one they
// happened to bookmark. Nothing is listed at zero — a list of things that are
// fine is a list nobody reads.
func (h *Handlers) attention(r *http.Request, c views.Ctx, chrome views.AdminChrome,
	insight *models.DashboardInsight) []views.AttentionItem {

	var out []views.AttentionItem

	if chrome.PendingReview > 0 {
		detail := ""
		if insight.OldestPendingReview != nil {
			detail = c.T("admin.attention.reviewOldest",
				c.Tr.RelativeTime(*insight.OldestPendingReview))
		}
		out = append(out, views.AttentionItem{
			Tone: "lamp", Icon: "i-review",
			Title:  c.T("admin.attention.review", chrome.PendingReview),
			Detail: detail,
			Href:   "/admin/review", Action: c.T("admin.attention.open"),
		})
	}
	if chrome.PoorlyRated > 0 {
		out = append(out, views.AttentionItem{
			Tone: "danger", Icon: "i-thumbs-down",
			Title:  c.T("admin.attention.rated", chrome.PoorlyRated),
			Detail: c.T("admin.attention.ratedDetail", models.MinRatingVotes),
			Href:   "/admin/rated", Action: c.T("admin.attention.inspect"),
		})
	}
	if chrome.AwaitingSupport > 0 {
		out = append(out, views.AttentionItem{
			Tone: "info", Icon: "i-support",
			Title: c.T("admin.attention.support", chrome.AwaitingSupport),
			Href:  "/admin/support", Action: c.T("admin.attention.open"),
		})
	}
	// The duplicate sweep is held for a few minutes at a time, so reading it
	// here costs a cached answer rather than a comparison of the whole bank.
	if pairs, _, err := h.duplicates.Pairs(r.Context(), c.Locale, service.ListAbove, 60); err == nil && len(pairs) > 0 {
		detail := ""
		if top := pairs[0]; top.SameFileLine == 0 {
			detail = c.T("admin.attention.duplicatesTop", top.LeftID, top.RightID, top.Percent())
		}
		out = append(out, views.AttentionItem{
			Tone: "lamp", Icon: "i-copy",
			Title:  c.T("admin.attention.duplicates", len(pairs)),
			Detail: detail,
			Href:   "/admin/integrity#duplicates", Action: c.T("admin.attention.compare"),
		})
	}

	return out
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

	library, err := h.repo.Library(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminIntegrity(c, chrome, views.AdminIntegrityData{
		Issues:     issues,
		Health:     views.BuildHealth(issues, len(duplicates), library.Questions),
		Library:    library,
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

	// The queue opens on what is still waiting. Hidden rows are kept for the
	// trail and reachable from their own tab, because a moderator has to be
	// able to see what they hid in order to put it back.
	filter := repository.CommentFilter{
		Query:  clip(strings.TrimSpace(r.URL.Query().Get("q")), 120),
		State:  validCommentState(r.URL.Query().Get("state")),
		Locale: validCommentLocale(r.URL.Query().Get("loc")),
	}

	total, err := h.repo.CountQuestionComments(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging := h.paging(w, r).withTotal(total)
	filter.Limit, filter.Offset = paging.Size, paging.Offset()

	comments, err := h.repo.QuestionCommentsPage(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	counts, err := h.repo.CommentCountsByState(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminComments(c, chrome, views.AdminCommentsData{
		Comments: comments,
		Counts:   counts,
		State:    filter.State,
		Query:    filter.Query,
		Locale:   filter.Locale,
		Pager:    pagerFor(paging),
	}))
}

// validCommentState keeps the queue's own split to the values it has. Anything
// else shows everything, which is the honest reading of an unknown tab.
func validCommentState(state string) string {
	switch state {
	case "open", "resolved", "hidden":
		return state
	}
	return ""
}

func validCommentLocale(locale string) string {
	if i18n.IsSupported(locale) {
		return locale
	}
	return ""
}

// AdminCommentAction is the three things a moderator does to one remark.
//
// Hiding and resolving are different acts and the screen offers both: a remark
// that reported a real fault should be marked dealt with, not hidden, because
// hiding a correct observation pretends it never arrived.
func (h *Handlers) AdminCommentAction(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}

	var action, message string
	switch chi.URLParam(r, "action") {
	case "hide":
		err = h.repo.SetQuestionCommentHidden(r.Context(), id, true, c.User.ID)
		action, message = "comment.hide", "admin.comments.hidden"
	case "show":
		err = h.repo.SetQuestionCommentHidden(r.Context(), id, false, c.User.ID)
		action, message = "comment.show", "admin.comments.shown"
	case "resolve":
		err = h.repo.SetQuestionCommentResolved(r.Context(), id, true, c.User.ID)
		action, message = "comment.resolve", "admin.comments.resolved"
	case "reopen":
		err = h.repo.SetQuestionCommentResolved(r.Context(), id, false, c.User.ID)
		action, message = "comment.reopen", "admin.comments.reopened"
	default:
		h.NotFound(w, r)
		return
	}
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	h.audit(r, action, "comment", chi.URLParam(r, "id"), nil)
	h.flash(w, "success", c.T(message))
	redirect(w, r, backTo(r, "/admin/comments"))
}

func (h *Handlers) AdminAudit(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	filter := auditFilterFrom(r)

	total, err := h.repo.CountAuditEntries(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging := h.paging(w, r).withTotal(total)
	filter.Limit, filter.Offset = paging.Size, paging.Offset()

	entries, err := h.repo.AuditPage(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// Whoever has ever acted, not whoever is staff today: the filter has to be
	// able to name a person whose account is gone, because those are the rows
	// the trail is kept for.
	actors, err := h.repo.AuditActors(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminAudit(c, chrome, views.AdminAuditData{
		Entries: entries,
		Actors:  actors,
		Query:   filter.Query,
		Actor:   filter.Actor,
		Area:    filter.Area,
		Window:  r.URL.Query().Get("window"),
		Pager:   pagerFor(paging),
		Sorting: views.SortState{
			Sort: filter.Sort, Path: "/admin/audit", Query: r.URL.Query(),
		},
	}))
}

// auditWindows is how far back the trail's time filter can look, as the
// query-string value and the span it means. Named here so the screen offers
// exactly the spans the query understands.
var auditWindows = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// auditFilterFrom reads the trail's filter off the query string. Anything
// unrecognised is dropped rather than refused: this screen is linked to from
// elsewhere, and a stale parameter should show the log, not a 404.
func auditFilterFrom(r *http.Request) repository.AuditFilter {
	q := r.URL.Query()
	f := repository.AuditFilter{
		Query: clip(strings.TrimSpace(q.Get("q")), 120),
		Actor: clip(strings.TrimSpace(q.Get("actor")), 60),
	}
	if area := q.Get("area"); slices.Contains(models.AuditAreas, area) {
		f.Area = area
	}
	if span, ok := auditWindows[q.Get("window")]; ok {
		f.Since = time.Now().Add(-span)
	}
	f.Sort = sortFrom(values(q))
	return f
}

// ratingWindows is how far back the overall figures look. The zero value is
// "all time", which is also what an unrecognised parameter falls back to.
var ratingWindows = map[string]time.Duration{
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
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

	// How the bank is rated as a whole, which is what gives the flagged list a
	// scale: five questions below the line out of a bank players mostly like is
	// a different morning from five out of a bank they do not.
	window := ratingWindows[r.URL.Query().Get("period")]
	overall, err := h.repo.RatingOverview(r.Context(), window)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminRated(c, chrome, views.AdminRatedData{
		Questions: questions,
		Pager:     pagerFor(paging),
		Threshold: fmt.Sprintf("%.0f", models.PoorRatingThreshold),
		MinVotes:  models.MinRatingVotes,
		Overall:   overall,
		Period:    r.URL.Query().Get("period"),
	}))
}

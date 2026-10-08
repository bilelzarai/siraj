package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/views"
)

// Taking the admin's lists out of the admin: the search field in the top bar,
// and the exports behind the download buttons.
//
// Both exist because an admin screen is a view and not the data. A pager can
// show forty users and a question somebody needs to count is the two thousandth;
// copying a table out of a browser loses every column that was hidden on a
// narrow window.

// adminSearchLimit is how many hits each area contributes. Short on purpose:
// this is a jump-to control, and a list long enough to scroll is a list you
// read instead of a shortcut you take.
const adminSearchLimit = 5

// AdminSearch answers the top bar's one search field.
//
// A fragment rather than a page: it is rendered into the drop-down under the
// field, so it returns the list and nothing around it.
func (h *Handlers) AdminSearch(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	query := clip(r.URL.Query().Get("q"), 120)

	// Users only for an admin. The directory itself is behind RequireAdmin, and
	// a search that answered out of a screen the reader cannot open would be a
	// way to read that screen.
	results, err := h.repo.AdminSearch(r.Context(), query, c.Locale,
		c.User != nil && c.User.IsAdmin(), adminSearchLimit)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.renderFragment(w, r, views.AdminSearchResults(c, views.AdminSearchData{
		Query:     query,
		Users:     results.Users,
		Questions: results.Questions,
		Tickets:   results.Tickets,
	}))
}

// ------------------------------------------------------------- exports --

// AdminExportUsers writes the user directory as CSV, filtered exactly as the
// screen it was launched from.
//
// The filter is read from the same query string the listing uses, so the file
// holds what the admin was looking at rather than the whole table — an export
// button that ignores the filter above it is a different feature wearing the
// same label.
func (h *Handlers) AdminExportUsers(w http.ResponseWriter, r *http.Request) {
	filter := adminUserFilterFrom(r)
	// Every matching row, not one page of them. The pager exists so a screen
	// stays readable; a file has no such limit, and a partial export is worse
	// than none because nothing in it says it is partial.
	filter.Limit, filter.Offset = exportLimit, 0

	users, _, err := h.repo.AdminUsers(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	out := h.beginCSV(w, r, "siraj-users")
	defer out.Flush()

	_ = out.Write([]string{"username", "display_name", "email", "role", "status",
		"country", "locale", "xp", "games_played", "best_streak",
		"created_at", "last_seen_at"})
	for _, u := range users {
		_ = out.Write([]string{u.Username, u.DisplayName, u.Email, u.Role, u.Status,
			u.Country, u.Locale,
			strconv.Itoa(u.XP), strconv.Itoa(u.GamesPlayed), strconv.Itoa(u.BestStreak),
			u.CreatedAt.UTC().Format(time.RFC3339), u.LastSeenAt.UTC().Format(time.RFC3339)})
	}
	h.audit(r, "users.export", "user", "", map[string]any{"rows": len(users)})
}

// AdminExportQuestions writes the question bank as CSV under the filter on
// screen, including the figures the table shows but a copy-paste loses.
func (h *Handlers) AdminExportQuestions(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	filter := adminQuestionFilterFrom(values(r.URL.Query()), c.Locale)
	filter.Limit, filter.Offset = exportLimit, 0

	questions, _, err := h.repo.AdminQuestions(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	out := h.beginCSV(w, r, "siraj-questions")
	defer out.Flush()

	_ = out.Write([]string{"id", "category", "difficulty", "points", "state",
		"languages", "awaiting_review", "changes_requested",
		"plays", "accuracy_pct", "prompt", "created_at"})
	for _, q := range questions {
		state := "retired"
		if q.IsActive {
			state = "live"
		}
		_ = out.Write([]string{
			strconv.Itoa(q.ID), q.CategoryName, strconv.Itoa(q.Difficulty),
			strconv.Itoa(q.Points), state,
			strconv.Itoa(q.LocaleCount), strconv.Itoa(q.PendingReview),
			strconv.Itoa(q.Noted),
			strconv.Itoa(q.Plays), strconv.Itoa(q.Accuracy()),
			q.Prompt, q.CreatedAt.UTC().Format(time.RFC3339)})
	}
	h.audit(r, "questions.export", "question", "", map[string]any{"rows": len(questions)})
}

// AdminExportAudit writes the trail as CSV under the filter on screen.
//
// The entries cannot be edited or deleted, and this is how a copy of them
// leaves the application — so the export is itself an audited action.
func (h *Handlers) AdminExportAudit(w http.ResponseWriter, r *http.Request) {
	filter := auditFilterFrom(r)
	filter.Limit, filter.Offset = exportLimit, 0

	entries, err := h.repo.AuditPage(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	out := h.beginCSV(w, r, "siraj-audit")
	defer out.Flush()

	_ = out.Write([]string{"time", "actor", "action", "area", "target_kind",
		"target", "changed", "ip"})
	for _, e := range entries {
		changed := ""
		for i, c := range e.Changes() {
			if i > 0 {
				changed += "; "
			}
			changed += fmt.Sprintf("%s: %s → %s", c.Field, c.From, c.To)
		}
		_ = out.Write([]string{
			e.CreatedAt.UTC().Format(time.RFC3339), e.ActorUsername, e.Action,
			e.Area(), e.TargetKind, e.Label(), changed, e.IP})
	}
	h.audit(r, "audit.export", "audit", "", map[string]any{"rows": len(entries)})
}

// exportLimit is the ceiling on one file. High enough that no realistic filter
// reaches it, low enough that a request cannot be made to read the whole bank
// into memory — the repository caps its own page size well below this, so the
// value is also what tells those methods the caller wants everything.
const exportLimit = 200

// beginCSV sets the headers that make a browser save the response instead of
// showing it, and returns the writer to put rows into.
//
// The filename carries the date because these files are read next to each
// other: three downloads called users.csv are three files nobody can tell
// apart an hour later.
func (h *Handlers) beginCSV(w http.ResponseWriter, r *http.Request, name string) *csv.Writer {
	filename := fmt.Sprintf("%s-%s.csv", name, time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	// The bank is Arabic first, and Excel reads a UTF-8 file without a mark as
	// the local code page — which turns every Arabic prompt in the column into
	// a row of symbols.
	_, _ = w.Write([]byte("\xef\xbb\xbf"))
	return csv.NewWriter(w)
}

// adminUserFilterFrom and adminQuestionFilterFrom read a listing's filter off
// the query string. Both exports and both listings go through them, which is
// what keeps a download honest about what it contains.
func adminUserFilterFrom(r *http.Request) repository.AdminUserFilter {
	q := r.URL.Query()
	return repository.AdminUserFilter{
		Query:   clip(q.Get("q"), 120),
		Kind:    validUserKind(q.Get("kind")),
		Role:    validRole(q.Get("role")),
		Status:  validStatus(q.Get("status")),
		Country: clip(q.Get("country"), 60),
		Sort:    sortFrom(values(q)),
	}
}

// sortFrom reads a list's ordering off the query string.
//
// Nothing is validated here on purpose: the column name is checked against the
// list's own whitelist in the repository, where the SQL that implements it
// lives, so there is one place that decides what is sortable rather than two
// that can disagree.
func sortFrom(v urlValues) repository.Sort {
	return repository.Sort{
		By:   clip(v.Get("sort"), 24),
		Desc: v.Get("dir") == "desc",
	}
}

// validUserKind keeps the directory's own split to the two values it has.
func validUserKind(kind string) string {
	switch kind {
	case "player", "staff":
		return kind
	}
	return ""
}

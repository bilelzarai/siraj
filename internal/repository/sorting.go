package repository

import "fmt"

// Sorting a list that is paged.
//
// The console's long lists are paged, so a sort has to happen in the database.
// Sorting the rows that are already on screen looks identical and is a
// different answer: "order users by rounds played" over page one of ten gives
// the busiest of those forty, not the busiest at all, and nothing on screen
// says which of the two you are looking at. The short lists — a dozen
// categories, a page of import rows — are fully loaded and may be sorted in
// the browser, where the two answers coincide.
//
// A column name arrives from a query string, so it is never interpolated. Each
// list declares the columns it will sort by as a map from the name the screen
// uses to the SQL that implements it, and anything not in that map falls back
// to the list's own default order.

// Sort is what a list was asked to sort by. The zero value is the list's
// natural order, which is what a screen opens on.
type Sort struct {
	// By is the column key as the screen names it — "name", "rounds", "seen".
	By string
	// Desc reverses it. Ascending is the default because it is what a reader
	// assumes of a name, and the columns where it is not — a date, a count —
	// say so by opening descending the first time they are pressed.
	Desc bool
}

// Active reports whether anything was asked for.
func (s Sort) Active() bool { return s.By != "" }

// On reports whether this is the column currently sorted by, which is what
// decides a header's arrow and its aria-sort.
func (s Sort) On(column string) bool { return s.By == column }

// Direction is the word aria-sort takes, empty when this is not the sorted
// column.
func (s Sort) Direction(column string) string {
	if !s.On(column) {
		return ""
	}
	if s.Desc {
		return "descending"
	}
	return "ascending"
}

// Next is the direction pressing this header should ask for: the other one if
// it is already sorted by this column, and otherwise the column's own sensible
// first direction — a name reads A to Z, a count and a date read largest and
// newest first.
func (s Sort) Next(column string, descFirst bool) bool {
	if s.On(column) {
		return !s.Desc
	}
	return descFirst
}

// orderBy builds the ORDER BY for one list.
//
// columns maps a screen's column key to the SQL that sorts by it. tiebreak is
// appended to every ordering: without it, rows that compare equal come back in
// whatever order the planner liked this time, so paging through a sort by
// status can show the same row twice and skip another.
func orderBy(s Sort, columns map[string]string, fallback, tiebreak string) string {
	expr, ok := columns[s.By]
	if !ok {
		return " ORDER BY " + fallback
	}
	direction := "ASC"
	if s.Desc {
		direction = "DESC"
	}
	// NULLs last in both directions: a row with nothing in the column is not
	// the smallest value, it is an absence, and it belongs at the end either
	// way rather than at the top of a descending sort.
	return fmt.Sprintf(" ORDER BY %s %s NULLS LAST, %s", expr, direction, tiebreak)
}

// The columns each list may be sorted by. Written as SQL fragments against the
// aliases the list's own query uses, and reachable only through the maps — a
// key that is not here cannot reach a statement.
var (
	userSortColumns = map[string]string{
		"name":   "lower(u.display_name)",
		"role":   "u.role",
		"status": "u.status",
		"lang":   "u.locale",
		"rounds": "u.games_played",
		"streak": "u.best_streak",
		// Nothing answered has no accuracy, which is not the same as zero —
		// NULLS LAST puts those players at the end of both directions.
		"accuracy": "CASE WHEN COALESCE(a.answered, 0) = 0 THEN NULL ELSE a.correct::float8 / a.answered END",
		"seen":     "u.last_seen_at",
		"joined":   "u.created_at",
		"country":  "u.country",
	}

	questionSortColumns = map[string]string{
		"id":         "q.id",
		"prompt":     "lower(COALESCE(t.prompt, any_t.prompt, ''))",
		"category":   "lower(COALESCE(ct.name, c.slug))",
		"difficulty": "q.difficulty",
		"points":     "q.points",
		"languages":  "(SELECT count(*) FROM question_translations x WHERE x.question_id = q.id)",
		"accuracy":   "CASE WHEN COALESCE(a.plays, 0) = 0 THEN NULL ELSE a.correct::float8 / a.plays END",
		"plays":      "COALESCE(a.plays, 0)",
		"status":     "q.is_active",
		"created":    "q.created_at",
	}

	auditSortColumns = map[string]string{
		"time":   "created_at",
		"actor":  "lower(actor_username)",
		"action": "action",
		"target": "lower(COALESCE(detail->>'label', target_id))",
		"area":   "target_kind",
	}
)

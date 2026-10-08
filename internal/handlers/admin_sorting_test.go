package handlers_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// A column name arrives from the query string and ends up deciding an ORDER
// BY. It is never interpolated — each list declares what it will sort by and
// anything else falls back to its own order — and this is the test that says
// so, because the failure mode is not a wrong sort but a broken database.
func TestAnUnknownSortColumnCannotReachTheSQL(t *testing.T) {
	a := newApp(t)
	a.register("sortprobe")
	a.promote("sortprobe", models.RoleAdmin)

	hostile := []string{
		"id; DROP TABLE users",
		"id) --",
		"(SELECT 1)",
		"created_at/**/DESC",
		"nonexistent_column",
		"'",
		strings.Repeat("a", 200),
	}
	for _, path := range []string{"/admin/users", "/admin/questions", "/admin/audit"} {
		for _, probe := range hostile {
			u := path + "?sort=" + url.QueryEscape(probe) + "&dir=desc"
			if status, _ := a.get(u); status != 200 {
				t.Errorf("%s with sort=%q → %d, want the list in its own order",
					path, probe, status)
			}
		}
	}

	// And the tables are all still there afterwards.
	if _, _, err := a.repo.AdminUsers(t.Context(), repository.AdminUserFilter{Limit: 1}); err != nil {
		t.Fatalf("the user directory is gone: %v", err)
	}
}

// Sorting a paged list has to happen in the database. Sorting the rows already
// on screen looks identical and answers a different question: the busiest of
// the forty you can see, not the busiest there are.
func TestSortingIsDoneByTheDatabaseNotThePage(t *testing.T) {
	a := newApp(t)
	a.register("sortreader")
	a.promote("sortreader", models.RoleAdmin)

	// A few accounts whose display names bracket the alphabet, so the first
	// page of a sorted list is checkably the first of the whole list and not
	// of whatever happened to be on page one.
	for _, name := range []string{"zzz_sorter", "aaa_sorter", "mmm_sorter"} {
		other := newAppSharing(t, a)
		other.register(name)
	}

	// The whole list, sorted, and the same sort one small page at a time.
	_, all := a.get("/admin/users?sort=name&size=50")
	full := displayNames(t, all)
	if len(full) < 3 {
		t.Fatalf("only %d accounts listed", len(full))
	}
	assertOrdered(t, full, false)

	_, firstPage := a.get("/admin/users?sort=name&size=5")
	page := displayNames(t, firstPage)
	if len(page) == 0 {
		t.Fatal("the first page is empty")
	}
	// The database did the sorting, so page one holds the first rows of the
	// whole order. Sorted in the browser it would hold the first rows of
	// whichever five the server happened to send.
	if page[0] != full[0] {
		t.Errorf("page one starts at %q; the sorted list starts at %q", page[0], full[0])
	}

	// And the other direction is ordered the other way.
	_, down := a.get("/admin/users?sort=name&dir=desc&size=50")
	assertOrdered(t, displayNames(t, down), true)
}

// assertOrdered checks a column is monotonic, which is the claim a sort makes.
// Comparing the list to its own reverse would be a stronger claim and a false
// one: the suite shares a database, so two pages of fifty are two windows into
// a longer list and are not reverses of each other.
func assertOrdered(t *testing.T, values []string, desc bool) {
	t.Helper()
	for i := 1; i < len(values); i++ {
		prev, cur := strings.ToLower(values[i-1]), strings.ToLower(values[i])
		if desc && prev < cur {
			t.Errorf("descending, but %q comes before %q", values[i-1], values[i])
			return
		}
		if !desc && prev > cur {
			t.Errorf("ascending, but %q comes before %q", values[i-1], values[i])
			return
		}
	}
}

// A sort that loses the filter sorts a different list than the one being read.
func TestSortingKeepsTheFilterAndDropsThePage(t *testing.T) {
	a := newApp(t)
	a.register("sortfilter")
	a.promote("sortfilter", models.RoleAdmin)

	_, body := a.get("/admin/users?q=sortfilter&status=active&page=1&size=5")
	for _, href := range sortHrefs(t, body) {
		for _, keep := range []string{"q=sortfilter", "status=active"} {
			if !strings.Contains(href, keep) {
				t.Errorf("a sort link dropped %s: %s", keep, href)
			}
		}
		// Page nine of one order is not a place in another, and landing there
		// would look like the rows had vanished.
		if strings.Contains(href, "page=") {
			t.Errorf("a sort link kept the page number: %s", href)
		}
		if status, _ := a.get(href); status != 200 {
			t.Errorf("%s → %d", href, status)
		}
	}
}

// Pressing the sorted column again turns it over, and the header says which
// way round it is.
func TestPressingTheSortedColumnReversesIt(t *testing.T) {
	a := newApp(t)
	a.register("sortflip")
	a.promote("sortflip", models.RoleAdmin)

	_, body := a.get("/admin/users?sort=name")
	if !strings.Contains(body, `aria-sort="ascending"`) {
		t.Error("the sorted column does not say which way it is sorted")
	}
	// The link on that same column now asks for the other direction.
	if !regexp.MustCompile(`sort=name[^"]*dir=desc|dir=desc[^"]*sort=name`).MatchString(body) {
		t.Errorf("pressing the sorted column again does not reverse it:\n%s", trim(body))
	}

	_, down := a.get("/admin/users?sort=name&dir=desc")
	if !strings.Contains(down, `aria-sort="descending"`) {
		t.Error("the reversed column does not say so")
	}
}

// Every sortable column on every paged list, pressed.
func TestEverySortableColumnAnswers(t *testing.T) {
	a := newApp(t)
	a.register("sortsweep")
	a.promote("sortsweep", models.RoleAdmin)

	// A question and an action, so each list has a table rather than its empty
	// state — an empty list has no headers and would pass this vacuously.
	if _, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		CategoryID: 1, Difficulty: 2, Points: 20, CorrectIndex: 0,
		Source: "test", IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "A question to sort", Choices: []string{"a", "b", "c", "d"}, Source: "human"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if status, _ := a.post("/admin/categories/1/retire", url.Values{}); status != 303 {
		t.Fatal("could not leave an entry in the trail")
	}
	if status, _ := a.post("/admin/categories/1/restore", url.Values{}); status != 303 {
		t.Fatal("could not restore")
	}

	lists := map[string][]string{
		"/admin/users":     {"name", "role", "status", "lang", "rounds", "streak", "seen"},
		"/admin/questions": {"prompt", "difficulty", "languages", "accuracy", "plays", "status"},
		"/admin/audit":     {"time", "actor", "action", "target", "area"},
	}
	for path, columns := range lists {
		for _, column := range columns {
			for _, dir := range []string{"", "desc"} {
				u := path + "?sort=" + column
				if dir != "" {
					u += "&dir=" + dir
				}
				status, body := a.get(u)
				if status != 200 {
					t.Errorf("%s → %d", u, status)
					continue
				}
				if !strings.Contains(body, `aria-sort=`) {
					t.Errorf("%s renders no sorted column", u)
				}
			}
		}
	}
}

// The fully-loaded tables sort in the browser, which is correct for them —
// they hold every row there is. The kit does that from data-sort on the
// header, so what has to be true is that the headers carry it.
func TestTheShortTablesOfferTheirOwnSort(t *testing.T) {
	a := newApp(t)
	a.register("shortsort")
	a.promote("shortsort", models.RoleAdmin)

	// The import screen's tables only exist once something has been imported;
	// with nothing there it is an empty state and has no headers to offer.
	header := csvHeader(t, a)
	if status, _ := a.upload("/admin/questions/import", "sorted.csv",
		header+"\n1,quran,1,10,0,ar,سؤال للفرز,أ,ب,ج,د,,\n", nil); status != 200 {
		t.Fatalf("seeding an import → %d", status)
	}

	for path, want := range map[string]int{
		"/admin/categories":       7,
		"/admin/questions/import": 4,
		"/admin":                  4,
	} {
		_, body := a.get(path)
		if got := strings.Count(body, "data-sort"); got < want {
			t.Errorf("%s has %d sortable headers, want at least %d", path, got, want)
		}
	}
}

// ---------------------------------------------------------------- helpers --

// The directory sorts by display name, so that is the column to read back.
var displayNameCell = regexp.MustCompile(`<div class="t truncate">\s*<a href="/u/[^"]*">([^<]*)</a>`)

func displayNames(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, m := range displayNameCell.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func sortHrefs(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`<a class="th-sort[^"]*" href="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, strings.ReplaceAll(m[1], "&amp;", "&"))
	}
	if len(out) == 0 {
		t.Fatalf("no sortable headers in:\n%s", trim(body))
	}
	return out
}

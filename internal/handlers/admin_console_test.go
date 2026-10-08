package handlers_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// The screens the admin console grew for the new design: a running order that
// can be dragged, a search that crosses the areas, exports, and the review
// verdict between approve and reject.
//
// Every one of these is a path whose SQL is only exercised at runtime, which
// is the failure mode these tests are for: a mistyped column in a query that
// no compiler sees and no page renders until somebody opens it.

// ------------------------------------------------------- category reorder --

func TestReorderWritesTheRunningOrderFromTheListPosition(t *testing.T) {
	a := newApp(t)
	a.register("orderer")
	a.promote("orderer", models.RoleModerator)

	for i, slug := range []string{"first", "second", "third"} {
		form := categoryForm(slug, strings.ToUpper(slug))
		form.Set("sort_order", "50")
		if status, _ := a.post("/admin/categories/save", form); status != http.StatusSeeOther {
			t.Fatalf("creating %s (%d) → %d", slug, i, status)
		}
	}
	first := categoryIDBySlug(t, a, "first")
	second := categoryIDBySlug(t, a, "second")
	third := categoryIDBySlug(t, a, "third")

	// Sent third, first, second. The position in the list is the order, so
	// what comes back must be exactly that sequence.
	form := url.Values{}
	for _, id := range []int{third, first, second} {
		form.Add("id", strconv.Itoa(id))
	}
	if status, _ := a.post("/admin/categories/reorder", form); status != http.StatusSeeOther {
		t.Fatalf("reorder → %d, want a redirect", status)
	}

	order := map[int]int{}
	cats, err := a.repo.AdminCategories(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, cat := range cats {
		order[cat.ID] = cat.SortOrder
	}
	if order[third] >= order[first] || order[first] >= order[second] {
		t.Errorf("order came back as third=%d first=%d second=%d, want ascending in that order",
			order[third], order[first], order[second])
	}
}

// An id sent twice would give one row two positions and the last write would
// win silently, leaving a list the admin did not ask for and no sign of it.
func TestReorderRefusesADuplicateID(t *testing.T) {
	a := newApp(t)
	a.register("dupeorder")
	a.promote("dupeorder", models.RoleModerator)

	if status, _ := a.post("/admin/categories/save",
		categoryForm("solo", "Solo")); status != http.StatusSeeOther {
		t.Fatal("could not create the category to reorder")
	}
	id := categoryIDBySlug(t, a, "solo")

	form := url.Values{"id": {strconv.Itoa(id), strconv.Itoa(id)}}
	if status, _ := a.post("/admin/categories/reorder", form); status != http.StatusNotFound {
		t.Errorf("reorder with a repeated id → %d, want 404", status)
	}
}

// ---------------------------------------------------------------- search --

// The search is the one control that crosses the areas, so the thing to prove
// is that it answers from each of them — and that it does not answer from the
// user directory for somebody who cannot open it.
func TestSearchCrossesTheAreasAndRespectsTheUserGate(t *testing.T) {
	admin := newApp(t)
	admin.register("searchadmin")
	admin.promote("searchadmin", models.RoleAdmin)

	// Asserted on the link a hit carries, not on the query appearing in the
	// markup: the "nothing matched" message quotes the query back, so a naive
	// substring check passes either way and the test would prove nothing.
	const userHit = `href="/admin/users?q=searchadmin"`

	status, body := admin.get("/admin/search?q=searchadmin")
	if status != http.StatusOK {
		t.Fatalf("search → %d", status)
	}
	if !strings.Contains(body, userHit) {
		t.Errorf("an admin searching for a username found no account:\n%s", body)
	}

	// A moderator may work the support inbox and the bank, but the user
	// directory is behind RequireAdmin — so a search that answered out of it
	// would be a way to read a screen they cannot open.
	mod := newAppSharing(t, admin)
	mod.register("searchmod")
	mod.promote("searchmod", models.RoleModerator)

	status, body = mod.get("/admin/search?q=searchadmin")
	if status != http.StatusOK {
		t.Fatalf("moderator search → %d", status)
	}
	if strings.Contains(body, userHit) {
		t.Errorf("a moderator's search returned an account from the directory:\n%s", body)
	}
}

// A question is referred to as #412 everywhere else in the admin, so that is
// what an admin pastes into the field.
func TestSearchFindsAQuestionByItsID(t *testing.T) {
	a := newApp(t)
	a.register("qsearcher")
	a.promote("qsearcher", models.RoleAdmin)

	id, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		CategoryID: 1, Difficulty: 1, Points: 10, CorrectIndex: 0,
		Source: "test", IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {
				Prompt:  "Which search result is this?",
				Choices: []string{"a", "b", "c", "d"},
				Source:  "human",
			},
		},
	})
	if err != nil {
		t.Fatalf("seeding a question: %v", err)
	}

	for _, query := range []string{strconv.Itoa(id), "#" + strconv.Itoa(id), "Which search result"} {
		status, body := a.get("/admin/search?q=" + url.QueryEscape(query))
		if status != http.StatusOK {
			t.Fatalf("search %q → %d", query, status)
		}
		if !strings.Contains(body, "Which search result is this?") {
			t.Errorf("searching %q did not find the question:\n%s", query, body)
		}
	}
}

// ---------------------------------------------------------------- export --

func TestExportsCarryTheFilterTheScreenWasShowing(t *testing.T) {
	a := newApp(t)
	a.register("exporter")
	a.promote("exporter", models.RoleAdmin)

	// A second account that the filter must leave out, so a file holding both
	// is distinguishable from a file holding the right one.
	other := newAppSharing(t, a)
	other.register("notinthefile")

	res := a.raw("/admin/users/export.csv?q=exporter")
	if res.status != http.StatusOK {
		t.Fatalf("user export → %d", res.status)
	}
	if got := res.header.Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("content type %q, want text/csv", got)
	}
	if !strings.Contains(res.header.Get("Content-Disposition"), "attachment") {
		t.Errorf("the export does not ask the browser to save it: %q",
			res.header.Get("Content-Disposition"))
	}
	if !strings.Contains(res.body, "exporter") {
		t.Errorf("the filtered account is missing from the file:\n%s", res.body)
	}
	if strings.Contains(res.body, "notinthefile") {
		t.Errorf("the export ignored the filter above it:\n%s", res.body)
	}

	// Excel reads a UTF-8 file with no byte-order mark as the local code page,
	// which turns every Arabic prompt in the column into symbols.
	if !strings.HasPrefix(res.body, "\xef\xbb\xbf") {
		t.Error("the CSV has no byte-order mark, so Arabic will not open correctly")
	}
}

func TestQuestionAndAuditExportsAnswer(t *testing.T) {
	a := newApp(t)
	a.register("csvreader")
	a.promote("csvreader", models.RoleAdmin)

	for _, path := range []string{
		"/admin/questions/export.csv",
		"/admin/audit/export.csv",
	} {
		res := a.raw(path)
		if res.status != http.StatusOK {
			t.Errorf("GET %s → %d, want 200", path, res.status)
			continue
		}
		if !strings.Contains(res.body, ",") {
			t.Errorf("GET %s returned no columns:\n%s", path, res.body)
		}
	}
}

// Taking a copy of the trail out of the application is itself an action worth
// recording — the entries cannot be edited or deleted, and this is how they
// leave.
func TestExportingTheTrailIsItselfRecorded(t *testing.T) {
	a := newApp(t)
	a.register("auditexporter")
	a.promote("auditexporter", models.RoleAdmin)

	if res := a.raw("/admin/audit/export.csv"); res.status != http.StatusOK {
		t.Fatalf("audit export → %d", res.status)
	}

	entries, err := a.repo.AuditPage(t.Context(), repository.AuditFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Action == "audit.export" {
			return
		}
	}
	t.Error("exporting the audit trail left no entry in it")
}

// ------------------------------------------------------- review verdicts --

// The verdict between approve and reject: the draft stays, the queue keeps
// showing it, and the note says what to change.
func TestRequestingChangesKeepsTheDraftPendingWithANote(t *testing.T) {
	a := newApp(t)
	a.register("reviewer")
	a.promote("reviewer", models.RoleModerator)

	id := pendingTranslation(t, a, "What needs changing here?")

	form := url.Values{"locale": {"fr"}, "note": {"The second choice repeats the first."}}
	if status, _ := a.post("/admin/review/"+strconv.Itoa(id)+"/changes", form); status != http.StatusSeeOther {
		t.Fatalf("requesting changes → %d, want a redirect", status)
	}

	draft, err := a.repo.QuestionDraftByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	fr := draft.Translations["fr"]
	if !fr.NeedsReview {
		t.Error("the translation was published; a requested change must stay pending")
	}
	if fr.ReviewNote != "The second choice repeats the first." {
		t.Errorf("note came back as %q", fr.ReviewNote)
	}

	// And it is listed as the author's work rather than only as a pending row.
	noted, err := a.repo.NotedTranslations(t.Context(), "en", 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range noted {
		if n.QuestionID == id && n.Locale == "fr" {
			found = true
		}
	}
	if !found {
		t.Error("the request is not in the changes-requested list")
	}
}

// "Changes requested" with nothing written is indistinguishable from leaving
// the row alone, except that it looks like somebody dealt with it.
func TestRequestingChangesNeedsANote(t *testing.T) {
	a := newApp(t)
	a.register("silentreviewer")
	a.promote("silentreviewer", models.RoleModerator)

	id := pendingTranslation(t, a, "Refuse an empty note")

	form := url.Values{"locale": {"fr"}, "note": {"   "}}
	if status, _ := a.post("/admin/review/"+strconv.Itoa(id)+"/changes", form); status != http.StatusSeeOther {
		t.Fatalf("empty note → %d", status)
	}
	draft, err := a.repo.QuestionDraftByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if note := draft.Translations["fr"].ReviewNote; note != "" {
		t.Errorf("an empty request was recorded as %q", note)
	}
}

// Approving has to clear the note with it: a request attached to published
// text is one nobody can see any more, and migration 0039 refuses the
// combination outright — so a stale note would make approving fail.
func TestApprovingClearsAnOutstandingRequest(t *testing.T) {
	a := newApp(t)
	a.register("approver")
	a.promote("approver", models.RoleModerator)

	id := pendingTranslation(t, a, "Approve over a note")

	if status, _ := a.post("/admin/review/"+strconv.Itoa(id)+"/changes",
		url.Values{"locale": {"fr"}, "note": {"Tighten the wording."}}); status != http.StatusSeeOther {
		t.Fatal("could not request changes")
	}
	if status, _ := a.post("/admin/review/"+strconv.Itoa(id)+"/approve",
		url.Values{"locale": {"fr"}}); status != http.StatusSeeOther {
		t.Fatalf("approving over a note → %d, want a redirect", status)
	}

	draft, err := a.repo.QuestionDraftByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	fr := draft.Translations["fr"]
	if fr.NeedsReview {
		t.Error("the translation is still pending after approval")
	}
	if fr.ReviewNote != "" {
		t.Errorf("the note survived approval as %q", fr.ReviewNote)
	}
}

// Rewriting the text answers the request that was made about it, so the note
// must not outlive the wording it was about.
func TestRewritingTheTextClearsTheNote(t *testing.T) {
	a := newApp(t)
	a.register("rewriter")
	a.promote("rewriter", models.RoleModerator)

	id := pendingTranslation(t, a, "Rewrite over a note")
	if status, _ := a.post("/admin/review/"+strconv.Itoa(id)+"/changes",
		url.Values{"locale": {"fr"}, "note": {"Fix the second choice."}}); status != http.StatusSeeOther {
		t.Fatal("could not request changes")
	}

	if _, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		ID: id, CategoryID: 1, Difficulty: 1, Points: 10, CorrectIndex: 0,
		Source: "test", IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"fr": {
				Prompt:      "Une formulation corrigée ?",
				Choices:     []string{"un", "deux", "trois", "quatre"},
				Source:      "human",
				NeedsReview: true,
			},
		},
	}); err != nil {
		t.Fatalf("rewriting the translation: %v", err)
	}

	draft, err := a.repo.QuestionDraftByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if note := draft.Translations["fr"].ReviewNote; note != "" {
		t.Errorf("the note survived a rewrite of the text it was about: %q", note)
	}
}

// --------------------------------------------------------------- insight --

// The dashboard's reporting queries are five statements that no page renders
// until somebody opens the overview, and an aggregate over an empty window is
// exactly where a query returns null into an int.
//
// Asserted as invariants rather than as values. The suite shares one database,
// so no test can claim the bank is empty — and the useful claim here is not
// "accuracy is zero" but "accuracy is a percentage and nothing divided by a
// window that holds nothing".
func TestDashboardInsightHoldsItsInvariants(t *testing.T) {
	a := newApp(t)
	a.register("dashreader")
	a.promote("dashreader", models.RoleAdmin)

	insight, err := a.repo.DashboardInsight(t.Context(), "en")
	if err != nil {
		t.Fatalf("dashboard insight: %v", err)
	}
	// The series is generated from the calendar and left-joined to play, so a
	// window nobody played in is still a full set of days — a chart that
	// skipped its quiet days would report a quiet Tuesday as if it had not
	// happened.
	if len(insight.Days) != 14 {
		t.Errorf("the chart carries %d days, want 14", len(insight.Days))
	}
	// Every bar divides by the peak.
	if insight.PeakGames() < 1 {
		t.Error("the peak is zero, which every bar would divide by")
	}
	for _, day := range insight.Days {
		if h := day.Height(insight.PeakGames()); h < 0 || h > 100 {
			t.Errorf("a bar is %d%% tall", h)
		}
	}
	for _, pct := range []int{insight.Accuracy(), insight.PriorAccuracy()} {
		if pct < 0 || pct > 100 {
			t.Errorf("accuracy came back as %d%%", pct)
		}
	}
	for _, lang := range insight.Languages {
		if lang.Percent < 0 || lang.Percent > 100 {
			t.Errorf("%s holds %d%% of the players", lang.Locale, lang.Percent)
		}
	}
	for _, cat := range insight.Categories {
		if pct := cat.Accuracy(); pct < 0 || pct > 100 {
			t.Errorf("%s has an accuracy of %d%%", cat.CategoryName, pct)
		}
		if cat.RatingVotes == 0 && cat.Rating != 0 {
			t.Errorf("%s has a rating of %.1f from no votes", cat.CategoryName, cat.Rating)
		}
	}
	if status, _ := a.get("/admin"); status != http.StatusOK {
		t.Errorf("the dashboard → %d", status)
	}
}

// And the division itself, which does not need a database: a count of zero
// must report zero rather than panic, and that is the state every one of these
// figures starts in.
func TestAccuracyOverNothingIsZeroNotAPanic(t *testing.T) {
	empty := &models.DashboardInsight{}
	if got := empty.Accuracy(); got != 0 {
		t.Errorf("accuracy over no answers = %d", got)
	}
	if got := empty.PriorAccuracy(); got != 0 {
		t.Errorf("prior accuracy over no answers = %d", got)
	}
	if got := empty.PeakGames(); got != 1 {
		t.Errorf("the peak of an empty series = %d, want 1 so the bars divide safely", got)
	}
	if got := (models.AdminQuestion{}).Accuracy(); got != 0 {
		t.Errorf("a question nobody has answered has an accuracy of %d", got)
	}
	if got := (models.CategoryStrength{}).Accuracy(); got != 0 {
		t.Errorf("a category nobody has answered has an accuracy of %d", got)
	}
	if got := (models.CategoryPerformance{}).Accuracy(); got != 0 {
		t.Errorf("an unplayed category has an accuracy of %d", got)
	}
}

// -------------------------------------------------------- question filter --

// The domain filter and the bulk selection read the same builder, so "apply
// this to everything matching" cannot resolve to a different set than the
// screen it was launched from.
func TestTheDomainFilterNarrowsTheQuestionBrowser(t *testing.T) {
	a := newApp(t)
	a.register("domainfilter")
	a.promote("domainfilter", models.RoleAdmin)

	cats, err := a.repo.AdminCategories(t.Context(), "en")
	if err != nil || len(cats) == 0 {
		t.Fatalf("no categories to filter by: %v", err)
	}
	inDomain := cats[0]

	id, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		CategoryID: inDomain.ID, Difficulty: 1, Points: 10, CorrectIndex: 0,
		Source: "test", IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "Filed under a domain", Choices: []string{"a", "b", "c", "d"}, Source: "human"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	matched, err := a.repo.AdminQuestionIDs(t.Context(), repository.AdminQuestionFilter{
		DomainID: inDomain.DomainID, Locale: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsInt(matched, id) {
		t.Errorf("the question is not in its own domain's results (%v)", matched)
	}

	// A domain id nothing is filed under must return nothing rather than
	// falling through to everything, which is how a filter silently widens a
	// bulk delete.
	empty, err := a.repo.AdminQuestionIDs(t.Context(), repository.AdminQuestionFilter{
		DomainID: 99999, Locale: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("an unknown domain matched %d questions", len(empty))
	}
}

// ---------------------------------------------------------------- helpers --

// pendingTranslation is a question with a French rendering still awaiting
// review — the state the queue operates on.
func pendingTranslation(t *testing.T, a *app, prompt string) int {
	t.Helper()
	id, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		CategoryID: 1, Difficulty: 1, Points: 10, CorrectIndex: 0,
		Source: "test", IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {
				Prompt:  prompt,
				Choices: []string{"a", "b", "c", "d"},
				Source:  "human",
			},
			"fr": {
				Prompt:      prompt + " (fr)",
				Choices:     []string{"un", "un", "trois", "quatre"},
				Source:      "machine",
				NeedsReview: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("seeding a pending translation: %v", err)
	}
	return id
}

func containsInt(haystack []int, needle int) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

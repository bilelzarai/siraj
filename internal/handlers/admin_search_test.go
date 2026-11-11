package handlers_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// The search field's fragment is what the drop-down renders. It is fetched, so
// nothing about it is exercised by loading a page — and a fragment that
// arrives wrapped in a whole document, or with no links in it, is a drop-down
// that cannot be used.
func TestSearchFragmentIsAFragmentWithUsableLinks(t *testing.T) {
	a := newApp(t)
	a.register("fragreader")
	a.promote("fragreader", models.RoleAdmin)

	status, body := a.get("/admin/search?q=fragreader")
	if status != 200 {
		t.Fatalf("search → %d", status)
	}
	for _, shell := range []string{"<!doctype", "<html", "<aside class=\"sidebar\"", "<header"} {
		if strings.Contains(strings.ToLower(body), shell) {
			t.Errorf("the fragment carries page chrome (%q); it is rendered into a panel", shell)
		}
	}
	if !strings.Contains(body, "search-hit") {
		t.Errorf("no result rows in the fragment:\n%s", body)
	}
	// Every hit has to be openable — a result that is not a link is a result
	// the reader cannot act on.
	hits := strings.Count(body, `class="search-hit"`)
	links := len(regexp.MustCompile(`<a class="search-hit"`).FindAllString(body, -1))
	if hits != links {
		t.Errorf("%d hits but %d of them are links", hits, links)
	}

	// Below the minimum length the script does not call at all, but the
	// endpoint still has to answer sensibly if something does.
	if status, body := a.get("/admin/search?q="); status != 200 || !strings.Contains(body, "search-hint") {
		t.Errorf("an empty query → %d, %q", status, body)
	}
}

// The domain filter and the category filter are two halves of one choice, and
// the category options carry the domain they belong to so the narrowing can
// happen without another round trip.
func TestQuestionFilterCarriesTheDomainOnEveryCategoryOption(t *testing.T) {
	a := newApp(t)
	a.register("filterreader")
	a.promote("filterreader", models.RoleAdmin)

	status, body := a.get("/admin/questions")
	if status != 200 {
		t.Fatalf("questions → %d", status)
	}

	// Inside the category select, every real option names its domain.
	sel := regexp.MustCompile(`(?s)<select[^>]*id="q-cat".*?</select>`).FindString(body)
	if sel == "" {
		t.Fatal("no category select on the question browser")
	}
	if !strings.Contains(sel, `data-narrow-by="q-domain"`) {
		t.Error("the category select is not wired to the domain select")
	}
	if !strings.Contains(sel, "<optgroup") {
		t.Error("the category select is not grouped by domain")
	}
	for _, opt := range regexp.MustCompile(`<option[^>]*>`).FindAllString(sel, -1) {
		if strings.Contains(opt, `value="0"`) {
			continue // the "all categories" option belongs to no domain
		}
		if !strings.Contains(opt, "data-domain=") {
			t.Errorf("category option without a domain: %s", opt)
		}
	}

	// And the filter actually filters: a question in one domain must not come
	// back under another.
	cats, err := a.repo.AdminCategories(t.Context(), "en")
	if err != nil || len(cats) == 0 {
		t.Fatalf("no categories: %v", err)
	}
	_, err = a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		CategoryID: cats[0].ID, Difficulty: 1, Points: 10, CorrectIndex: 0,
		Source: "test", IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "Domain filter probe", Choices: []string{"a", "b", "c", "d"}, Source: "human"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	in := "/admin/questions?domain=" + url.QueryEscape(itoa(int64(cats[0].DomainID)))
	if _, body := a.get(in); !strings.Contains(body, "Domain filter probe") {
		t.Error("the question is missing from its own domain")
	}
	// Another domain, if the taxonomy has one.
	for _, cat := range cats {
		if cat.DomainID != cats[0].DomainID {
			other := "/admin/questions?domain=" + itoa(int64(cat.DomainID))
			if _, body := a.get(other); strings.Contains(body, "Domain filter probe") {
				t.Error("the question appears under a domain it is not filed in")
			}
			break
		}
	}
}

// Every filter the toolbar shows has to survive paging and be the same filter
// a bulk action resolves — that is the whole reason they read one builder.
func TestQuestionFilterSurvivesIntoTheBulkFormAndTheExport(t *testing.T) {
	a := newApp(t)
	a.register("bulkreader")
	a.promote("bulkreader", models.RoleAdmin)

	status, body := a.get("/admin/questions?difficulty=2&state=live&coverage=partial&q=probe")
	if status != 200 {
		t.Fatalf("filtered questions → %d", status)
	}

	// The hidden fields the bulk form carries, so "everything matching" is
	// rebuilt from the same names the listing read.
	form := regexp.MustCompile(`(?s)<form id="questions-bulk".*?</form>`).FindString(body)
	if form == "" {
		// With no rows there is no bulk form, which is correct — but then the
		// export link still has to carry the filter.
		if !strings.Contains(body, "difficulty=2") {
			t.Error("the export link lost the filter")
		}
		return
	}
	for _, want := range []string{`name="difficulty" value="2"`, `name="state" value="live"`,
		`name="coverage" value="partial"`, `name="q" value="probe"`} {
		if !strings.Contains(form, want) {
			t.Errorf("the bulk form is missing %s", want)
		}
	}
}

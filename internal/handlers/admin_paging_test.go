package handlers_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// A pager that drops the filter sends you from page two of a search to page
// two of everything — the rows change under you and nothing says why.
func TestPagingKeepsTheFilterAndTheSize(t *testing.T) {
	a := newApp(t)
	a.register("pager")
	a.promote("pager", models.RoleAdmin)

	// Enough rows that there is a second page at the smallest size.
	for i := 0; i < 7; i++ {
		if _, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
			CategoryID: 1, Difficulty: 2, Points: 20, CorrectIndex: 0,
			Source: "test", IsActive: true,
			Translations: map[string]models.TranslationDraft{
				"en": {
					Prompt:  "Paging probe " + strconv.Itoa(i),
					Choices: []string{"a", "b", "c", "d"},
					Source:  "human",
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	status, body := a.get("/admin/questions?q=Paging+probe&difficulty=2&size=5")
	if status != 200 {
		t.Fatalf("filtered page → %d", status)
	}

	links := regexp.MustCompile(`href="(/admin/questions\?[^"]+)"`).FindAllStringSubmatch(body, -1)
	var paging []string
	for _, m := range links {
		if strings.Contains(m[1], "page=") || strings.Contains(m[1], "size=") {
			paging = append(paging, strings.ReplaceAll(m[1], "&amp;", "&"))
		}
	}
	if len(paging) == 0 {
		t.Fatalf("the pager offered no links at all:\n%s", body)
	}
	for _, href := range paging {
		for _, want := range []string{"q=Paging+probe", "difficulty=2"} {
			if !strings.Contains(href, want) {
				t.Errorf("a pager link dropped %s: %s", want, href)
			}
		}
		// And following it must land on the same filter rather than a 404.
		if status, body := a.get(href); status != 200 {
			t.Errorf("%s → %d", href, status)
		} else if !strings.Contains(body, "Paging probe") {
			t.Errorf("%s came back with none of the filtered rows", href)
		}
	}
}

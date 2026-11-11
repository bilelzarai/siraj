package handlers_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// categoryForm is a complete, valid submission, so each test can name the one
// field it is actually about.
//
// domain_id is part of "complete" since the taxonomy gained a level: every
// category belongs to exactly one subject area, and the column has no default
// to fall back on.
func categoryForm(slug, name string) url.Values {
	return url.Values{
		"slug":       {slug},
		"domain_id":  {"1"},
		"icon":       {"📗"},
		"color":      {"#0ea5a4"},
		"sort_order": {"50"},
		"is_active":  {"1"},
		"name_ar":    {name},
	}
}

// categoryIDBySlug finds what the form just created, since the id is assigned
// by the database rather than chosen by the test.
func categoryIDBySlug(t *testing.T, a *app, slug string) int {
	t.Helper()
	cats, err := a.repo.AdminCategories(t.Context(), "en")
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	for _, cat := range cats {
		if cat.Slug == slug {
			return cat.ID
		}
	}
	t.Fatalf("no category with slug %q", slug)
	return 0
}

// The round trip the screen exists for: a moderator adds a category, names it
// in more than one language, and the player's setup screen offers it.
func TestAdminCreatesACategoryAndThePlayerSeesIt(t *testing.T) {
	a := newApp(t)
	a.register("catmaker")
	a.promote("catmaker", models.RoleModerator)

	form := categoryForm("tajweed", "التجويد")
	form.Set("name_en", "Tajweed")
	form.Set("description_en", "Reciting the Qur'an correctly")
	if status, _ := a.post("/admin/categories/save", form); status != http.StatusSeeOther {
		t.Fatalf("saving a new category → %d, want a redirect", status)
	}

	id := categoryIDBySlug(t, a, "tajweed")

	// The name resolves per language, like every other piece of content.
	cats, err := a.repo.Categories(t.Context(), "en", 0)
	if err != nil {
		t.Fatal(err)
	}
	var found *models.Category
	for _, cat := range cats {
		if cat.ID == id {
			found = cat
		}
	}
	if found == nil {
		t.Fatal("the new category is not in the player's list")
	}
	if found.Name != "Tajweed" {
		t.Errorf("English name is %q, want %q", found.Name, "Tajweed")
	}

	// And it is on the screen the player chooses a round from.
	status, body := a.get("/play?lang=en")
	if status != http.StatusOK {
		t.Fatalf("GET /play → %d", status)
	}
	if !strings.Contains(body, "Tajweed") {
		t.Error("the setup screen does not offer the new category")
	}
}

// Editing writes over the row rather than adding a second one, and the names
// that were not submitted are left as they were.
func TestEditingACategoryKeepsTheLanguagesItWasNotAsked(t *testing.T) {
	a := newApp(t)
	a.register("cateditor")
	a.promote("cateditor", models.RoleModerator)

	form := categoryForm("adab", "الأدب")
	form.Set("name_fr", "Les belles manières")
	a.post("/admin/categories/save", form)
	id := categoryIDBySlug(t, a, "adab")

	// A second submission naming only Arabic and English.
	edit := categoryForm("adab", "الأدب والسلوك")
	edit.Set("id", fmt.Sprint(id))
	edit.Set("name_en", "Manners")
	if status, _ := a.post("/admin/categories/save", edit); status != http.StatusSeeOther {
		t.Fatalf("editing → %d, want a redirect", status)
	}

	draft, err := a.repo.CategoryDraft(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Names["ar"].Name != "الأدب والسلوك" {
		t.Errorf("Arabic name is %q, want the edited one", draft.Names["ar"].Name)
	}
	if draft.Names["fr"].Name != "Les belles manières" {
		t.Errorf("French name is %q — a language left out of the form was wiped",
			draft.Names["fr"].Name)
	}

	// One row, not two.
	if n := len(categoriesWithSlug(t, a, "adab")); n != 1 {
		t.Errorf("%d categories carry the slug adab, want 1", n)
	}
}

func categoriesWithSlug(t *testing.T, a *app, slug string) []*models.AdminCategory {
	t.Helper()
	cats, err := a.repo.AdminCategories(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	var out []*models.AdminCategory
	for _, cat := range cats {
		if cat.Slug == slug {
			out = append(out, cat)
		}
	}
	return out
}

// The form refuses what the player's screen cannot render, and says which
// field is wrong rather than failing the whole page.
func TestACategoryFormRefusesWhatItCannotStore(t *testing.T) {
	a := newApp(t)
	a.register("catvalid")
	a.promote("catvalid", models.RoleModerator)

	cases := []struct {
		name   string
		mutate func(url.Values)
	}{
		{"no slug", func(f url.Values) { f.Set("slug", "") }},
		{"a slug with spaces", func(f url.Values) { f.Set("slug", "two words") }},
		{"a slug in capitals with punctuation", func(f url.Values) { f.Set("slug", "Fiqh!") }},
		{"a colour that is not one", func(f url.Values) { f.Set("color", "teal") }},
		{"a position out of range", func(f url.Values) { f.Set("sort_order", "4000") }},
		{"no Arabic name", func(f url.Values) { f.Set("name_ar", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := categoryForm("valid-slug", "اسم")
			tc.mutate(form)
			status, _ := a.post("/admin/categories/save", form)
			if status != http.StatusUnprocessableEntity {
				t.Errorf("saving with %s → %d, want 422", tc.name, status)
			}
		})
	}

	// And none of them created anything.
	if n := len(categoriesWithSlug(t, a, "valid-slug")); n != 0 {
		t.Errorf("%d categories were created by refused submissions", n)
	}
}

// Two categories cannot share a slug: it is what links and imports address
// them by. The clash is reported on the field, not as a 500.
func TestACategorySlugIsNotShared(t *testing.T) {
	a := newApp(t)
	a.register("catslug")
	a.promote("catslug", models.RoleModerator)

	a.post("/admin/categories/save", categoryForm("sirah-kids", "سيرة للأطفال"))
	status, _ := a.post("/admin/categories/save", categoryForm("sirah-kids", "آخر"))
	if status != http.StatusUnprocessableEntity {
		t.Errorf("reusing a slug → %d, want 422", status)
	}
	if n := len(categoriesWithSlug(t, a, "sirah-kids")); n != 1 {
		t.Errorf("%d categories carry the slug, want 1", n)
	}
}

// Retiring is the reversible half of deleting: the questions stay where they
// are, and stop being dealt into rounds.
func TestRetiringACategoryTakesItsQuestionsOutOfTheDraw(t *testing.T) {
	a := newApp(t)
	a.register("catretire")
	a.promote("catretire", models.RoleAdmin)

	a.post("/admin/categories/save", categoryForm("retire-me", "للإيقاف"))
	id := categoryIDBySlug(t, a, "retire-me")

	for i := 0; i < 8; i++ {
		_, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
			CategoryID: id, Difficulty: 1, Points: 10, CorrectIndex: 0, IsActive: true,
			Translations: map[string]models.TranslationDraft{
				"en": {Prompt: fmt.Sprintf("Retire question %d?", i),
					Choices: []string{"a", "b", "c", "d"}, Source: "human"},
			},
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	before, err := a.repo.AvailableCounts(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	if before.Count(id, 0) != 8 {
		t.Fatalf("the bank says %d questions in the new category, want 8", before.Count(id, 0))
	}

	if status, _ := a.post(fmt.Sprintf("/admin/categories/%d/retire", id), url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("retiring → %d", status)
	}

	after, err := a.repo.AvailableCounts(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	if after.Count(id, 0) != 0 {
		t.Errorf("a retired category still offers %d questions", after.Count(id, 0))
	}
	// Not just hidden from its own row — out of the grand total too, or the
	// "all categories" round would keep dealing them.
	if after.Count(0, 0) != before.Count(0, 0)-8 {
		t.Errorf("the all-categories total went from %d to %d; the retired "+
			"category's questions are still being drawn",
			before.Count(0, 0), after.Count(0, 0))
	}
	// The questions themselves are untouched.
	draft, err := a.repo.CategoryDraft(t.Context(), id)
	if err != nil || draft.IsActive {
		t.Errorf("the category is not marked retired: %v", err)
	}

	// And it comes back.
	a.post(fmt.Sprintf("/admin/categories/%d/restore", id), url.Values{})
	back, err := a.repo.AvailableCounts(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	if back.Count(id, 0) != 8 {
		t.Errorf("restoring left %d questions drawable, want 8", back.Count(id, 0))
	}
}

// Deleting a category cascades into its questions and into every answer anybody
// gave them. While it holds any, the delete is refused and the admin is sent
// back with the reason.
func TestACategoryHoldingQuestionsIsNotDeleted(t *testing.T) {
	a := newApp(t)
	a.register("catdelete")
	a.promote("catdelete", models.RoleAdmin)

	a.post("/admin/categories/save", categoryForm("keep-me", "لا يُحذف"))
	id := categoryIDBySlug(t, a, "keep-me")

	qid, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		CategoryID: id, Difficulty: 1, Points: 10, CorrectIndex: 0, IsActive: true,
		Translations: map[string]models.TranslationDraft{
			"en": {Prompt: "Does deleting take me with it?",
				Choices: []string{"a", "b", "c", "d"}, Source: "human"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if status, _ := a.post(fmt.Sprintf("/admin/categories/%d/delete", id), url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("a refused delete → %d, want a redirect carrying the reason", status)
	}
	if n := len(categoriesWithSlug(t, a, "keep-me")); n != 1 {
		t.Fatal("the category was deleted while it still held a question")
	}
	if _, err := a.repo.QuestionDraftByID(t.Context(), qid); err != nil {
		t.Errorf("the question went with it: %v", err)
	}

	// Emptied, it goes.
	if _, err := a.repo.DeleteQuestions(t.Context(), []int{qid}); err != nil {
		t.Fatalf("clearing the category: %v", err)
	}
	if status, _ := a.post(fmt.Sprintf("/admin/categories/%d/delete", id), url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("deleting an empty category → %d", status)
	}
	if n := len(categoriesWithSlug(t, a, "keep-me")); n != 0 {
		t.Error("an empty category was not deleted")
	}
}

// A moderator does content work and does not destroy it — the same line the
// bulk question actions are drawn on.
func TestAModeratorMayEditCategoriesButNotDeleteThem(t *testing.T) {
	a := newApp(t)
	a.register("catmod")
	a.promote("catmod", models.RoleAdmin)
	a.post("/admin/categories/save", categoryForm("mod-test", "اختبار"))
	id := categoryIDBySlug(t, a, "mod-test")

	mod := newAppSharing(t, a)
	mod.register("catmod2")
	mod.promote("catmod2", models.RoleModerator)

	if status, _ := mod.get("/admin/categories"); status != http.StatusOK {
		t.Errorf("a moderator cannot reach the categories screen: %d", status)
	}
	edit := categoryForm("mod-test", "اختبار معدّل")
	edit.Set("id", fmt.Sprint(id))
	if status, _ := mod.post("/admin/categories/save", edit); status != http.StatusSeeOther {
		t.Errorf("a moderator cannot edit a category: %d", status)
	}
	if status, _ := mod.post(fmt.Sprintf("/admin/categories/%d/delete", id), url.Values{}); status != http.StatusNotFound {
		t.Errorf("delete as a moderator → %d, want 404", status)
	}
	if n := len(categoriesWithSlug(t, a, "mod-test")); n != 1 {
		t.Error("a moderator deleted a category")
	}
}

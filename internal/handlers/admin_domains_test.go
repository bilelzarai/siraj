package handlers_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// The screens that make the taxonomy's second level real. Until they existed
// the level was in the schema and nowhere else: an admin could not create a
// subject area without writing SQL, which is the deploy this work removes.

// domainForm is a complete, valid submission, so each test can name the one
// field it is actually about.
func domainForm(slug, name string) url.Values {
	return url.Values{
		"slug":       {slug},
		"icon":       {"🗂️"},
		"color":      {"#1d4ed8"},
		"sort_order": {"50"},
		"is_active":  {"1"},
		"name_ar":    {name},
	}
}

func domainIDBySlug(t *testing.T, a *app, slug string) int {
	t.Helper()
	domains, err := a.repo.AdminDomains(t.Context(), "en")
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	for _, d := range domains {
		if d.Slug == slug {
			return d.ID
		}
	}
	t.Fatalf("no domain with slug %q", slug)
	return 0
}

// The round trip the whole stage exists for: an admin adds a subject area, a
// category goes under it, and nothing in Go was changed to allow either.
func TestAnAdminCreatesASubjectAreaAndFilesACategoryUnderIt(t *testing.T) {
	a := newApp(t)
	a.register("domainmaker")
	a.promote("domainmaker", models.RoleAdmin)

	form := domainForm("sport", "الرياضة")
	form.Set("name_en", "Sport")
	form.Set("description_en", "Football, handball, judo")
	if status, _ := a.post("/admin/domains/save", form); status != http.StatusSeeOther {
		t.Fatalf("saving a new domain → %d, want a redirect", status)
	}
	id := domainIDBySlug(t, a, "sport")

	// It reads back in the language it was named in, and falls back to Arabic
	// in one it was not.
	domains, err := a.repo.Domains(t.Context(), "fr")
	if err != nil {
		t.Fatalf("reading domains: %v", err)
	}
	var seen bool
	for _, d := range domains {
		if d.ID == id {
			seen = true
			if d.Name != "الرياضة" {
				t.Errorf("French resolved to %q, want the Arabic fallback", d.Name)
			}
		}
	}
	if !seen {
		t.Fatal("the domain just created is not offered")
	}

	// And a category can be filed under it, through the same form a moderator
	// uses — the field is a select over what exists, not a constant.
	cat := categoryForm("football", "كرة القدم")
	cat.Set("domain_id", fmt.Sprint(id))
	cat.Set("name_en", "Football")
	if status, _ := a.post("/admin/categories/save", cat); status != http.StatusSeeOther {
		t.Fatalf("filing a category under the new domain → %d", status)
	}
	for _, c := range listCategories(t, a) {
		if c.Slug == "football" && c.DomainID != id {
			t.Errorf("the category was filed under domain %d, want %d", c.DomainID, id)
		}
	}
}

// The role line, one level up from categories: reshaping the taxonomy is
// structural, writing inside it is content work (D11). A moderator gets 404
// rather than 403, so the area does not advertise its own existence.
func TestAModeratorMayEditCategoriesButIsRefusedDomains(t *testing.T) {
	a := newApp(t)
	a.register("domadmin")
	a.promote("domadmin", models.RoleAdmin)
	a.post("/admin/domains/save", domainForm("history-mod", "التاريخ"))
	id := domainIDBySlug(t, a, "history-mod")

	mod := newAppSharing(t, a)
	mod.register("dommod")
	mod.promote("dommod", models.RoleModerator)

	if status, _ := mod.get("/admin/categories"); status != http.StatusOK {
		t.Errorf("a moderator cannot reach the categories screen: %d", status)
	}
	for _, path := range []string{"/admin/domains", "/admin/domains/new",
		fmt.Sprintf("/admin/domains/%d/edit", id)} {
		if status, _ := mod.get(path); status != http.StatusNotFound {
			t.Errorf("GET %s as a moderator → %d, want 404", path, status)
		}
	}
	edit := domainForm("history-mod", "تاريخ معدّل")
	edit.Set("id", fmt.Sprint(id))
	if status, _ := mod.post("/admin/domains/save", edit); status != http.StatusNotFound {
		t.Errorf("a moderator saved a domain → %d, want 404", status)
	}
	for _, action := range []string{"retire", "restore", "delete"} {
		path := fmt.Sprintf("/admin/domains/%d/%s", id, action)
		if status, _ := mod.post(path, url.Values{}); status != http.StatusNotFound {
			t.Errorf("POST %s as a moderator → %d, want 404", path, status)
		}
	}

	// And none of it happened.
	domains, err := a.repo.AdminDomains(t.Context(), "ar")
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	for _, d := range domains {
		if d.ID == id && (d.Name != "التاريخ" || !d.IsActive) {
			t.Errorf("a moderator changed a domain: name=%q active=%v", d.Name, d.IsActive)
		}
	}
}

// Every privileged action leaves a trail. Five of them, because retiring and
// restoring are as much a decision as creating.
func TestEveryDomainActionIsRecorded(t *testing.T) {
	a := newApp(t)
	a.register("domauditor")
	a.promote("domauditor", models.RoleAdmin)

	a.post("/admin/domains/save", domainForm("audited", "مدقّق"))
	id := domainIDBySlug(t, a, "audited")

	edit := domainForm("audited", "مدقّق معدّل")
	edit.Set("id", fmt.Sprint(id))
	a.post("/admin/domains/save", edit)
	a.post(fmt.Sprintf("/admin/domains/%d/retire", id), url.Values{})
	a.post(fmt.Sprintf("/admin/domains/%d/restore", id), url.Values{})
	a.post(fmt.Sprintf("/admin/domains/%d/delete", id), url.Values{})

	entries, err := a.repo.AuditTrail(t.Context(), 200)
	if err != nil {
		t.Fatalf("reading the audit trail: %v", err)
	}
	recorded := map[string]bool{}
	for _, e := range entries {
		if e.TargetKind == "domain" {
			recorded[e.Action] = true
		}
	}
	for _, want := range []string{"domain.create", "domain.update", "domain.retire",
		"domain.restore", "domain.delete"} {
		if !recorded[want] {
			t.Errorf("%s left no audit entry", want)
		}
	}
}

// Deleting is refused while anything hangs below, at both levels. The schema
// refuses it too — this is the screen saying so first, in a sentence.
func TestADomainHoldingCategoriesIsNotDeleted(t *testing.T) {
	a := newApp(t)
	a.register("domkeeper")
	a.promote("domkeeper", models.RoleAdmin)

	a.post("/admin/domains/save", domainForm("geography", "الجغرافيا"))
	id := domainIDBySlug(t, a, "geography")

	cat := categoryForm("capitals", "العواصم")
	cat.Set("domain_id", fmt.Sprint(id))
	if status, _ := a.post("/admin/categories/save", cat); status != http.StatusSeeOther {
		t.Fatalf("filing a category → %d", status)
	}

	if status, _ := a.post(fmt.Sprintf("/admin/domains/%d/delete", id), url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("deleting a full domain → %d, want a redirect carrying the refusal", status)
	}
	if domainIDBySlug(t, a, "geography") != id {
		t.Fatal("a domain holding a category was deleted")
	}

	// Retiring is the answer the screen offers instead, and it reaches the
	// categories below: they stop being offered without being destroyed.
	if status, _ := a.post(fmt.Sprintf("/admin/domains/%d/retire", id), url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("retiring → %d", status)
	}
	for _, c := range listCategories(t, a) {
		if c.Slug == "capitals" && c.DomainActive {
			t.Error("retiring a domain left its category reachable")
		}
	}
	offered, err := a.repo.Categories(t.Context(), "en", 0)
	if err != nil {
		t.Fatalf("player's category list: %v", err)
	}
	for _, c := range offered {
		if c.Slug == "capitals" {
			t.Error("a category under a retired domain is still offered to players")
		}
	}
}

// The field the category form gained. A submission that names no domain is
// refused on the field rather than reaching the foreign key as a 500.
func TestACategoryMustNameADomain(t *testing.T) {
	a := newApp(t)
	a.register("catdomain")
	a.promote("catdomain", models.RoleModerator)

	for _, tc := range []struct {
		name  string
		value string
	}{
		{"no domain at all", ""},
		{"a domain that does not exist", "999999"},
		{"a domain that is not a number", "islamic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := categoryForm("unfiled-"+strings.ReplaceAll(tc.name, " ", "-"), "بلا مجال")
			form.Set("domain_id", tc.value)
			// postBody rather than post: a refusal re-renders the form, and
			// what matters is that the message is attached to the field the
			// person has to fix, not merely that the status was 422.
			status, body := a.postBody("/admin/categories/save", form)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("saving → %d, want 422", status)
			}
			if !strings.Contains(body, `id="domain_id-error"`) {
				t.Error("the refusal is not attached to the domain field")
			}
			if !strings.Contains(body, `aria-describedby="domain_id-error"`) {
				t.Error("the field does not point at its own error, so a screen reader never hears it")
			}
		})
	}
}

// listCategories is the admin list, which is where a category's domain shows.
func listCategories(t *testing.T, a *app) []*models.AdminCategory {
	t.Helper()
	cats, err := a.repo.AdminCategories(t.Context(), "en")
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	return cats
}

// Retiring a domain must not make the categories under it uneditable, and
// editing one of them must not quietly move it somewhere else.
//
// The form offers active domains. A category already filed under a retired one
// is the exception: without it the select would hold no option for the
// category's own domain, nothing would be selected, the browser would
// preselect the first entry, and saving a name correction would relocate the
// category to another subject area without anybody asking for it.
func TestEditingACategoryInARetiredDomainDoesNotMoveIt(t *testing.T) {
	a := newApp(t)
	a.register("retiredit")
	a.promote("retiredit", models.RoleAdmin)

	a.post("/admin/domains/save", domainForm("archive", "الأرشيف"))
	domainID := domainIDBySlug(t, a, "archive")

	cat := categoryForm("old-ways", "القديم")
	cat.Set("domain_id", fmt.Sprint(domainID))
	if status, _ := a.post("/admin/categories/save", cat); status != http.StatusSeeOther {
		t.Fatalf("filing the category → %d", status)
	}
	catID := categoryIDBySlug(t, a, "old-ways")

	if status, _ := a.post(fmt.Sprintf("/admin/domains/%d/retire", domainID), url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("retiring the domain → %d", status)
	}

	// The edit form still offers the domain it is in, marked as retired.
	status, body := a.get(fmt.Sprintf("/admin/categories/%d/edit", catID))
	if status != http.StatusOK {
		t.Fatalf("editing a category in a retired domain → %d", status)
	}
	if !strings.Contains(body, fmt.Sprintf(`value="%d" selected`, domainID)) {
		t.Error("the form does not preselect the category's own domain, so saving would move it")
	}

	// And a save that changes only the name leaves it where it was.
	edit := categoryForm("old-ways", "القديم المعدّل")
	edit.Set("id", fmt.Sprint(catID))
	edit.Set("domain_id", fmt.Sprint(domainID))
	if status, _ := a.post("/admin/categories/save", edit); status != http.StatusSeeOther {
		t.Fatalf("saving → %d, want a redirect", status)
	}
	for _, c := range listCategories(t, a) {
		if c.ID == catID && c.DomainID != domainID {
			t.Errorf("the category moved to domain %d; it was in %d", c.DomainID, domainID)
		}
	}
}

// Every way the domain form can refuse a submission. The category screen has
// the same table; without it a validation branch can be deleted and nothing
// goes red.
func TestTheDomainFormRefusesWhatItShould(t *testing.T) {
	a := newApp(t)
	a.register("domrefuse")
	a.promote("domrefuse", models.RoleAdmin)

	for _, tc := range []struct {
		name   string
		break_ func(url.Values)
	}{
		{"no slug", func(f url.Values) { f.Set("slug", "") }},
		{"a slug with spaces", func(f url.Values) { f.Set("slug", "two words") }},
		{"a slug in capitals with punctuation", func(f url.Values) { f.Set("slug", "Sport!") }},
		{"a colour that is not one", func(f url.Values) { f.Set("color", "blue") }},
		{"a position out of range", func(f url.Values) { f.Set("sort_order", "1000") }},
		{"no Arabic name", func(f url.Values) { f.Del("name_ar") }},
		{"a description with no name", func(f url.Values) {
			f.Del("name_ar")
			f.Set("description_en", "A subject area with nothing to call it")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := domainForm("refused", "مرفوض")
			tc.break_(form)
			status, _ := a.post("/admin/domains/save", form)
			if status != http.StatusUnprocessableEntity {
				t.Errorf("saving → %d, want 422", status)
			}
		})
	}

	// And none of them wrote a row.
	domains, err := a.repo.AdminDomains(t.Context(), "en")
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	for _, d := range domains {
		if d.Slug == "refused" {
			t.Error("a refused submission was stored anyway")
		}
	}
}

// A moderator must not be offered what they cannot reach. The admin area
// answers 404 rather than 403 so it does not advertise what it withholds, and
// a navigation entry that 404s on click hands that straight back.
func TestTheNavOffersAModeratorOnlyWhatTheyCanReach(t *testing.T) {
	a := newApp(t)
	a.register("navadmin")
	a.promote("navadmin", models.RoleAdmin)
	_, adminNav := a.get("/admin/questions")

	mod := newAppSharing(t, a)
	mod.register("navmod")
	mod.promote("navmod", models.RoleModerator)
	_, modNav := mod.get("/admin/questions")

	for _, path := range []string{"/admin/domains", "/admin/users", "/admin/audit"} {
		if !strings.Contains(adminNav, `href="`+path+`"`) {
			t.Errorf("an admin is not offered %s", path)
		}
		if strings.Contains(modNav, `href="`+path+`"`) {
			t.Errorf("a moderator is offered %s, which answers 404 for them", path)
		}
	}
	// And they still see the content half.
	for _, path := range []string{"/admin/categories", "/admin/questions", "/admin/review"} {
		if !strings.Contains(modNav, `href="`+path+`"`) {
			t.Errorf("a moderator is not offered %s", path)
		}
	}
}

// Retiring the last subject area must not leave the category form as a trap:
// every submission refused on a field with nothing to choose, and no way for a
// moderator to fix it, since creating a domain is not theirs to do.
func TestWithNoSubjectAreaTheCategoryFormSaysSoInsteadOfRefusingForever(t *testing.T) {
	a := newApp(t)
	a.register("lastdomain")
	a.promote("lastdomain", models.RoleAdmin)

	domains, err := a.repo.AdminDomains(t.Context(), "en")
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	for _, d := range domains {
		if d.IsActive {
			if status, _ := a.post(fmt.Sprintf("/admin/domains/%d/retire", d.ID), url.Values{}); status != http.StatusSeeOther {
				t.Fatalf("retiring %s → %d", d.Slug, status)
			}
			defer a.post(fmt.Sprintf("/admin/domains/%d/restore", d.ID), url.Values{})
		}
	}

	status, body := a.get("/admin/categories/new")
	if status != http.StatusOK {
		t.Fatalf("the category form → %d", status)
	}
	if !strings.Contains(body, "subject area") && !strings.Contains(body, "مجال") {
		t.Error("the form offers no explanation of why nothing can be chosen")
	}
}

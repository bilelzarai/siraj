package tests

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// staff signs in a member and makes them an admin, returning their token.
func (a *app) staff(t *testing.T, username string) string {
	t.Helper()
	token := a.member(username)
	if _, err := a.DB.Pool.Exec(t.Context(),
		"UPDATE users SET role = 'admin' WHERE username = $1", username); err != nil {
		t.Fatal(err)
	}
	return token
}

// htmx sends a request the way the console's drawers do.
func (a *app) htmx(method, path string, token string) reply {
	a.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return reply{t: a.t, Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

// Every list screen of the console edits in a drawer, and the drawer the server
// sends carries the fields the design draws (A04b, A05b, A06b).
func TestAdminDrawersCarryTheDesignsFields(t *testing.T) {
	a := newApp(t)
	token := a.staff(t, "drawerstaff")

	question := string(a.htmx(http.MethodGet, "/admin/questions/new", token).expect(http.StatusOK).Body)
	for _, want := range []string{
		`<aside class="drawer"`, `role="tab"`, `data-segmented`, `name="correct_en"`,
		`name="answer_en_0"`, `data-count=`, `name="points"`, `drawer-foot`,
	} {
		if !strings.Contains(question, want) {
			t.Errorf("the question drawer is missing %s", want)
		}
	}
	// Three language tabs, four answers in each.
	if n := strings.Count(question, `role="tab"`); n != 3 {
		t.Errorf("want a tab per language, got %d", n)
	}
	if n := strings.Count(question, `name="answer_`); n != 12 {
		t.Errorf("want four answers per language, got %d", n)
	}

	domain := string(a.htmx(http.MethodGet, "/admin/domains/new", token).expect(http.StatusOK).Body)
	for _, want := range []string{`dom-preview`, `data-icon=`, `data-color=`, `data-sets="family"`, `name="slug"`} {
		if !strings.Contains(domain, want) {
			t.Errorf("the domain drawer is missing %s", want)
		}
	}
	category := string(a.htmx(http.MethodGet, "/admin/categories/new", token).expect(http.StatusOK).Body)
	if !strings.Contains(category, `data-preview`) || !strings.Contains(category, `cat-tile`) {
		t.Error("the category drawer lost its preview")
	}

	// Without JavaScript the same URL is a page, with the same fields in it.
	page := string(a.get("/admin/questions/new", token).expect(http.StatusOK).Body)
	if !strings.Contains(page, `name="answer_en_0"`) || strings.Contains(page, `<aside class="drawer"`) {
		t.Error("the no-JavaScript fallback should be a page with the same fields")
	}
}

// A02b: staff open an account, change its role and its settings, and choose the
// domains the player's app shows. The write is one transaction with its audit row.
func TestAdminConfiguresAUser(t *testing.T) {
	a := newApp(t)
	staff := a.staff(t, "configstaff")
	a.member("configplayer")
	var playerID uuid.UUID
	if err := a.DB.Pool.QueryRow(t.Context(),
		"SELECT id FROM users WHERE username = 'configplayer'").Scan(&playerID); err != nil {
		t.Fatal(err)
	}

	drawer := string(a.htmx(http.MethodGet, "/admin/users/"+playerID.String(), staff).expect(http.StatusOK).Body)
	for _, want := range []string{`name="role"`, `name="lang"`, `name="daily_goal"`, `name="islamic_only"`, `name="domain_id"`, `dom-chips`} {
		if !strings.Contains(drawer, want) {
			t.Errorf("the user drawer is missing %s", want)
		}
	}

	form := fmt.Sprintf("role=moderator&lang=fr&theme=dark&daily_goal=7&islamic_only=1&domain_id=%d", a.Content.DomainID)
	r := a.form(t, http.MethodPost, "/admin/users/"+playerID.String(), form, staff)
	if r.Code != http.StatusSeeOther && r.Code != http.StatusOK {
		t.Fatalf("saving the drawer: %d %s", r.Code, r.Body)
	}

	var role, lang, theme string
	var goal int
	var islamic bool
	if err := a.DB.Pool.QueryRow(t.Context(), `
		SELECT u.role, u.lang, s.theme, s.daily_goal, s.islamic_only
		FROM users u JOIN user_settings s ON s.user_id = u.id WHERE u.id = $1`, playerID).
		Scan(&role, &lang, &theme, &goal, &islamic); err != nil {
		t.Fatal(err)
	}
	if role != "moderator" || lang != "fr" || theme != "dark" || goal != 7 || !islamic {
		t.Errorf("the configuration was not written: %s %s %s %d %v", role, lang, theme, goal, islamic)
	}
	var domains int
	_ = a.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM user_domains WHERE user_id = $1", playerID).Scan(&domains)
	if domains != 1 {
		t.Errorf("the player should follow the one domain picked, got %d", domains)
	}
	var audits int
	_ = a.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM audit_log WHERE action = 'user.configure'").Scan(&audits)
	if audits != 1 {
		t.Errorf("a staff write records one audit row, got %d", audits)
	}

	// A player must keep at least one domain (DM-03): a save with none is refused,
	// and what they had stays as it was.
	a.form(t, http.MethodPost, "/admin/users/"+playerID.String(),
		"role=player&lang=en&theme=system&daily_goal=5", staff)
	_ = a.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM user_domains WHERE user_id = $1", playerID).Scan(&domains)
	if domains != 1 {
		t.Errorf("a save with no domain emptied the player's domains: %d", domains)
	}
	// The drawer says why, over the fields, when the console asked with htmx.
	again := a.formHTMX(t, "/admin/users/"+playerID.String(),
		"role=player&lang=en&theme=system&daily_goal=5", staff)
	if !bytes.Contains(again.Body, []byte("domain")) {
		t.Errorf("the drawer should come back saying what was wrong: %s", trim(again.Body))
	}
}

// The file goes out of one screen and comes back into it: a template is imported,
// the rows appear, and the export reads back what was written (A04, A05, A06).
func TestAdminCSVRoundTrip(t *testing.T) {
	a := newApp(t)
	token := a.staff(t, "csvstaff")

	// 1. The templates download, with their header and one example line.
	for _, kind := range []string{"domains", "categories", "questions"} {
		r := a.get("/admin/"+kind+"/template.csv", token).expect(http.StatusOK)
		if ct := r.Hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
			t.Errorf("%s template: content type %q", kind, ct)
		}
		if n := strings.Count(strings.TrimSpace(string(r.Body)), "\n"); n != 1 {
			t.Errorf("%s template: want a header and one line, got %d lines", kind, n+1)
		}
	}

	// 2. A domain, then a category, then questions — each from its own file.
	a.upload(t, token, "/admin/domains/import", "write",
		"slug,family,name_en,name_ar,name_fr,is_published\naqeedah,islamic,Creed,العقيدة,Croyance,yes\n")
	a.upload(t, token, "/admin/categories/import", "write",
		"domain_slug,slug,name_en,name_ar,name_fr,is_published\naqeedah,tawhid,Tawhid,التوحيد,Tawhid,yes\n")
	body := "domain_slug,category_slug,difficulty,points,correct,status,main_lang,question_en,answer_1_en,answer_2_en,answer_3_en,answer_4_en,explanation_en\n" +
		"aqeedah,tawhid,1,10,B,published,en,Imported question one?,A,B,C,D,Because B.\n" +
		"aqeedah,tawhid,2,20,C,published,en,Imported question two?,A,B,C,D,Because C.\n"
	report := a.upload(t, token, "/admin/questions/import", "write", body)
	if !strings.Contains(report, "2") {
		t.Logf("report: %s", trim([]byte(report)))
	}

	var questions int
	if err := a.DB.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM questions q JOIN categories c ON c.id = q.category_id WHERE c.slug = 'tawhid'").
		Scan(&questions); err != nil {
		t.Fatal(err)
	}
	if questions != 2 {
		t.Fatalf("the import should have written two questions, got %d", questions)
	}

	// 3. The export reads them back, in the columns the import takes.
	out := string(a.get("/admin/questions/export.csv", token).expect(http.StatusOK).Body)
	if !strings.Contains(out, "Imported question one?") || !strings.Contains(out, "aqeedah,tawhid") {
		t.Errorf("the export does not carry what was imported:\n%s", trim([]byte(out)))
	}
	// 4. Re-importing the export updates rather than doubles.
	a.upload(t, token, "/admin/questions/import", "write", strings.TrimPrefix(out, "\ufeff"))
	_ = a.DB.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM questions q JOIN categories c ON c.id = q.category_id WHERE c.slug = 'tawhid'").Scan(&questions)
	if questions != 2 {
		t.Errorf("re-importing the same file doubled the bank: %d rows", questions)
	}

	// 5. A broken file is refused whole: nothing of it is written.
	before := questions
	out2 := a.upload(t, token, "/admin/questions/import", "write",
		"domain_slug,category_slug,correct,main_lang,question_en,answer_1_en,answer_2_en,answer_3_en,answer_4_en\n"+
			"aqeedah,nope,B,en,Bad row?,A,B,C,D\n")
	if !strings.Contains(out2, "no such category") {
		t.Errorf("the report should name the problem:\n%s", trim([]byte(out2)))
	}
	_ = a.DB.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM questions q JOIN categories c ON c.id = q.category_id WHERE c.slug = 'tawhid'").Scan(&questions)
	if questions != before {
		t.Errorf("a file with a bad line wrote anyway: %d → %d", before, questions)
	}
}

// The account file carries emails, so only an admin may take it away (AD-02).
func TestUserExportIsAdminOnly(t *testing.T) {
	a := newApp(t)
	admin := a.staff(t, "exportadmin")
	a.get("/admin/users/export.csv", admin).expect(http.StatusOK)

	moderator := a.member("exportmod")
	if _, err := a.DB.Pool.Exec(t.Context(), "UPDATE users SET role = 'moderator' WHERE username = 'exportmod'"); err != nil {
		t.Fatal(err)
	}
	a.get("/admin/users/export.csv", moderator).expect(http.StatusForbidden)
	// The screen they can see does not offer the button either.
	if strings.Contains(string(a.get("/admin/users", moderator).expect(http.StatusOK).Body), "/admin/users/export.csv") {
		t.Error("a moderator is offered a download they cannot have")
	}
}

// form posts an urlencoded body the way a browser does.
func (a *app) form(t *testing.T, method, path, body, token string) reply {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return reply{t: t, Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

// formHTMX is form() the way a drawer posts, so the answer is the drawer again.
func (a *app) formHTMX(t *testing.T, path, body, token string) reply {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return reply{t: t, Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

// upload posts a CSV to an import endpoint and returns the report it renders.
func (a *app) upload(t *testing.T, token, path, mode, csv string) string {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("mode", mode)
	part, err := w.CreateFormFile("file", "rows.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(csv)); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: %d %s", path, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// A02's table is the design's: the columns it names, the segmented control with
// its counts, the checkbox that drives the bulk bar, and headings that sort.
func TestAdminUsersTableIsTheDesigns(t *testing.T) {
	a := newApp(t)
	staff := a.staff(t, "admin")

	page := string(a.get("/admin/users", staff).expect(http.StatusOK).Body)
	for _, want := range []string{
		"Invite staff", "Name, handle or email",
		"Everyone", "Players", "Staff",
		"Any status", "Any language",
		"Games", "Accuracy", "Last seen",
		`data-select-all`, `id="bulk-bar"`, `data-selectable="bulk-bar"`,
		"sort=user&amp;dir=asc", "More actions",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the users table is missing %q", want)
		}
	}
	// The old table printed a placeholder nobody had filled in.
	if strings.Contains(page, "{when}") {
		t.Error("a date placeholder reached the screen")
	}

	// Sorting is the server's: the heading links carry it, and the query answers.
	sorted := string(a.get("/admin/users?sort=user&dir=asc", staff).expect(http.StatusOK).Body)
	if !strings.Contains(sorted, `aria-sort="ascending"`) {
		t.Error("a sorted column should say so to a screen reader")
	}
	if !strings.Contains(sorted, "sort=user&amp;dir=desc") {
		t.Error("the heading of the sorted column should offer the other direction")
	}
	// A sort key nobody offers is ignored rather than pasted into the query.
	a.get("/admin/users?sort=;DROP%20TABLE%20users&dir=asc", staff).expect(http.StatusOK)

	// Every status of the design's filter answers.
	for _, status := range []string{"active", "suspended", "banned", "invited"} {
		a.get("/admin/users?status="+status, staff).expect(http.StatusOK)
	}
	empty := string(a.get("/admin/users?q=zzqx-nobody", staff).expect(http.StatusOK).Body)
	if !strings.Contains(empty, "No account matches that search.") {
		t.Error("a search with no hits should say so in the table")
	}
}

// A04's table: the family tabs with their counts, a search, and the three numbers
// staff can order the catalog by.
func TestAdminDomainsTableIsTheDesigns(t *testing.T) {
	a := newApp(t)
	staff := a.staff(t, "domadmin")

	page := string(a.get("/admin/domains", staff).expect(http.StatusOK).Body)
	for _, want := range []string{
		"Islamic family", "General", "Domain name or slug",
		"sort=name&amp;dir=asc", "sort=categories&amp;dir=asc",
		"sort=questions&amp;dir=asc", "sort=players&amp;dir=asc",
		"More actions", "Open categories", "Open questions",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the domains table is missing %q", want)
		}
	}
	for _, q := range []string{"?family=islamic", "?family=general", "?sort=players&dir=desc"} {
		a.get("/admin/domains"+q, staff).expect(http.StatusOK)
	}
	// A sort key nobody offers never reaches the query.
	a.get("/admin/domains?sort=1;DROP%20TABLE%20domains", staff).expect(http.StatusOK)

	empty := string(a.get("/admin/domains?q=zzqx-nothing", staff).expect(http.StatusOK).Body)
	if !strings.Contains(empty, "No domain matches that search.") {
		t.Error("a search with no hits should say so in the table")
	}
}

// A05 and A06: the filters and the orders the design offers, each answering with
// the rows it names and nothing reaching the query that was not offered.
func TestAdminCatalogTablesAreTheDesigns(t *testing.T) {
	a := newApp(t)
	staff := a.staff(t, "catadmin")

	cats := string(a.get("/admin/categories", staff).expect(http.StatusOK).Body)
	for _, want := range []string{
		"Category name or slug", "Any domain", "Retired",
		"sort=name&amp;dir=asc", "sort=order&amp;dir=asc", "sort=questions&amp;dir=asc",
		"Open questions",
	} {
		if !strings.Contains(cats, want) {
			t.Errorf("the categories table is missing %q", want)
		}
	}

	questions := string(a.get("/admin/questions", staff).expect(http.StatusOK).Body)
	for _, want := range []string{
		"Question text or #id", "Any category", "Any difficulty", "Any translations",
		"All 3 languages", "Missing a language",
		"sort=body&amp;dir=asc", "sort=difficulty&amp;dir=asc",
		"sort=accuracy&amp;dir=asc", "sort=plays&amp;dir=asc",
	} {
		if !strings.Contains(questions, want) {
			t.Errorf("the questions table is missing %q", want)
		}
	}

	// Every filter and order the toolbars offer has to answer.
	for _, u := range []string{
		"/admin/categories?live=live", "/admin/categories?live=retired",
		"/admin/categories?sort=order&dir=desc", "/admin/categories?q=nothing-here",
		"/admin/questions?status=review", "/admin/questions?status=draft",
		"/admin/questions?status=archived", "/admin/questions?langs=all",
		"/admin/questions?langs=missing", "/admin/questions?level=2",
		"/admin/questions?sort=plays&dir=desc", "/admin/questions?sort=accuracy&dir=asc",
		// And a column name nobody offers is ignored rather than run.
		"/admin/categories?sort=1;DROP%20TABLE%20categories",
		"/admin/questions?sort=1;DROP%20TABLE%20questions",
		"/admin/questions?langs=1;DROP%20TABLE%20questions",
	} {
		a.get(u, staff).expect(http.StatusOK)
	}

	for _, probe := range []struct{ url, want string }{
		{"/admin/categories?q=zzqx-nothing", "No category matches that search."},
		{"/admin/questions?q=zzqx-nothing", "No question matches those filters."},
	} {
		if body := string(a.get(probe.url, staff).expect(http.StatusOK).Body); !strings.Contains(body, probe.want) {
			t.Errorf("%s should show its empty state", probe.url)
		}
	}
}

// The moderation and system screens: the tabs the design gives Comments, the
// support inbox's priority and assignee filters, and an empty state on each.
func TestAdminModerationScreensAreTheDesigns(t *testing.T) {
	a := newApp(t)
	staff := a.staff(t, "modadmin")

	comments := string(a.get("/admin/comments", staff).expect(http.StatusOK).Body)
	for _, want := range []string{"Needs action", "Flagged", "Resolved", "Hidden", "Comment text or author"} {
		if !strings.Contains(comments, want) {
			t.Errorf("the comments screen is missing %q", want)
		}
	}
	for _, tab := range []string{"needs", "flagged", "resolved", "hidden"} {
		a.get("/admin/comments?tab="+tab, staff).expect(http.StatusOK)
	}

	support := string(a.get("/admin/support", staff).expect(http.StatusOK).Body)
	for _, want := range []string{"Any priority", "Anyone", "Unassigned", "kpi-link"} {
		if !strings.Contains(support, want) {
			t.Errorf("the support inbox is missing %q", want)
		}
	}
	// Each tile is a filter, and each filter answers.
	for _, u := range []string{
		"/admin/support?status=open", "/admin/support?status=waiting",
		"/admin/support?priority=high", "/admin/support?priority=normal",
		"/admin/support?priority=low", "/admin/support?assignee=mine",
		"/admin/support?assignee=unassigned",
		// A priority or an assignee nobody offers filters nothing, rather than
		// reaching the query.
		"/admin/support?priority=1;DROP%20TABLE%20support_tickets",
		"/admin/support?assignee=everyone",
	} {
		a.get(u, staff).expect(http.StatusOK)
	}

	for _, probe := range []struct{ url, want string }{
		{"/admin/support?q=zzqx-nothing", "No ticket here"},
		{"/admin/audit?actor=zzqx-nothing", "Nothing matches that search."},
		{"/admin/rated", "Rated poorly"},
	} {
		if body := string(a.get(probe.url, staff).expect(http.StatusOK).Body); !strings.Contains(body, probe.want) {
			t.Errorf("%s should contain %q", probe.url, probe.want)
		}
	}
}

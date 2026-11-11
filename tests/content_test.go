package tests

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"siraj/internal/modules/admin"
)

// No migration seeds content any more: a domain, a category and a question are
// written by staff in the admin console. This walks that path and then plays the
// result as a player, so "the database is the only source of content" is a test and
// not a promise.
func TestContentWrittenInTheAdminIsPlayable(t *testing.T) {
	a := newApp(t)
	svc := admin.NewService(a.DB)
	ctx := t.Context()

	// Staff: the actor of every write, recorded in audit_log (AD-03).
	a.member("contentstaff")
	var staffID uuid.UUID
	if err := a.DB.Pool.QueryRow(ctx,
		"UPDATE users SET role = 'admin' WHERE username = 'contentstaff' RETURNING id").Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	actor := admin.Actor{ID: staffID, IP: "192.0.2.1"}

	domainID, err := svc.SaveDomain(ctx, actor, admin.DomainInput{
		Slug: "aqeedah", Family: "islamic", Icon: "mosque", Color: "#0b7a5c", Motif: "mosque",
		Position: 1, IsPublished: true, IsFeatured: true,
		Names:        map[string]string{"en": "Creed", "ar": "العقيدة", "fr": "Croyance"},
		Descriptions: map[string]string{"en": "What a Muslim believes"},
	})
	if err != nil {
		t.Fatalf("save domain: %v", err)
	}
	categoryID, err := svc.SaveCategory(ctx, actor, admin.CategoryInput{
		DomainID: domainID, Slug: "tawhid", Icon: "book", Color: "#0d9488",
		Position: 1, IsPublished: true,
		Names: map[string]string{"en": "Tawhid", "ar": "التوحيد", "fr": "Tawhid"},
	})
	if err != nil {
		t.Fatalf("save category: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := svc.SaveQuestion(ctx, actor, admin.QuestionInput{
			CategoryID: categoryID, Difficulty: 1, CorrectIndex: 1, MainLang: "en",
			Status: "published", Source: "Qur'an",
			Bodies:       map[string]string{"en": "Written in the admin, question " + string(rune('A'+i)) + "?"},
			Answers:      map[string][]string{"en": {"One", "Two", "Three", "Four"}},
			Explanations: map[string]string{"en": "Because two."},
		}); err != nil {
			t.Fatalf("save question %d: %v", i, err)
		}
	}

	// A player sees it through the public catalog…
	player := a.member("contentplayer")
	domains := a.get("/api/v1/domains?scope=all", player).expect(http.StatusOK).list()
	found := false
	for _, raw := range domains {
		if raw.(map[string]any)["slug"] == "aqeedah" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the domain staff created is not in the catalog: %v", domains)
	}
	category := a.get("/api/v1/categories/"+itoa(categoryID), player).expect(http.StatusOK).data()
	if category["name"] != "Tawhid" {
		t.Errorf("endpoint 20 reads the translation: %v", category)
	}
	search := a.get("/api/v1/search?scope=all&q=tawhid", player).expect(http.StatusOK).data()
	if len(search["categories"].([]any)) == 0 {
		t.Errorf("endpoint 21 does not find the new category: %v", search)
	}

	// …and can play it. The game deals the questions staff published, nothing else.
	a.post("/api/v1/me/domains/"+itoa(domainID), nil, player).expect(http.StatusOK)
	start := a.post("/api/v1/games", map[string]any{
		"kind": "quick", "domain_id": domainID,
	}, player).expect(http.StatusCreated).data()
	question := start["question"].(map[string]any)
	if question["body"] == "" {
		t.Fatalf("the game dealt an empty question: %v", question)
	}

	// Every staff write left its trail (AD-03).
	rows, _, err := svc.Audit(ctx, admin.AuditFilter{Page: 1, PerPag: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 7 {
		t.Errorf("want an audit row per write, got %d", len(rows))
	}
}

// An empty database is an empty app, not a broken one: the catalog answers with
// nothing and a game says why it cannot start.
func TestEmptyDatabaseServesNothingRatherThanSamples(t *testing.T) {
	a := newApp(t)
	// Drop what the fixture wrote, so this test sees a database as it ships.
	if _, err := a.DB.Pool.Exec(t.Context(),
		"TRUNCATE questions, question_translations, categories, category_translations, domains, domain_translations, user_domains RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	token := a.guest("Empty")
	if list := a.get("/api/v1/domains?scope=all", token).expect(http.StatusOK).list(); len(list) != 0 {
		t.Errorf("a fresh database has no domains, got %v", list)
	}
	r := a.post("/api/v1/games", map[string]any{"kind": "quick"}, token)
	if r.Code == http.StatusCreated {
		t.Error("a game started without a single question in the database")
	}
	if code := r.errorCode(); code == "" {
		t.Errorf("the refusal should name itself: %s", r.Body)
	}
	// The screens still open.
	for _, path := range []string{"/home", "/play", "/explore", "/onboarding"} {
		if got := a.get(path, token).Code; got >= 500 {
			t.Errorf("GET %s on an empty database: %d", path, got)
		}
	}
}

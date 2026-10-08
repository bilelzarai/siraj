package handlers_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// The player's side of the second level. Until this prompt a player could only
// choose one category or the whole bank; a subject area was a thing an admin
// could create and nobody could play.

// aSecondSubjectArea creates one with a category and enough questions to fill
// a round, and returns both ids. Everything here is made through the public
// repository, so the test is not asserting against fixtures it invented.
func aSecondSubjectArea(t *testing.T, a *app, slug string) (domainID, categoryID int) {
	t.Helper()
	ctx := t.Context()

	id, err := a.repo.UpsertDomain(ctx, &models.DomainDraft{
		Slug: slug, Icon: "⚽", Color: "#1d4ed8", SortOrder: 90, IsActive: true,
		Names: map[string]models.NameDraft{
			"ar": {Name: "الرياضة", Source: "human"},
			"en": {Name: "Sport", Source: "human"},
		},
	})
	if err != nil {
		t.Fatalf("creating a subject area: %v", err)
	}
	catID, err := a.repo.UpsertCategory(ctx, &models.CategoryDraft{
		Slug: slug + "-football", DomainID: id, Icon: "🥅", Color: "#1d4ed8",
		SortOrder: 91, IsActive: true,
		Names: map[string]models.NameDraft{
			"ar": {Name: "كرة القدم", Source: "human"},
			"en": {Name: "Football", Source: "human"},
		},
	})
	if err != nil {
		t.Fatalf("creating a category: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := a.repo.UpsertQuestion(ctx, &models.QuestionDraft{
			ID: 990000 + i, CategoryID: catID, Difficulty: 1, Points: 10,
			CorrectIndex: 0, IsActive: true,
			Translations: map[string]models.TranslationDraft{
				"en": {Prompt: fmt.Sprintf("Sport question %d?", i),
					Choices: []string{"a", "b", "c", "d"}, Source: "human"},
				"ar": {Prompt: fmt.Sprintf("سؤال رياضي %d؟", i),
					Choices: []string{"أ", "ب", "ج", "د"}, Source: "human"},
			},
		}); err != nil {
			t.Fatalf("writing a question: %v", err)
		}
	}
	return id, catID
}

// The setup screen asks once. A subject area and a category are alternatives,
// so they share one list and one radio group — with no script there is nothing
// to keep two groups in step, and two answers could disagree.
func TestTheSetupScreenOffersSubjectAreasOnceThereIsMoreThanOne(t *testing.T) {
	a := newApp(t)
	a.register("setupplayer")

	// With one subject area the screen must be what it always was. The
	// handler package shares one database, so how many exist is whatever the
	// tests before this one left behind — the precondition is read, not
	// assumed.
	existing, err := a.repo.Domains(t.Context(), "en")
	if err != nil {
		t.Fatalf("reading subject areas: %v", err)
	}
	if len(existing) == 1 {
		_, body := a.get("/play?lang=en")
		if strings.Contains(body, `value="d:`) {
			t.Error("a single subject area is offered as a choice, which is not a decision anybody has to take")
		}
	}

	domainID, categoryID := aSecondSubjectArea(t, a, "sport-setup")

	_, body := a.get("/play?lang=en")
	for _, want := range []string{
		`value="all"`,
		fmt.Sprintf(`value="d:%d"`, domainID),
		fmt.Sprintf(`value="c:%d"`, categoryID),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the setup list is missing %s", want)
		}
	}
	// One group, one answer.
	if n := strings.Count(body, `name="pick"`); n < 3 {
		t.Errorf("only %d entries in the choice list", n)
	}
	if strings.Contains(body, `name="category" value=`) {
		t.Error("the old second group is still rendered, so two answers could disagree")
	}
}

// A round drawn from a whole subject area records which one, and reads back as
// that subject area rather than as "all categories" — which would claim it
// could have asked about anything in the bank.
func TestAWholeSubjectAreaRoundRecordsAndReadsBackAsItself(t *testing.T) {
	a := newApp(t)
	a.register("domainplayer")
	domainID, _ := aSecondSubjectArea(t, a, "sport-round")

	if status, _ := a.post("/play/start", url.Values{
		"pick":       {fmt.Sprintf("d:%d", domainID)},
		"difficulty": {"1"},
		"count":      {"5"},
	}); status != http.StatusSeeOther {
		t.Fatalf("starting a whole-subject-area round → %d", status)
	}

	round, err := a.repo.ActiveGame(t.Context(), a.userID(t, "domainplayer"), "en")
	if err != nil {
		t.Fatalf("reading the round back: %v", err)
	}
	if round.DomainID == nil || *round.DomainID != domainID {
		t.Fatalf("the round recorded domain %v, want %d", round.DomainID, domainID)
	}
	if round.CategoryID != nil {
		t.Error("a whole-subject-area round also recorded a category, so the two could disagree")
	}
	if round.DomainName == "" {
		t.Error("the round does not read back the name of what it was drawn from")
	}

	// And every question in it came from that subject area.
	for _, id := range round.QuestionIDs {
		q, err := a.repo.Question(t.Context(), id, "en", "ar")
		if err != nil {
			t.Fatalf("reading question %d: %v", id, err)
		}
		cats, _ := a.repo.Categories(t.Context(), "en", domainID)
		var inside bool
		for _, c := range cats {
			if c.ID == q.CategoryID {
				inside = true
			}
		}
		if !inside {
			t.Errorf("question %d is not in the subject area the round named", id)
		}
	}

	// The history row names it.
	_, body := a.get("/app?lang=en")
	if !strings.Contains(body, "Sport") {
		t.Error("the round does not say which subject area it was drawn from")
	}
}

// Choosing a category still means that category, and a link carrying the old
// field still works — bookmarks, the smoke walk and every existing test use it.
func TestTheOlderCategoryFieldStillStartsARound(t *testing.T) {
	a := newApp(t)
	a.register("legacyplayer")
	_, categoryID := aSecondSubjectArea(t, a, "sport-legacy")

	if status, _ := a.post("/play/start", url.Values{
		"category":   {fmt.Sprint(categoryID)},
		"difficulty": {"1"},
		"count":      {"5"},
	}); status != http.StatusSeeOther {
		t.Fatalf("starting a round the old way → %d", status)
	}
	round, err := a.repo.ActiveGame(t.Context(), a.userID(t, "legacyplayer"), "en")
	if err != nil {
		t.Fatalf("reading the round back: %v", err)
	}
	if round.CategoryID == nil || *round.CategoryID != categoryID {
		t.Fatalf("the round recorded category %v, want %d", round.CategoryID, categoryID)
	}
	if round.DomainID != nil {
		t.Error("a category round also recorded a subject area, which would claim it drew more widely than it did")
	}
}

// userID is who the harness is signed in as, which the tests above need to
// read a round back through the repository rather than through a screen.
func (a *app) userID(t *testing.T, username string) uuid.UUID {
	t.Helper()
	user, err := a.repo.UserByUsername(t.Context(), username)
	if err != nil {
		t.Fatalf("load %s: %v", username, err)
	}
	return user.ID
}

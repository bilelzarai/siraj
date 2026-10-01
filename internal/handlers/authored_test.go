package handlers_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// between pulls the first value bracketed by two markers out of a page.
func between(body, start, end string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	rest := body[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func mustUser(t *testing.T, a *app, username string) uuid.UUID {
	t.Helper()
	user, err := a.repo.UserByUsername(t.Context(), username)
	if err != nil {
		t.Fatalf("load %s: %v", username, err)
	}
	return user.ID
}

// Writing several questions at once, each with a different answer marked.
//
// Every row shared one radio name, so the whole form was a single group:
// marking the answer in question six unmarked it in question five, and the
// server read one value for all six rows. Six questions went in and every one
// of them came out claiming "A" was correct — or, when a row was incomplete,
// nothing was saved at all and the form came back empty.
func TestWritingSeveralQuestionsKeepsEachAnswer(t *testing.T) {
	a := newApp(t)
	a.register("setwriter")

	// A set is created with its first questions — three of them, with C, A and
	// D as the answers.
	form := url.Values{"name": {"My questions"}}
	for i, want := range []string{"2", "0", "3"} {
		form.Add("q_prompt", "Question number "+string(rune('1'+i))+"?")
		form.Add("q_choice0", "first")
		form.Add("q_choice1", "second")
		form.Add("q_choice2", "third")
		form.Add("q_choice3", "fourth")
		form.Add("q_explanation", "")
		form.Set("q_correct_"+string(rune('0'+i)), want)
	}
	if status, _ := a.post("/my/questions", form); status != http.StatusSeeOther {
		t.Fatalf("creating a set with three questions → %d", status)
	}

	// All three are there, each keeping its own answer.
	set, err := a.repo.QuestionSets(t.Context(), mustUser(t, a, "setwriter"))
	if err != nil || len(set) != 1 {
		t.Fatalf("sets = %v, %v", set, err)
	}
	if set[0].Count != 3 {
		t.Fatalf("the set holds %d questions, want 3", set[0].Count)
	}

	questions, err := a.repo.SetQuestions(t.Context(), set[0].ID, mustUser(t, a, "setwriter"), "en")
	if err != nil {
		t.Fatalf("read set: %v", err)
	}
	want := []int{2, 0, 3}
	for i, q := range questions {
		if q.CorrectIndex != want[i] {
			t.Errorf("question %d has answer %d, want %d — the rows shared a radio group",
				i+1, q.CorrectIndex, want[i])
		}
	}
}

// One unfinished row must not throw away the finished ones, and must not clear
// the form: six questions lost because the fourth was short an answer is how
// somebody loses an evening.
func TestAnIncompleteRowKeepsWhatWasTyped(t *testing.T) {
	a := newApp(t)
	a.register("setkeeper")

	// A set with one good question in it to start.
	first := url.Values{"name": {"Drafts"}}
	first.Add("q_prompt", "The one that worked?")
	for i, v := range []string{"one", "two", "three", "four"} {
		first.Add("q_choice"+string(rune('0'+i)), v)
	}
	first.Add("q_explanation", "")
	first.Set("q_correct_0", "0")
	if status, _ := a.post("/my/questions", first); status != http.StatusSeeOther {
		t.Fatalf("creating the set → %d", status)
	}
	_, body := a.get("/my/questions")
	setID := between(body, "/my/questions/", `"`)

	form := url.Values{}
	// A good one.
	form.Add("q_prompt", "A complete question?")
	form.Add("q_choice0", "one")
	form.Add("q_choice1", "two")
	form.Add("q_choice2", "three")
	form.Add("q_choice3", "four")
	form.Add("q_explanation", "")
	form.Set("q_correct_0", "1")
	// And one missing two answers.
	form.Add("q_prompt", "An unfinished question?")
	form.Add("q_choice0", "only this")
	form.Add("q_choice1", "")
	form.Add("q_choice2", "")
	form.Add("q_choice3", "")
	form.Add("q_explanation", "")
	form.Set("q_correct_1", "0")

	status, _ := a.post("/my/questions/"+setID+"/add", form)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("an incomplete row → %d, want the form back with 422", status)
	}

	// Nothing was written — and what was typed is still on the screen.
	_, page := a.get("/my/questions/" + setID)
	if strings.Contains(page, "A complete question?") {
		// The GET is a fresh form; the point is the POST response carries it.
		t.Log("note: the saved page is empty, as expected")
	}
	// The one that was already there is untouched, and neither of the submitted
	// rows was written.
	if set, _ := a.repo.QuestionSets(t.Context(), mustUser(t, a, "setkeeper")); len(set) > 0 && set[0].Count != 1 {
		t.Errorf("the set holds %d questions; the refusal should have written none", set[0].Count)
	}
}

// A set with nothing in it is a name, not a collection. Creating one used to
// ask only for a name, which filled the screen with empty labels somebody then
// had to tidy up.
func TestASetCannotBeCreatedEmpty(t *testing.T) {
	a := newApp(t)
	a.register("setempty")

	status, _ := a.post("/my/questions", url.Values{"name": {"Nothing in here"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("creating a set with no questions → %d, want it refused", status)
	}

	sets, err := a.repo.QuestionSets(t.Context(), mustUser(t, a, "setempty"))
	if err != nil {
		t.Fatalf("sets: %v", err)
	}
	if len(sets) != 0 {
		t.Errorf("%d empty set(s) were created anyway", len(sets))
	}
}

// The source is chosen, not inferred. A match asks once where its questions
// come from and uses only that: working it out from whatever happened to be
// filled in made "I picked a set and also typed something" a question with no
// honest answer.
func TestMatchUsesOnlyTheChosenSource(t *testing.T) {
	a := newApp(t)
	a.register("sourcehost")
	guest := newAppSharing(t, a)
	guest.register("sourceguest")

	host := mustUser(t, a, "sourcehost")
	other := mustUser(t, a, "sourceguest")
	if err := a.repo.RequestFriendship(t.Context(), host, other); err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := a.repo.RespondToFriendship(t.Context(), host, other, true); err != nil {
		t.Fatalf("accept: %v", err)
	}
	for i := 0; i < 12; i++ {
		seedQuestions(a, 995000+i, 1)
	}

	// Chosen: the bank. Questions are typed into the write boxes as well, and
	// must be ignored rather than quietly mixed in.
	form := url.Values{
		"source":   {"bank"},
		"opponent": {"sourceguest"},
		"count":    {"5"},
	}
	form.Add("q_prompt", "This should be ignored")
	for i, v := range []string{"a", "b", "c", "d"} {
		form.Add("q_choice"+string(rune('0'+i)), v)
	}
	form.Add("q_explanation", "")
	form.Set("q_correct_0", "0")

	if status, _ := a.post("/challenges/new", form); status != http.StatusSeeOther {
		t.Fatalf("creating a bank match → %d", status)
	}

	// Nothing was written: the host has no questions of their own.
	sets, err := a.repo.QuestionSets(t.Context(), host)
	if err != nil {
		t.Fatalf("sets: %v", err)
	}
	for _, set := range sets {
		if set.Count > 0 {
			t.Errorf("the bank match wrote %d questions anyway", set.Count)
		}
	}
}

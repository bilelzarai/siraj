package handlers_test

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// The queue's second page used to be its first.
//
// QuestionCommentsPage accepted an offset and never used it, so the pager
// moved and the rows did not: a moderation queue longer than one page could
// not be worked through at all. Nothing failed, because every page was a valid
// page of remarks — just always the same one.
func TestTheCommentQueuePagesPastTheFirstPage(t *testing.T) {
	a := newApp(t)
	a.register("pagemod")
	a.promote("pagemod", models.RoleAdmin)

	id := upsert(t, a, 0, 1, 1, true, map[string]models.TranslationDraft{
		"en": {Prompt: "A question to remark on", Choices: en4(), Source: "human"},
	})
	for i := 0; i < 4; i++ {
		voice := newAppSharing(t, a)
		voice.register("remarker" + strconv.Itoa(i))
		who := mustUUID(t, userIDByName(t, a, "remarker"+strconv.Itoa(i)))
		if err := a.repo.UpsertQuestionComment(t.Context(), int64(id), who, "en",
			"Remark number "+strconv.Itoa(i)); err != nil {
			t.Fatalf("seeding a remark: %v", err)
		}
	}

	first, err := a.repo.QuestionCommentsPage(t.Context(),
		repository.CommentFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.repo.QuestionCommentsPage(t.Context(),
		repository.CommentFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 2 || len(second) < 1 {
		t.Fatalf("not enough remarks to page: %d then %d", len(first), len(second))
	}
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID {
				t.Fatalf("page two repeats a remark from page one (%d); the offset is being dropped", a.ID)
			}
		}
	}
}

// Resolved is the third state, beside visible and hidden.
//
// A remark that reported a real fault should be marked dealt with, not hidden:
// hiding a correct observation pretends it never arrived, and before this the
// queue could only be cleared by doing exactly that.
func TestAResolvedRemarkLeavesTheQueueWithoutBeingHidden(t *testing.T) {
	a := newApp(t)
	a.register("resolver")
	a.promote("resolver", models.RoleAdmin)

	id := upsert(t, a, 0, 1, 1, true, map[string]models.TranslationDraft{
		"en": {Prompt: "A question with a fault", Choices: en4(), Source: "human"},
	})
	cid := seedComment(t, a, id)

	if status, _ := a.post("/admin/comments/"+strconv.FormatInt(cid, 10)+"/resolve",
		url.Values{}); status != 303 {
		t.Fatal("could not resolve a remark")
	}

	open, err := a.repo.QuestionCommentsPage(t.Context(),
		repository.CommentFilter{State: "open", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, cm := range open {
		if cm.ID == cid {
			t.Error("a resolved remark is still in the queue")
		}
	}

	resolved, err := a.repo.QuestionCommentsPage(t.Context(),
		repository.CommentFilter{State: "resolved", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var found *models.QuestionComment
	for _, cm := range resolved {
		if cm.ID == cid {
			found = cm
		}
	}
	if found == nil {
		t.Fatal("the resolved remark is not in the resolved list")
	}
	// It is resolved and not hidden: the player's words stand on the question.
	if found.Hidden {
		t.Error("resolving hid the remark")
	}
	if !found.Resolved() {
		t.Error("the remark is not marked resolved")
	}
	if found.ResolvedBy == "" {
		t.Error("the remark does not record who resolved it")
	}

	// And it can be put back.
	if status, _ := a.post("/admin/comments/"+strconv.FormatInt(cid, 10)+"/reopen",
		url.Values{}); status != 303 {
		t.Fatal("could not reopen a remark")
	}
	open, _ = a.repo.QuestionCommentsPage(t.Context(),
		repository.CommentFilter{State: "open", Limit: 50})
	back := false
	for _, cm := range open {
		if cm.ID == cid {
			back = true
		}
	}
	if !back {
		t.Error("a reopened remark did not come back to the queue")
	}
}

// The tabs are which part of the queue, not a reset of what you were looking
// for — so the search and the language survive a tab change.
func TestTheCommentTabsKeepTheSearch(t *testing.T) {
	a := newApp(t)
	a.register("tabmod")
	a.promote("tabmod", models.RoleAdmin)

	_, body := a.get("/admin/comments?q=probe&loc=en")
	for _, want := range []string{"q=probe", "loc=en"} {
		if !strings.Contains(body, want) {
			t.Errorf("a tab link dropped %s", want)
		}
	}
	// An unknown state shows everything rather than nothing.
	if status, _ := a.get("/admin/comments?state=nonsense"); status != 200 {
		t.Errorf("an unrecognised tab → %d", status)
	}
}

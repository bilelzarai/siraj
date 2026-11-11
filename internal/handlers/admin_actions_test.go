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

// Every action the console offers, pressed. The screens were rebuilt on a new
// design and the one thing a redesign breaks invisibly is the wiring: a button
// that still looks right and posts to nothing, or posts to the right place
// having lost a field it needed.
func TestEveryConsoleActionStillWorks(t *testing.T) {
	a := newApp(t)
	a.register("presser")
	a.promote("presser", models.RoleAdmin)

	// ---- a question, created through the form the screen actually renders
	qForm := url.Values{
		"category_id": {"1"}, "difficulty": {"2"}, "points": {"20"},
		"correct_index": {"0"}, "is_active": {"1"},
		"prompt_en": {"Does the save path still work?"},
		"choice_en_0": {"yes"}, "choice_en_1": {"no"},
		"choice_en_2": {"maybe"}, "choice_en_3": {"later"},
	}
	if status, loc := a.post("/admin/questions/save", qForm); status != http.StatusSeeOther {
		t.Fatalf("saving a question → %d (%s)", status, loc)
	}
	id := newestQuestionID(t, a, "Does the save path still work?")

	// ---- retire, restore, and the ratings dismissal
	for _, step := range []struct{ path, name string }{
		{"/admin/questions/" + strconv.Itoa(id) + "/deactivate", "retire a question"},
		{"/admin/questions/" + strconv.Itoa(id) + "/activate", "restore a question"},
		{"/admin/rated/" + strconv.Itoa(id) + "/dismiss", "dismiss ratings"},
	} {
		if status, _ := a.post(step.path, url.Values{}); status != http.StatusSeeOther {
			t.Errorf("%s → %d, want a redirect", step.name, status)
		}
	}

	// ---- the bulk form, with the scope the screen submits
	bulk := url.Values{"ids": {strconv.Itoa(id)}, "action": {"deactivate"}}
	if status, _ := a.post("/admin/questions/bulk", bulk); status != http.StatusSeeOther {
		t.Errorf("a bulk retire → %d", status)
	}
	draft, err := a.repo.QuestionDraftByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if draft.IsActive {
		t.Error("the bulk retire did not take")
	}

	// ---- "everything matching the filter", rebuilt server-side
	wide := url.Values{"scope": {"filter"}, "action": {"activate"}, "q": {"Does the save path"}}
	if status, _ := a.post("/admin/questions/bulk", wide); status != http.StatusSeeOther {
		t.Errorf("a filter-scoped bulk → %d", status)
	}
	if draft, _ := a.repo.QuestionDraftByID(t.Context(), id); !draft.IsActive {
		t.Error("the filter-scoped bulk activate did not reach the question its filter matched")
	}

	// ---- review: all three verdicts on one translation
	pending := pendingTranslation(t, a, "Three verdicts")
	pid := strconv.Itoa(pending)
	if status, _ := a.post("/admin/review/"+pid+"/changes",
		url.Values{"locale": {"fr"}, "note": {"The second choice repeats the first."}}); status != http.StatusSeeOther {
		t.Errorf("requesting changes → %d", status)
	}
	if status, _ := a.post("/admin/review/"+pid+"/approve",
		url.Values{"locale": {"fr"}}); status != http.StatusSeeOther {
		t.Errorf("approving → %d", status)
	}
	again := pendingTranslation(t, a, "Reject me")
	if status, _ := a.post("/admin/review/"+strconv.Itoa(again)+"/reject",
		url.Values{"locale": {"fr"}}); status != http.StatusSeeOther {
		t.Errorf("rejecting → %d", status)
	}

	// ---- a user: role, suspend with a reason, reinstate
	other := newAppSharing(t, a)
	other.register("pressee")
	target := userIDByName(t, a, "pressee")

	if status, _ := a.post("/admin/users/"+target+"/role",
		url.Values{"role": {models.RoleModerator}}); status != http.StatusSeeOther {
		t.Errorf("changing a role → %d", status)
	}
	if status, _ := a.post("/admin/users/"+target+"/suspend",
		url.Values{"reason": {"Pressed every button"}}); status != http.StatusSeeOther {
		t.Errorf("suspending → %d", status)
	}
	u, err := a.repo.AdminUser(t.Context(), mustUUID(t, target))
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsSuspended() || u.SuspendedReason != "Pressed every button" {
		t.Errorf("the suspension did not record its reason: %+v", u.Status)
	}
	if status, _ := a.post("/admin/users/"+target+"/reinstate", url.Values{}); status != http.StatusSeeOther {
		t.Errorf("reinstating → %d", status)
	}

	// ---- a comment, hidden and shown again
	cid := seedComment(t, a, id)
	for _, action := range []string{"hide", "show"} {
		if status, _ := a.post("/admin/comments/"+strconv.FormatInt(cid, 10)+"/"+action,
			url.Values{}); status != http.StatusSeeOther {
			t.Errorf("%s a comment → %d", action, status)
		}
	}

	// ---- support: triage, a reply, and an internal note
	tid := seedTicket(t, a)
	if status, _ := a.post("/admin/support/"+tid+"/update", url.Values{
		"status": {models.TicketInProgress}, "priority": {models.PriorityHigh},
		"kind": {models.TicketBug}, "assignee": {"me"},
	}); status != http.StatusSeeOther {
		t.Errorf("triage → %d", status)
	}
	for _, body := range []url.Values{
		{"body": {"A reply the player sees."}},
		{"body": {"A note only staff see."}, "internal": {"1"}},
	} {
		if status, _ := a.post("/admin/support/"+tid+"/reply", body); status != http.StatusSeeOther {
			t.Errorf("replying → %d", status)
		}
	}

	// ---- the trail recorded all of it, which is the other half of working
	entries, err := a.repo.AuditPage(t.Context(), auditAll())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Action] = true
	}
	for _, want := range []string{
		"question.create", "question.deactivate", "question.activate",
		"question.bulk.deactivate", "translation.changes", "translation.approve",
		"translation.reject", "user.role", "user.suspend", "user.reinstate",
		"comment.hide", "comment.show", "support.triage", "support.reply",
		"support.note",
	} {
		if !seen[want] {
			t.Errorf("%q was pressed but left no entry in the trail", want)
		}
	}
}

// A suspension's before/after has to be in the entry, or the audit screen's
// diff panel has nothing to open.
func TestTheTrailRecordsWhatChanged(t *testing.T) {
	a := newApp(t)
	a.register("differ")
	a.promote("differ", models.RoleAdmin)
	other := newAppSharing(t, a)
	other.register("diffee")
	target := userIDByName(t, a, "diffee")

	if status, _ := a.post("/admin/users/"+target+"/suspend",
		url.Values{"reason": {"For the diff"}}); status != http.StatusSeeOther {
		t.Fatal("could not suspend")
	}

	entries, err := a.repo.AuditPage(t.Context(), auditAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Action != "user.suspend" {
			continue
		}
		if e.Label() != "diffee" {
			t.Errorf("the entry names its target as %q", e.Label())
		}
		changes := e.Changes()
		if len(changes) == 0 {
			t.Fatal("a suspension recorded no change")
		}
		var statusChange bool
		for _, ch := range changes {
			if ch.Field == "status" && ch.From == "active" && ch.To == "suspended" {
				statusChange = true
			}
		}
		if !statusChange {
			t.Errorf("no status change in %+v", changes)
		}
		return
	}
	t.Error("no user.suspend entry in the trail")
}

// ---------------------------------------------------------------- helpers --

func auditAll() repository.AuditFilter { return repository.AuditFilter{Limit: 200} }

func userIDByName(t *testing.T, a *app, username string) string {
	t.Helper()
	users, _, err := a.repo.AdminUsers(t.Context(), repository.AdminUserFilter{Query: username, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Username == username {
			return u.ID.String()
		}
	}
	t.Fatalf("no account %q", username)
	return ""
}

func newestQuestionID(t *testing.T, a *app, prompt string) int {
	t.Helper()
	ids, err := a.repo.AdminQuestionIDs(t.Context(), repository.AdminQuestionFilter{
		Query: prompt, Locale: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatalf("no question matching %q", prompt)
	}
	return ids[0]
}

func seedComment(t *testing.T, a *app, questionID int) int64 {
	t.Helper()
	// A remark needs an author who has answered the question, which is the
	// player-side rule; seeding through the repository keeps this test about
	// the moderation action rather than about playing a round.
	users, _, err := a.repo.AdminUsers(t.Context(), repository.AdminUserFilter{Limit: 1})
	if err != nil || len(users) == 0 {
		t.Fatalf("no account to author a remark: %v", err)
	}
	if err := a.repo.UpsertQuestionComment(t.Context(), int64(questionID),
		users[0].ID, "en", "A remark to moderate."); err != nil {
		t.Fatalf("seeding a remark: %v", err)
	}
	comments, err := a.repo.QuestionCommentsPage(t.Context(), repository.CommentFilter{Limit: 10})
	if err != nil || len(comments) == 0 {
		t.Fatalf("no remark came back: %v", err)
	}
	return comments[0].ID
}

func seedTicket(t *testing.T, a *app) string {
	t.Helper()
	users, _, err := a.repo.AdminUsers(t.Context(), repository.AdminUserFilter{Limit: 1})
	if err != nil || len(users) == 0 {
		t.Fatalf("no account to open a ticket: %v", err)
	}
	ticket := &models.Ticket{
		UserID: users[0].ID, Kind: models.TicketQuestion,
		Priority: models.PriorityNormal, Subject: "A ticket to triage",
		Locale: "en", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) Safari/604.1",
	}
	if err := a.repo.CreateTicket(t.Context(), ticket, "Something is wrong."); err != nil {
		t.Fatalf("seeding a ticket: %v", err)
	}
	return ticket.ID.String()
}

var _ = strings.TrimSpace

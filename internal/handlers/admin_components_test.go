package handlers_test

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// Every console screen with data in it, checked for the components the design
// names.
//
// An empty screen is a legitimate render and most of these pages have an empty
// state of their own, which is why checking them against a fresh database
// proves very little: every table is absent because there is nothing in it. So
// this fills each screen with the rows it is about first — questions in three
// states, a translation awaiting review and another sent back, a round of
// answers, ratings low enough to flag, a hidden remark, a triaged ticket, and
// trail entries carrying a before and an after.
func TestEveryAdminScreenRendersItsComponents(t *testing.T) {
	a := newApp(t)
	a.register("seeder")
	a.promote("seeder", models.RoleAdmin)

	player := newAppSharing(t, a)
	player.register("seedplayer")
	playerID := mustUUID(t, userIDByName(t, a, "seedplayer"))

	// ---- questions: complete, partly translated, retired, and a near-pair
	full := upsert(t, a, 0, 1, 2, true, map[string]models.TranslationDraft{
		"ar": {Prompt: "من جمع المصحف في نسخة واحدة؟", Choices: ar4(), Source: "human"},
		"en": {Prompt: "Who compiled the mushaf into one copy?", Choices: en4(), Source: "human"},
		"fr": {Prompt: "Qui a compilé le mushaf en un exemplaire ?", Choices: fr4(), Source: "human"},
	})
	partial := upsert(t, a, 0, 1, 1, true, map[string]models.TranslationDraft{
		"ar": {Prompt: "سؤال بلغة واحدة فقط", Choices: ar4(), Source: "human"},
	})
	retired := upsert(t, a, 0, 2, 3, false, map[string]models.TranslationDraft{
		"ar": {Prompt: "سؤال مؤرشف", Choices: ar4(), Source: "human"},
		"en": {Prompt: "A retired question", Choices: en4(), Source: "human"},
	})
	// Two that read alike, so the duplicate sweep has a pair to raise.
	twinA := upsert(t, a, 0, 1, 2, true, map[string]models.TranslationDraft{
		"en": {Prompt: "Which battle stopped the Mongol advance in 1260?", Choices: en4(), Source: "human"},
	})
	twinB := upsert(t, a, 0, 1, 2, true, map[string]models.TranslationDraft{
		"en": {Prompt: "Which battle halted the Mongol advance in 1260?", Choices: en4(), Source: "human"},
	})
	_ = twinA
	_ = twinB

	// ---- a translation awaiting review, and one sent back with a note
	pending := pendingTranslation(t, a, "Awaiting a verdict")
	noted := pendingTranslation(t, a, "Sent back with a note")
	if status, _ := a.post("/admin/review/"+strconv.Itoa(noted)+"/changes",
		url.Values{"locale": {"fr"}, "note": {"The second choice repeats the first."}}); status != 303 {
		t.Fatal("could not request changes")
	}
	_ = pending

	// ---- plays and answers, so accuracy and the chart have something in them
	seedPlay(t, a, playerID, []int{full, partial, retired, twinA})

	// ---- ratings low enough to flag, from enough people to count
	for i := 0; i < models.MinRatingVotes; i++ {
		voter := newAppSharing(t, a)
		voter.register("rater" + strconv.Itoa(i))
		id := mustUUID(t, userIDByName(t, a, "rater"+strconv.Itoa(i)))
		if err := a.repo.RateQuestion(t.Context(), int64(full), id, 1); err != nil {
			t.Fatalf("rating: %v", err)
		}
	}

	// ---- a remark to moderate, and one already hidden
	first := seedComment(t, a, full)
	second := seedComment(t, a, partial)
	if status, _ := a.post("/admin/comments/"+strconv.FormatInt(second, 10)+"/hide",
		url.Values{}); status != 303 {
		t.Fatal("could not hide a remark")
	}
	_ = first

	// ---- a ticket, triaged, so the inbox and the thread both have content
	tid := seedTicket(t, a)
	if status, _ := a.post("/admin/support/"+tid+"/update", url.Values{
		"status": {models.TicketInProgress}, "priority": {models.PriorityHigh},
		"kind": {models.TicketBug}, "assignee": {"me"},
	}); status != 303 {
		t.Fatal("could not triage")
	}

	// ---- entries in the trail that carry a before and an after
	if status, _ := a.post("/admin/categories/1/retire", url.Values{}); status != 303 {
		t.Fatal("could not retire a category")
	}
	if status, _ := a.post("/admin/categories/1/restore", url.Values{}); status != 303 {
		t.Fatal("could not restore a category")
	}

	pages := map[string]string{}
	for k, v := range adminPaths {
		pages[k] = v
	}
	pages["thread"] = "/admin/support/" + tid
	pages["qedit"] = "/admin/questions/" + strconv.Itoa(full) + "/edit"
	pages["catedit"] = "/admin/categories/1/edit"
	pages["catedit2"] = "/admin/categories/1/edit"
	pages["domedit"] = "/admin/domains/1/edit"

	for name, path := range pages {
		status, body := a.get(path)
		if status != 200 {
			t.Errorf("%-11s %s → %d", name, path, status)
			continue
		}
		for _, want := range componentsOf[name] {
			if !regexp.MustCompile(want.pattern).MatchString(body) {
				t.Errorf("%-11s is missing its %s", name, want.what)
			}
		}
		// A stacking table whose cells are unlabelled shows a column of bare
		// values on a phone with nothing saying what any of them is.
		for _, cell := range unlabelledCells(body) {
			t.Errorf("%-11s has a stacking <td> with no data-label: %s", name, cell)
		}
	}
}

type component struct{ what, pattern string }

// What the design names for each screen, reduced to a marker that has to be in
// the rendered page. Not a style check — it cannot see a layout — but it does
// catch a panel that quietly stopped being rendered, which is the failure a
// screen-by-screen port actually produces.
var componentsOf = map[string][]component{
	"dashboard": {
		{"KPI tiles", `class="kpis"`},
		{"play chart", `class="bars"`},
		{"needs-attention list", `class="list-item"`},
		{"category performance", `class="cat-tile"`},
		{"language split", `class="meter-bar"`},
		{"activity timeline", `class="timeline"`},
		{"coverage table", `class="cov`},
	},
	"users": {
		{"toolbar", `class="toolbar"`},
		{"stacking table", `class="table stack-sm"`},
		{"row actions", `class="cell-actions"`},
		{"role select", `name="role"`},
		{"actions disclosure", `class="menu-wrap disclosure"`},
	},
	"support": {
		{"stat tiles", `class="kpis"`},
		{"status tabs", `class="tabs"`},
		{"topic chips", `class="chip-row"`},
		{"two-pane inbox", `class="inbox`},
		{"ticket rows", `ticket`},
		{"empty thread pane", `class="thread"`},
	},
	"thread": {
		{"two-pane inbox", `show-thread`},
		{"the list beside it", `class="inbox-list"`},
		{"triage controls", `name="status"`},
		{"assignee", `name="assignee"`},
		{"conversation", `class="thread-body"`},
		{"composer", `class="composer"`},
		{"reply or note", `name="internal"`},
		{"send and resolve", `name="resolve"`},
	},
	"domains": {
		{"domain cards", `domain-card`},
		{"counts", `class="stats"`},
		{"language chips", `class="langs"`},
	},
	"categories": {
		{"info callout", `class="callout"`},
		{"filter toolbar", `class="toolbar"`},
		{"stacking table", `class="table stack-sm"`},
		{"state badges", `badge-live|badge-retired`},
		{"drag handle", `class="grip"`},
		{"reorder form", `data-reorder-form`},
	},
	"questions": {
		{"domain filter", `id="q-domain"`},
		{"grouped category select", `<optgroup`},
		{"difficulty pips", `class="pips"`},
		{"coverage chips", `class="langs"`},
		{"state tabs", `class="segmented"`},
		{"result count", `class="result-meta"`},
		{"id links", `class="qid"`},
		{"bulk selection", `data-bulk-item`},
		{"accuracy meter", `class="meter`},
	},
	"review": {
		{"two-pane layout", `class="review-layout"`},
		{"the queue", `class="list-item`},
		{"languages side by side", `class="locale-cols"`},
		{"answers with the correct one marked", `class="answers"`},
		{"automatic checks", `check-pass|check-warn|check-fail`},
		{"approve verdict", `data-key-approve`},
		{"reject verdict", `data-key-reject`},
		{"request-changes disclosure", `disclosure`},
		{"sent-back note", `class="review-note"`},
	},
	"comments": {
		{"state tabs", `class="tab`},
		{"search", `name="q"`},
		{"language filter", `name="loc"`},
		{"comment cards", `class="comment`},
		{"hidden state", `is-hidden`},
		{"question reference", `class="on"`},
		{"resolve action", `/resolve"`},
		{"edit the question", `admin.comments.editQuestion|Edit question|حرّر السؤال`},
	},
	"rated": {
		{"split bars", `class="rating-split"`},
		{"rows", `class="rating-row"`},
	},
	"integrity": {
		{"score ring", `class="score-ring"`},
		{"library panel", `class="side-facts"`},
		{"checks", `class="check-row"`},
		{"severity marks", `class="sev`},
		{"duplicates section", `id="duplicates"`},
		{"language tabs", `class="seg-link`},
		{"duplicate card", `dup-card`},
	},
	"audit": {
		{"day grouping", `class="day-sep"`},
		{"before/after diff", `class="audit-diff"`},
		{"filters", `class="toolbar"`},
		{"export", `export.csv`},
		{"action phrase", `class="act"`},
	},
	"qedit": {
		{"language tabs", `data-tabs`},
		{"a pane per language", `class="locale-pane"`},
		{"locale fields", `name="prompt_ar"`},
		{"points", `name="points"`},
	},
	"catedit2": {
		{"live preview", `data-preview`},
		{"icon picker", `class="icon-picker"`},
		{"colour swatches", `class="swatches"`},
	},
	"catedit": {
		{"domain select", `name="domain_id"`},
		{"colour field", `x-data="colourField"`},
	},
	"newuser": {
		{"role picker", `class="chip-radio"`},
	},
}

// unlabelledCells finds cells in a stacking table that carry no data-label.
// The row that is only a day heading is exempt: it spans the table and labels
// the rows under it.
func unlabelledCells(body string) []string {
	var out []string
	tables := regexp.MustCompile(`(?s)<table[^>]*class="[^"]*stack-sm[^"]*"[^>]*>(.*?)</table>`)
	rows := regexp.MustCompile(`(?s)<tr\b([^>]*)>(.*?)</tr>`)
	cells := regexp.MustCompile(`<td\b([^>]*)>`)
	for _, table := range tables.FindAllStringSubmatch(body, -1) {
		for _, row := range rows.FindAllStringSubmatch(table[1], -1) {
			if strings.Contains(row[1], "day-sep") {
				continue
			}
			for _, cell := range cells.FindAllStringSubmatch(row[2], -1) {
				attrs := cell[1]
				if strings.Contains(attrs, "data-label") ||
					strings.Contains(attrs, "t-check") ||
					strings.Contains(attrs, "cell-actions") ||
					strings.Contains(attrs, "t-grip") {
					continue
				}
				out = append(out, "<td"+attrs+">")
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- helpers --

func ar4() []string { return []string{"أبو بكر", "عمر", "عثمان", "علي"} }
func en4() []string { return []string{"Abu Bakr", "Umar", "Uthman", "Ali"} }
func fr4() []string { return []string{"Abou Bakr", "Omar", "Othman", "Ali"} }

func upsert(t *testing.T, a *app, id, category, difficulty int, active bool,
	tr map[string]models.TranslationDraft) int {
	t.Helper()
	out, err := a.repo.UpsertQuestion(t.Context(), &models.QuestionDraft{
		ID: id, CategoryID: category, Difficulty: difficulty,
		Points: difficulty * 10, CorrectIndex: 2, Source: "test",
		IsActive: active, Translations: tr,
	})
	if err != nil {
		t.Fatalf("seeding a question: %v", err)
	}
	return out
}

// seedPlay records a finished round, which is what the accuracy figures, the
// per-question plays and the fortnight chart are all computed from.
func seedPlay(t *testing.T, a *app, userID uuid.UUID, questions []int) {
	t.Helper()
	game := &models.GameSession{
		UserID: userID, Locale: "en", QuestionIDs: questions, Mode: "solo",
	}
	if err := a.repo.CreateGame(t.Context(), game); err != nil {
		t.Fatalf("seeding a round: %v", err)
	}
	for i, q := range questions {
		if err := a.repo.RecordAnswer(t.Context(), game.ID, &models.GameAnswer{
			QuestionID: q, Position: i, SelectedIndex: i % 4,
			IsCorrect: i%2 == 0, TimeMS: 4000, PointsAwarded: 10,
		}, 0); err != nil {
			t.Fatalf("recording an answer: %v", err)
		}
	}
	if _, err := a.repo.FinishGame(t.Context(), game.ID, 40, "en", userID, 0, 0); err != nil {
		t.Fatalf("finishing the round: %v", err)
	}
}

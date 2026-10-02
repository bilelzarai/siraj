package handlers_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

var linkRe = regexp.MustCompile(`(?:href|action)="(/[^"]*)"`)

// A match round one device, played by somebody with no account, screen for
// screen against the version an account holder gets.
//
// The mechanism was never the problem — the hot seat is the same code whoever
// is holding the phone. What differed was the furniture around it: a Review
// button that bounced a guest to the sign-up page, a next step offered only
// to people with friends, and nothing anywhere saying that the scoreboard
// they were looking at would be gone in the morning.
//
// So this walks the whole flow and follows every link on every screen. A
// guest's page may not offer a door that answers Not Found, and may not
// offer one that leads to a sign-up form unless signing up is what the link
// says it does.
func TestAGuestGetsTheSameMatchAsAnAccount(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 923000, 12)
	guest := startGuest(t, a)

	for _, n := range []string{"kjkj", "jkbbbb"} {
		guest.post("/players", url.Values{"name": {n}})
	}
	host := userOf(t, guest)
	players, _ := a.repo.LocalPlayers(t.Context(), host.ID)

	form := url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "player_source": {"device"}, "request_key": {"parity"},
	}
	for _, p := range players {
		form.Add("local", p.ID.String())
	}
	guest.post("/challenges/new", form)
	match, _ := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")

	// Every link is followed. The exceptions are the ones that are not links
	// to a page: POST-only routes, and the ones that deliberately end the
	// match or the session.
	check := func(label, page string) string {
		status, body := guest.get(page)
		if status != 200 {
			t.Logf("%s: %s -> %d", label, page, status)
			return body
		}
		seen := map[string]bool{}
		for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
			u := m[1]
			if seen[u] || strings.HasPrefix(u, "/static") ||
				strings.Contains(u, "logout") || strings.Contains(u, "quit") ||
				strings.Contains(u, "cancel") || strings.Contains(u, "/leave") ||
				strings.Contains(u, "/settings/locale") || strings.Contains(u, "/play/") ||
				strings.Contains(u, "/messages/new") || strings.Contains(u, "/players") {
				continue
			}
			seen[u] = true
			s, b := guest.get(u)
			if s == 404 {
				t.Errorf("%s: %s -> 404", label, u)
			}
			if s == 303 && (strings.Contains(b, "/register") || strings.Contains(b, "/login")) {
				t.Errorf("%s: %s -> bounced to sign-up", label, u)
			}
		}
		return body
	}

	check("challenges", "/challenges")
	check("setup", "/challenges/new")
	guest.post("/challenges/"+match.ID.String()+"/start", url.Values{})
	check("handover", "/play/round")

	order := append([]*models.User{host}, players...)
	for position := 0; position < len(match.QuestionIDs); position++ {
		for _, p := range order {
			guest.get("/play/round")
			guest.post("/play/seat", url.Values{"player": {p.ID.String()}})
			guest.get("/play/round")
			answerAs(t, guest, position, 0)
		}
	}
	guest.get("/play/round")
	guest.post("/play/finish", url.Values{})

	var sid string
	testPool.QueryRow(t.Context(),
		`SELECT id FROM game_sessions WHERE challenge_id = $1 AND user_id = $2`,
		match.ID, host.ID).Scan(&sid)
	res := check("result", "/play/result/"+sid)
	for _, want := range []string{"kjkj", "jkbbbb", "Review answers", "Create an account"} {
		if !strings.Contains(res, want) {
			t.Errorf("the result page does not show %q", want)
		}
	}
	if !strings.Contains(res, "Create an account to keep your progress") {
		t.Error("the result page does not say the round is temporary")
	}
}

// Nothing a guest wrote outlives them.
//
// Most of it goes by cascade. A question comment and an authored question do
// not: both point at their writer with ON DELETE SET NULL, which is right for
// an account — closing one should not erase a discussion other people joined
// — and wrong for a guest, who was told on the way in that nothing is saved.
// An authorless comment sitting in a thread forever is that promise broken.
func TestASweptGuestLeavesNothingBehind(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 971000, 12)
	guest := startGuest(t, a)
	me := userOf(t, guest)

	guest.post("/play/start", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
	})
	guest.get("/play/round")
	answerRemote(t, guest, 0, 0)

	const said = "something a guest said about this question"
	status := postJSON(t, guest, "/play/comment", `{"body":"`+said+`"}`)
	if status != http.StatusOK {
		t.Fatalf("a guest commenting → %d", status)
	}

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM question_comments WHERE user_id = $1`, me.ID); n != 1 {
		t.Fatalf("the comment was not written: %d rows", n)
	}

	// Their time is up.
	if _, err := testPool.Exec(t.Context(),
		`UPDATE users SET expires_at = now() - interval '1 hour' WHERE id = $1`, me.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.PurgeExpiredGuests(t.Context()); err != nil {
		t.Fatal(err)
	}

	if n := count(`SELECT count(*) FROM users WHERE id = $1`, me.ID); n != 0 {
		t.Error("the guest is still here")
	}
	if n := count(`SELECT count(*) FROM question_comments WHERE body = $1`, said); n != 0 {
		t.Errorf("%d authorless comments outlived the guest who wrote them", n)
	}

	// An account's comment is left alone: anonymised, not erased.
	keeper := newAppSharing(t, a)
	keeper.register("comment_keeper")
	keeper.post("/play/start", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
	})
	keeper.get("/play/round")
	answerRemote(t, keeper, 0, 0)
	const kept = "something an account said about this question"
	if status := postJSON(t, keeper, "/play/comment", `{"body":"`+kept+`"}`); status != http.StatusOK {
		t.Fatalf("an account commenting → %d", status)
	}
	if _, err := a.repo.PurgeExpiredGuests(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM question_comments WHERE body = $1`, kept); n != 1 {
		t.Error("the guest sweep took an account's comment with it")
	}
}

// postJSON sends a body the script would and answers with the status.
func postJSON(t *testing.T, a *app, path, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, a.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", a.csrf())
	res, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

// The setup screen opens a match, even for somebody with no friends.
//
// It used to decide where to post by asking whether the viewer had any — so a
// guest, who has none by definition, reached "New challenge", ticked the two
// people at their device, chose a category, and submitted to the solo
// endpoint. What came back was a round of ten questions on their own, with
// the people they had picked silently dropped. The same happened to any
// account that had joined a room but not yet made a friend.
func TestTheMatchScreenOpensAMatchWithNoFriends(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 972500, 12)

	for _, who := range []struct {
		name  string
		open  func() *app
		label string
	}{
		{"guest", func() *app { return startGuest(t, newAppSharing(t, a)) }, "a guest"},
		{"account", func() *app {
			b := newAppSharing(t, a)
			b.register("friendless_host")
			return b
		}, "an account with no friends"},
	} {
		t.Run(who.name, func(t *testing.T) {
			browser := who.open()
			if status, _ := browser.post("/players", url.Values{"name": {"Someone"}}); status != http.StatusSeeOther {
				t.Fatal("adding a player failed")
			}
			host := userOf(t, browser)

			// The form the screen renders points at the match, not at a
			// solo round.
			status, body := browser.get("/challenges/new")
			if status != http.StatusOK {
				t.Fatalf("the setup screen → %d", status)
			}
			if strings.Contains(body, `action="/play/start"`) {
				t.Fatalf("%s is offered a form that starts a round on their own", who.label)
			}
			if !strings.Contains(body, `action="/challenges/new"`) {
				t.Fatalf("%s is offered no way to open a match", who.label)
			}

			// And submitting it opens one, with the person they picked in it.
			players, err := a.repo.LocalPlayers(t.Context(), host.ID)
			if err != nil || len(players) != 1 {
				t.Fatalf("players at the device = %d (err %v)", len(players), err)
			}
			form := url.Values{
				"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
				"source": {"bank"}, "player_source": {"device"},
				"local": {players[0].ID.String()}, "request_key": {"dest-" + who.name},
			}
			if status, _ := browser.post("/challenges/new", form); status != http.StatusSeeOther {
				t.Fatalf("opening the match → %d", status)
			}
			match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
			if err != nil {
				t.Fatalf("%s got no match out of the match screen: %v", who.label, err)
			}
			if len(match.Players) != 2 {
				t.Errorf("the match has %d players, want the host and the person they picked",
					len(match.Players))
			}

			// The solo screen still opens a solo round.
			_, solo := browser.get("/play")
			if !strings.Contains(solo, `action="/play/start"`) {
				t.Error("the solo screen no longer starts a solo round")
			}
		})
	}
}

// The group bar has to show which group is chosen.
//
// The markup and the script both write is-on; the stylesheet only knew
// is-active, so the selected tab of "Friends / My room / This device" was
// drawn exactly like the two beside it — on the control that decides who the
// invitation goes to.
func TestTheChosenPlayerGroupLooksChosen(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 972800, 12)
	guest := startGuest(t, a)
	guest.post("/players", url.Values{"name": {"Someone"}})

	_, body := guest.get("/challenges/new?group=device")
	if !strings.Contains(body, "segment__item is-on") {
		t.Fatal("no group tab is marked as chosen")
	}

	css, err := os.ReadFile(repoRootOf(t) + "/static/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	// The class the markup writes has to be one the stylesheet answers, on
	// this element, in its resting state. A bare ".is-on" elsewhere in the
	// file is not enough, and neither is a :hover rule — the tab has to look
	// chosen while nobody is pointing at it.
	resting := regexp.MustCompile(`\.segment__item\.is-on\s*[,{]`)
	if !resting.Match(css) {
		t.Error("the stylesheet has no resting-state rule for the chosen group tab")
	}
}

func repoRootOf(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("no go.mod above the working directory")
	return ""
}

package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
)

// Several people round one phone, none of whom has an account.
//
// Anonymous play already had the mechanism — the hot seat is the same code
// whoever is holding the phone — so what these pin down is the part that is
// easy to lose: the last question is reviewed like every other one, and a
// match played in sides is scored by side rather than by four separate names.

// deviceMatch seats everybody named at the device and opens a match between
// them, returning the match and the order the phone goes round in.
func deviceMatch(t *testing.T, a *app, key string, format string, sides []int, names ...string) (*models.Challenge, []*models.User) {
	t.Helper()

	for _, name := range names {
		if status, _ := a.post("/players", url.Values{"name": {name}}); status != http.StatusSeeOther {
			t.Fatalf("adding %s to the device failed", name)
		}
	}
	host := userOf(t, a)
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != len(names) {
		t.Fatalf("players at the device = %d (err %v), want %d", len(guests), err, len(names))
	}

	form := url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "player_source": {"device"}, "request_key": {key},
	}
	if format == models.FormatTeam {
		form.Set("format", models.FormatTeam)
		form.Set("host_team", strconv.Itoa(sides[0]))
	}
	for i, g := range guests {
		form.Add("local", g.ID.String())
		if format == models.FormatTeam {
			form.Set("team_"+g.ID.String(), strconv.Itoa(sides[i+1]))
		}
	}
	if status, _ := a.post("/challenges/new", form); status != http.StatusSeeOther {
		t.Fatal("opening the match failed")
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatalf("no match was opened: %v", err)
	}
	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("starting the match failed")
	}

	order := append([]*models.User{host}, guests...)
	return match, order
}

// takeTurn is one player's whole turn: take the phone, answer, hand it back.
func takeTurn(t *testing.T, a *app, player *models.User, position, choice int) {
	t.Helper()
	if status, _ := a.post("/play/seat",
		url.Values{"player": {player.ID.String()}}); status != http.StatusSeeOther {
		t.Fatalf("q%d: %s could not take the phone", position, player.DisplayName)
	}
	if _, body := a.get("/play/round"); !strings.Contains(body, "data-answers") {
		t.Fatalf("q%d: no question for %s after taking the phone", position, player.DisplayName)
	}
	answerAs(t, a, position, choice)
}

// The last question is reviewed at the table like every other one.
//
// It was the one question nobody ever saw the answer to. The reveal was worked
// out from the players who still had a question left, so the moment the last
// of them committed there were none, and the device went straight to the
// result page — four questions discussed and the fifth skipped.
func TestTheLastQuestionIsRevealedBeforeTheResult(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 964000, 12)
	guest := startGuest(t, a)

	match, order := deviceMatch(t, guest, "last-q", "", nil, "Amina", "Bilal")
	total := len(match.QuestionIDs)

	for position := 0; position < total; position++ {
		for _, player := range order {
			guest.get("/play/round")
			takeTurn(t, guest, player, position, 0)
		}

		// After the last of them, every question gets the same treatment.
		status, body := guest.get("/play/round")
		if status != http.StatusOK {
			t.Fatalf("q%d: the review screen → %d", position, status)
		}
		question, err := a.repo.Question(t.Context(), match.QuestionIDs[position], "en", "en")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, question.Choices[question.CorrectIndex]) {
			t.Errorf("q%d: the answer was never shown", position)
		}
		for _, p := range order {
			if !strings.Contains(body, p.DisplayName) {
				t.Errorf("q%d: the scoreboard leaves out %s", position, p.DisplayName)
			}
		}
	}

	// And only then is the match over.
	if status, _ := guest.post("/play/finish", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the last screen does not lead anywhere")
	}
	settled, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != models.ChallengeCompleted {
		t.Errorf("status = %q after everybody played, want completed", settled.Status)
	}
}

// A match in sides is scored by side, on the handover as well as at the end.
func TestADeviceTeamMatchIsScoredBySide(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 965000, 12)
	guest := startGuest(t, a)

	// Host and Amina against Bilal and Karim.
	match, order := deviceMatch(t, guest, "sides", models.FormatTeam,
		[]int{1, 1, 2, 2}, "Amina", "Bilal", "Karim")
	if match.Format != models.FormatTeam {
		t.Fatalf("format = %q, want a team match", match.Format)
	}

	// Side one answers correctly and side two does not, so the result is not
	// the draw that four identical answers would produce.
	total := len(match.QuestionIDs)
	for position := 0; position < total; position++ {
		question, err := a.repo.Question(t.Context(), match.QuestionIDs[position], "en", "en")
		if err != nil {
			t.Fatal(err)
		}
		wrong := (question.CorrectIndex + 1) % len(question.Choices)

		for i, player := range order {
			guest.get("/play/round")
			choice := question.CorrectIndex
			if i >= 2 {
				choice = wrong
			}
			takeTurn(t, guest, player, position, choice)
		}

		_, body := guest.get("/play/round")
		// The sides, with their totals, above the individual scores.
		for _, want := range []string{"Team 1", "Team 2"} {
			if !strings.Contains(body, want) {
				t.Fatalf("q%d: the handover does not name %s", position, want)
			}
		}
	}

	guest.post("/play/finish", url.Values{})

	settled, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if settled.WinnerTeam == nil {
		t.Fatal("a team match with one side ahead ended without a winning side")
	}
	if *settled.WinnerTeam != 1 {
		t.Errorf("winning side = %d, want 1 — the side that answered correctly", *settled.WinnerTeam)
	}
	// Everything about them is still temporary.
	for _, p := range settled.Players {
		who, err := a.repo.UserByID(t.Context(), p.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if !who.IsTemporary {
			t.Errorf("%s came out of an anonymous match with a permanent account", who.DisplayName)
		}
	}
}

// Nobody sees a score they could answer a question with.
//
// The scoreboard is as much of a leak as the verdict: the next player watching
// the previous one jump from 40 to 68 knows they got it right, on a question
// about to be asked.
func TestTheDeviceScoreboardIsHeldBackMidQuestion(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 966000, 12)
	guest := startGuest(t, a)

	_, order := deviceMatch(t, guest, "held", "", nil, "Amina", "Bilal")

	// The host answers question one; two people still have it.
	guest.get("/play/round")
	takeTurn(t, guest, order[0], 0, 0)

	_, body := guest.get("/play/round")
	if strings.Contains(body, "hotseat.standing") || strings.Contains(body, "Scores so far") {
		t.Error("the scoreboard is on screen while the question is still being played")
	}
	if strings.Contains(body, "Question 1 — everybody has answered") {
		t.Error("the answer was revealed before everybody had answered")
	}

	// The second answers, the third still has it.
	takeTurn(t, guest, order[1], 0, 0)
	_, body = guest.get("/play/round")
	if strings.Contains(body, "Scores so far") {
		t.Error("the scoreboard appeared before the last player had answered")
	}

	// The third answers, and now it is everybody's business.
	takeTurn(t, guest, order[2], 0, 0)
	_, body = guest.get("/play/round")
	if !strings.Contains(body, "Scores so far") {
		t.Error("the scoreboard is still hidden after everybody answered")
	}
}

// userOf is whoever the browser is signed in as, account or guest.
func userOf(t *testing.T, a *app) *models.User {
	t.Helper()
	base, err := url.Parse(a.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var sessionID string
	for _, c := range a.client.Jar.Cookies(base) {
		if c.Name == "siraj_session" {
			sessionID = c.Value
		}
	}
	if sessionID == "" {
		t.Fatal("this browser is not signed in")
	}
	var id uuid.UUID
	if err := testPool.QueryRow(t.Context(),
		`SELECT user_id FROM sessions WHERE id = $1`, sessionID).Scan(&id); err != nil {
		t.Fatalf("resolving the session: %v", err)
	}
	who, err := a.repo.UserByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return who
}

// answerAs is answerHotSeat with a say in what is chosen.
func answerAs(t *testing.T, a *app, position, choice int) {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(
		`{"position":%d,"choice":%d,"timeMs":1200}`, position, choice))
	req, err := http.NewRequest(http.MethodPost, a.server.URL+"/play/answer", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("X-CSRF-Token", a.csrf())

	res, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("answering: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("answering position %d → %d", position, res.StatusCode)
	}
	var out answerReply
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	if !out.Held {
		t.Errorf("a hot-seat answer came back with its verdict rather than held")
	}
}

// A team match played by four people on four phones settles on the side with
// the higher total, not on the single best player.
//
// The device tests above never exercise this: there the whole match is one
// browser, so the ordering that decides when a match is settled — everybody
// invited has answered — is trivially satisfied. Remotely it is the thing
// most likely to be wrong.
func TestARemoteTeamMatchIsWonBySide(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 967000, 12)
	a.register("side_host")

	mate := newAppSharing(t, a)
	mate.register("side_mate")
	rival := newAppSharing(t, a)
	rival.register("side_rival")
	ally := newAppSharing(t, a)
	ally.register("side_ally")

	host, err := a.repo.UserByUsername(t.Context(), "side_host")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"side_mate", "side_rival", "side_ally"} {
		who, err := a.repo.UserByUsername(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		befriendThrough(t, a, host.ID, who.ID)
	}

	// Host and their mate on side one, the rival and their ally on side two.
	status, body := a.postJSONForm(t, "/challenges/new", url.Values{
		"opponent": {"side_mate", "side_rival", "side_ally"}, "player_source": {"friends"},
		"format": {"team"}, "host_team": {"1"},
		"team_side_mate": {"1"}, "team_side_rival": {"2"}, "team_side_ally": {"2"},
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "request_key": {"remote-sides"},
	})
	if status != http.StatusOK {
		t.Fatalf("opening a team match → %d (%s)", status, clip(body, 160))
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatal(err)
	}

	for _, who := range []*app{mate, rival, ally} {
		if status, _ := who.post("/challenges/"+match.ID.String()+"/accept", url.Values{}); status != http.StatusSeeOther {
			t.Fatal("an invitee could not accept")
		}
	}
	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("the host could not start the match")
	}

	// Side one answers correctly, side two does not.
	total := len(match.QuestionIDs)
	play := func(who *app, correct bool) {
		for position := 0; position < total; position++ {
			question, err := a.repo.Question(t.Context(), match.QuestionIDs[position], "en", "en")
			if err != nil {
				t.Fatal(err)
			}
			choice := question.CorrectIndex
			if !correct {
				choice = (question.CorrectIndex + 1) % len(question.Choices)
			}
			who.get("/play/round")
			answerRemote(t, who, position, choice)
		}
		who.post("/play/finish", url.Values{})
	}
	play(a, true)
	play(mate, true)
	play(rival, false)
	play(ally, false)

	settled, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != models.ChallengeCompleted {
		t.Fatalf("status = %q after all four played, want completed", settled.Status)
	}
	if settled.WinnerID != nil {
		t.Error("a team match named an individual winner")
	}
	if settled.WinnerTeam == nil {
		t.Fatal("a team match ended with no winning side")
	}
	if *settled.WinnerTeam != 1 {
		t.Errorf("winning side = %d, want 1", *settled.WinnerTeam)
	}

	// The side won on its total, which is the point: the rival may well have
	// been beaten by a pair who each scored less than they did.
	byTeam := map[int]int{}
	for _, p := range settled.Players {
		if p.Score != nil {
			byTeam[p.Team] += *p.Score
		}
	}
	if byTeam[1] <= byTeam[2] {
		t.Errorf("side totals came out %v, which does not match the declared winner", byTeam)
	}
}

// answerRemote is one answer from somebody playing on their own phone, where
// the verdict comes back rather than being held.
func answerRemote(t *testing.T, a *app, position, choice int) {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(
		`{"position":%d,"choice":%d,"timeMs":1200}`, position, choice))
	req, err := http.NewRequest(http.MethodPost, a.server.URL+"/play/answer", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("X-CSRF-Token", a.csrf())

	res, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("answering: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("answering position %d → %d", position, res.StatusCode)
	}
}

// A team match opens on a split the server will accept.
//
// Sides used to default to one for everybody except the people at your
// device, so a team match with friends or with a room opened with all four on
// the same side and was refused the moment it was sent — one-sided — with
// nothing on screen having said so. The person setting it up had to move half
// the names by hand to discover what the form wanted.
func TestATeamMatchOpensOnAWorkableSplit(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 973500, 12)
	a.register("split_host")
	host, err := a.repo.UserByUsername(t.Context(), "split_host")
	if err != nil {
		t.Fatal(err)
	}

	// Three friends, so a team match is possible at all.
	for _, name := range []string{"split_one", "split_two", "split_three"} {
		mate := newAppSharing(t, a)
		mate.register(name)
		who, err := a.repo.UserByUsername(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		befriendThrough(t, a, host.ID, who.ID)
	}
	// And three people at the device, for the other group.
	for _, name := range []string{"Amal", "Bilqis", "Hind"} {
		if status, _ := a.post("/players", url.Values{"name": {name}}); status != http.StatusSeeOther {
			t.Fatalf("adding %s failed", name)
		}
	}

	selected := regexp.MustCompile(`<option value="(\d)" selected`)

	for _, group := range []string{"friends", "device", "room"} {
		t.Run(group, func(t *testing.T) {
			status, body := a.get("/challenges/new?group=" + group)
			if status != http.StatusOK {
				t.Fatalf("the setup screen for %s → %d", group, status)
			}
			// Each team row's pre-selected side, in order, plus the host on
			// side one. Rendered sides, not sides somebody had to choose.
			rows := regexp.MustCompile(`data-team-row="[^"]*"[\s\S]*?</label>`).
				FindAllString(body, -1)
			if len(rows) == 0 {
				if group == "room" {
					t.Skip("no room joined, so no room rows to arrange")
				}
				t.Fatalf("the %s group rendered no side pickers", group)
			}

			sides := map[string]int{"1": 1} // the host
			for _, row := range rows {
				m := selected.FindStringSubmatch(row)
				if m == nil {
					t.Fatalf("a %s side picker has nothing selected", group)
				}
				sides[m[1]]++
			}
			if len(sides) < 2 {
				t.Errorf("the %s group opens with everybody on one side: %v", group, sides)
			}
			for side, n := range sides {
				if n < models.MinPerSide {
					t.Errorf("the %s group opens with %d on side %s, and the server wants %d",
						group, n, side, models.MinPerSide)
				}
			}
		})
	}
}

// Start, on a match played round one device, starts it.
//
// It used to build the match and then park it on the challenge list behind a
// second button. That list is the right screen for a match with people on
// other phones — the host waits for them to accept, then sets everyone going
// at once — and the wrong one here: the players are standing in the room,
// there is no invitation, nothing to accept and nobody to wait for. So the
// list was a stop on the way to the handover, and the button on it asked a
// question with one answer.
//
// What a guest actually saw was worse than an extra press. The form posted to
// the solo endpoint (fixed separately), so Start produced a round of ten
// questions on their own with the people they had picked silently dropped.
// This pins down where Start lands, for one player at the device and for
// three.
func TestStartingADeviceMatchGoesStraightToTheHandover(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 974500, 12)

	for _, players := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("%d_at_the_device", players), func(t *testing.T) {
			guest := startGuest(t, newAppSharing(t, a))
			names := []string{"Amal", "Bilqis", "Hind"}[:players]
			for _, name := range names {
				if status, _ := guest.post("/players", url.Values{"name": {name}}); status != http.StatusSeeOther {
					t.Fatalf("adding %s failed", name)
				}
			}
			host := userOf(t, guest)
			guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
			if err != nil || len(guests) != players {
				t.Fatalf("players at the device = %d (err %v)", len(guests), err)
			}

			form := url.Values{
				"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
				"source": {"bank"}, "player_source": {"device"},
				"request_key": {fmt.Sprintf("straight-%d", players)},
			}
			for _, g := range guests {
				form.Add("local", g.ID.String())
			}

			status, where := guest.post("/challenges/new", form)
			if status != http.StatusSeeOther {
				t.Fatalf("Start → %d", status)
			}
			// Start goes to /challenges, and /challenges hands the phone on
			// rather than drawing a list about a match that is already under
			// way. Both halves are asserted, because the destination that
			// matters is the one the player ends up looking at.
			if where != "/challenges" {
				t.Fatalf("Start landed at %q, want /challenges", where)
			}
			code, onward := guest.get(where)
			if code != http.StatusSeeOther {
				t.Fatalf("/challenges during a live device match → %d, want the handover", code)
			}
			if !strings.Contains(onward, "/play/round") {
				t.Fatalf("/challenges sent the phone to %q, want the handover", clip(onward, 80))
			}

			// The handover, naming the first player, with no question drawn
			// until somebody takes the phone.
			code, body := guest.get("/play/round")
			if code != http.StatusOK {
				t.Fatalf("the handover → %d", code)
			}
			if !strings.Contains(body, host.DisplayName) {
				t.Error("the handover does not name the first player")
			}
			if strings.Contains(body, "data-answers") {
				t.Error("the question was drawn before anybody took the phone")
			}

			// Turn order is the host, then the guests as they were added.
			round, err := a.repo.ActiveGame(t.Context(), host.ID, "en")
			if err != nil {
				t.Fatalf("the host has no round: %v", err)
			}
			if round.ChallengeID == nil {
				t.Error("the round that was opened is not part of a match")
			}
			for _, g := range guests {
				if _, err := a.repo.ActiveGame(t.Context(), g.ID, "en"); err != nil {
					t.Errorf("%s was left without a round: %v", g.DisplayName, err)
				}
			}

			// A refresh restores the same screen rather than moving anything on.
			code, again := guest.get("/play/round")
			if code != http.StatusOK || again != body {
				t.Error("refreshing the handover did not restore the same screen")
			}
		})
	}
}

// Pressing Start twice makes one match, not two.
func TestPressingStartAgainDoesNotOpenASecondMatch(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 974800, 12)
	guest := startGuest(t, a)
	if status, _ := guest.post("/players", url.Values{"name": {"Amal"}}); status != http.StatusSeeOther {
		t.Fatal("adding a player failed")
	}
	host := userOf(t, guest)
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != 1 {
		t.Fatalf("players at the device = %d (err %v)", len(guests), err)
	}

	form := url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "player_source": {"device"},
		"local": {guests[0].ID.String()}, "request_key": {"one-press"},
	}
	for press := 1; press <= 4; press++ {
		status, where := guest.post("/challenges/new", form)
		if status != http.StatusSeeOther {
			t.Errorf("press %d → %d", press, status)
		}
		if where != "/challenges" {
			t.Errorf("press %d landed at %q", press, where)
		}
	}

	var matches, rounds int
	if err := testPool.QueryRow(t.Context(),
		`SELECT count(*) FROM challenges WHERE host_id = $1`, host.ID).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(t.Context(),
		`SELECT count(*) FROM game_sessions WHERE user_id = $1`, host.ID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if matches != 1 {
		t.Errorf("four presses opened %d matches, want 1", matches)
	}
	if rounds != 1 {
		t.Errorf("four presses opened %d rounds for the host, want 1", rounds)
	}
}

// A match of one is not a match, and the refusal names the group it is about.
func TestADeviceMatchNeedsSomebodyElseInIt(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 974900, 12)
	guest := startGuest(t, a)

	status, body := guest.postJSONForm(t, "/challenges/new", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "player_source": {"device"}, "request_key": {"alone"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("a match with nobody in it → %d, want a refusal", status)
	}
	// "Pick at least one friend" is no help on the device tab and actively
	// wrong for a guest, who cannot have one.
	if strings.Contains(body, "friend") {
		t.Errorf("the refusal talks about friends on the device tab: %s", clip(body, 120))
	}
	if _, err := a.repo.ActiveMatchFor(t.Context(), userOf(t, guest).ID, "en"); err == nil {
		t.Error("a match of one was opened")
	}
}

// The start button says which of the two things it does.
//
// Both screens are the same template, and both used to read "Start" — the one
// word that does not distinguish a round on your own from a match with three
// other people. For a guest that mattered more than for anybody else: with no
// friends they have no other way to tell the screens apart.
func TestTheStartButtonSaysWhatItStarts(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 975000, 12)
	guest := startGuest(t, a)

	button := regexp.MustCompile(`(?s)<button[^>]*data-start-button[^>]*>(.*?)</button>`)
	tagged := regexp.MustCompile(`<[^>]*>`)
	label := func(body string) (string, bool) {
		m := button.FindStringSubmatch(body)
		if m == nil {
			return "", false
		}
		return strings.TrimSpace(tagged.ReplaceAllString(m[1], "")), strings.Contains(m[0], "disabled")
	}

	// Solo: no count, no challenge wording.
	status, body := guest.get("/play")
	if status != http.StatusOK {
		t.Fatalf("the solo screen → %d", status)
	}
	if !strings.Contains(body, "Start play") {
		t.Error("the solo screen does not say what its button starts")
	}
	if regexp.MustCompile(`Start[^<]*\(\d`).MatchString(body) {
		t.Error("the solo screen shows a player count")
	}

	// A match with nobody in it yet: refused before it is pressed, and said.
	status, body = guest.get("/challenges/new")
	if status != http.StatusOK {
		t.Fatalf("the match screen → %d", status)
	}
	text, disabled := label(body)
	if !disabled {
		t.Error("the start button is live on a match with nobody else in it")
	}
	if !strings.Contains(body, "Add at least 2 players") {
		t.Error("nothing on the page says why the button will not go")
	}
	if !strings.Contains(text, "challenge") {
		t.Errorf("the match screen's button reads %q", text)
	}

	// Never a format string, in any language. This is the fault that shipped
	// once already: a counted label used somewhere it was not given a count.
	for _, locale := range []string{"en", "fr", "ar"} {
		_, page := guest.get("/challenges/new?lang=" + locale)
		if strings.Contains(page, "%d") {
			t.Errorf("the %s match screen shows a raw format string", locale)
		}
		m := button.FindStringSubmatch(page)
		if m == nil {
			t.Fatalf("the %s match screen has no start button", locale)
		}
		// The two sentences the script rebuilds the label from have to be
		// there, and each has to carry a number for it to swap out.
		for _, attr := range []string{"data-label-one", "data-label-many"} {
			val := regexp.MustCompile(attr + `="([^"]*)"`).FindStringSubmatch(m[0])
			if val == nil || val[1] == "" {
				t.Errorf("the %s button carries no %s", locale, attr)
				continue
			}
			if !regexp.MustCompile(`\d`).MatchString(val[1]) {
				t.Errorf("the %s %s has no number to substitute: %q", locale, attr, val[1])
			}
			if strings.Contains(val[1], "%") {
				t.Errorf("the %s %s still holds a format verb: %q", locale, attr, val[1])
			}
		}
	}
}

// Solo start lands on the solo round, not on anything to do with a challenge.
func TestSoloStartLandsOnTheRound(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 975500, 12)
	guest := startGuest(t, a)

	status, where := guest.post("/play/start", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("Start play → %d", status)
	}
	if where != "/play/round" {
		t.Fatalf("Start play landed at %q, want /play/round", where)
	}

	status, body := guest.get("/play/round")
	if status != http.StatusOK {
		t.Fatalf("the round → %d", status)
	}
	if !strings.Contains(body, "data-answers") {
		t.Error("the solo round does not open on a question")
	}
	if strings.Contains(body, "Pass the device") {
		t.Error("a solo round opened on the pass-the-device screen")
	}

	// Refreshing restores the same question, at the same position, with the
	// clock carried over rather than restarted — a solo round is resumable,
	// so the elapsed time is the one thing that should have moved on.
	position := regexp.MustCompile(`data-position="(\d+)"`)
	elapsed := regexp.MustCompile(`data-elapsed="(\d+)"`)
	_, again := guest.get("/play/round")

	was, now := position.FindStringSubmatch(body), position.FindStringSubmatch(again)
	if was == nil || now == nil {
		t.Fatal("the round screen does not say which question it is on")
	}
	if was[1] != now[1] {
		t.Errorf("refreshing moved the round from question %s to %s", was[1], now[1])
	}
	prompt := regexp.MustCompile(`(?s)<h1[^>]*class="question__prompt"[^>]*>(.*?)</h1>`)
	if a, b := prompt.FindStringSubmatch(body), prompt.FindStringSubmatch(again); a != nil && b != nil && a[1] != b[1] {
		t.Error("refreshing drew a different question")
	}
	if elapsed.FindStringSubmatch(again) == nil {
		t.Error("the restored round has no clock")
	}
}

// Naming a tab reaches that tab, match or no match.
//
// The default landing hands the phone on when a match is live here, and that
// is right for somebody in the middle of one. Forwarding every visit would
// make the finished list unreachable for as long as a match is open.
func TestANamedTabIsNotSweptIntoTheRound(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 975800, 12)
	guest := startGuest(t, a)
	if status, _ := guest.post("/players", url.Values{"name": {"Amal"}}); status != http.StatusSeeOther {
		t.Fatal("adding a player failed")
	}
	host := userOf(t, guest)
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != 1 {
		t.Fatalf("players at the device = %d (err %v)", len(guests), err)
	}
	if status, _ := guest.post("/challenges/new", url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "player_source": {"device"},
		"local": {guests[0].ID.String()}, "request_key": {"named-tab"},
	}); status != http.StatusSeeOther {
		t.Fatal("opening the match failed")
	}

	// The default landing goes to the phone.
	if status, _ := guest.get("/challenges"); status != http.StatusSeeOther {
		t.Errorf("the default landing → %d during a live device match, want the handover", status)
	}
	// A named one does not.
	for _, tab := range []string{"incoming", "outgoing", "finished"} {
		status, _ := guest.get("/challenges?tab=" + tab)
		if status != http.StatusOK {
			t.Errorf("/challenges?tab=%s → %d, want the list", tab, status)
		}
	}
}

// The match screen always carries the means of arranging a match.
//
// A closed loop, and one I made: the "Who is playing" block was hidden until
// somebody was pickable, and the panel for adding people to the device had
// just been moved inside it. A new player — which every guest is — opened the
// screen to "Add at least 2 players to start" printed under a form with
// nothing to add them with, and no tab to reach one.
//
// Friends and the room stay shut when they are empty, because there is
// nothing to pick and nothing this screen can do about it. The device is not
// a list, it is a place: you can always add somebody to the phone in your
// hand, so that tab is always open and it is where an empty screen starts.
func TestTheMatchScreenAlwaysOffersAWayToAddSomebody(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 976000, 12)

	deviceTabShut := regexp.MustCompile(`value="device"[^>]*disabled`)
	devicePool := regexp.MustCompile(`<div data-source-pool="device"[^>]*>`)

	for _, who := range []struct {
		name string
		open func() *app
	}{
		{"guest", func() *app { return startGuest(t, newAppSharing(t, a)) }},
		{"account", func() *app {
			b := newAppSharing(t, a)
			b.register("empty_setup_host")
			return b
		}},
	} {
		t.Run(who.name, func(t *testing.T) {
			browser := who.open()

			status, body := browser.get("/challenges/new")
			if status != http.StatusOK {
				t.Fatalf("the match screen → %d", status)
			}

			// The way to add somebody, on a screen with nobody on it. It
			// is in the panel above, which is its own form outside the one
			// that sets the round up.
			if !strings.Contains(body, `action="/players"`) {
				t.Error("no way to add a player to this device")
			}
			// And a tab to reach it by, which is not shut.
			if !strings.Contains(body, `value="device"`) {
				t.Fatal("there is no device tab")
			}
			if deviceTabShut.MatchString(body) {
				t.Error("the device tab is disabled on a screen whose whole job is filling it")
			}
			// It is the tab that opens, since it is the only actionable one.
			tag := devicePool.FindString(body)
			if tag == "" {
				t.Fatal("the device group did not render")
			}
			if strings.Contains(tag, "hidden") {
				t.Errorf("the device group opens hidden with nothing else to show: %s", tag)
			}

			// Adding one works from here, and then they can be ticked.
			if status, _ := browser.post("/players", url.Values{"name": {"Amal"}}); status != http.StatusSeeOther {
				t.Fatal("adding a player from the match screen failed")
			}
			_, body = browser.get("/challenges/new")
			if !strings.Contains(body, "Amal") {
				t.Error("the player who was just added is not offered")
			}
		})
	}
}

// And the solo screen does not grow a device panel from any of this.
func TestTheSoloScreenHasNoDevicePanel(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 976500, 12)
	guest := startGuest(t, a)
	if status, _ := guest.post("/players", url.Values{"name": {"Amal"}}); status != http.StatusSeeOther {
		t.Fatal("adding a player failed")
	}

	status, body := guest.get("/play")
	if status != http.StatusOK {
		t.Fatalf("the solo screen → %d", status)
	}
	for _, unwanted := range []string{"Playing on this device", `action="/players"`, `value="device"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the solo screen carries %q, which is about a match", unwanted)
		}
	}
}

// The device tab is one list, and its buttons belong to forms that exist.
//
// Two faults, both from moving the panel inside the group it describes. It
// listed everybody as a chip with a ✕ and then listed them again underneath
// as tick-boxes — one guest, three appearances of their name, two different
// questions asked without either being named.
//
// And it brought its forms with it, inside the form that sets the round up. A
// form inside a form is not something HTML has: the parser drops the inner
// tag and every control in it is adopted by the outer one, so "Add" and "✕"
// submitted the challenge instead of adding and dropping a player.
//
// Lifting the panel back above the card fixes the HTML and reopens the older
// complaint — "Playing on this device" heading a challenge to a friend three
// cities away. So the controls stay in the device tab and the forms they
// belong to are declared outside it, named by id. That is what the form
// attribute is for.
func TestTheDeviceTabIsOneListWithWorkingButtons(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 977000, 12)
	guest := startGuest(t, a)
	for _, name := range []string{"Amal", "Bilqis"} {
		if status, _ := guest.post("/players", url.Values{"name": {name}}); status != http.StatusSeeOther {
			t.Fatalf("adding %s failed", name)
		}
	}

	status, body := guest.get("/challenges/new?group=device")
	if status != http.StatusOK {
		t.Fatalf("the match screen → %d", status)
	}

	// No form opens between the setup form and its close.
	form := strings.Index(body, "data-setup")
	if form < 0 {
		t.Fatal("the setup form is not there")
	}
	end := strings.Index(body[form:], "</form>")
	if end < 0 {
		t.Fatal("the setup form is never closed")
	}
	if inner := regexp.MustCompile(`<form[^>]*>`).FindAllString(body[form:form+end], -1); len(inner) > 0 {
		t.Errorf("%d form(s) nested inside the form that sets the round up: %v", len(inner), inner)
	}

	// Every control that names a form points at one that is on the page.
	owners := regexp.MustCompile(`\bform="([^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(owners) == 0 {
		t.Fatal("no control is associated with a form by id, so add and drop have no owner")
	}
	for _, m := range owners {
		if !strings.Contains(body, `id="`+m[1]+`"`) {
			t.Errorf("a control belongs to form %q, which is not on the page", m[1])
		}
	}

	// The device controls are in the device group, not above the whole form.
	pool := strings.Index(body, `data-source-pool="device"`)
	add := strings.Index(body, `form="add-player"`)
	if pool < 0 || add < 0 {
		t.Fatal("the device group or its add control is missing")
	}
	if add < pool {
		t.Error("the add control sits outside the device group")
	}

	// One tick-box each, and one way to drop each — not a chip list as well.
	if boxes := strings.Count(body, `<input type="checkbox" name="local"`); boxes != 2 {
		t.Errorf("%d device tick-boxes for 2 people", boxes)
	}
	if drops := strings.Count(body, "seat__drop"); drops != 2 {
		t.Errorf("%d ways to drop a player, want one each", drops)
	}

	// The host plays and is not offered as a choice.
	host := userOf(t, guest)
	if !strings.Contains(body, host.DisplayName) {
		t.Error("the host is not shown among the people at the device")
	}
	if strings.Contains(body, `name="local" value="`+host.ID.String()) {
		t.Error("the host is offered as somebody to tick, and they are always in")
	}

	// Both actions still work through their own routes.
	if status, _ := guest.post("/players", url.Values{"name": {"Hind"}}); status != http.StatusSeeOther {
		t.Fatal("adding from the setup screen failed")
	}
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != 3 {
		t.Fatalf("players at the device = %d (err %v), want 3", len(guests), err)
	}
	if status, _ := guest.post("/players/"+guests[0].ID.String()+"/remove", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("dropping from the setup screen failed")
	}
	if guests, err = a.repo.LocalPlayers(t.Context(), host.ID); err != nil || len(guests) != 2 {
		t.Fatalf("players after a drop = %d (err %v), want 2", len(guests), err)
	}
}

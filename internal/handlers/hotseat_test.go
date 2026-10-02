package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// A match played round one phone, a question at a time.
//
// What the test is actually pinning down is the order and the secrecy: the
// phone goes round the table per question rather than per player, and the
// answer to a question is not on screen until everybody at the device has
// committed to theirs. Both are things a refactor could quietly lose while
// every page still returned 200.

func TestHotSeatGoesRoundThePlayersOnEveryQuestion(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 962000, 12)
	a.register("hotseat_host")

	host, err := a.repo.UserByUsername(t.Context(), "hotseat_host")
	if err != nil {
		t.Fatal(err)
	}

	// Two guests at the device, so the rotation is host → Amina → Bilal.
	for _, name := range []string{"Amina", "Bilal"} {
		if status, _ := a.post("/players", url.Values{"name": {name}}); status != http.StatusSeeOther {
			t.Fatalf("adding %s: %d", name, status)
		}
	}
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != 2 {
		t.Fatalf("local players = %d (err %v), want 2", len(guests), err)
	}
	order := []*models.User{host, guests[0], guests[1]}

	// A match between the three of them, all on this device.
	form := url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "request_key": {"hotseat-match"},
	}
	for _, g := range guests {
		form.Add("local", g.ID.String())
	}
	if status, _ := a.post("/challenges/new", form); status != http.StatusSeeOther {
		t.Fatalf("opening the match: %d", status)
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatalf("no match was opened: %v", err)
	}
	if len(match.Players) != 3 {
		t.Fatalf("match has %d players, want 3", len(match.Players))
	}

	// The host sets it going, which opens a round for everybody at the device
	// at once — nobody has an invitation to accept, because nobody was invited
	// anywhere.
	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("starting the match failed")
	}
	for _, p := range order {
		if _, err := a.repo.ActiveGame(t.Context(), p.ID, "en"); err != nil {
			t.Fatalf("%s has no round: %v", p.DisplayName, err)
		}
	}

	total := len(match.QuestionIDs)
	if total < 5 {
		t.Fatalf("the match drew %d questions, want at least 5", total)
	}

	// Play it out. Every question goes round all three before the next one
	// starts, and that is what is asserted at each step.
	for position := 0; position < total; position++ {
		for _, player := range order {
			// The handover names the player whose turn it is.
			status, body := a.get("/play/round")
			if status != http.StatusOK {
				t.Fatalf("q%d, %s: the round screen → %d", position, player.DisplayName, status)
			}
			if !strings.Contains(body, player.DisplayName) {
				t.Fatalf("q%d: the handover does not name %s", position, player.DisplayName)
			}

			// Nothing on the handover may give the answer away.
			if position > 0 && strings.Contains(body, "data-answers") {
				t.Fatalf("q%d, %s: the question was drawn without the phone being taken",
					position, player.DisplayName)
			}

			// Take the phone, which is also what starts their clock.
			if status, _ := a.post("/play/seat",
				url.Values{"player": {player.ID.String()}}); status != http.StatusSeeOther {
				t.Fatalf("q%d, %s: taking the phone failed", position, player.DisplayName)
			}

			// Now — and only now — the question is on screen.
			status, body = a.get("/play/round")
			if status != http.StatusOK || !strings.Contains(body, "data-answers") {
				t.Fatalf("q%d, %s: no question after taking the phone (%d)",
					position, player.DisplayName, status)
			}

			// Nobody runs ahead. In a hot seat everybody is on the question in
			// play or has just finished it, so a cursor further on than that
			// means somebody was served two questions in a row.
			for _, other := range order {
				round, err := a.repo.ActiveGame(t.Context(), other.ID, "en")
				if err != nil {
					continue // they may have finished the whole round
				}
				if round.Cursor > position+1 {
					t.Fatalf("q%d: %s has run ahead to question %d",
						position, other.DisplayName, round.Cursor)
				}
			}

			answerHotSeat(t, a, position)
		}
	}

	// The last answer goes back to /play/round like every other one, and what
	// is there is the last screen of the match rather than a redirect: the
	// final question reviewed at the table, the scores, and the way on.
	status, body := a.get("/play/round")
	if status != http.StatusOK {
		t.Fatalf("the last question did not get a screen of its own: %d", status)
	}
	last, err := a.repo.Question(t.Context(), match.QuestionIDs[total-1], "en", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, last.Choices[last.CorrectIndex]) {
		t.Error("the last question's answer was never shown")
	}
	for _, p := range order {
		if !strings.Contains(body, p.DisplayName) {
			t.Errorf("the final scoreboard leaves out %s", p.DisplayName)
		}
	}

	// Pressing on from it closes every round at the device.
	if status, _ := a.post("/play/finish", url.Values{}); status != http.StatusSeeOther {
		t.Fatalf("the way out of the last screen → %d", status)
	}

	// Everybody has played, so the match is settled and scored.
	settled, err := a.repo.Challenge(t.Context(), match.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != models.ChallengeCompleted {
		t.Errorf("match status = %q after everybody played, want completed", settled.Status)
	}
	for _, p := range settled.Players {
		if p.Score == nil {
			t.Errorf("%s finished without a score", p.UserID)
		}
	}
}

// The verdict is held back while other people at the device still have the
// question to answer, and the answer appears on the handover once they have.
func TestHotSeatHoldsTheAnswerBackUntilEverybodyHasCommitted(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 963000, 12)
	a.register("hotseat_secret")

	host, err := a.repo.UserByUsername(t.Context(), "hotseat_secret")
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := a.post("/players", url.Values{"name": {"Khadija"}}); status != http.StatusSeeOther {
		t.Fatal("adding a player failed")
	}
	guests, err := a.repo.LocalPlayers(t.Context(), host.ID)
	if err != nil || len(guests) != 1 {
		t.Fatalf("local players = %d (err %v), want 1", len(guests), err)
	}

	form := url.Values{
		"category": {"0"}, "difficulty": {"0"}, "count": {"5"},
		"source": {"bank"}, "local": {guests[0].ID.String()},
		"request_key": {"hotseat-secret"},
	}
	if status, _ := a.post("/challenges/new", form); status != http.StatusSeeOther {
		t.Fatal("opening the match failed")
	}
	match, err := a.repo.ActiveMatchFor(t.Context(), host.ID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := a.post("/challenges/"+match.ID.String()+"/start", url.Values{}); status != http.StatusSeeOther {
		t.Fatal("starting the match failed")
	}

	// The host answers question one.
	a.get("/play/round")
	a.post("/play/seat", url.Values{"player": {host.ID.String()}})
	a.get("/play/round")
	reply := answerHotSeat(t, a, 0)

	// The response must carry no verdict at all — the next player is standing
	// right there.
	if !reply.Held {
		t.Error("a hot-seat answer came back with its verdict rather than held")
	}
	if reply.Explanation != "" || reply.CorrectIndex != 0 || reply.Correct {
		t.Errorf("the held response leaked a verdict: %+v", reply)
	}

	// The handover to the guest must not show the answer either.
	_, body := a.get("/play/round")
	if strings.Contains(body, "hotseat.roundUp") {
		t.Error("the handover revealed the question before everybody answered")
	}
	if !strings.Contains(body, "Khadija") {
		t.Error("the handover does not name the player the phone is for")
	}

	// The guest answers it, and now the reveal is due.
	a.post("/play/seat", url.Values{"player": {guests[0].ID.String()}})
	a.get("/play/round")
	answerHotSeat(t, a, 0)

	_, body = a.get("/play/round")
	question, err := a.repo.Question(t.Context(), match.QuestionIDs[0], "en", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, question.Choices[question.CorrectIndex]) {
		t.Error("the handover did not reveal the answer once everybody had committed")
	}
}

// answerHotSeat submits the answer the play screen would, and returns what came
// back.
func answerHotSeat(t *testing.T, a *app, position int) answerReply {
	t.Helper()

	body := strings.NewReader(fmt.Sprintf(
		`{"position":%d,"choice":0,"timeMs":1200}`, position))
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
	return out
}

type answerReply struct {
	Correct      bool   `json:"correct"`
	CorrectIndex int    `json:"correctIndex"`
	Explanation  string `json:"explanation"`
	Score        int    `json:"score"`
	Finished     bool   `json:"finished"`
	NextURL      string `json:"nextUrl"`
	Held         bool   `json:"held"`
}

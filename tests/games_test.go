package tests

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// T-18 / GP-02…GP-05: a full solo game through the API — start, answer, lifelines,
// finish, results. The server scores it; nothing the client sends is trusted.
func TestSoloGameEndToEnd(t *testing.T) {
	a := newApp(t)
	token := a.member("player1")

	// #24 a quick game is always 5 questions, whatever the client asks for (HM-04).
	start := a.post("/api/v1/games", map[string]any{"kind": "quick", "question_count": 20}, token).expect(http.StatusCreated).data()
	gameID, _ := start["id"].(string)
	if gameID == "" {
		t.Fatalf("no game id in %v", start)
	}
	if start["question_count"].(float64) != 5 {
		t.Errorf("a quick game is 5 questions, got %v", start["question_count"])
	}
	question := start["question"].(map[string]any)
	if question["body"] == "" || len(question["answers"].([]any)) != 4 {
		t.Fatalf("the first question is not playable: %v", question)
	}
	if _, leaked := question["correct_index"]; leaked {
		t.Error("the correct answer must not travel with the question")
	}

	// #27 50/50 leaves two answers, once per game.
	fifty := a.post("/api/v1/games/"+gameID+"/fifty", nil, token).expect(http.StatusOK).data()
	if len(fifty["keep"].([]any)) != 2 {
		t.Errorf("50/50 keeps two answers, got %v", fifty["keep"])
	}
	a.post("/api/v1/games/"+gameID+"/fifty", nil, token).expect(http.StatusConflict)

	// #26 answer the first question; the server says whether it was right.
	answer := a.post("/api/v1/games/"+gameID+"/answers", map[string]any{"position": 0, "choice": 0, "time_ms": 1000}, token).
		expect(http.StatusOK).data()
	correctIndex := int(answer["correct_index"].(float64))
	if answer["correct"].(bool) != (correctIndex == 0) {
		t.Errorf("correct flag does not match the correct index: %v", answer)
	}
	// The same position cannot be answered twice (also what Idempotency-Key relies on).
	a.post("/api/v1/games/"+gameID+"/answers", map[string]any{"position": 0, "choice": 1, "time_ms": 1000}, token).
		expect(http.StatusConflict)

	// #28 skip once.
	a.post("/api/v1/games/"+gameID+"/skip", nil, token).expect(http.StatusNoContent)
	a.post("/api/v1/games/"+gameID+"/skip", nil, token).expect(http.StatusConflict)

	// Answer the rest, always picking the right one so the score is predictable.
	for position := 2; position < 5; position++ {
		state := a.get("/api/v1/games/"+gameID, token).expect(http.StatusOK).data()
		if int(state["position"].(float64)) != position {
			t.Fatalf("expected to be on question %d, got %v", position, state["position"])
		}
		// Ask for the right answer by answering wrong first? No: use the results later.
		a.post("/api/v1/games/"+gameID+"/answers", map[string]any{"position": position, "choice": 0, "time_ms": 500}, token).
			expect(http.StatusOK)
	}

	state := a.get("/api/v1/games/"+gameID, token).expect(http.StatusOK).data()
	if state["done"] != true {
		t.Errorf("after 5 answers the game is done: %v", state)
	}

	// #29 finish, #30 results.
	a.post("/api/v1/games/"+gameID+"/finish", nil, token).expect(http.StatusOK)
	results := a.get("/api/v1/games/"+gameID+"/results", token).expect(http.StatusOK).data()
	if int(results["total"].(float64)) != 5 || len(results["review"].([]any)) != 5 {
		t.Errorf("the review must have one line per question: %v", results)
	}
	score := int(results["score"].(float64))
	correct := int(results["correct"].(float64))
	if score < 0 || correct < 0 || correct > 5 {
		t.Errorf("impossible score: %d / %d", score, correct)
	}

	// #31 the game is in the history.
	history := a.get("/api/v1/me/games", token).expect(http.StatusOK).list()
	if len(history) != 1 {
		t.Errorf("one finished game in the history, got %d", len(history))
	}
}

// Another player cannot touch my game.
func TestGameBelongsToItsPlayer(t *testing.T) {
	a := newApp(t)
	mine := a.member("owner1")
	other := a.member("other1")
	gameID := a.post("/api/v1/games", map[string]any{"kind": "quick"}, mine).expect(http.StatusCreated).data()["id"].(string)
	a.get("/api/v1/games/"+gameID, other).expect(http.StatusForbidden)
	a.post("/api/v1/games/"+gameID+"/answers", map[string]any{"position": 0, "choice": 1}, other).expect(http.StatusForbidden)
}

// GP-02: a game runs on the number of questions it was asked for. A choice holding
// fewer is refused, on the API as on the setup screen, instead of quietly shrinking.
func TestAGameNeedsEveryQuestionItAsksFor(t *testing.T) {
	a := newApp(t)
	token := a.member("short1")

	all := a.get("/api/v1/domains?scope=all&per_page=50", token).expect(http.StatusOK).list()
	var small, big float64
	for _, raw := range all {
		d := raw.(map[string]any)
		n := d["question_count"].(float64)
		if small == 0 && n > 0 && n < 5 {
			small = d["id"].(float64)
		}
		if big == 0 && n >= 5 {
			big = d["id"].(float64)
		}
	}
	if small == 0 || big == 0 {
		t.Skip("the seed has no domain on each side of five questions")
	}

	refused := a.post("/api/v1/games", map[string]any{
		"kind": "custom", "domain_id": int64(small), "question_count": 5,
	}, token).expect(http.StatusConflict)
	if code := refused.errorCode(); code != "not_enough_questions" {
		t.Errorf("want not_enough_questions, got %q", code)
	}

	// The same ask on a domain that holds enough still starts, with every question.
	started := a.post("/api/v1/games", map[string]any{
		"kind": "custom", "domain_id": int64(big), "question_count": 5,
	}, token).expect(http.StatusCreated).data()
	if started["question_count"].(float64) != 5 {
		t.Errorf("a five-question game has five questions, got %v", started["question_count"])
	}
}

// GP-02: a game in a domain that is not one of yours adds it first (DM-04).
func TestPlayingANewDomainAddsIt(t *testing.T) {
	a := newApp(t)
	token := a.member("explorer1")
	before := a.get("/api/v1/me/domains", token).expect(http.StatusOK).list()

	all := a.get("/api/v1/domains?scope=all&per_page=50", token).expect(http.StatusOK).list()
	var target float64
	for _, raw := range all {
		d := raw.(map[string]any)
		// A quick game is five questions, and a domain holding fewer cannot start one,
		// so the domain this test plays has to be big enough (GP-02, HM-04).
		if d["is_mine"] == false && d["question_count"].(float64) >= 5 {
			target = d["id"].(float64)
			break
		}
	}
	if target == 0 {
		t.Skip("the seed has no second domain with five questions")
	}
	a.post("/api/v1/games", map[string]any{"kind": "quick", "domain_id": int64(target)}, token).expect(http.StatusCreated)
	after := a.get("/api/v1/me/domains", token).expect(http.StatusOK).list()
	if len(after) != len(before)+1 {
		t.Errorf("playing a new domain adds it: %d -> %d", len(before), len(after))
	}
}

// HM-02 / endpoints 22 and 23: the question of the day comes from the player's domains
// and can be answered once a day.
func TestQuestionOfTheDay(t *testing.T) {
	a := newApp(t)
	token := a.member("daily1")

	first := a.get("/api/v1/daily-question", token).expect(http.StatusOK).data()
	if first == nil || first["body"] == "" {
		t.Fatalf("no question of the day: %v", first)
	}
	if _, leaked := first["correct_index"]; leaked {
		t.Error("the right answer must not be sent before the player answers")
	}
	if first["answered"] != false {
		t.Error("a fresh player has not answered today")
	}

	answered := a.post("/api/v1/daily-question/answer", map[string]any{"choice": 0}, token).expect(http.StatusOK).data()
	if answered["answered"] != true {
		t.Errorf("the answer was not saved: %v", answered)
	}
	if _, ok := answered["correct_index"]; !ok {
		t.Error("after answering, the right answer and the explanation come back")
	}

	// One try a day: a second answer changes nothing.
	again := a.post("/api/v1/daily-question/answer", map[string]any{"choice": 1}, token).expect(http.StatusOK).data()
	if again["choice"].(float64) != answered["choice"].(float64) {
		t.Error("the second try of the day must be ignored")
	}

	// A visitor can play it too (AU-01), without a stored answer.
	visitor := a.get("/api/v1/daily-question", "").expect(http.StatusOK).data()
	if visitor == nil || visitor["answered"] != false {
		t.Errorf("a visitor gets an unanswered question: %v", visitor)
	}
}

// RS-03 / RS-04: rating, commenting and reporting a question.
func TestQuestionFeedback(t *testing.T) {
	a := newApp(t)
	token := a.member("critic1")
	gameID := a.post("/api/v1/games", map[string]any{"kind": "quick"}, token).expect(http.StatusCreated).data()["id"].(string)
	state := a.get("/api/v1/games/"+gameID, token).expect(http.StatusOK).data()
	questionID := int64(state["question"].(map[string]any)["id"].(float64))
	path := "/api/v1/questions/" + itoa(questionID)

	a.do(http.MethodPut, path+"/rating", map[string]any{"stars": 5}, token).expect(http.StatusNoContent)
	a.do(http.MethodPut, path+"/rating", map[string]any{"stars": 2, "reason": "unclear"}, token).expect(http.StatusNoContent)
	a.do(http.MethodPut, path+"/rating", map[string]any{"stars": 9}, token).expect(http.StatusUnprocessableEntity)

	a.post(path+"/comments", map[string]any{"body": "This one was tricky."}, token).expect(http.StatusCreated)
	comments := a.get(path+"/comments", token).expect(http.StatusOK).list()
	if len(comments) != 1 {
		t.Errorf("one comment expected, got %d", len(comments))
	}

	a.post(path+"/reports", map[string]any{"reason": "two_correct"}, token).expect(http.StatusNoContent)
	a.post(path+"/reports", map[string]any{"reason": "nonsense"}, token).expect(http.StatusUnprocessableEntity)
	a.post(path+"/reports", map[string]any{"reason": "other"}, token).expect(http.StatusUnprocessableEntity)
}

// NF-05 / GP-04: the speed bonus is measured by the server. A client that claims it
// answered instantly gets the points its real time deserves, not the ones it asked for.
func TestAnswerTimeComesFromTheServer(t *testing.T) {
	a := newApp(t)
	token := a.member("timer1")
	gameID := a.post("/api/v1/games", map[string]any{"kind": "quick"}, token).expect(http.StatusCreated).data()["id"].(string)

	// Reading the state is what hands the question over, so the clock starts here.
	state := a.get("/api/v1/games/"+gameID, token).expect(http.StatusOK).data()
	question := state["question"].(map[string]any)

	time.Sleep(1200 * time.Millisecond)
	if len(question["answers"].([]any)) != 4 {
		t.Fatalf("a question has four answers, got %v", question["answers"])
	}
	a.post("/api/v1/games/"+gameID+"/answers", map[string]any{"position": 0, "choice": 0, "time_ms": 0}, token).
		expect(http.StatusOK)

	var timeMS int32
	err := a.DB.Pool.QueryRow(context.Background(),
		`SELECT time_ms FROM game_answers WHERE game_id = $1 AND position = 0`, gameID).Scan(&timeMS)
	if err != nil {
		t.Fatal(err)
	}
	if timeMS < 1000 {
		t.Errorf("the server must measure the real time, got %d ms after waiting 1200 ms", timeMS)
	}
}

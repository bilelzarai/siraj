package handlers

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// Dashboard is the signed-in home screen.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	ctx := r.Context()
	user := c.User

	cats, err := h.repo.Categories(ctx, c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	d := views.HomeData{Categories: cats}

	// Only a solo round is offered back. A challenge round does not wait — its
	// questions expire whether the player is looking at them or not — and the
	// daily is one attempt, so "carry on where you left off" would be a promise
	// neither of them keeps.
	if active, err := h.repo.ResumableGame(ctx, user.ID, c.Locale); err == nil {
		d.Resume = active
	} else if !errors.Is(err, repository.ErrNotFound) {
		h.serverError(w, r, err)
		return
	}

	if recent, err := h.repo.RecentActivity(ctx, user.ID, c.Locale, 5); err == nil {
		d.Recent = recent
	}
	if friends, err := h.repo.Friends(ctx, user.ID); err == nil {
		d.FriendsOnline = friends
		if len(d.FriendsOnline) > 6 {
			d.FriendsOnline = d.FriendsOnline[:6]
		}
	}
	if incoming, err := h.repo.ChallengesFor(ctx, user.ID, "incoming", c.Locale, 4); err == nil {
		d.Incoming = filterMyTurn(incoming, user.ID)
	}
	if rank, err := h.repo.MyRank(ctx, user.ID, "global"); err == nil {
		d.Rank = rank
	}
	if played, err := h.repo.PlayedDaily(ctx, user.ID, time.Now().UTC().Format("2006-01-02")); err == nil {
		d.DailyPlayed = played
	}

	h.render(w, r, http.StatusOK, views.Home(c, d))
}

// filterMyTurn keeps only matches the viewer still has to play.
//
// Read from the player rows rather than the two legacy columns: those name the
// first two people in a match and nobody else, so a five-player match never
// appeared on the third player's home screen.
func filterMyTurn(list []*models.Challenge, userID uuid.UUID) []*models.Challenge {
	out := make([]*models.Challenge, 0, len(list))
	for _, ch := range list {
		me := ch.Player(userID)
		if me == nil || me.Out() || me.HasPlayed() {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// setupData assembles the round configuration form, including how many
// questions each category and difficulty can actually draw. Every screen that
// renders the form goes through here, so none of them can forget the counts.
func (h *Handlers) setupData(r *http.Request, locale string, category, difficulty, count int) (views.SetupData, error) {
	cats, err := h.repo.Categories(r.Context(), locale)
	if err != nil {
		return views.SetupData{}, err
	}
	available, err := h.repo.AvailableCounts(r.Context(), locale)
	if err != nil {
		return views.SetupData{}, err
	}
	d := views.SetupData{
		Categories:    cats,
		SelectedCat:   category,
		SelectedDiff:  difficulty,
		SelectedCount: count,
		Availability:  available,
	}
	return d, nil
}

// PlaySetup renders the round configuration form.
func (h *Handlers) PlaySetup(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	d, err := h.setupData(r, c.Locale, queryInt(r, "category", 0), 0, service.DefaultQuestions)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, views.Setup(c, d))
}

// PlayStart creates the round and sends the player into it.
func (h *Handlers) PlayStart(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	actor := actorFrom(r)

	category := intParam(r, "category", 0)
	difficulty := intParam(r, "difficulty", 0)
	count := intParam(r, "count", service.DefaultQuestions)

	_, err := h.game.Start(r.Context(), actor.ID, service.StartOptions{
		CategoryID: nilIfZero(category),
		Difficulty: difficulty,
		Count:      count,
		Locale:     c.Locale,
		Mode:       models.ModeSolo,
	})
	if err != nil {
		if errors.Is(err, service.ErrNotEnoughQuestions) {
			// Come back on the choices that were made, with the counts, rather
			// than resetting the form to its defaults and saying no.
			d, dErr := h.setupData(r, c.Locale, category, difficulty, count)
			if dErr != nil {
				h.serverError(w, r, dErr)
				return
			}
			d.Error = c.T("game.noQuestions",
				d.Availability.Count(category, difficulty), service.MinQuestions)
			h.render(w, r, http.StatusUnprocessableEntity, views.Setup(c, d))
			return
		}
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, "/play/round")
}

// PlayDaily starts today's round: one shared question set, one attempt.
func (h *Handlers) PlayDaily(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	_, err := h.game.StartDaily(r.Context(), actorFrom(r).ID, c.Locale, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDailyDone):
			h.flash(w, "info", c.T("game.daily.alreadyPlayed"))
			redirect(w, r, "/history?mode=daily")
		case errors.Is(err, service.ErrRoundInProgress):
			h.flash(w, "info", c.T("game.daily.finishFirst"))
			redirect(w, r, "/play/round")
		case errors.Is(err, service.ErrNotEnoughQuestions):
			h.flash(w, "error", c.T("game.daily.unavailable"))
			redirect(w, r, "/app")
		default:
			h.serverError(w, r, err)
		}
		return
	}
	redirect(w, r, "/play/round")
}

// PlayRound shows the current question, or forwards to the results when the
// round has run out of questions.
func (h *Handlers) PlayRound(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	actor := actorFrom(r)

	// A match being played round one phone decides whose turn it is, and the
	// answer is often "not whoever last held the seat". Asked before the
	// actor's own round, because in a hot seat the actor is a consequence of
	// the turn rather than the other way round.
	device, err := h.deviceRound(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if device.Hot() {
		// Everybody here has played their last question. The handover screen
		// shows it — the answer, who got it and the final scores — and the
		// rounds are closed when somebody presses on from it. Going straight
		// to the result page instead, which is what this did, meant the last
		// question of every match was the one nobody saw the answer to.
		if device.Done() {
			h.handover(w, r, device)
			return
		}
		// Until the next player has actually taken the phone — which is the
		// press on the handover screen, and which is what starts their clock —
		// the question is not drawn for anybody.
		if device.Next.Player.ID != actor.ID || device.Next.Session.ServedAt == nil {
			h.handover(w, r, device)
			return
		}
	}

	session, err := h.repo.ActiveGame(r.Context(), actor.ID, c.Locale)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			redirect(w, r, "/play")
			return
		}
		h.serverError(w, r, err)
		return
	}

	// Anything this round has already lost is written off before it is drawn.
	// A challenge question that ran out while the page was closed is gone, and
	// the round is showing the next one — not the one that was on screen when
	// the phone went in a pocket.
	lost, err := h.game.Lapse(r.Context(), session)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if lost > 0 {
		// Re-read rather than trusting the in-memory patch: another tab may
		// have moved the round on further.
		if session, err = h.repo.ActiveGame(r.Context(), actor.ID, c.Locale); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				redirect(w, r, "/app")
				return
			}
			h.serverError(w, r, err)
			return
		}
		h.flash(w, "warning", c.T("game.questionsLost", lost))
	}

	if session.IsComplete() {
		h.finishAndRedirect(w, r, session)
		return
	}

	question, err := h.game.Current(r.Context(), session, c.Locale)
	if err != nil {
		if errors.Is(err, service.ErrRoundComplete) {
			h.finishAndRedirect(w, r, session)
			return
		}
		h.serverError(w, r, err)
		return
	}

	streak, _ := h.repo.CurrentStreak(r.Context(), session.ID)

	h.render(w, r, http.StatusOK, views.Round(c, views.RoundData{
		Session:   session,
		Question:  question,
		Streak:    streak,
		ElapsedMS: servedElapsedMS(session),
		Actor:     actor,
		// A challenge round cannot be paused and cannot be resumed, so the
		// screen says so rather than offering a "leave" that quietly costs a
		// question.
		Unforgiving: session.Timed(),
		// In a hot seat the verdict is held back until everybody here has
		// answered, so the screen must not promise one.
		HotSeat: device.Hot(),
	}))
}

// answerRequest is the JSON body the play screen posts.
type answerRequest struct {
	Position int `json:"position"`
	Choice   int `json:"choice"`
	TimeMS   int `json:"timeMs"`
}

// answerResponse feeds the client's reveal animation.
type answerResponse struct {
	Correct      bool   `json:"correct"`
	CorrectIndex int    `json:"correctIndex"`
	Explanation  string `json:"explanation"`
	Points       int    `json:"points"`
	Streak       int    `json:"streak"`
	Score        int    `json:"score"`
	Finished     bool   `json:"finished"`
	NextURL      string `json:"nextUrl"`
	// Held marks an answer whose verdict is deliberately not in this response.
	//
	// In a match played round one phone the next person is standing there
	// looking at the screen: showing them the correct answer and the
	// explanation the moment somebody else commits hands them the question.
	// So the client is told to say "locked in" and move to the handover, and
	// the reveal happens there — once, after the last player.
	Held bool `json:"held"`
}

// PlayAnswer scores one submission. It is the only JSON endpoint in the
// gameplay loop; everything else is a normal page.
func (h *Handlers) PlayAnswer(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	locale := localeFrom(r)

	var req answerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request")
		return
	}

	// Whose turn it is, when several people are playing on this phone. A
	// submission from anybody but the seated player is refused: two taps in
	// quick succession must not spend the next person's question.
	device, err := h.deviceRound(r)
	if err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}
	if device.Hot() {
		if device.Done() {
			writeJSONError(w, http.StatusConflict, "round complete")
			return
		}
		if device.Next.Player.ID != actor.ID {
			writeJSONError(w, http.StatusConflict, "not your turn")
			return
		}
	}

	session, err := h.repo.ActiveGame(r.Context(), actor.ID, locale)
	if err != nil {
		writeJSONError(w, http.StatusConflict, "no active round")
		return
	}

	// A question whose deadline has passed is written off before the answer is
	// looked at, so a submission that arrives late scores nothing rather than
	// scoring against the question after it.
	if _, err := h.game.Lapse(r.Context(), session); err != nil {
		slogError(r, err)
	}

	result, err := h.game.Answer(r.Context(), session, req.Position, req.Choice, req.TimeMS, locale)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAlreadyAnswered):
			writeJSONError(w, http.StatusConflict, "already answered")
		case errors.Is(err, service.ErrRoundComplete):
			writeJSONError(w, http.StatusConflict, "round complete")
		default:
			slogError(r, err)
			writeJSONError(w, http.StatusInternalServerError, "server error")
		}
		return
	}

	next := "/play/round"
	if result.Finished {
		next = "/play/finish"
	}

	// In a hot seat the phone goes back to the handover screen whatever
	// happens — including after the last question, because the other players
	// here still have theirs to play.
	if device.Hot() {
		writeJSON(w, http.StatusOK, answerResponse{
			Score:    result.TotalScore,
			Streak:   result.Streak,
			NextURL:  "/play/round",
			Held:     true,
			Finished: false,
		})
		return
	}

	writeJSON(w, http.StatusOK, answerResponse{
		Correct:      result.Correct,
		CorrectIndex: result.CorrectIndex,
		Explanation:  result.Explanation,
		Points:       result.PointsAwarded,
		Streak:       result.Streak,
		Score:        result.TotalScore,
		Finished:     result.Finished,
		NextURL:      next,
	})
}

// PlayFinish closes the round and redirects to its result page.
func (h *Handlers) PlayFinish(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	actor := actorFrom(r)

	// On a shared device this is the press on the last handover screen, and
	// it closes everybody's round rather than only the seated player's. One
	// person finishing while three stay open is a match that never settles.
	device, err := h.deviceRound(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if device.Hot() && device.Done() {
		h.finishDevice(w, r, device)
		return
	}

	session, err := h.repo.ActiveGame(r.Context(), actor.ID, c.Locale)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			redirect(w, r, "/app")
			return
		}
		h.serverError(w, r, err)
		return
	}
	h.finishAndRedirect(w, r, session)
}

func (h *Handlers) finishAndRedirect(w http.ResponseWriter, r *http.Request, session *models.GameSession) {
	actor := actorFrom(r)

	result, err := h.game.Finish(r.Context(), session, actor)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	// Finish already knows which badges were newly granted — GrantBadges
	// returns exactly the rows it inserted. Carry that across the redirect so
	// the result screen celebrates those and nothing else; it used to throw the
	// list away and re-derive it by comparing every badge's earned_at against
	// the round's start, which also caught badges from other rounds.
	if len(result.NewBadges) > 0 {
		slugs := make([]string, 0, len(result.NewBadges))
		for _, b := range result.NewBadges {
			slugs = append(slugs, b.Slug)
		}
		h.rememberNewBadges(w, session.ID, slugs)
	}

	redirect(w, r, "/play/result/"+session.ID.String())
}

// newBadgeCookie carries the slugs granted by the round that has just finished
// across the redirect to its result page. It is signed, like every other cookie
// the server reads back, and scoped to one session id so revisiting an older
// result does not re-celebrate.
const newBadgeCookie = "siraj_badges"

func (h *Handlers) rememberNewBadges(w http.ResponseWriter, sessionID uuid.UUID, slugs []string) {
	payload := sessionID.String() + "|" + strings.Join(slugs, ",")
	http.SetCookie(w, &http.Cookie{
		Name:     newBadgeCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.signFlash(payload),
		Path:     "/play/result",
		HttpOnly: true,
		Secure:   h.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   120,
	})
}

// takeNewBadges reads and clears the slugs for this round.
func (h *Handlers) takeNewBadges(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) map[string]bool {
	c, err := r.Cookie(newBadgeCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{
		Name: newBadgeCookie, Value: "", Path: "/play/result", MaxAge: -1,
		HttpOnly: true, Secure: h.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})

	encoded, mac, ok := strings.Cut(c.Value, ".")
	if !ok {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	payload := string(raw)
	if subtle.ConstantTimeCompare([]byte(mac), []byte(h.signFlash(payload))) != 1 {
		return nil
	}

	id, slugs, ok := strings.Cut(payload, "|")
	if !ok || id != sessionID.String() || slugs == "" {
		return nil
	}

	out := map[string]bool{}
	for _, slug := range strings.Split(slugs, ",") {
		if slug != "" {
			out[slug] = true
		}
	}
	return out
}

// PlayQuit abandons the active round.
func (h *Handlers) PlayQuit(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	locale := localeFrom(r)

	// Stopping a match played round one phone stops it for everybody at it.
	// Each score stands where it is and the match settles: there is no way to
	// stop for one person only, because the phone is the one that stops.
	if device, err := h.deviceRound(r); err == nil && device.Hot() {
		h.finishDevice(w, r, device)
		return
	}

	session, err := h.repo.ActiveGame(r.Context(), actor.ID, locale)
	if err != nil {
		redirect(w, r, "/app")
		return
	}

	// A solo round is kept: leaving it is how you come back to it later. A
	// challenge round is not — walking out loses the question on screen, and
	// the round stays open for whatever is left of it, because the other
	// players are still waiting on a score.
	if session.Timed() {
		if err := h.game.AbandonInFlightQuestion(r.Context(), session); err != nil {
			h.serverError(w, r, err)
			return
		}
		c := h.viewCtx(w, r)
		h.flash(w, "warning", c.T("game.leftChallenge"))
		redirect(w, r, "/challenges")
		return
	}

	if err := h.game.Abandon(r.Context(), session.ID, actor.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, "/app")
}

// PlayResult renders the summary of a finished round.
func (h *Handlers) PlayResult(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}

	session, err := h.repo.Game(r.Context(), id, c.Locale)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	// Your own round, or one played by a guest at your device. The second is
	// not a loosening: those players exist only because this account made them,
	// and their result is on the same screen as the match they played in.
	if session.UserID != c.User.ID {
		if _, err := h.repo.LocalPlayer(r.Context(), c.User.ID, session.UserID); err != nil {
			h.forbidden(w, r)
			return
		}
	}

	d := views.ResultData{Session: session, XPEarned: session.XPEarned}

	if session.ChallengeID != nil {
		if ch, err := h.repo.Challenge(r.Context(), *session.ChallengeID, c.Locale); err == nil {
			d.Challenge = ch
			// Only the person who wrote them is offered the chance to keep them.
			if authors, err := h.repo.MatchQuestionAuthors(r.Context(), ch.QuestionIDs); err == nil {
				for _, card := range authors {
					if card != nil && card.ID == c.User.ID {
						d.WroteQuestions = true
						break
					}
				}
			}
		}
	}

	// Celebrate exactly the badges this round granted. Reading them from the
	// one-shot cookie also means a refresh does not replay the celebration.
	if granted := h.takeNewBadges(w, r, session.ID); len(granted) > 0 {
		if badges, err := h.repo.Badges(r.Context(), c.User.ID, c.Locale); err == nil {
			for _, b := range badges {
				if granted[b.Slug] {
					d.NewBadges = append(d.NewBadges, b)
				}
			}
		}
	}

	if friends, err := h.repo.Friends(r.Context(), c.User.ID); err == nil {
		d.Friends = friends
	}

	h.render(w, r, http.StatusOK, views.Result(c, d))
}

// PlayPauseClock stops the clock on the question the player is leaving, so the
// time they have already spent is what greets them when they resume — not a
// question that expired while the tab was closed.
//
// Sent with navigator.sendBeacon as the page goes away, which is why it takes
// a form body rather than JSON: a beacon cannot set the X-CSRF-Token header
// the JSON endpoints use, but it can post csrf_token as a field like any form.
//
// It answers 204 whatever happens. Nothing is listening: the document is being
// torn down, and a round that has already moved on simply has no clock here to
// stop.
func (h *Handlers) PlayPauseClock(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)

	session, err := h.repo.ActiveGame(r.Context(), actor.ID, localeFrom(r))
	// A challenge round cannot be paused. Banking the clock on leaving is what
	// makes a solo round resumable, and doing it here would hand a challenge
	// player a way to stop their own timer by closing the tab.
	if err == nil && session.Timed() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err == nil {
		if _, err := h.repo.BankQuestionClock(r.Context(), session.ID,
			intParam(r, "position", -1)); err != nil &&
			!errors.Is(err, repository.ErrNotFound) {
			slogError(r, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// servedElapsedMS is how much of the question's clock is already gone by the
// time the page renders: time banked by earlier visits, plus time since it was
// stamped if the clock is still running.
//
// Non-zero whenever the player has been here before — a refresh, the language
// switcher in the round bar, or resuming a round from the dashboard. The
// server scores against the same total, so the ring has to be told where to
// pick up or it counts down time the player does not have.
func servedElapsedMS(session *models.GameSession) int {
	ms := session.ElapsedMS
	if session.ServedAt != nil && !session.ServedAt.IsZero() {
		ms += int(time.Since(*session.ServedAt).Milliseconds())
	}
	if ms < 0 {
		return 0
	}
	if ms > service.QuestionTimeLimitMS {
		return service.QuestionTimeLimitMS
	}
	return ms
}

// ------------------------------------------------------------- JSON utils --

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

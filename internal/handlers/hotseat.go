package handlers

import (
	"errors"
	"net/http"
	"sort"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// A match played round one phone, a question at a time.
//
// Everybody answers question one, then everybody answers question two, and so
// on to the end — rather than one person playing the whole round while three
// people watch. Two things follow from that and neither is optional:
//
//   - The verdict is held back. If the answer and the explanation appeared the
//     moment the first player committed, the next person to hold the phone
//     would be reading them off the screen. So a hot-seat answer shows nothing
//     but "locked in", and the reveal happens once on the handover screen,
//     after the last player of that question — which is also the only moment it
//     is fair to show it.
//
//   - Nobody taps twice. Between two players there is a screen naming who the
//     phone is for, and it takes a press to move past it. Going straight from
//     one player's answer into the next player's question is how somebody
//     answers a question that was not theirs.
//
// Underneath, each player still has their own round: their own session, their
// own clock, their own score. All the hot seat decides is whose turn is served
// next, and that is derived rather than stored — the player with the fewest
// answers behind them, ties broken by the order they sit in. That ordering
// cannot drift out of agreement with the answers, because it is computed from
// them.

// deviceSeat is one person at this device and the round they are playing.
type deviceSeat struct {
	Player  *models.User
	Session *models.GameSession
}

// deviceRound is the state of a match being played on one device.
type deviceRound struct {
	MatchID uuid.UUID
	// Seats in the order the phone goes round: the account holder first, then
	// the guests in the order they were added.
	Seats []deviceSeat
	// Next is whose turn it is, or nil when everybody here has finished.
	Next *deviceSeat
	// Reveal is the position every player at this device has now answered and
	// has not yet been shown, or -1. It is set only when the rotation has just
	// wrapped back to the first seat, which is exactly when the last player of
	// a question has committed.
	Reveal int
}

// Hot reports a device with more than one person playing this match on it.
// With one person there is no seat to hand over and no verdict to hold back.
func (d *deviceRound) Hot() bool { return d != nil && len(d.Seats) > 1 }

// Done reports that everyone at this device has run out of questions.
func (d *deviceRound) Done() bool { return d != nil && d.Next == nil }

// SeatOf finds one player's seat.
func (d *deviceRound) SeatOf(id uuid.UUID) *deviceSeat {
	for i := range d.Seats {
		if d.Seats[i].Player.ID == id {
			return &d.Seats[i]
		}
	}
	return nil
}

// deviceRoundFor assembles the hot seat around whoever is signed in.
//
// The people at a device are the account holder and the guests they made. Only
// those of them who are in the same match and still have a round to play are
// seated; a remote opponent is playing on their own phone and is no part of
// this.
//
// It returns nil when there is no match being played here, which is the
// ordinary case and not an error.
func (h *Handlers) deviceRound(r *http.Request) (*deviceRound, error) {
	host := userFrom(r)
	if host == nil {
		return nil, nil
	}
	ctx := r.Context()
	locale := localeFrom(r)

	guests, err := h.repo.LocalPlayers(ctx, host.ID)
	if err != nil {
		return nil, err
	}
	if len(guests) == 0 {
		// Nobody else is here. Everything below would come to the same answer
		// by a longer road.
		return nil, nil
	}

	people := make([]*models.User, 0, len(guests)+1)
	people = append(people, host)
	people = append(people, guests...)

	// Which match is being played here. Any one of them answers it: the seats
	// are only ever filled from a single match.
	var matchID uuid.UUID
	ids := make([]uuid.UUID, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.ID)
		if matchID != uuid.Nil {
			continue
		}
		if id, err := h.repo.MatchOf(ctx, p.ID); err == nil {
			matchID = id
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}
	if matchID == uuid.Nil {
		return nil, nil
	}

	sessions, err := h.repo.DeviceSessions(ctx, matchID, ids, locale)
	if err != nil {
		return nil, err
	}
	byPlayer := make(map[uuid.UUID]*models.GameSession, len(sessions))
	for _, s := range sessions {
		byPlayer[s.UserID] = s
	}

	d := &deviceRound{MatchID: matchID, Reveal: -1}
	for _, p := range people {
		if s := byPlayer[p.ID]; s != nil {
			d.Seats = append(d.Seats, deviceSeat{Player: p, Session: s})
		}
	}
	if len(d.Seats) == 0 {
		return nil, nil
	}

	// A question that ran out while the phone was in somebody's pocket is lost
	// for whoever was holding it, and the seat moves on. Done before the turn
	// is worked out so the answer is about the round as it stands now.
	for i := range d.Seats {
		if _, err := h.game.Lapse(ctx, d.Seats[i].Session); err != nil {
			return nil, err
		}
	}

	d.chooseNext()
	return d, nil
}

// chooseNext picks the seat with the fewest answers behind it, ties broken by
// where they sit — which is a plain round-robin over the question in play.
func (d *deviceRound) chooseNext() {
	d.Next = nil
	d.Reveal = -1

	playing := make([]*deviceSeat, 0, len(d.Seats))
	for i := range d.Seats {
		if !d.Seats[i].Session.IsComplete() {
			playing = append(playing, &d.Seats[i])
		}
	}
	if len(playing) == 0 {
		return
	}
	// SliceStable, so equal cursors keep seating order and the phone goes
	// round the table the same way every question.
	sort.SliceStable(playing, func(a, b int) bool {
		return playing[a].Session.Cursor < playing[b].Session.Cursor
	})
	d.Next = playing[0]

	// The verdict is due exactly when everybody still playing has answered the
	// same number of questions: that is the moment the last of them committed,
	// and the first moment showing the answer tells nobody anything they could
	// have used. Mid-rotation the cursors differ and nothing is shown.
	level := playing[0].Session.Cursor
	if level == 0 {
		return
	}
	for _, seat := range playing {
		if seat.Session.Cursor != level {
			return
		}
	}
	d.Reveal = level - 1
}

// TakeTurn is the press on the handover screen: the phone changes hands.
//
// It does three things, in this order and for a reason. It checks that the
// person named is really the one whose turn it is, so a stale screen in a
// second tab cannot hand the phone to the wrong player. It moves the seat. And
// it starts their clock — because the question's twenty-five seconds should
// begin when somebody takes the phone, not whenever the next page happens to
// finish loading.
func (h *Handlers) TakeTurn(w http.ResponseWriter, r *http.Request) {
	d, err := h.deviceRound(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !d.Hot() || d.Done() {
		redirect(w, r, "/play/round")
		return
	}

	wanted, ok := parseUUID(r.PostFormValue("player"))
	if !ok || wanted != d.Next.Player.ID {
		// Not an error worth a page: the turn has moved on, and /play/round
		// will draw whatever the truth is now.
		redirect(w, r, "/play/round")
		return
	}

	h.setSeat(w, d.Next.Player.ID.String())
	if _, _, err := h.repo.MarkQuestionServed(r.Context(), d.Next.Session.ID,
		service.QuestionTimeLimitMS+service.GraceMS); err != nil &&
		!errors.Is(err, repository.ErrNotFound) {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, "/play/round")
}

// handover renders the screen between two players: who the phone is for, and —
// when the question has just been answered by everybody here — what the answer
// was and who got it.
func (h *Handlers) handover(w http.ResponseWriter, r *http.Request, d *deviceRound) {
	c := h.viewCtx(w, r)
	ctx := r.Context()

	view := views.HandoverData{
		Next:     d.Next.Player,
		Position: d.Next.Session.Cursor,
		Total:    d.Next.Session.TotalQuestions,
	}

	// The scoreboard is held back for the same reason the verdict is.
	//
	// Mid-rotation a running total is an answer key: the player waiting to be
	// handed the phone watches the person before them go from 40 to 68 and
	// knows they got it right — which, on a question they are about to be
	// asked, is most of the answer. So the scores appear only at the moment
	// everybody has committed, alongside the reveal, and are blank in between.
	if d.Reveal >= 0 {
		for i := range d.Seats {
			view.Standing = append(view.Standing, views.SeatScore{
				Player: d.Seats[i].Player,
				Score:  d.Seats[i].Session.Score,
				Turn:   d.Seats[i].Player.ID == d.Next.Player.ID,
			})
		}
	}

	if d.Reveal >= 0 {
		reveal, err := h.buildReveal(r, d, d.Reveal)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		view.Reveal = reveal
	}

	_ = ctx
	h.render(w, r, http.StatusOK, views.Handover(c, view))
}

// buildReveal gathers what to show about a question everybody here has now
// answered: the question itself, and what each of them chose.
func (h *Handlers) buildReveal(r *http.Request, d *deviceRound, position int) (*views.RevealData, error) {
	ctx := r.Context()
	locale := localeFrom(r)

	first := d.Seats[0].Session
	if position < 0 || position >= len(first.QuestionIDs) {
		return nil, nil
	}
	question, err := h.repo.Question(ctx, first.QuestionIDs[position], locale, first.Locale)
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, 0, len(d.Seats))
	for i := range d.Seats {
		ids = append(ids, d.Seats[i].Session.ID)
	}
	answers, err := h.repo.AnswersAt(ctx, ids, position)
	if err != nil {
		return nil, err
	}

	out := &views.RevealData{Question: question, Position: position}
	for i := range d.Seats {
		seat := d.Seats[i]
		row := views.RevealRow{Player: seat.Player}
		if a := answers[seat.Session.ID]; a != nil {
			row.Answered = a.Answered()
			row.Correct = a.IsCorrect
			row.Points = a.PointsAwarded
			if a.Answered() && a.SelectedIndex < len(question.Choices) {
				row.Chose = question.Choices[a.SelectedIndex]
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// finishDevice closes every round at this device and sends the phone to the
// result. Each one files its own score, and the last of them settles the match.
func (h *Handlers) finishDevice(w http.ResponseWriter, r *http.Request, d *deviceRound) {
	ctx := r.Context()

	var last *models.GameSession
	for i := range d.Seats {
		seat := d.Seats[i]
		if _, err := h.game.Finish(ctx, seat.Session, seat.Player); err != nil {
			h.serverError(w, r, err)
			return
		}
		last = seat.Session
	}

	// The device belongs to the account holder again once the match is over.
	h.clearSeat(w)

	if last == nil {
		redirect(w, r, "/challenges")
		return
	}
	// The host's own result page, which carries the match scoreboard with
	// everybody's score on it.
	redirect(w, r, "/play/result/"+d.Seats[0].Session.ID.String())
}

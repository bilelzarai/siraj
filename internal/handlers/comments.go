package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

const (
	commentMinRunes = 2
	commentMaxRunes = 2000
)

type commentRequest struct {
	Position int    `json:"position"`
	Body     string `json:"body"`
}

type commentResponse struct {
	Saved bool   `json:"saved"`
	Body  string `json:"body"`
	Error string `json:"error,omitempty"`
}

// PlayComment saves a remark about the question the player just answered.
//
// It is keyed by position in the active round rather than by question id, for
// the same reason the answer endpoint is: a client that can name any question
// id could comment on questions it was never served.
func (h *Handlers) PlayComment(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	user := userFrom(r)

	if h.tooManyWrites(w, r, service.LimitComment) {
		return
	}

	var req commentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request")
		return
	}

	body := strings.TrimSpace(req.Body)
	if n := len([]rune(body)); n < commentMinRunes || n > commentMaxRunes {
		writeJSON(w, http.StatusUnprocessableEntity, commentResponse{
			Error: c.T("comments.error.length", commentMaxRunes),
		})
		return
	}

	session, err := h.repo.ActiveGame(r.Context(), user.ID, c.Locale)
	if err != nil {
		writeJSONError(w, http.StatusConflict, "no active round")
		return
	}

	questionID, err := h.repo.AnsweredQuestionAt(r.Context(), session.ID, req.Position, user.ID)
	if err != nil {
		// Either the position is not in this round, or it has not been
		// answered yet. A comment is only offered after the reveal.
		writeJSONError(w, http.StatusConflict, "not answered yet")
		return
	}

	if err := h.repo.UpsertQuestionComment(r.Context(), questionID, user.ID, c.Locale, body); err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}

	writeJSON(w, http.StatusOK, commentResponse{Saved: true, Body: body})
}

// QuestionComments renders the remarks on one question, reached from the
// review screen after a round.
func (h *Handlers) QuestionComments(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	// Only questions this person has actually seen: the comment thread would
	// otherwise be a way to read questions before being served them.
	seen, err := h.repo.HasAnsweredQuestion(r.Context(), c.User.ID, id)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !seen && !c.User.CanModerate() {
		h.NotFound(w, r)
		return
	}

	// No round in play here, so there is no second preference to offer: the
	// reader's language, then the Arabic fallback inside the query.
	question, err := h.repo.Question(r.Context(), int(id), c.Locale, c.Locale)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	comments, err := h.repo.QuestionComments(r.Context(), id, 100)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	mine, err := h.repo.MyQuestionComment(r.Context(), id, c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.QuestionComments(c, views.QuestionCommentsData{
		QuestionID: id,
		Prompt:     question.Prompt,
		Comments:   comments,
		Mine:       mine,
	}))
}

// PostQuestionComment is the non-JavaScript path, used from the review screen.
func (h *Handlers) PostQuestionComment(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	if h.tooManyWrites(w, r, service.LimitComment) {
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	seen, err := h.repo.HasAnsweredQuestion(r.Context(), c.User.ID, id)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// Same exemption as the GET: a moderator reaches the thread in order to
	// moderate it, and needs to be able to answer in it. Without this, opening
	// a thread as a moderator worked and replying in it silently 404'd.
	if !seen && !c.User.CanModerate() {
		h.NotFound(w, r)
		return
	}

	body := strings.TrimSpace(r.PostFormValue("body"))
	if n := len([]rune(body)); n < commentMinRunes || n > commentMaxRunes {
		h.flash(w, "error", c.T("comments.error.length", commentMaxRunes))
		redirect(w, r, "/questions/"+chi.URLParam(r, "id")+"/comments")
		return
	}

	if err := h.repo.UpsertQuestionComment(r.Context(), id, c.User.ID, c.Locale, body); err != nil {
		h.serverError(w, r, err)
		return
	}

	h.flash(w, "success", c.T("comments.saved"))
	redirect(w, r, "/questions/"+chi.URLParam(r, "id")+"/comments")
}

// ----------------------------------------------------------------- rating --

type ratingRequest struct {
	Position int `json:"position"`
	Stars    int `json:"stars"`
}

type ratingResponse struct {
	Saved   bool    `json:"saved"`
	Stars   int     `json:"stars"`
	Votes   int     `json:"votes"`
	Average float64 `json:"average"`
	Error   string  `json:"error,omitempty"`
}

// PlayRate records a player's stars for the question they have just answered.
//
// Same gate as the note beside it: the position must be one this player has
// already answered in this round. Rating a question you have not seen would
// let anyone mark down the bank without playing it.
func (h *Handlers) PlayRate(w http.ResponseWriter, r *http.Request) {
	if h.tooManyWrites(w, r, service.LimitRating) {
		return
	}

	c := h.viewCtx(w, r)
	user := userFrom(r)

	var req ratingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Stars < 1 || req.Stars > models.MaxStars {
		writeJSON(w, http.StatusUnprocessableEntity, ratingResponse{
			Error: c.T("rating.error.range", models.MaxStars),
		})
		return
	}

	session, err := h.repo.ActiveGame(r.Context(), user.ID, c.Locale)
	if err != nil {
		writeJSONError(w, http.StatusConflict, "no active round")
		return
	}

	questionID, err := h.repo.AnsweredQuestionAt(r.Context(), session.ID, req.Position, user.ID)
	if err != nil {
		writeJSONError(w, http.StatusConflict, "not answered yet")
		return
	}

	if err := h.repo.RateQuestion(r.Context(), questionID, user.ID, req.Stars); err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}

	mine, votes, average, err := h.repo.QuestionRating(r.Context(), questionID, user.ID)
	if err != nil {
		slogError(r, err)
		writeJSONError(w, http.StatusInternalServerError, "server error")
		return
	}

	writeJSON(w, http.StatusOK, ratingResponse{
		Saved: true, Stars: mine, Votes: votes, Average: average,
	})
}

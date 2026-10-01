package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// Tuning constants for a round. Time limit is enforced server-side: the
// client clock is advisory only.
const (
	QuestionTimeLimitMS = 25_000
	GraceMS             = 3_000 // tolerance for network latency
	MinQuestions        = 5
	MaxQuestions        = 20
	DefaultQuestions    = 10
)

var (
	ErrNotEnoughQuestions = errors.New("not enough questions")
	ErrRoundComplete      = errors.New("round already complete")
	ErrAlreadyAnswered    = errors.New("question already answered")
)

// Game turns raw repository access into round lifecycle operations.
type Game struct {
	repo *repository.Repo
	hub  *Hub
}

func NewGame(repo *repository.Repo, hub *Hub) *Game { return &Game{repo: repo, hub: hub} }

// StartOptions configures a new round.
type StartOptions struct {
	CategoryID *int
	Difficulty int // 0 = mixed
	Count      int
	Locale     string
	Mode       string
	// ChallengeID and QuestionIDs are set when replaying a duel's fixed set.
	ChallengeID *uuid.UUID
	QuestionIDs []int
}

// Start creates a round, abandoning any stale one the player left behind.
func (g *Game) Start(ctx context.Context, userID uuid.UUID, opts StartOptions) (*models.GameSession, error) {
	if opts.Count < MinQuestions || opts.Count > MaxQuestions {
		opts.Count = DefaultQuestions
	}
	if opts.Mode == "" {
		opts.Mode = models.ModeSolo
	}

	// A match is one attempt at its questions, so a round already started for
	// this one is handed back rather than replaced. Without this a second
	// press of accept — a double tap, a retried request, a second tab —
	// abandoned the round in progress and opened a fresh one, losing whatever
	// had been answered.
	if opts.ChallengeID != nil {
		existing, err := g.repo.RoundInMatch(ctx, userID, *opts.ChallengeID, opts.Locale)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}

	// One active round per player: replacing it is friendlier than refusing.
	if existing, err := g.repo.ActiveGame(ctx, userID, opts.Locale); err == nil {
		if err := g.repo.AbandonGame(ctx, existing.ID, userID); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	ids := opts.QuestionIDs
	if len(ids) == 0 {
		var err error
		ids, err = g.repo.PickQuestionIDs(ctx, opts.CategoryID, opts.Difficulty, opts.Count, opts.Locale)
		if err != nil {
			return nil, err
		}
	}
	if len(ids) < MinQuestions {
		return nil, ErrNotEnoughQuestions
	}

	session := &models.GameSession{
		UserID:         userID,
		Mode:           opts.Mode,
		CategoryID:     opts.CategoryID,
		Difficulty:     opts.Difficulty,
		Locale:         opts.Locale,
		QuestionIDs:    ids,
		TotalQuestions: len(ids),
		ChallengeID:    opts.ChallengeID,
	}
	if err := g.repo.CreateGame(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// Current returns the question the player is looking at, and starts the
// server's clock on it.
//
// viewLocale is the language the player is reading the site in right now,
// which can differ from the one the round was assembled in once they use the
// language switcher mid-round. It is a display preference only: the round's
// questions were picked for session.Locale and stay picked, so switching
// language re-reads the same question rather than drawing a different one.
func (g *Game) Current(ctx context.Context, session *models.GameSession, viewLocale string) (*models.Question, error) {
	if session.IsComplete() {
		return nil, ErrRoundComplete
	}
	if session.Cursor < 0 || session.Cursor >= len(session.QuestionIDs) {
		return nil, ErrRoundComplete
	}

	question, err := g.repo.Question(ctx, session.QuestionIDs[session.Cursor],
		viewLocale, session.Locale)
	if err != nil {
		return nil, err
	}

	// Stamp the serve time only once per position, so a refresh cannot rewind
	// the clock. A failure here costs the player nothing — the bonus falls back
	// to what the client claims — so it is logged by the caller, not fatal.
	//
	// A challenge round also gets a deadline the first time the question is
	// shown, and that deadline does not move: closing the page does not pause
	// it, because the other players are not waiting.
	timeout := 0
	if session.Timed() {
		timeout = QuestionTimeLimitMS + GraceMS
	}
	if servedAt, deadline, err := g.repo.MarkQuestionServed(ctx, session.ID, timeout); err == nil {
		session.ServedAt = &servedAt
		session.DeadlineAt = deadline
	}
	return question, nil
}

// Lapse writes off every question this round has already lost, and reports how
// many that was.
//
// A challenge does not wait. A question left unanswered — the clock ran out,
// the page was closed, the phone was put down — is gone, and the round moves to
// the next one. This is where that happens: before the round is shown, before
// an answer is scored, and before it is finished, so no path can quietly find a
// lapsed question still sitting there waiting to be answered.
//
// It loops because a player who has been away can have lost the question they
// were on and then be served the next one — but only one question has a
// deadline at a time, so in practice this settles after one pass.
func (g *Game) Lapse(ctx context.Context, session *models.GameSession) (int, error) {
	if !session.Timed() || session.Status != models.StatusActive {
		return 0, nil
	}

	lost := 0
	for session.Expired(time.Now()) && !session.IsComplete() {
		if session.Cursor < 0 || session.Cursor >= len(session.QuestionIDs) {
			break
		}
		err := g.repo.LapseQuestion(ctx, session.ID,
			session.QuestionIDs[session.Cursor], session.Cursor, QuestionTimeLimitMS)
		if err != nil {
			if errors.Is(err, repository.ErrConflict) {
				// Another tab got there first. Re-read and carry on from
				// wherever it left the round.
				break
			}
			return lost, err
		}
		lost++

		session.Cursor++
		session.DurationMS += QuestionTimeLimitMS
		session.ServedAt = nil
		session.ElapsedMS = 0
		session.DeadlineAt = nil
	}
	return lost, nil
}

// AbandonInFlightQuestion is what leaving a challenge round costs: the question
// on screen, immediately, rather than when its clock happens to run out.
//
// Rule 3 says an unsubmitted question is lost on leaving as well as on timing
// out, and those are different moments — a player who closes the page with
// twenty seconds left has left, and waiting for the clock would let them come
// back and answer it.
func (g *Game) AbandonInFlightQuestion(ctx context.Context, session *models.GameSession) error {
	if !session.Timed() || session.Status != models.StatusActive || session.IsComplete() {
		return nil
	}
	if session.Cursor < 0 || session.Cursor >= len(session.QuestionIDs) {
		return nil
	}
	// Only a question that has actually been shown. One never served has not
	// been left — the round simply has not reached it.
	if session.ServedAt == nil && session.ElapsedMS == 0 && session.DeadlineAt == nil {
		return nil
	}
	err := g.repo.LapseQuestion(ctx, session.ID,
		session.QuestionIDs[session.Cursor], session.Cursor, QuestionTimeLimitMS)
	if errors.Is(err, repository.ErrConflict) {
		return nil
	}
	return err
}

// AnswerResult is what the play screen renders after a submission.
type AnswerResult struct {
	Correct       bool
	CorrectIndex  int
	SelectedIndex int
	Explanation   string
	PointsAwarded int
	Streak        int
	TotalScore    int
	Position      int
	Total         int
	Finished      bool
}

// Answer scores one submission. selectedIndex of -1 means the timer ran out.
//
// viewLocale only picks the language of the explanation sent back with the
// verdict; correct_index lives on the question itself, so scoring is the same
// whichever language the player is reading.
func (g *Game) Answer(ctx context.Context, session *models.GameSession, position, selectedIndex, clientTimeMS int, viewLocale string) (*AnswerResult, error) {
	if session.Status != models.StatusActive {
		return nil, ErrRoundComplete
	}
	// Position must match the server's cursor: this rejects replays and
	// out-of-order submissions rather than trusting the client.
	if position != session.Cursor {
		return nil, ErrAlreadyAnswered
	}
	if session.Cursor >= len(session.QuestionIDs) {
		return nil, ErrRoundComplete
	}

	question, err := g.repo.Question(ctx, session.QuestionIDs[session.Cursor],
		viewLocale, session.Locale)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	timeMS := elapsedMS(clientTimeMS, session.ElapsedMS, session.ServedAt, now)
	// The server's own deadline wins over any arithmetic on the client's
	// claimed time: in a challenge the question is gone when it is gone.
	timedOut := session.Expired(now) || timeMS >= QuestionTimeLimitMS ||
		selectedIndex < 0 || selectedIndex >= len(question.Choices)
	if timedOut {
		selectedIndex = models.NoAnswer
	}
	correct := !timedOut && selectedIndex == question.CorrectIndex

	streak, err := g.repo.CurrentStreak(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	newStreak := 0
	if correct {
		newStreak = streak + 1
	}

	points := 0
	if correct {
		points = scorePoints(question.Points, timeMS, newStreak)
	}

	answer := &models.GameAnswer{
		QuestionID:    question.ID,
		Position:      position,
		SelectedIndex: selectedIndex,
		IsCorrect:     correct,
		TimeMS:        timeMS,
		PointsAwarded: points,
	}
	if err := g.repo.RecordAnswer(ctx, session.ID, answer, newStreak); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrAlreadyAnswered
		}
		return nil, err
	}

	return &AnswerResult{
		Correct:       correct,
		CorrectIndex:  question.CorrectIndex,
		SelectedIndex: selectedIndex,
		Explanation:   question.Explanation,
		PointsAwarded: points,
		Streak:        newStreak,
		TotalScore:    session.Score + points,
		Position:      position,
		Total:         session.TotalQuestions,
		Finished:      position+1 >= session.TotalQuestions,
	}, nil
}

// FinishResult bundles everything the results screen shows.
type FinishResult struct {
	Session   *models.GameSession
	XPEarned  int
	Coins     int
	NewBadges []*models.Badge
	Challenge *models.Challenge
}

// Finish closes the round, awards XP and evaluates badge thresholds.
func (g *Game) Finish(ctx context.Context, session *models.GameSession, user *models.User) (*FinishResult, error) {
	if session.Status != models.StatusActive {
		// Idempotent: finishing twice returns the stored outcome.
		return &FinishResult{Session: session, XPEarned: session.XPEarned}, nil
	}

	xp := xpForRound(session.Score, session.CorrectCount)
	coins := session.CorrectCount

	// Closing the round and crediting it are one write. games_won is not part
	// of it: that column counts duel victories and is credited where the duel
	// is settled. A flawless solo round is reported as a perfect round instead
	// — counting both under one column left two screens showing different
	// numbers under the same word.
	finished, err := g.repo.FinishGame(ctx, session.ID, xp, session.Locale,
		user.ID, coins, session.BestStreak)
	if err != nil {
		return nil, err
	}

	result := &FinishResult{Session: finished, XPEarned: xp, Coins: coins}

	// File the score against this player's place in the match, and settle it if
	// nobody is left to play.
	if finished.ChallengeID != nil {
		if err := g.repo.RecordMatchScore(ctx, *finished.ChallengeID, user.ID, finished.Score); err != nil {
			return nil, fmt.Errorf("record match score: %w", err)
		}
		settled, err := g.repo.Challenge(ctx, *finished.ChallengeID, session.Locale)
		if err == nil {
			result.Challenge = settled
			g.notifyMatchOutcome(ctx, settled, user.ID)
		}
	}

	badges, err := g.evaluateBadges(ctx, user, session.Locale)
	if err != nil {
		// The round is finished and the XP is awarded; a badge sweep that fails
		// is worth a line in the log, not an error page over a played round.
		slog.WarnContext(ctx, "badge evaluation failed", "session", session.ID, "error", err)
	} else {
		result.NewBadges = badges
	}
	return result, nil
}

// notifyMatchOutcome tells everyone else in the match that a score has landed.
//
// Everyone, not just one other person: in a match of five, the other four are
// all watching the same scoreboard. And on the stream as well as in the
// notification table — this was the one kind of news in the application that
// wrote a row and stopped there, so the other players' badges moved and their
// screens did not.
func (g *Game) notifyMatchOutcome(ctx context.Context, ch *models.Challenge, actorID uuid.UUID) {
	if ch == nil {
		return
	}
	kind := EventChallengePlayed
	if ch.Status == models.ChallengeCompleted {
		kind = EventChallengeCompleted
	}

	for _, p := range ch.Players {
		// Everybody who is out, not only the ones who declined: somebody the
		// match went ahead without has no more stake in it than somebody who
		// said no. And a guest at a device has no screen to read it on.
		if p.UserID == actorID || p.Out() || p.Local {
			continue
		}
		_ = g.repo.Notify(ctx, p.UserID, kind, map[string]any{"challenge_id": ch.ID.String()})
		if g.hub != nil {
			g.hub.Publish(p.UserID, Event{Type: kind, ChallengeID: ch.ID.String()})
		}
	}
}

// evaluateBadges re-checks every threshold and grants whatever is newly met.
//
// The thresholds come from the badges table, which is the only place they are
// written. They used to be restated here as a hardcoded ladder, so adding a
// badge meant editing two files that nothing checked against each other.
func (g *Game) evaluateBadges(ctx context.Context, user *models.User, locale string) ([]*models.Badge, error) {
	// Re-read the user so XP and counters reflect the award just made.
	fresh, err := g.repo.UserByID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	definitions, err := g.repo.BadgeDefinitions(ctx)
	if err != nil {
		return nil, err
	}

	// One measure per badge kind. A kind with no measure here is simply never
	// granted, which is a visible gap rather than a silent wrong award.
	progress := map[string]int{
		"games":  fresh.GamesPlayed,
		"streak": fresh.BestStreak,
		"xp":     fresh.XP,
	}
	if n, err := g.repo.PerfectRoundCount(ctx, user.ID); err == nil {
		progress["perfect"] = n
	}
	if n, err := g.repo.FriendCount(ctx, user.ID); err == nil {
		progress["friends"] = n
	}
	if n, err := g.repo.ChallengeWins(ctx, user.ID); err == nil {
		progress["duels"] = n
	}

	var earned []string
	for _, d := range definitions {
		reached, measured := progress[d.Kind]
		if !measured {
			slog.WarnContext(ctx, "badge has no measure for its kind; it can never be earned",
				"slug", d.Slug, "kind", d.Kind)
			continue
		}
		if d.Threshold > 0 && reached >= d.Threshold {
			earned = append(earned, d.Slug)
		}
	}

	return g.repo.GrantBadges(ctx, user.ID, earned, locale)
}

// ErrDailyDone reports that today's round has already been played.
var ErrDailyDone = errors.New("today's round is already played")

// ErrRoundInProgress reports that a round is open and would be lost.
var ErrRoundInProgress = errors.New("a round is already in progress")

// DailyQuestions is how long the daily round is. Fixed, because everyone plays
// the same one and comparing scores only means something at the same length.
const DailyQuestions = 10

// StartDaily begins today's round: the same question set for every player, once
// per day.
//
// The mode has existed in the schema, the model and the history filter since the
// beginning with nothing that could create one, so `?mode=daily` filtered a list
// that could never have rows in it.
func (g *Game) StartDaily(ctx context.Context, userID uuid.UUID, locale string, now time.Time) (*models.GameSession, error) {
	day := now.UTC().Format("2006-01-02")

	played, err := g.repo.PlayedDaily(ctx, userID, day)
	if err != nil {
		return nil, err
	}
	if played {
		return nil, ErrDailyDone
	}

	// Refuse rather than replace. Start abandons whatever round is open, which
	// is the right call for "play again" — but the daily is one attempt, and an
	// abandoned round counts as played. Pressing it with a solo round open lost
	// the round *and* spent the day's attempt, neither of which was asked for.
	if _, err := g.repo.ActiveGame(ctx, userID, locale); err == nil {
		return nil, ErrRoundInProgress
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	// The set is drawn from the day itself, so every player gets the same
	// questions and the same order without anything being stored up front.
	ids, err := g.repo.PickDailyQuestionIDs(ctx, day, DailyQuestions, locale)
	if err != nil {
		return nil, err
	}
	if len(ids) < MinQuestions {
		return nil, ErrNotEnoughQuestions
	}

	return g.Start(ctx, userID, StartOptions{
		Count:       len(ids),
		Locale:      locale,
		Mode:        models.ModeDaily,
		QuestionIDs: ids,
	})
}

// Abandon discards an in-progress round.
func (g *Game) Abandon(ctx context.Context, sessionID, userID uuid.UUID) error {
	return g.repo.AbandonGame(ctx, sessionID, userID)
}

// ------------------------------------------------------------- scoring --

// scorePoints combines the question's base value with a speed bonus and a
// streak multiplier. Everything is integer maths so scores are reproducible.
func scorePoints(base, timeMS, streak int) int {
	if base <= 0 {
		base = 10
	}

	// Speed bonus: up to +50% of base, linear in the time left.
	remaining := QuestionTimeLimitMS - timeMS
	if remaining < 0 {
		remaining = 0
	}
	bonus := base * remaining / QuestionTimeLimitMS / 2

	total := base + bonus

	// Streak multiplier, capped so a long run cannot run away with it.
	switch {
	case streak >= 8:
		total = total * 3 / 2
	case streak >= 5:
		total = total * 13 / 10
	case streak >= 3:
		total = total * 12 / 10
	}
	return total
}

// xpForRound converts a round's score into durable experience.
func xpForRound(score, correct int) int {
	return score/2 + correct*5
}

// elapsedMS decides how long the player actually took.
//
// The client's own figure is kept when it is the larger of the two, because a
// slow network or a slow render should not cost someone their speed bonus. But
// it can never be smaller than what the server observed, minus a grace for the
// round trip — which is what stops a client from posting timeMs: 0 on every
// answer and collecting the maximum bonus every time.
//
// What the server observed is bankedMS — time this question collected on
// earlier visits — plus the time since servedAt, if the question is being read
// right now. A resumed round has banked time and no stamp; a question being
// read for the first time has a stamp and nothing banked; a question resumed
// and still open has both.
//
// With neither (the round predates the stamp, or the stamping write failed)
// the client's figure stands, clamped as before.
func elapsedMS(clientMS, bankedMS int, servedAt *time.Time, now time.Time) int {
	ms := clampTime(clientMS)

	floor := bankedMS
	if servedAt != nil && !servedAt.IsZero() {
		floor += int(now.Sub(*servedAt).Milliseconds())
	}
	if floor -= GraceMS; floor > ms {
		ms = floor
	}
	return clampTime(ms)
}

func clampTime(ms int) int {
	if ms < 0 {
		return 0
	}
	if ms > QuestionTimeLimitMS+GraceMS {
		return QuestionTimeLimitMS
	}
	return ms
}

// Praise picks the results-screen message key for an accuracy percentage.
func Praise(accuracy int) string {
	switch {
	case accuracy >= 90:
		return "result.praise.excellent"
	case accuracy >= 70:
		return "result.praise.good"
	case accuracy >= 40:
		return "result.praise.average"
	default:
		return "result.praise.low"
	}
}

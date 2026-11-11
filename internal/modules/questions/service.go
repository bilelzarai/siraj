// Package questions owns what players do with a question once they have seen it:
// rate it, comment on it, report a problem (endpoints 32–35), and the question of
// the day (endpoints 22 and 23).
package questions

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/modules/games"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

type Service struct{ db *db.DB }

func NewService(d *db.DB) *Service { return &Service{db: d} }

// ReportReasons are the ones the drawer offers (RS-04).
var ReportReasons = []string{"answer_wrong", "unclear", "translation", "two_correct", "typo", "other"}

// RatingReasons are asked for when a player gives 1 or 2 stars (RS-03).
var RatingReasons = []string{"too_easy", "too_hard", "unclear", "wrong_answer", "translation"}

// Rate saves a 1–5 star rating (32).
func (s *Service) Rate(ctx context.Context, who *authctx.Principal, questionID int64, stars int32, reason string) error {
	if stars < 1 || stars > 5 {
		return apperr.Invalid(map[string]string{"stars": "1 to 5"})
	}
	if reason != "" && !allowed(RatingReasons, reason) {
		return apperr.Invalid(map[string]string{"reason": "unknown reason"})
	}
	if err := s.mustExist(ctx, questionID); err != nil {
		return err
	}
	return s.db.Q.RateQuestion(ctx, sqlc.RateQuestionParams{
		QuestionID: questionID, UserID: who.ID, Stars: int16(stars), Reason: reason,
	})
}

// Comment is one comment under a question (33, 34).
type Comment struct {
	ID        uuid.UUID
	Body      string
	Author    string
	CreatedAt time.Time
}

func (s *Service) Comments(ctx context.Context, questionID int64, limit int32) ([]Comment, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.Q.ListComments(ctx, sqlc.ListCommentsParams{QuestionID: questionID, Lim: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(rows))
	for _, r := range rows {
		item := Comment{ID: r.ID, Body: r.Body}
		if r.DisplayName.Valid {
			item.Author = r.DisplayName.String
		}
		if r.CreatedAt.Valid {
			item.CreatedAt = r.CreatedAt.Time
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) AddComment(ctx context.Context, who *authctx.Principal, questionID int64, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if len([]rune(body)) < 2 || len([]rune(body)) > 600 {
		return Comment{}, apperr.Invalid(map[string]string{"body": "2 to 600 characters"})
	}
	if err := s.mustExist(ctx, questionID); err != nil {
		return Comment{}, err
	}
	row, err := s.db.Q.AddComment(ctx, sqlc.AddCommentParams{
		QuestionID: questionID, UserID: uuid.NullUUID{UUID: who.ID, Valid: true}, Body: body,
	})
	if err != nil {
		return Comment{}, err
	}
	return Comment{ID: row.ID, Body: row.Body, CreatedAt: row.CreatedAt.Time}, nil
}

// Report sends a problem to the review queue (35).
func (s *Service) Report(ctx context.Context, who *authctx.Principal, questionID int64, reason, note, lang string) error {
	if !allowed(ReportReasons, reason) {
		return apperr.Invalid(map[string]string{"reason": "pick one of the reasons"})
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 600 {
		return apperr.Invalid(map[string]string{"note": "600 characters at most"})
	}
	if reason == "other" && note == "" {
		return apperr.Invalid(map[string]string{"note": "tell us what the problem is"})
	}
	if err := s.mustExist(ctx, questionID); err != nil {
		return err
	}
	return s.db.Q.ReportQuestion(ctx, sqlc.ReportQuestionParams{
		QuestionID: questionID, UserID: uuid.NullUUID{UUID: who.ID, Valid: true},
		Reason: reason, Note: note, Lang: pgtype.Text{String: lang, Valid: lang != ""},
	})
}

// Daily is the question of the day of this player, on this day (HM-02).
type Daily struct {
	QuestionID   int64
	Day          time.Time
	DomainID     int64
	DomainSlug   string
	DomainName   string
	DomainColor  string
	DomainIcon   string
	DomainMotif  string
	DomainFamily string
	CategoryName string
	Difficulty   int32
	Points       int32
	CorrectIndex int32
	Source       string
	Body         string
	Answers      []string
	Explanation  string
	InLanguage   bool

	Answered     bool
	Choice       int32
	Correct      bool
	PercentRight int32
}

// Today returns the question of the day for a player, inside their domains. Visitors
// (who == nil) get one from any published domain, so the welcome screen can show it.
func (s *Service) Today(ctx context.Context, who *authctx.Principal, lang string) (*Daily, error) {
	day := time.Now().UTC()
	dayNumber := int64(day.YearDay())
	date := pgtype.Date{Time: time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC), Valid: true}

	// Make sure today's rows exist (the worker normally does this ahead of time).
	domains, err := s.db.Q.DomainsWithQuestions(ctx)
	if err != nil {
		return nil, err
	}
	for _, domainID := range domains {
		if err := s.db.Q.EnsureDailyQuestion(ctx, sqlc.EnsureDailyQuestionParams{
			Day: date, DomainID: domainID, DayNumber: dayNumber,
		}); err != nil {
			return nil, err
		}
	}

	params := sqlc.TodaysQuestionParams{Lang: lang, Day: date, DayNumber: dayNumber}
	if who != nil {
		params.UserID = who.ID
		params.MineOnly = true
		params.IslamicOnly = who.IslamicOnly
	}
	row, err := s.db.Q.TodaysQuestion(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &Daily{
		QuestionID: row.QuestionID, Day: date.Time, DomainID: row.DomainID,
		DomainSlug: row.DomainSlug, DomainName: row.DomainName, DomainColor: row.DomainColor,
		DomainIcon: row.DomainIcon, DomainMotif: row.DomainMotif, DomainFamily: string(row.DomainFamily),
		CategoryName: row.CategoryName, Difficulty: int32(row.Difficulty), Points: row.Points,
		CorrectIndex: int32(row.CorrectIndex), Source: row.Source, Body: row.Body,
		Answers: row.Answers, Explanation: row.Explanation, InLanguage: row.InLanguage,
	}
	if who != nil {
		if answer, err := s.db.Q.MyDailyAnswer(ctx, sqlc.MyDailyAnswerParams{Day: date, UserID: who.ID}); err == nil {
			out.Answered = true
			out.Choice = int32(answer.Choice)
			out.Correct = answer.IsCorrect
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if out.Answered {
		out.PercentRight = s.percentRight(ctx, out.QuestionID)
	}
	return out, nil
}

// AnswerToday saves the one try of the day (23).
func (s *Service) AnswerToday(ctx context.Context, who *authctx.Principal, choice int32, lang string) (*Daily, error) {
	if choice < 0 || choice > 3 {
		return nil, apperr.Invalid(map[string]string{"choice": "0 to 3"})
	}
	today, err := s.Today(ctx, who, lang)
	if err != nil || today == nil {
		return today, err
	}
	if today.Answered {
		return today, nil // one try a day: the second one changes nothing
	}
	correct := choice == today.CorrectIndex
	points := int32(0)
	if correct {
		points = games.BasePoints(today.Difficulty)
	}
	date := pgtype.Date{Time: today.Day, Valid: true}
	if _, err := s.db.Q.AnswerDaily(ctx, sqlc.AnswerDailyParams{
		Day: date, UserID: who.ID, QuestionID: today.QuestionID,
		Choice: int16(choice), IsCorrect: correct, Points: points,
	}); err != nil {
		return nil, err
	}
	today.Answered = true
	today.Choice = choice
	today.Correct = correct
	today.PercentRight = s.percentRight(ctx, today.QuestionID)
	return today, nil
}

func (s *Service) percentRight(ctx context.Context, questionID int64) int32 {
	stats, err := s.db.Q.DailyStats(ctx, questionID)
	if err != nil || stats.Answers == 0 {
		return 0
	}
	return int32(stats.Correct * 100 / stats.Answers)
}

func (s *Service) mustExist(ctx context.Context, questionID int64) error {
	found, err := s.db.Q.QuestionExists(ctx, questionID)
	if err != nil {
		return err
	}
	if !found {
		return apperr.NotFound("question")
	}
	return nil
}

func allowed(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

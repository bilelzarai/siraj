// Package sets is "My questions" (MQ-01…MQ-03, endpoints 96–105): a player writes
// their own questions in one or more sets. A set is playable at 5 questions; below
// that it is a draft, and games and challenges refuse it (set_too_small).
package sets

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// MinQuestions is the 5-question rule (MQ-02).
const MinQuestions = 5

var (
	ErrTooSmall = apperr.Conflict("set_too_small", "A set needs 5 questions to play")
	// ErrNotTranslated is the bank's own rule: it publishes in three languages, so a
	// set reaches it written in three (MQ-06, LG-05).
	ErrNotTranslated = apperr.Conflict("set_not_translated", "Every question needs all three languages before the bank can review it")
	ErrGuest         = apperr.Forbidden("Create a free account to write your own questions")
)

type Service struct{ db *db.DB }

func NewService(d *db.DB) *Service { return &Service{db: d} }

// Set is one row of My questions.
type Set struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	MainLang  string    `json:"main_lang"`
	Plays     int32     `json:"plays"`
	Questions int32     `json:"question_count"`
	Ready     bool      `json:"ready"`
	// Complete is true when every question is written in all three languages, which
	// is what the bank needs before it will look at a set (MQ-06, LG-05).
	Complete bool `json:"complete"`
	// Missing is how many questions still want a translation, for the line under the
	// Suggest button.
	Missing   int    `json:"missing"`
	UpdatedAt string `json:"updated_at"`
}

// Question is one question of a set, with its text in every language it has.
type Question struct {
	ID           int64           `json:"id"`
	Position     int32           `json:"position"`
	CategoryID   int64           `json:"category_id,omitempty"`
	Difficulty   int32           `json:"difficulty"`
	Points       int32           `json:"points"`
	CorrectIndex int32           `json:"correct_index"`
	MainLang     string          `json:"main_lang"`
	Text         map[string]Text `json:"text"`
}

type Text struct {
	Body        string   `json:"body"`
	Answers     []string `json:"answers"`
	Explanation string   `json:"explanation,omitempty"`
}

func setOf(r sqlc.ListSetsRow) Set {
	s := Set{
		ID: r.ID, Name: r.Name, MainLang: r.MainLang, Plays: r.Plays,
		Questions: r.QuestionCount, Ready: r.QuestionCount >= MinQuestions,
	}
	if r.UpdatedAt.Valid {
		s.UpdatedAt = r.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	return s
}

func (s *Service) List(ctx context.Context, who *authctx.Principal, page, perPage int) ([]Set, int, error) {
	if who.IsGuest() {
		return nil, 0, ErrGuest
	}
	rows, err := s.db.Q.ListSets(ctx, sqlc.ListSetsParams{
		OwnerID: who.ID, Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountSets(ctx, who.ID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Set, 0, len(rows))
	for _, r := range rows {
		out = append(out, setOf(r))
	}
	return out, int(total), nil
}

// Get reads one set with its questions, and says whether the bank could take it.
func (s *Service) Get(ctx context.Context, who *authctx.Principal, id uuid.UUID) (Set, []Question, error) {
	row, err := s.db.Q.GetSet(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Set{}, nil, apperr.NotFound("set")
		}
		return Set{}, nil, err
	}
	if row.OwnerID != who.ID {
		return Set{}, nil, apperr.NotFound("set")
	}
	set := setOf(sqlc.ListSetsRow(row))
	questions, err := s.questions(ctx, id)
	set.Missing = s.Completeness(questions)
	set.Complete = set.Ready && set.Missing == 0
	return set, questions, err
}

func (s *Service) questions(ctx context.Context, setID uuid.UUID) ([]Question, error) {
	rows, err := s.db.Q.SetQuestions(ctx, setID)
	if err != nil {
		return nil, err
	}
	out := []Question{}
	byID := map[int64]int{}
	for _, r := range rows {
		idx, ok := byID[r.ID]
		if !ok {
			q := Question{
				ID: r.ID, Position: int32(r.Position), Difficulty: int32(r.Difficulty),
				Points: int32(r.Points), CorrectIndex: int32(r.CorrectIndex),
				MainLang: r.MainLang, Text: map[string]Text{},
			}
			if r.CategoryID.Valid {
				q.CategoryID = r.CategoryID.Int64
			}
			out = append(out, q)
			idx = len(out) - 1
			byID[r.ID] = idx
		}
		if r.Lang.Valid {
			t := Text{Body: r.Body.String, Answers: r.Answers}
			if r.Explanation.Valid {
				t.Explanation = r.Explanation.String
			}
			out[idx].Text[r.Lang.String] = t
		}
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, who *authctx.Principal, name, mainLang string) (Set, error) {
	if who.IsGuest() {
		return Set{}, ErrGuest
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Untitled set"
	}
	if len([]rune(name)) > 40 {
		return Set{}, apperr.Invalid(map[string]string{"name": "40 characters at most"})
	}
	if !i18n.Known(mainLang) {
		mainLang = who.Lang
	}
	row, err := s.db.Q.CreateSet(ctx, sqlc.CreateSetParams{OwnerID: who.ID, Name: name, MainLang: mainLang})
	if err != nil {
		return Set{}, err
	}
	return Set{ID: row.ID, Name: row.Name, MainLang: row.MainLang}, nil
}

func (s *Service) Rename(ctx context.Context, who *authctx.Principal, id uuid.UUID, name, mainLang string) error {
	p := sqlc.UpdateSetParams{ID: id, OwnerID: who.ID}
	if name = strings.TrimSpace(name); name != "" {
		if len([]rune(name)) > 40 {
			return apperr.Invalid(map[string]string{"name": "40 characters at most"})
		}
		p.Name = pgtype.Text{String: name, Valid: true}
	}
	if i18n.Known(mainLang) {
		p.MainLang = pgtype.Text{String: mainLang, Valid: true}
	}
	_, err := s.db.Q.UpdateSet(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("set")
	}
	return err
}

func (s *Service) Delete(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	n, err := s.db.Q.DeleteSet(ctx, sqlc.DeleteSetParams{ID: id, OwnerID: who.ID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("set")
	}
	return nil
}

// QuestionInput is the question editor drawer (O04): one row plus its three tabs.
type QuestionInput struct {
	CategoryID   int64
	Difficulty   int32
	Points       int32
	CorrectIndex int32
	MainLang     string
	Text         map[string]Text
}

func (in QuestionInput) validate() error {
	fields := map[string]string{}
	if in.CorrectIndex < 0 || in.CorrectIndex > 3 {
		fields["correct_index"] = "0 to 3"
	}
	if in.Difficulty < 1 || in.Difficulty > 3 {
		fields["difficulty"] = "1 to 3"
	}
	if !i18n.Known(in.MainLang) {
		fields["main_lang"] = "ar, en or fr"
	}
	main, ok := in.Text[in.MainLang]
	if !ok || strings.TrimSpace(main.Body) == "" {
		fields["body"] = "the main language needs a question"
	}
	for lang, t := range in.Text {
		if strings.TrimSpace(t.Body) == "" {
			continue
		}
		filled := 0
		for _, a := range t.Answers {
			if strings.TrimSpace(a) != "" {
				filled++
			}
		}
		if filled < 4 {
			fields["answers_"+lang] = "four answers"
		}
	}
	if len(fields) > 0 {
		return apperr.Invalid(fields)
	}
	return nil
}

func (s *Service) AddQuestion(ctx context.Context, who *authctx.Principal, setID uuid.UUID, in QuestionInput) (int64, error) {
	if _, _, err := s.Get(ctx, who, setID); err != nil {
		return 0, err
	}
	if err := in.validate(); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		pos, err := q.NextSetPosition(ctx, setID)
		if err != nil {
			return err
		}
		row, err := q.AddSetQuestion(ctx, sqlc.AddSetQuestionParams{
			SetID: setID, Position: int16(pos), CategoryID: nullInt8(in.CategoryID),
			Difficulty: int16(in.Difficulty), Points: int16(points(in)), CorrectIndex: int16(in.CorrectIndex),
			MainLang: in.MainLang,
		})
		if err != nil {
			return err
		}
		id = row.ID
		return writeTexts(ctx, q, row.ID, in.Text)
	})
	return id, err
}

func (s *Service) UpdateQuestion(ctx context.Context, who *authctx.Principal, setID uuid.UUID, questionID int64, in QuestionInput) error {
	if _, _, err := s.Get(ctx, who, setID); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.UpdateSetQuestion(ctx, sqlc.UpdateSetQuestionParams{
			ID: questionID, SetID: setID, CategoryID: nullInt8(in.CategoryID),
			Difficulty: nullInt2(in.Difficulty), Points: nullInt2(points(in)),
			// correct_index is always written: 0 is a real answer, not "leave it".
			CorrectIndex: pgtype.Int2{Int16: int16(in.CorrectIndex), Valid: true},
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound("question")
			}
			return err
		}
		return writeTexts(ctx, q, questionID, in.Text)
	})
}

func (s *Service) DeleteQuestion(ctx context.Context, who *authctx.Principal, setID uuid.UUID, questionID int64) error {
	if _, _, err := s.Get(ctx, who, setID); err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		n, err := q.DeleteSetQuestion(ctx, sqlc.DeleteSetQuestionParams{ID: questionID, SetID: setID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("question")
		}
		return q.ResequenceSet(ctx, setID)
	})
}

// Duplicate is endpoint 104.
func (s *Service) Duplicate(ctx context.Context, who *authctx.Principal, setID uuid.UUID, questionID int64) (int64, error) {
	_, questions, err := s.Get(ctx, who, setID)
	if err != nil {
		return 0, err
	}
	for _, q := range questions {
		if q.ID != questionID {
			continue
		}
		return s.AddQuestion(ctx, who, setID, QuestionInput{
			CategoryID: q.CategoryID, Difficulty: q.Difficulty, Points: q.Points,
			CorrectIndex: q.CorrectIndex, MainLang: q.MainLang, Text: q.Text,
		})
	}
	return 0, apperr.NotFound("question")
}

// Completeness counts how many of a set's questions are written in every language.
// A question counts when its body and its four answers are there in all three; an
// explanation is optional, as the editor says (MQ-03).
func (s *Service) Completeness(questions []Question) (missing int) {
	for _, q := range questions {
		if !questionComplete(q) {
			missing++
		}
	}
	return missing
}

// questionComplete is the rule behind the Suggest button.
func questionComplete(q Question) bool {
	for _, lang := range i18n.Langs {
		t, ok := q.Text[lang]
		if !ok || strings.TrimSpace(t.Body) == "" {
			return false
		}
		filled := 0
		for _, a := range t.Answers {
			if strings.TrimSpace(a) != "" {
				filled++
			}
		}
		if filled < 4 {
			return false
		}
	}
	return true
}

// Suggest is endpoint 105: every question of the set goes to the review queue.
func (s *Service) Suggest(ctx context.Context, who *authctx.Principal, setID uuid.UUID) error {
	set, questions, err := s.Get(ctx, who, setID)
	if err != nil {
		return err
	}
	if !set.Ready {
		return ErrTooSmall
	}
	// The bank reads a set in every language it publishes, so a set that is only
	// written in one is not ready to be read (MQ-06, LG-05).
	if missing := s.Completeness(questions); missing > 0 {
		return ErrNotTranslated
	}
	return s.db.Q.SuggestSetToBank(ctx, sqlc.SuggestSetToBankParams{UserID: who.ID, SetID: setID})
}

// PlayableQuestions is what games and challenges read: the set in one language,
// refused below five questions (MQ-02).
func (s *Service) PlayableQuestions(ctx context.Context, ownerID uuid.UUID, setID uuid.UUID, lang string) ([]sqlc.SetQuestionsForPlayRow, error) {
	set, err := s.db.Q.GetSet(ctx, setID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.NotFound("set")
		}
		return nil, err
	}
	if set.OwnerID != ownerID {
		return nil, apperr.NotFound("set")
	}
	if set.QuestionCount < MinQuestions {
		return nil, ErrTooSmall
	}
	return s.db.Q.SetQuestionsForPlay(ctx, sqlc.SetQuestionsForPlayParams{Lang: lang, SetID: setID})
}

func writeTexts(ctx context.Context, q *sqlc.Queries, questionID int64, texts map[string]Text) error {
	for _, lang := range i18n.Langs {
		t, ok := texts[lang]
		if !ok || strings.TrimSpace(t.Body) == "" {
			if err := q.DeleteSetQuestionText(ctx, sqlc.DeleteSetQuestionTextParams{SetQuestionID: questionID, Lang: lang}); err != nil {
				return err
			}
			continue
		}
		answers := make([]string, 4)
		copy(answers, t.Answers)
		if err := q.UpsertSetQuestionText(ctx, sqlc.UpsertSetQuestionTextParams{
			SetQuestionID: questionID, Lang: lang, Body: strings.TrimSpace(t.Body), Answers: answers,
			Explanation: pgtype.Text{String: t.Explanation, Valid: t.Explanation != ""},
		}); err != nil {
			return err
		}
	}
	return nil
}

func points(in QuestionInput) int32 {
	if in.Points > 0 {
		return in.Points
	}
	return in.Difficulty * 10
}

func nullInt8(v int64) pgtype.Int8 {
	if v <= 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: v, Valid: true}
}

func nullInt2(v int32) pgtype.Int2 {
	if v <= 0 {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: int16(v), Valid: true}
}

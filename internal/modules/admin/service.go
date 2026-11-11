// Package admin is the staff back office (AD-01…AD-04): the eleven screens of the
// Sirāj Admin console on the same tables as the player app. Every write records
// audit_log in the same transaction as the change.
package admin

import (
	"context"
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

type Service struct{ db *db.DB }

func NewService(d *db.DB) *Service { return &Service{db: d} }

// Actor is who is writing, and from where, for the audit log.
type Actor struct {
	ID uuid.UUID
	IP string
}

func actorOf(who *authctx.Principal, ip string) Actor { return Actor{ID: who.ID, IP: ip} }

// audit writes the trail. It always runs on the transaction's queries, so a change
// without its audit row is impossible (AD-03).
func audit(ctx context.Context, q *sqlc.Queries, a Actor, action, entity, entityID string, diff any) error {
	raw, err := json.Marshal(diff)
	if err != nil {
		return err
	}
	if diff == nil {
		raw = []byte("{}")
	}
	return q.WriteAudit(ctx, sqlc.WriteAuditParams{
		ActorID:  uuid.NullUUID{UUID: a.ID, Valid: a.ID != uuid.Nil},
		Action:   action,
		Entity:   entity,
		EntityID: pgtype.Text{String: entityID, Valid: entityID != ""},
		Diff:     raw,
		Ip:       parseIP(a.IP),
	})
}

// ---------------------------------------------------------------- A01 dashboard

type Dashboard struct {
	// IslamicOnlyPercent is the share of players with "Islamic content only" on (A01).
	IslamicOnlyPercent int
	KPIs               sqlc.AdminKPIsRow
	PlayersDelta       int
	GamesDelta         int
	Accuracy           int
	GamesPerDay        []sqlc.GamesPerDayRow
	PlayersByDomain    []sqlc.PlayersByDomainRow
	ByLanguage         []sqlc.PlayersByLanguageRow
	Categories         []sqlc.CategoryPerformanceRow
	Recent             []sqlc.AuditLogRow
}

func (s *Service) Dashboard(ctx context.Context, lang string, days int32) (Dashboard, error) {
	var out Dashboard
	var err error
	if days <= 0 {
		days = 7
	}
	if out.KPIs, err = s.db.Q.AdminKPIs(ctx); err != nil {
		return out, err
	}
	out.PlayersDelta = percentChange(int(out.KPIs.PlayersWeekBefore), int(out.KPIs.PlayersWeek))
	out.GamesDelta = percentChange(int(out.KPIs.GamesYesterday), int(out.KPIs.GamesToday))
	if out.GamesPerDay, err = s.db.Q.GamesPerDay(ctx, days); err != nil {
		return out, err
	}
	acc, err := s.db.Q.AdminAccuracy(ctx, days)
	if err != nil {
		return out, err
	}
	if acc.Games > 0 {
		// daily_activity.correct counts right answers; five questions per game is the
		// quick game, so the ratio is per game, not per question — good enough as a trend.
		out.Accuracy = int(acc.Correct) * 100 / (int(acc.Games) * 5)
	}
	if out.PlayersByDomain, err = s.db.Q.PlayersByDomain(ctx, lang); err != nil {
		return out, err
	}
	if out.ByLanguage, err = s.db.Q.PlayersByLanguage(ctx); err != nil {
		return out, err
	}
	if out.Categories, err = s.db.Q.CategoryPerformance(ctx, sqlc.CategoryPerformanceParams{Lang: lang, Lim: 8}); err != nil {
		return out, err
	}
	if share, err := s.db.Q.IslamicOnlyShare(ctx); err == nil {
		out.IslamicOnlyPercent = int(share)
	}
	out.Recent, err = s.db.Q.AuditLog(ctx, sqlc.AuditLogParams{Lim: 8})
	return out, err
}

func percentChange(before, now int) int {
	if before == 0 {
		if now == 0 {
			return 0
		}
		return 100
	}
	return (now - before) * 100 / before
}

// ---------------------------------------------------------------- A02 users

type UserFilter struct {
	Kind, Role, Lang, Status, Q string
	// Sort is one of user, role, games, accuracy, last_seen; anything else leaves
	// the newest account first, which is the order the screen opens on.
	Sort          string
	Asc           bool
	Page, PerPage int
}

// userSorts is the set a column heading may ask for. Nothing outside it reaches the
// query, which is what keeps an ORDER BY from being written by the browser.
var userSorts = map[string]bool{
	"user": true, "role": true, "games": true, "accuracy": true, "last_seen": true,
}

func (f UserFilter) sort() string {
	if userSorts[f.Sort] {
		return f.Sort
	}
	return ""
}

// UserCounts is the segmented control over the table: Everyone, Players, Staff.
type UserCounts struct{ Everyone, Players, Staff int }

func (s *Service) Users(ctx context.Context, f UserFilter) ([]sqlc.AdminUsersRow, int, error) {
	rows, err := s.db.Q.AdminUsers(ctx, sqlc.AdminUsersParams{
		Kind: f.Kind, Role: f.Role, Lang: f.Lang, Status: f.Status, Q: f.Q,
		Sort: f.sort(), Ascending: f.Asc,
		Lim: int32(f.PerPage), Off: int32((f.Page - 1) * f.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountAdminUsers(ctx, sqlc.CountAdminUsersParams{
		Kind: f.Kind, Role: f.Role, Lang: f.Lang, Status: f.Status, Q: f.Q,
	})
	return rows, int(total), err
}

// UserCounts fills the segmented control, which shows its numbers whatever the
// filters below it say.
func (s *Service) UserCounts(ctx context.Context) (UserCounts, error) {
	row, err := s.db.Q.CountAdminUsersByKind(ctx)
	if err != nil {
		return UserCounts{}, err
	}
	return UserCounts{Everyone: int(row.Everyone), Players: int(row.Players), Staff: int(row.Staff)}, nil
}

// SetRole is admin-only (the handler checks the role).
func (s *Service) SetRole(ctx context.Context, a Actor, userID uuid.UUID, role string) error {
	switch role {
	case "player", "translator", "moderator", "support", "admin":
	default:
		return apperr.Invalid(map[string]string{"role": "player, translator, moderator, support or admin"})
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.SetUserRole(ctx, sqlc.SetUserRoleParams{Role: sqlc.UserRole(role), ID: userID}); err != nil {
			return err
		}
		return audit(ctx, q, a, "user.role", "user", userID.String(), map[string]any{"role": role})
	})
}

// Sanction warns, suspends or bans; a suspension or ban also revokes the sessions,
// so the player stops where they are.
func (s *Service) Sanction(ctx context.Context, a Actor, userID uuid.UUID, kind, reason string, days int) error {
	switch kind {
	case "warning", "suspension", "ban":
	default:
		return apperr.Invalid(map[string]string{"kind": "warning, suspension or ban"})
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		p := sqlc.AddSanctionParams{
			UserID: userID, Kind: sqlc.SanctionKind(kind), Reason: reason,
			ByID: uuid.NullUUID{UUID: a.ID, Valid: true},
		}
		if kind == "suspension" && days > 0 {
			p.Until = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, days), Valid: true}
		}
		if _, err := q.AddSanction(ctx, p); err != nil {
			return err
		}
		if kind != "warning" {
			if err := q.RevokeUserSessions(ctx, userID); err != nil {
				return err
			}
		}
		return audit(ctx, q, a, "user."+kind, "user", userID.String(),
			map[string]any{"reason": reason, "days": days})
	})
}

func (s *Service) LiftSanctions(ctx context.Context, a Actor, userID uuid.UUID) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.LiftSanctions(ctx, userID); err != nil {
			return err
		}
		return audit(ctx, q, a, "user.lift", "user", userID.String(), nil)
	})
}

func (s *Service) Sanctions(ctx context.Context, userID uuid.UUID) ([]sqlc.UserSanctionsRow, error) {
	return s.db.Q.UserSanctions(ctx, userID)
}

// ---------------------------------------------------------------- A04 domains

// DomainFilter is A04's toolbar: the family tab, the search and the column the
// table is ordered by.
type DomainFilter struct {
	Lang, Family, Q, Sort string
	Asc                   bool
	Page, PerPage         int
}

var domainSorts = map[string]bool{
	"name": true, "categories": true, "questions": true, "players": true,
}

func (f DomainFilter) sort() string {
	if domainSorts[f.Sort] {
		return f.Sort
	}
	return ""
}

// DomainCounts is the segmented control: every domain, the Islamic family, the rest.
type DomainCounts struct{ All, Islamic, General int }

func (s *Service) Domains(ctx context.Context, f DomainFilter) ([]sqlc.AdminDomainsRow, int, error) {
	rows, err := s.db.Q.AdminDomains(ctx, sqlc.AdminDomainsParams{
		Lang: f.Lang, Family: f.Family, Q: f.Q, Sort: f.sort(), Ascending: f.Asc,
		Lim: int32(f.PerPage), Off: int32((f.Page - 1) * f.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountAdminDomains(ctx, sqlc.CountAdminDomainsParams{
		Lang: f.Lang, Family: f.Family, Q: f.Q,
	})
	return rows, int(total), err
}

func (s *Service) DomainCounts(ctx context.Context) (DomainCounts, error) {
	row, err := s.db.Q.CountAdminDomainsByFamily(ctx)
	if err != nil {
		return DomainCounts{}, err
	}
	return DomainCounts{All: int(row.AllDomains), Islamic: int(row.Islamic), General: int(row.General)}, nil
}

// DomainInput is the domain drawer (A04b): names ×3, family, colour, icon, motif…
type DomainInput struct {
	ID           int64
	Slug         string
	Family       string
	Icon         string
	Color        string
	Motif        string
	Position     int32
	IsFeatured   bool
	IsPublished  bool
	NewUntil     string // yyyy-mm-dd, empty clears it
	Names        map[string]string
	Descriptions map[string]string
}

func (s *Service) SaveDomain(ctx context.Context, a Actor, in DomainInput) (int64, error) {
	if in.Family != "islamic" && in.Family != "general" {
		return 0, apperr.Invalid(map[string]string{"family": "islamic or general"})
	}
	if strings.TrimSpace(in.Slug) == "" {
		return 0, apperr.Invalid(map[string]string{"slug": "a slug is required"})
	}
	if strings.TrimSpace(in.Names[i18n.Fallback]) == "" {
		return 0, apperr.Invalid(map[string]string{"name_en": "the English name is required"})
	}
	var id int64
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		var newUntil pgtype.Date
		if in.NewUntil != "" {
			t, err := time.Parse("2006-01-02", in.NewUntil)
			if err != nil {
				return apperr.Invalid(map[string]string{"is_new_until": "use yyyy-mm-dd"})
			}
			newUntil = pgtype.Date{Time: t, Valid: true}
		}
		action := "domain.update"
		if in.ID == 0 {
			row, err := q.CreateDomain(ctx, sqlc.CreateDomainParams{
				Slug: in.Slug, Family: sqlc.DomainFamily(in.Family), Icon: in.Icon, Color: in.Color,
				Motif: in.Motif, Position: in.Position, IsFeatured: in.IsFeatured,
				IsNewUntil: newUntil, IsPublished: in.IsPublished,
			})
			if err != nil {
				return err
			}
			id, action = row.ID, "domain.create"
		} else {
			row, err := q.UpdateDomain(ctx, sqlc.UpdateDomainParams{
				ID: in.ID, Slug: text(in.Slug), Family: sqlc.NullDomainFamily{DomainFamily: sqlc.DomainFamily(in.Family), Valid: true},
				Icon: text(in.Icon), Color: text(in.Color), Motif: text(in.Motif),
				Position:    pgtype.Int4{Int32: in.Position, Valid: true},
				IsFeatured:  pgtype.Bool{Bool: in.IsFeatured, Valid: true},
				IsPublished: pgtype.Bool{Bool: in.IsPublished, Valid: true},
				IsNewUntil:  newUntil, ClearNew: in.NewUntil == "",
			})
			if err != nil {
				return err
			}
			id = row.ID
		}
		if in.IsFeatured {
			// Only one flagship at a time (the card on Home).
			if err := q.UnfeatureDomains(ctx, id); err != nil {
				return err
			}
		}
		for _, lang := range i18n.Langs {
			name := strings.TrimSpace(in.Names[lang])
			if name == "" {
				continue
			}
			if err := q.UpsertDomainText(ctx, sqlc.UpsertDomainTextParams{
				DomainID: id, Lang: lang, Name: name, Description: in.Descriptions[lang],
			}); err != nil {
				return err
			}
		}
		return audit(ctx, q, a, action, "domain", itoa(id), in)
	})
	return id, err
}

// ---------------------------------------------------------------- A05 categories

// CategoryFilter is A05's toolbar: which domain, which state, the search and the
// column the table is ordered by.
type CategoryFilter struct {
	Lang          string
	DomainID      int64
	Live          string
	Q, Sort       string
	Asc           bool
	Page, PerPage int
}

var categorySorts = map[string]bool{"name": true, "order": true, "questions": true}

func (f CategoryFilter) sort() string {
	if categorySorts[f.Sort] {
		return f.Sort
	}
	return ""
}

// CategoryCounts is the segmented control: All, Live, Retired — counted inside the
// domain that is being looked at, so the numbers match the rows below them.
type CategoryCounts struct{ All, Live, Retired int }

func (s *Service) Categories(ctx context.Context, f CategoryFilter) ([]sqlc.AdminCategoriesRow, int, error) {
	rows, err := s.db.Q.AdminCategories(ctx, sqlc.AdminCategoriesParams{
		Lang: f.Lang, DomainID: f.DomainID, Live: f.Live, Q: f.Q,
		Sort: f.sort(), Ascending: f.Asc,
		Lim: int32(f.PerPage), Off: int32((f.Page - 1) * f.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountAdminCategories(ctx, sqlc.CountAdminCategoriesParams{
		Lang: f.Lang, DomainID: f.DomainID, Live: f.Live, Q: f.Q,
	})
	return rows, int(total), err
}

func (s *Service) CategoryCounts(ctx context.Context, domainID int64) (CategoryCounts, error) {
	row, err := s.db.Q.CountAdminCategoriesByState(ctx, domainID)
	if err != nil {
		return CategoryCounts{}, err
	}
	return CategoryCounts{All: int(row.AllCategories), Live: int(row.Live), Retired: int(row.Retired)}, nil
}

type CategoryInput struct {
	ID          int64
	DomainID    int64
	Slug        string
	Icon        string
	Color       string
	Position    int32
	IsPublished bool
	Names       map[string]string
}

func (s *Service) SaveCategory(ctx context.Context, a Actor, in CategoryInput) (int64, error) {
	if strings.TrimSpace(in.Slug) == "" {
		return 0, apperr.Invalid(map[string]string{"slug": "a slug is required"})
	}
	if strings.TrimSpace(in.Names[i18n.Fallback]) == "" {
		return 0, apperr.Invalid(map[string]string{"name_en": "the English name is required"})
	}
	var id int64
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		action := "category.update"
		if in.ID == 0 {
			row, err := q.CreateCategory(ctx, sqlc.CreateCategoryParams{
				DomainID: in.DomainID, Slug: in.Slug, Icon: in.Icon, Color: in.Color,
				Position: in.Position, IsPublished: in.IsPublished,
			})
			if err != nil {
				return err
			}
			id, action = row.ID, "category.create"
		} else {
			row, err := q.UpdateCategory(ctx, sqlc.UpdateCategoryParams{
				ID: in.ID, DomainID: pgtype.Int8{Int64: in.DomainID, Valid: in.DomainID > 0},
				Slug: text(in.Slug), Icon: text(in.Icon), Color: text(in.Color),
				Position:    pgtype.Int4{Int32: in.Position, Valid: true},
				IsPublished: pgtype.Bool{Bool: in.IsPublished, Valid: true},
			})
			if err != nil {
				return err
			}
			id = row.ID
		}
		for _, lang := range i18n.Langs {
			name := strings.TrimSpace(in.Names[lang])
			if name == "" {
				continue
			}
			if err := q.UpsertCategoryText(ctx, sqlc.UpsertCategoryTextParams{CategoryID: id, Lang: lang, Name: name}); err != nil {
				return err
			}
		}
		return audit(ctx, q, a, action, "category", itoa(id), in)
	})
	return id, err
}

// ---------------------------------------------------------------- A06 questions

type QuestionFilter struct {
	Lang       string
	Status     string
	DomainID   int64
	CategoryID int64
	Difficulty int32
	// Langs is A06's "Translations" filter: all, missing, or no preference.
	Langs         string
	Q, Sort       string
	Asc           bool
	Page, PerPage int
}

var questionSorts = map[string]bool{
	"body": true, "difficulty": true, "accuracy": true, "plays": true,
}

func (f QuestionFilter) sort() string {
	if questionSorts[f.Sort] {
		return f.Sort
	}
	return ""
}

func (f QuestionFilter) langs() string {
	if f.Langs == "all" || f.Langs == "missing" {
		return f.Langs
	}
	return ""
}

func (s *Service) Questions(ctx context.Context, f QuestionFilter) ([]sqlc.AdminQuestionsRow, int, error) {
	rows, err := s.db.Q.AdminQuestions(ctx, sqlc.AdminQuestionsParams{
		Lang: f.Lang, Status: f.Status, DomainID: f.DomainID, CategoryID: f.CategoryID,
		Difficulty: f.Difficulty, Langs: f.langs(), Q: f.Q,
		Sort: f.sort(), Ascending: f.Asc,
		Lim: int32(f.PerPage), Off: int32((f.Page - 1) * f.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountAdminQuestions(ctx, sqlc.CountAdminQuestionsParams{
		Lang: f.Lang, Status: f.Status, DomainID: f.DomainID, CategoryID: f.CategoryID,
		Difficulty: f.Difficulty, Langs: f.langs(), Q: f.Q,
	})
	return rows, int(total), err
}

func (s *Service) QuestionCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.Q.QuestionCountsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, r := range rows {
		out[string(r.Status)] = int(r.N)
	}
	return out, nil
}

// QuestionInput is the question drawer (A06b): one row and its three language tabs.
type QuestionInput struct {
	ID           int64
	CategoryID   int64
	Difficulty   int32
	Points       int32
	CorrectIndex int32
	Source       string
	MainLang     string
	Status       string
	Bodies       map[string]string
	Answers      map[string][]string
	Explanations map[string]string
}

func (s *Service) SaveQuestion(ctx context.Context, a Actor, in QuestionInput) (int64, error) {
	fields := map[string]string{}
	if in.CategoryID <= 0 {
		fields["category_id"] = "pick a category"
	}
	if in.Difficulty < 1 || in.Difficulty > 3 {
		fields["difficulty"] = "1 to 3"
	}
	if in.CorrectIndex < 0 || in.CorrectIndex > 3 {
		fields["correct_index"] = "0 to 3"
	}
	if !i18n.Known(in.MainLang) {
		fields["main_lang"] = "ar, en or fr"
	}
	if strings.TrimSpace(in.Bodies[in.MainLang]) == "" {
		fields["body"] = "the main language needs a question"
	}
	switch in.Status {
	case "draft", "review", "published", "archived":
	default:
		fields["status"] = "draft, review, published or archived"
	}
	if len(fields) > 0 {
		return 0, apperr.Invalid(fields)
	}
	if in.Points <= 0 {
		in.Points = in.Difficulty * 10
	}
	var id int64
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		action := "question.update"
		if in.ID == 0 {
			row, err := q.CreateQuestion(ctx, sqlc.CreateQuestionParams{
				CategoryID: in.CategoryID, Difficulty: int16(in.Difficulty), Points: in.Points,
				CorrectIndex: int16(in.CorrectIndex), Status: sqlc.QuestionStatus(in.Status),
				Source: in.Source, MainLang: in.MainLang,
				AuthorID: uuid.NullUUID{UUID: a.ID, Valid: true},
			})
			if err != nil {
				return err
			}
			id, action = row.ID, "question.create"
		} else {
			id = in.ID
			if _, err := q.UpdateQuestion(ctx, sqlc.UpdateQuestionParams{
				ID: in.ID, CategoryID: pgtype.Int8{Int64: in.CategoryID, Valid: true},
				Difficulty:   pgtype.Int2{Int16: int16(in.Difficulty), Valid: true},
				Points:       pgtype.Int4{Int32: in.Points, Valid: true},
				CorrectIndex: pgtype.Int2{Int16: int16(in.CorrectIndex), Valid: true},
				Source:       text(in.Source), MainLang: text(in.MainLang),
			}); err != nil {
				return err
			}
			if _, err := q.SetQuestionStatus(ctx, sqlc.SetQuestionStatusParams{
				Status: sqlc.QuestionStatus(in.Status), ID: in.ID,
			}); err != nil {
				return err
			}
		}
		for _, lang := range i18n.Langs {
			body := strings.TrimSpace(in.Bodies[lang])
			if body == "" {
				continue
			}
			answers := make([]string, 4)
			copy(answers, in.Answers[lang])
			if err := q.UpsertQuestionText(ctx, sqlc.UpsertQuestionTextParams{
				QuestionID: id, Lang: lang, Body: body, Answers: answers,
				Explanation: in.Explanations[lang],
			}); err != nil {
				return err
			}
		}
		snapshot, _ := json.Marshal(in)
		if err := q.AddQuestionRevision(ctx, sqlc.AddQuestionRevisionParams{
			QuestionID: id, EditorID: uuid.NullUUID{UUID: a.ID, Valid: true}, Snapshot: snapshot,
		}); err != nil {
			return err
		}
		if err := q.RefreshCategoryCount(ctx, in.CategoryID); err != nil {
			return err
		}
		return audit(ctx, q, a, action, "question", itoa(id), map[string]any{"status": in.Status})
	})
	return id, err
}

// Review is the queue's Approve / Reject / Send back (keys A / R / S).
// ReviewCheck is one line of A07's "Automatic checks". Level is ok, warn or bad, and
// is what decides the mark in front of it.
type ReviewCheck struct {
	Level string
	Text  string
	Arg   int
}

// ReviewDetail is everything the pane on the right of A07 shows about one question:
// who sent it, what it says in each language, and what the checks found.
type ReviewDetail struct {
	Author     string
	FirstName  string
	Source     string
	Points     int32
	MainLang   string
	Submitted  time.Time
	Updated    time.Time
	Texts      []sqlc.QuestionTranslation
	Checks     []ReviewCheck
	Translated int
}

// ReviewDetail reads one question of the queue and runs the checks over it. The
// checks are read-only: they advise the reviewer, they never decide for them.
func (s *Service) ReviewDetail(ctx context.Context, questionID int64) (ReviewDetail, error) {
	var out ReviewDetail
	row, err := s.db.Q.QuestionAuthor(ctx, questionID)
	if err != nil {
		return out, err
	}
	out.Author, out.FirstName = row.Author, row.FirstName
	out.Source, out.Points, out.MainLang = row.Source, row.Points, row.MainLang
	if row.CreatedAt.Valid {
		out.Submitted = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		out.Updated = row.UpdatedAt.Time
	}
	if out.Texts, err = s.db.Q.QuestionTexts(ctx, questionID); err != nil {
		return out, err
	}
	out.Translated = len(out.Texts)

	// 1 · a pair the duplicates job found and nobody has settled.
	duplicate, err := s.db.Q.QuestionLooksDuplicate(ctx, questionID)
	if err != nil {
		return out, err
	}
	if duplicate {
		out.Checks = append(out.Checks, ReviewCheck{Level: "bad", Text: "admin.check.duplicate_found"})
	} else {
		out.Checks = append(out.Checks, ReviewCheck{Level: "ok", Text: "admin.check.no_duplicate"})
	}

	// 2 · a question reaches players in the languages it has, so a missing one is
	// worth saying out loud rather than discovering in a game.
	if out.Translated >= len(i18n.Langs) {
		out.Checks = append(out.Checks, ReviewCheck{Level: "ok", Text: "admin.check.all_translations"})
	} else {
		out.Checks = append(out.Checks, ReviewCheck{
			Level: "warn", Text: "admin.check.missing_translations",
			Arg: len(i18n.Langs) - out.Translated,
		})
	}

	// 3 · every answer cites its source (the rule the schema's own comment states).
	if strings.TrimSpace(out.Source) != "" {
		out.Checks = append(out.Checks, ReviewCheck{Level: "ok", Text: "admin.check.source_cited"})
	} else {
		out.Checks = append(out.Checks, ReviewCheck{Level: "warn", Text: "admin.check.no_source"})
	}

	// 4 · an explanation far shorter than the main language's is usually a
	// translation that was cut short, which is what the design flags.
	main := 0
	for _, t := range out.Texts {
		if t.Lang == out.MainLang {
			main = len([]rune(t.Explanation))
		}
	}
	if main > 0 {
		for _, t := range out.Texts {
			if t.Lang == out.MainLang {
				continue
			}
			got := len([]rune(t.Explanation))
			if got == 0 {
				out.Checks = append(out.Checks, ReviewCheck{
					Level: "warn", Text: "admin.check.no_explanation." + t.Lang,
				})
				continue
			}
			if short := 100 - got*100/main; short >= 40 {
				out.Checks = append(out.Checks, ReviewCheck{
					Level: "warn", Text: "admin.check.short_explanation." + t.Lang, Arg: short,
				})
			}
		}
	}
	return out, nil
}

// ReviewedToday is the ring in A07's header: what this reviewer has decided today.
func (s *Service) ReviewedToday(ctx context.Context, reviewer uuid.UUID) (int, error) {
	n, err := s.db.Q.ReviewedToday(ctx, uuid.NullUUID{UUID: reviewer, Valid: reviewer != uuid.Nil})
	return int(n), err
}

// Unreview puts a decided question back in the queue — the Undo the toast offers
// right after a decision. It records its own trail, so the undo is as auditable as
// the decision it takes back (AD-03).
func (s *Service) Unreview(ctx context.Context, a Actor, questionID int64) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.SetQuestionStatus(ctx, sqlc.SetQuestionStatusParams{
			Status: sqlc.QuestionStatusReview, ID: questionID,
		})
		if err != nil {
			return err
		}
		if err := q.RefreshCategoryCount(ctx, row.CategoryID); err != nil {
			return err
		}
		return audit(ctx, q, a, "question.undo", "question", itoa(questionID), nil)
	})
}

func (s *Service) Review(ctx context.Context, a Actor, questionID int64, decision, note string) error {
	status := map[string]string{"approved": "published", "rejected": "archived", "sent_back": "draft"}[decision]
	if status == "" {
		return apperr.Invalid(map[string]string{"decision": "approved, rejected or sent_back"})
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.SetQuestionStatus(ctx, sqlc.SetQuestionStatusParams{
			Status: sqlc.QuestionStatus(status), ID: questionID,
		})
		if err != nil {
			return err
		}
		if err := q.AddQuestionReview(ctx, sqlc.AddQuestionReviewParams{
			QuestionID: questionID, ReviewerID: uuid.NullUUID{UUID: a.ID, Valid: true},
			Decision: sqlc.ReviewDecision(decision), Note: note,
		}); err != nil {
			return err
		}
		if err := q.RefreshCategoryCount(ctx, row.CategoryID); err != nil {
			return err
		}
		return audit(ctx, q, a, "question."+decision, "question", itoa(questionID), map[string]any{"note": note})
	})
}

// BulkStatus is the table's bulk bar (publish, retire…).
func (s *Service) BulkStatus(ctx context.Context, a Actor, ids []int64, status string) (int64, error) {
	switch status {
	case "draft", "review", "published", "archived":
	default:
		return 0, apperr.Invalid(map[string]string{"status": "draft, review, published or archived"})
	}
	var n int64
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		var err error
		n, err = q.BulkSetQuestionStatus(ctx, sqlc.BulkSetQuestionStatusParams{
			Status: sqlc.QuestionStatus(status), Ids: ids,
		})
		if err != nil {
			return err
		}
		return audit(ctx, q, a, "question.bulk_"+status, "question", "", map[string]any{"ids": ids})
	})
	return n, err
}

// ---------------------------------------------------------------- A08 comments

// CommentTab is one of A09's four: what is waiting, what is flagged, what has
// been dealt with, and what is hidden. Each is a pair of the two things the query
// can filter on, so the screen names a tab and the service decides what that means.
// CommentFilter is A08's toolbar: which tab, a search, and the language a comment
// was written in (not the language the moderator reads the question in).
type CommentFilter struct {
	Tab     string
	Q       string
	Lang    string
	ReadAs  string
	Page    int
	PerPage int
}

type CommentTab struct {
	Status       string
	ReportedOnly bool
	// ResolvedOnly means reported and settled, which is a different set from either
	// "visible" or "reported" alone.
	ResolvedOnly bool
}

func commentTab(name string) CommentTab {
	switch name {
	case "flagged":
		return CommentTab{ReportedOnly: true}
	case "resolved":
		// Reported, and the reports dealt with. Reading this as "every visible
		// comment" put the whole bank under a tab labelled Resolved.
		return CommentTab{Status: "visible", ResolvedOnly: true}
	case "hidden":
		return CommentTab{Status: "hidden"}
	}
	// "Needs action" is the one staff open on: visible, and reported.
	return CommentTab{Status: "visible", ReportedOnly: true}
}

// CommentCounts is the number beside each tab.
type CommentCounts struct{ NeedsAction, Flagged, Resolved, Hidden int }

func (s *Service) Comments(ctx context.Context, f CommentFilter) ([]sqlc.AdminCommentsRow, int, error) {
	t := commentTab(f.Tab)
	rows, err := s.db.Q.AdminComments(ctx, sqlc.AdminCommentsParams{
		Status: t.Status, ReportedOnly: t.ReportedOnly, ResolvedOnly: t.ResolvedOnly,
		CommentLang: f.Lang, Lang: f.ReadAs, Q: f.Q,
		Lim: int32(f.PerPage), Off: int32((f.Page - 1) * f.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountAdminComments(ctx, sqlc.CountAdminCommentsParams{
		Status: t.Status, ReportedOnly: t.ReportedOnly, ResolvedOnly: t.ResolvedOnly,
		CommentLang: f.Lang, Q: f.Q,
	})
	return rows, int(total), err
}

func (s *Service) CommentCounts(ctx context.Context) (CommentCounts, error) {
	row, err := s.db.Q.CountAdminCommentsByTab(ctx)
	if err != nil {
		return CommentCounts{}, err
	}
	return CommentCounts{
		NeedsAction: int(row.NeedsAction), Flagged: int(row.Flagged),
		Resolved: int(row.Resolved), Hidden: int(row.Hidden),
	}, nil
}

func (s *Service) ModerateComment(ctx context.Context, a Actor, id uuid.UUID, action string) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		switch action {
		case "hide", "show":
			status := "hidden"
			if action == "show" {
				status = "visible"
			}
			if err := q.SetCommentStatus(ctx, sqlc.SetCommentStatusParams{Status: status, ID: id}); err != nil {
				return err
			}
			if err := q.ResolveCommentReports(ctx, sqlc.ResolveCommentReportsParams{Status: "actioned", CommentID: id}); err != nil {
				return err
			}
		case "dismiss", "keep", "resolve":
			// Three words for one decision from the moderator's side: the comment
			// stays and its reports are settled. "keep" is what the screen says over
			// a flagged card, "resolve" over one that only needed an answer.
			if err := q.ResolveCommentReports(ctx, sqlc.ResolveCommentReportsParams{Status: "dismissed", CommentID: id}); err != nil {
				return err
			}
		case "reopen":
			// Settled by mistake: the reports go back to open and the comment returns
			// to Needs action.
			if err := q.ReopenCommentReports(ctx, id); err != nil {
				return err
			}
		case "suspend":
			// The comment goes, and so does the author for a week. Suspending is a
			// sanction like any other, so it is recorded as one (§13).
			author, err := q.CommentAuthor(ctx, id)
			if err != nil {
				return err
			}
			if !author.Valid {
				return apperr.Invalid(map[string]string{"author": "this comment has no account behind it"})
			}
			if err := q.SetCommentStatus(ctx, sqlc.SetCommentStatusParams{Status: "hidden", ID: id}); err != nil {
				return err
			}
			if err := q.ResolveCommentReports(ctx, sqlc.ResolveCommentReportsParams{Status: "actioned", CommentID: id}); err != nil {
				return err
			}
			if _, err := q.AddSanction(ctx, sqlc.AddSanctionParams{
				UserID: author.UUID, Kind: sqlc.SanctionKindSuspension,
				Reason: "comment", ByID: uuid.NullUUID{UUID: a.ID, Valid: true},
				Until: pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, 7), Valid: true},
			}); err != nil {
				return err
			}
			if err := q.RevokeUserSessions(ctx, author.UUID); err != nil {
				return err
			}
		default:
			return apperr.Invalid(map[string]string{"action": "hide, show, keep, resolve, reopen or suspend"})
		}
		return audit(ctx, q, a, "comment."+action, "comment", id.String(), nil)
	})
}

// ---------------------------------------------------------------- A09 / A10

// RatingSummary is A09's three cards: how players feel overall, how many questions
// are below the threshold, and which reason they pick most.
type RatingSummary struct {
	Ratings         int
	PositivePercent int
	ReasonsGiven    int
	TopReason       string
	TopReasonCount  int
	BelowThreshold  int
}

// RatedFilter is the screen's toolbar: a window in days and one reason.
type RatedFilter struct {
	Lang    string
	Days    int
	Reason  string
	Page    int
	PerPage int
}

// RatedQuestion is one row of "Questions to fix".
type RatedQuestion struct {
	ID          int64
	Body        string
	Category    string
	Color       string
	Icon        string
	Status      string
	Ratings     int
	Up          int
	Down        int
	DownPercent int
	Reasons     map[string]int
}

// RatingOverview reads the three cards. The count below the threshold comes from the
// same query the list is built on, so the card and the list cannot disagree.
func (s *Service) RatingOverview(ctx context.Context, f RatedFilter) (RatingSummary, error) {
	row, err := s.db.Q.RatingSummary(ctx, int32(f.Days))
	if err != nil {
		return RatingSummary{}, err
	}
	out := RatingSummary{
		Ratings: int(row.Ratings), PositivePercent: int(row.PositivePercent),
		ReasonsGiven: int(row.ReasonsGiven),
		TopReason:    row.TopReason, TopReasonCount: int(row.TopReasonCount),
	}
	below, err := s.db.Q.CountRatedPoorly(ctx)
	if err != nil {
		return out, err
	}
	out.BelowThreshold = int(below)
	return out, nil
}

// RatingReasons is the segmented control, built from what players have picked.
func (s *Service) RatingReasons(ctx context.Context) ([]string, error) {
	rows, err := s.db.Q.RatingReasons(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Reason)
	}
	return out, nil
}

// RatedDetailed is the list, worst share of negative first.
func (s *Service) RatedDetailed(ctx context.Context, f RatedFilter) ([]RatedQuestion, error) {
	rows, err := s.db.Q.RatedPoorlyDetailed(ctx, sqlc.RatedPoorlyDetailedParams{
		Lang: f.Lang, Reason: f.Reason,
		Lim: int32(f.PerPage), Off: int32((f.Page - 1) * f.PerPage),
	})
	if err != nil {
		return nil, err
	}
	out := make([]RatedQuestion, 0, len(rows))
	for _, r := range rows {
		q := RatedQuestion{
			ID: r.ID, Body: r.Body, Category: r.CategoryName,
			Color: r.CategoryColor, Icon: r.CategoryIcon, Status: string(r.Status),
			Ratings: int(r.Ratings), Up: int(r.Up), Down: int(r.Down),
			DownPercent: int(r.DownPercent),
		}
		_ = json.Unmarshal(r.Reasons, &q.Reasons)
		out = append(out, q)
	}
	return out, nil
}

func (s *Service) RatedPoorly(ctx context.Context, lang string, page, perPage int) ([]sqlc.RatedPoorlyRow, int, error) {
	rows, err := s.db.Q.RatedPoorly(ctx, sqlc.RatedPoorlyParams{
		Lang: lang, Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountRatedPoorly(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, int(total), nil
}

type Health struct {
	Coverage   []sqlc.TranslationCoverageRow
	Duplicates []sqlc.OpenDuplicatesRow
	Reports    []sqlc.OpenReportsRow
	Counts     map[string]int
	// The two lists are paged separately: they sit on one screen but are different
	// piles of work.
	DuplicateTotal int
	ReportTotal    int
}

// countInt runs one of the :one count queries and widens the result.
func (s *Service) countInt(ctx context.Context, q func(context.Context) (int32, error)) (int, error) {
	n, err := q(ctx)
	return int(n), err
}

// HealthPerPage is how many pairs and how many reports A10 shows at once. The two
// lists used to stop at twenty and say nothing about the rest.
const HealthPerPage = 20

func (s *Service) Health(ctx context.Context, lang string, dupPage, repPage int) (Health, error) {
	var out Health
	var err error
	if dupPage < 1 {
		dupPage = 1
	}
	if repPage < 1 {
		repPage = 1
	}
	if out.Coverage, err = s.db.Q.TranslationCoverage(ctx); err != nil {
		return out, err
	}
	if out.Duplicates, err = s.db.Q.OpenDuplicates(ctx, sqlc.OpenDuplicatesParams{
		Lang: lang, Lim: HealthPerPage, Off: int32((dupPage - 1) * HealthPerPage),
	}); err != nil {
		return out, err
	}
	if out.DuplicateTotal, err = s.countInt(ctx, s.db.Q.CountOpenDuplicates); err != nil {
		return out, err
	}
	if out.Reports, err = s.db.Q.OpenReports(ctx, sqlc.OpenReportsParams{
		Lang: lang, Lim: HealthPerPage, Off: int32((repPage - 1) * HealthPerPage),
	}); err != nil {
		return out, err
	}
	if out.ReportTotal, err = s.countInt(ctx, s.db.Q.CountOpenReports); err != nil {
		return out, err
	}
	out.Counts, err = s.QuestionCounts(ctx)
	return out, err
}

func (s *Service) ResolveDuplicate(ctx context.Context, a Actor, aID, bID int64, status string) error {
	switch status {
	case "merged", "dismissed":
	default:
		return apperr.Invalid(map[string]string{"status": "merged or dismissed"})
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.SetDuplicateStatus(ctx, sqlc.SetDuplicateStatusParams{
			Status: status, QuestionA: aID, QuestionB: bID,
		}); err != nil {
			return err
		}
		if status == "merged" {
			if _, err := q.SetQuestionStatus(ctx, sqlc.SetQuestionStatusParams{
				Status: sqlc.QuestionStatusArchived, ID: bID,
			}); err != nil {
				return err
			}
		}
		return audit(ctx, q, a, "duplicate."+status, "question", itoa(bID), map[string]any{"other": aID})
	})
}

func (s *Service) ResolveReport(ctx context.Context, a Actor, id uuid.UUID, status string) error {
	switch status {
	case "resolved", "dismissed":
	default:
		return apperr.Invalid(map[string]string{"status": "resolved or dismissed"})
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.SetReportStatus(ctx, sqlc.SetReportStatusParams{
			Status: status, ResolvedBy: uuid.NullUUID{UUID: a.ID, Valid: true}, ID: id,
		}); err != nil {
			return err
		}
		return audit(ctx, q, a, "report."+status, "question_report", id.String(), nil)
	})
}

// FindDuplicates is the worker job behind A10.
func (s *Service) FindDuplicates(ctx context.Context, threshold float32) error {
	return s.db.Q.FindDuplicates(ctx, threshold)
}

// ---------------------------------------------------------------- A11 audit log

// AuditFilter is everything A11's toolbar can narrow the log by. Days is the range
// the segmented control picks: 1, 7 or 30, and 0 for the whole log.
type AuditFilter struct {
	Q      string
	Actor  string
	Action string
	Area   string
	Days   int
	Page   int
	PerPag int
}

// AuditActor is one entry of the "Anyone" menu.
type AuditActor struct {
	Username string
	Name     string
	Entries  int
}

func (s *Service) Audit(ctx context.Context, f AuditFilter) ([]sqlc.AuditLogRow, int, error) {
	since := pgtype.Timestamptz{}
	if f.Days > 0 {
		since = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, -f.Days), Valid: true}
	}
	rows, err := s.db.Q.AuditLog(ctx, sqlc.AuditLogParams{
		Actor: f.Actor, Action: f.Action, Entity: f.Area, Q: f.Q, Since: since,
		Lim: int32(f.PerPag), Off: int32((f.Page - 1) * f.PerPag),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountAuditLog(ctx, sqlc.CountAuditLogParams{
		Actor: f.Actor, Action: f.Action, Entity: f.Area, Q: f.Q, Since: since,
	})
	return rows, int(total), err
}

// AuditNames resolves the ids on one page of the log into the names A11 prints in
// its Target column. Three queries for a page, whatever its length — the alternative
// is one per row, which is what a 50-row page would have cost.
func (s *Service) AuditNames(ctx context.Context, lang string, rows []sqlc.AuditLogRow) map[string]string {
	var users []uuid.UUID
	var categories, domains []int64
	for _, r := range rows {
		if !r.EntityID.Valid || r.EntityID.String == "" {
			continue
		}
		switch r.Entity {
		case "user":
			if id, err := uuid.Parse(r.EntityID.String); err == nil {
				users = append(users, id)
			}
		case "category", "categories":
			if id, err := strconv.ParseInt(r.EntityID.String, 10, 64); err == nil {
				categories = append(categories, id)
			}
		case "domain", "domains":
			if id, err := strconv.ParseInt(r.EntityID.String, 10, 64); err == nil {
				domains = append(domains, id)
			}
		}
	}
	out := map[string]string{}
	if len(users) > 0 {
		if named, err := s.db.Q.AuditUserNames(ctx, users); err == nil {
			for _, n := range named {
				out["user:"+n.ID] = "@" + n.Name
			}
		}
	}
	if len(categories) > 0 {
		if named, err := s.db.Q.AuditCategoryNames(ctx, sqlc.AuditCategoryNamesParams{Lang: lang, Ids: categories}); err == nil {
			for _, n := range named {
				out["category:"+n.ID] = n.Name
			}
		}
	}
	if len(domains) > 0 {
		if named, err := s.db.Q.AuditDomainNames(ctx, sqlc.AuditDomainNamesParams{Lang: lang, Ids: domains}); err == nil {
			for _, n := range named {
				out["domain:"+n.ID] = n.Name
			}
		}
	}
	return out
}

func (s *Service) AuditActions(ctx context.Context) ([]string, error) {
	return s.db.Q.AuditActions(ctx)
}

func (s *Service) AuditAreas(ctx context.Context) ([]string, error) {
	return s.db.Q.AuditAreas(ctx)
}

func (s *Service) AuditActors(ctx context.Context) ([]AuditActor, error) {
	rows, err := s.db.Q.AuditActors(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AuditActor, 0, len(rows))
	for _, r := range rows {
		name := r.Username.String
		if r.DisplayName != "" {
			name = r.DisplayName
		}
		out = append(out, AuditActor{Username: r.Username.String, Name: name, Entries: int(r.Entries)})
	}
	return out, nil
}

// Write records a staff action taken outside a transaction of its own (e.g. a support
// reply), so the audit trail stays complete.
func (s *Service) Write(ctx context.Context, a Actor, action, entity, entityID string, diff any) error {
	return audit(ctx, s.db.Q, a, action, entity, entityID, diff)
}

func text(v string) pgtype.Text { return pgtype.Text{String: v, Valid: v != ""} }

// parseIP keeps a bad or missing address out of the audit row rather than failing a write.
func parseIP(v string) *netip.Addr {
	addr, err := netip.ParseAddr(v)
	if err != nil {
		return nil
	}
	return &addr
}

func itoa(v int64) string {
	if v == 0 {
		return ""
	}
	digits := ""
	negative := v < 0
	if negative {
		v = -v
	}
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	if negative {
		return "-" + digits
	}
	return digits
}

var _ = actorOf

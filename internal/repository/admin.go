package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// ------------------------------------------------------------- audit log --

// Audit records a privileged action. It is deliberately best-effort at the
// call site — an admin action must not fail because logging did — but the
// error is returned so callers can log it.
func (r *Repo) Audit(ctx context.Context, e models.AuditEntry) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO admin_audit
			(actor_id, actor_username, action, target_kind, target_id, detail, ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ActorID, e.ActorUsername, e.Action, e.TargetKind, e.TargetID, e.Detail, e.IP)
	return err
}

// CountAuditEntries is how many lines the trail holds, so the screen can page
// through it instead of stopping silently at the first two hundred — which is
// exactly the wrong behaviour for the record you consult after something went
// wrong.
func (r *Repo) CountAuditEntries(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit`).Scan(&n)
	return n, err
}

// AuditPage is one page of the trail.
func (r *Repo) AuditPage(ctx context.Context, limit, offset int) ([]*models.AuditEntry, error) {
	return r.auditQuery(ctx, limit, offset)
}

func (r *Repo) AuditTrail(ctx context.Context, limit int) ([]*models.AuditEntry, error) {
	return r.auditQuery(ctx, limit, 0)
}

func (r *Repo) auditQuery(ctx context.Context, limit, offset int) ([]*models.AuditEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, actor_id, actor_username, action, target_kind, target_id,
		       detail, ip, created_at
		  FROM admin_audit ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.AuditEntry
	for rows.Next() {
		var e models.AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorUsername, &e.Action,
			&e.TargetKind, &e.TargetID, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------ user admin --

// AdminUserFilter is the filter set for the admin user directory. Unlike the
// player-facing search this one may legitimately list everyone.
type AdminUserFilter struct {
	Query   string
	Role    string
	Status  string
	Country string
	Limit   int
	Offset  int
}

func (r *Repo) AdminUsers(ctx context.Context, f AdminUserFilter) ([]*models.AdminUser, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}

	// Accounts only. Temporary players are swept on a timer and have no
	// address, no role and nothing to administer; listing them would fill the
	// screen with rows whose every action is meaningless.
	where := []string{"NOT is_temporary"}
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	if q := strings.TrimSpace(f.Query); q != "" {
		add("(username ILIKE '%%' || $%d || '%%' OR display_name ILIKE '%%' || $%[1]d || '%%' OR COALESCE(email::text, '') ILIKE '%%' || $%[1]d || '%%')", q)
	}
	if f.Role != "" {
		add("role = $%d::user_role", f.Role)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.Country != "" {
		add("country = $%d", f.Country)
	}

	clause := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT id, username, display_name, avatar_seed, COALESCE(email::text, ''), role::text, status,
		       suspended_reason, country, locale, xp, games_played,
		       created_at, last_seen_at
		  FROM users WHERE `+clause+`
		 ORDER BY created_at DESC
		 LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.AdminUser
	for rows.Next() {
		var u models.AdminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarSeed, &u.Email,
			&u.Role, &u.Status, &u.SuspendedReason, &u.Country, &u.Locale,
			&u.XP, &u.GamesPlayed, &u.CreatedAt, &u.LastSeenAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &u)
	}
	return out, total, rows.Err()
}

// CreateUserWithRole is the admin-side account creation path. It mirrors
// registration but lets the caller choose the role up front.
func (r *Repo) CreateUserWithRole(ctx context.Context, u *models.User) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, display_name,
		                   avatar_seed, locale, role, country)
		VALUES ($1, $2, $3, $4, $5, $6, $7::user_role, $8)
		RETURNING id, created_at, updated_at, last_seen_at`,
		u.Username, u.Email, u.PasswordHash, u.DisplayName,
		u.AvatarSeed, u.Locale, u.Role, u.Country,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.LastSeenAt)

	if isUniqueViolation(err) {
		if strings.Contains(err.Error(), "email") {
			return fmt.Errorf("%w: email", ErrConflict)
		}
		return fmt.Errorf("%w: username", ErrConflict)
	}
	return err
}

// SetUserRole changes a role, refusing to remove the last admin.
func (r *Repo) SetUserRole(ctx context.Context, id uuid.UUID, role string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var current string
		err := tx.QueryRow(ctx,
			`SELECT role::text FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		if current == models.RoleAdmin && role != models.RoleAdmin {
			var others int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE role = 'admin' AND id <> $1`, id).Scan(&others); err != nil {
				return err
			}
			if others == 0 {
				return ErrLastAdmin
			}
		}

		_, err = tx.Exec(ctx,
			`UPDATE users SET role = $2::user_role, updated_at = now() WHERE id = $1`, id, role)
		return err
	})
}

// SetUserStatus suspends or reinstates an account. A suspended admin would be
// able to un-suspend themselves, so demote before suspending.
func (r *Repo) SetUserStatus(ctx context.Context, id uuid.UUID, status, reason string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE users SET status = $2, suspended_reason = $3, updated_at = now()
		 WHERE id = $1`, id, status, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUserAsAdmin removes an account, refusing to delete the last admin.
func (r *Repo) DeleteUserAsAdmin(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(ctx,
			`SELECT role::text FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if role == models.RoleAdmin {
			var others int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE role = 'admin' AND id <> $1`, id).Scan(&others); err != nil {
				return err
			}
			if others == 0 {
				return ErrLastAdmin
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		return err
	})
}

func (r *Repo) AdminUser(ctx context.Context, id uuid.UUID) (*models.AdminUser, error) {
	var u models.AdminUser
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, display_name, avatar_seed, COALESCE(email::text, ''), role::text, status,
		       suspended_reason, country, locale, xp, games_played,
		       created_at, last_seen_at
		  FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Role, &u.Status,
		&u.SuspendedReason, &u.Country, &u.Locale, &u.XP, &u.GamesPlayed,
		&u.CreatedAt, &u.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

// CountriesInUse backs the country filter without a hardcoded list.
func (r *Repo) CountriesInUse(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT country FROM users
		  WHERE country <> '' AND NOT is_temporary ORDER BY country`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// -------------------------------------------------------- question admin --

// AdminQuestionFilter drives the admin question browser.
type AdminQuestionFilter struct {
	Query      string
	CategoryID int
	Difficulty int
	Locale     string
	OnlyReview bool
	OnlyActive bool
	// MinLocales, when positive, keeps only questions with fewer translations
	// than that — the queue of things still to be written or translated. The
	// number is passed in rather than read from i18n so this package stays
	// free of the shipped-locale list.
	MinLocales int
	Limit      int
	Offset     int
}

// questionWhere builds the filter shared by the listing and by any operation
// that acts on "everything matching what the admin is looking at". Both go
// through here so a bulk action can never select a different set than the
// screen it was launched from.
//
// $1 is the locale. It has to appear in the clause because the count query and
// the page query share one argument list, and pgx rejects a statement whose
// parameter count does not match.
func questionWhere(f AdminQuestionFilter) (clause string, args []any) {
	locale := f.Locale
	if locale == "" {
		locale = "ar"
	}

	// The bank is everything without a match behind it. A question one player
	// wrote for one match is not content anybody administers, and it must not
	// appear in a list, a count or a bulk selection that means "the bank".
	where := []string{"$1::text IS NOT NULL", "q.author_id IS NULL"}
	args = []any{locale}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	if f.CategoryID > 0 {
		add("q.category_id = $%d", f.CategoryID)
	}
	if f.Difficulty > 0 {
		add("q.difficulty = $%d", f.Difficulty)
	}
	if f.OnlyActive {
		where = append(where, "q.is_active")
	}
	if f.OnlyReview {
		where = append(where,
			"EXISTS (SELECT 1 FROM question_translations rt WHERE rt.question_id = q.id AND rt.needs_review)")
	}
	if f.MinLocales > 0 {
		add("(SELECT count(*) FROM question_translations lt WHERE lt.question_id = q.id) < $%d", f.MinLocales)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		add("EXISTS (SELECT 1 FROM question_translations st WHERE st.question_id = q.id AND st.prompt ILIKE '%%' || $%d || '%%')", q)
	}

	return strings.Join(where, " AND "), args
}

// AdminQuestionIDs lists every question the filter matches, ignoring the page
// the admin happens to be on. It is what "select everything that matches"
// resolves to, so a bulk action can be applied to the whole result set rather
// than only to the rows currently on screen.
func (r *Repo) AdminQuestionIDs(ctx context.Context, f AdminQuestionFilter) ([]int, error) {
	clause, args := questionWhere(f)

	rows, err := r.pool.Query(ctx,
		`SELECT q.id FROM questions q WHERE `+clause+` ORDER BY q.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repo) AdminQuestions(ctx context.Context, f AdminQuestionFilter) ([]*models.AdminQuestion, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}

	clause, args := questionWhere(f)

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM questions q WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT q.id, q.category_id, COALESCE(ct.name, c.slug), c.icon,
		       q.difficulty, q.points, q.correct_index, q.is_active,
		       COALESCE(t.prompt, any_t.prompt, ''),
		       ARRAY(SELECT x.locale FROM question_translations x
		              WHERE x.question_id = q.id ORDER BY x.locale),
		       (SELECT count(*) FROM question_translations x WHERE x.question_id = q.id AND x.needs_review),
		       ARRAY(SELECT x.locale FROM question_translations x
		              WHERE x.question_id = q.id AND x.needs_review ORDER BY x.locale),
		       q.created_at
		  FROM questions q
		  JOIN categories c ON c.id = q.category_id
		  LEFT JOIN category_translations ct ON ct.category_id = c.id AND ct.locale = $1
		  LEFT JOIN question_translations t  ON t.question_id  = q.id AND t.locale  = $1
		  LEFT JOIN LATERAL (
		        SELECT prompt FROM question_translations
		         WHERE question_id = q.id ORDER BY locale LIMIT 1
		  ) any_t ON true
		 WHERE `+clause+`
		 ORDER BY q.id DESC
		 LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.AdminQuestion
	for rows.Next() {
		var q models.AdminQuestion
		if err := rows.Scan(&q.ID, &q.CategoryID, &q.CategoryName, &q.CategoryIcon,
			&q.Difficulty, &q.Points, &q.CorrectIndex, &q.IsActive,
			&q.Prompt, &q.PresentLocales, &q.PendingReview,
			&q.PendingLocales, &q.CreatedAt); err != nil {
			return nil, 0, err
		}
		q.LocaleCount = len(q.PresentLocales)
		out = append(out, &q)
	}
	return out, total, rows.Err()
}

func (r *Repo) SetQuestionActive(ctx context.Context, id int, active bool) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE questions SET is_active = $2, updated_at = now() WHERE id = $1`, id, active)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteQuestion removes a question. Because game_answers cascades from
// questions, deleting one rewrites history — callers should prefer
// SetQuestionActive(false) and are warned in the UI.
func (r *Repo) DeleteQuestion(ctx context.Context, id int) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM questions WHERE id = $1`, id)
	return err
}

// SetQuestionsActive flips activation for a set and reports how many changed.
//
// Deactivating in bulk is the non-destructive twin of deleting in bulk: it
// takes questions out of play without touching the answers already recorded
// against them, which is what an admin clearing out a bad import usually
// wants.
func (r *Repo) SetQuestionsActive(ctx context.Context, ids []int, active bool) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	ct, err := r.pool.Exec(ctx,
		`UPDATE questions SET is_active = $2, updated_at = now() WHERE id = ANY($1)`,
		ids, active)
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// DeleteQuestions removes a set of questions and reports how many rows went.
// One statement rather than a loop, so a bulk delete either takes the whole
// selection or leaves it alone — a half-applied delete would leave the admin
// with no way to tell which rows had gone.
func (r *Repo) DeleteQuestions(ctx context.Context, ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	ct, err := r.pool.Exec(ctx, `DELETE FROM questions WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// QuestionAnswerCount tells the UI whether deleting would destroy history.
func (r *Repo) QuestionAnswerCount(ctx context.Context, id int) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM game_answers WHERE question_id = $1`, id).Scan(&n)
	return n, err
}

// QuestionsAnswerImpact is the same warning as QuestionAnswerCount, for a set:
// how many of these questions have been answered, and how many recorded
// answers deleting them would destroy.
//
// Both numbers, because they say different things to the person about to press
// the button — "3 of the 40 you picked" is what makes it survivable, "and 812
// answers" is what makes it serious.
func (r *Repo) QuestionsAnswerImpact(ctx context.Context, ids []int) (questions, answers int, err error) {
	if len(ids) == 0 {
		return 0, 0, nil
	}
	err = r.pool.QueryRow(ctx, `
		SELECT count(DISTINCT question_id), count(*)
		  FROM game_answers WHERE question_id = ANY($1)`, ids).Scan(&questions, &answers)
	return questions, answers, err
}

// UpsertQuestion writes a question and the locales supplied with it, in one
// transaction so a half-translated question can never be stored.
func (r *Repo) UpsertQuestion(ctx context.Context, q *models.QuestionDraft) (int, error) {
	var id int

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if q.ID > 0 {
			// An explicit id is authoritative, exactly as it is for the bundled
			// seed file: re-importing a file updates the rows it names instead
			// of inserting parallel copies under ids Postgres picked. The
			// importer used to drop the file's id on insert, so a second run of
			// the same file was refused by the duplicate check rather than
			// updating what it had already added.
			err := tx.QueryRow(ctx, `
				INSERT INTO questions (id, category_id, difficulty, points,
				                       correct_index, source, is_active, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (id) DO UPDATE SET
					category_id   = EXCLUDED.category_id,
					difficulty    = EXCLUDED.difficulty,
					points        = EXCLUDED.points,
					correct_index = EXCLUDED.correct_index,
					source        = EXCLUDED.source,
					is_active     = EXCLUDED.is_active,
					updated_at    = now()
				RETURNING id`,
				q.ID, q.CategoryID, q.Difficulty, q.Points, q.CorrectIndex,
				q.Source, q.IsActive, q.CreatedBy).Scan(&id)
			if err != nil {
				return err
			}
			// Keep the sequence past the ids written by hand, or the next
			// serial insert collides with one of them.
			if _, err := tx.Exec(ctx, `
				SELECT setval('questions_id_seq',
				              GREATEST((SELECT max(id) FROM questions), 1))`); err != nil {
				return err
			}
		} else {
			err := tx.QueryRow(ctx, `
				INSERT INTO questions (category_id, difficulty, points,
				                       correct_index, source, is_active, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				RETURNING id`,
				q.CategoryID, q.Difficulty, q.Points, q.CorrectIndex,
				q.Source, q.IsActive, q.CreatedBy).Scan(&id)
			if err != nil {
				return err
			}
		}

		for locale, t := range q.Translations {
			if len(t.Choices) != 4 {
				return fmt.Errorf("locale %s needs exactly 4 choices, got %d", locale, len(t.Choices))
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO question_translations
					(question_id, locale, prompt, choices, explanation, source, needs_review)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (question_id, locale) DO UPDATE SET
					prompt       = EXCLUDED.prompt,
					choices      = EXCLUDED.choices,
					explanation  = EXCLUDED.explanation,
					source       = EXCLUDED.source,
					needs_review = EXCLUDED.needs_review`,
				id, locale, t.Prompt, t.Choices, t.Explanation, t.Source, t.NeedsReview)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// QuestionDraftsByIDs loads many drafts in two round trips instead of two per
// question. The duplicate sweep compares sixty pairs at once, and reading each
// side one at a time made a page of comparisons into a couple of hundred
// queries. Ids with no question behind them are simply absent from the result.
func (r *Repo) QuestionDraftsByIDs(ctx context.Context, ids []int) (map[int]*models.QuestionDraft, error) {
	out := make(map[int]*models.QuestionDraft, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, category_id, difficulty, points, correct_index, source, is_active
		  FROM questions WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		d := &models.QuestionDraft{Translations: map[string]models.TranslationDraft{}}
		if err := rows.Scan(&d.ID, &d.CategoryID, &d.Difficulty, &d.Points,
			&d.CorrectIndex, &d.Source, &d.IsActive); err != nil {
			rows.Close()
			return nil, err
		}
		out[d.ID] = d
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	trs, err := r.pool.Query(ctx, `
		SELECT question_id, locale, prompt, choices, explanation, source, needs_review
		  FROM question_translations WHERE question_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer trs.Close()

	for trs.Next() {
		var questionID int
		var locale string
		var t models.TranslationDraft
		if err := trs.Scan(&questionID, &locale, &t.Prompt, &t.Choices,
			&t.Explanation, &t.Source, &t.NeedsReview); err != nil {
			return nil, err
		}
		if d, ok := out[questionID]; ok {
			d.Translations[locale] = t
		}
	}
	return out, trs.Err()
}

// QuestionDraftByID loads every locale of a question for the edit form.
func (r *Repo) QuestionDraftByID(ctx context.Context, id int) (*models.QuestionDraft, error) {
	d := &models.QuestionDraft{ID: id, Translations: map[string]models.TranslationDraft{}}

	err := r.pool.QueryRow(ctx, `
		SELECT category_id, difficulty, points, correct_index, source, is_active
		  FROM questions WHERE id = $1`, id,
	).Scan(&d.CategoryID, &d.Difficulty, &d.Points, &d.CorrectIndex, &d.Source, &d.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT locale, prompt, choices, explanation, source, needs_review
		  FROM question_translations WHERE question_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var locale string
		var t models.TranslationDraft
		if err := rows.Scan(&locale, &t.Prompt, &t.Choices, &t.Explanation,
			&t.Source, &t.NeedsReview); err != nil {
			return nil, err
		}
		d.Translations[locale] = t
	}
	return d, rows.Err()
}

// ApproveTranslation clears the review flag so players can finally see it.
func (r *Repo) ApproveTranslation(ctx context.Context, questionID int, locale string, reviewer uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE question_translations
		   SET needs_review = false, reviewed_by = $3, reviewed_at = now()
		 WHERE question_id = $1 AND locale = $2`, questionID, locale, reviewer)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) RejectTranslation(ctx context.Context, questionID int, locale string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM question_translations WHERE question_id = $1 AND locale = $2`,
		questionID, locale)
	return err
}

// PendingReviewCount drives the admin nav badge.
func (r *Repo) PendingReviewCount(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM question_translations WHERE needs_review`).Scan(&n)
	return n, err
}

// --------------------------------------------------------- platform stats --

func (r *Repo) PlatformStats(ctx context.Context, locale string) (*models.PlatformStats, error) {
	s := &models.PlatformStats{}

	err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users WHERE NOT is_temporary),
			(SELECT count(*) FROM users WHERE role = 'admin' AND NOT is_temporary),
			(SELECT count(*) FROM users WHERE role = 'moderator' AND NOT is_temporary),
			(SELECT count(*) FROM users WHERE status = 'suspended' AND NOT is_temporary),
			(SELECT count(*) FROM users WHERE last_seen_at > now() - interval '1 day' AND NOT is_temporary),
			(SELECT count(*) FROM users WHERE created_at > now() - interval '7 days' AND NOT is_temporary),
			(SELECT count(*) FROM questions WHERE author_id IS NULL),
			(SELECT count(*) FROM questions WHERE is_active AND author_id IS NULL),
			(SELECT count(*) FROM categories WHERE is_active),
			(SELECT count(*) FROM question_translations),
			(SELECT count(*) FROM question_translations WHERE needs_review),
			(SELECT count(*) FROM game_sessions WHERE status = 'finished'),
			(SELECT count(*) FROM game_answers),
			(SELECT count(*) FROM challenges),
			(SELECT count(*) FROM messages),
			(SELECT count(*) FROM friendships WHERE status = 'accepted')`,
	).Scan(&s.Users, &s.Admins, &s.Moderators, &s.Suspended, &s.ActiveToday,
		&s.NewThisWeek, &s.Questions, &s.ActiveQuestions, &s.Categories,
		&s.Translations, &s.PendingReview, &s.RoundsPlayed, &s.AnswersRecorded,
		&s.Duels, &s.Messages, &s.Friendships)
	if err != nil {
		return nil, err
	}

	if s.Coverage, err = r.coverage(ctx, locale); err != nil {
		return nil, err
	}
	return s, nil
}

// coverage reports how many questions exist per category and difficulty, which
// is what tells an operator where the bank is thin.
func (r *Repo) coverage(ctx context.Context, locale string) ([]models.CoverageRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, COALESCE(ct.name, fb.name, c.slug), c.icon, q.difficulty, count(*)
		  FROM questions q
		  JOIN categories c ON c.id = q.category_id
		  LEFT JOIN category_translations ct ON ct.category_id = c.id AND ct.locale = $1
		  LEFT JOIN category_translations fb ON fb.category_id = c.id AND fb.locale = 'ar'
		 WHERE q.is_active
		 GROUP BY c.id, ct.name, fb.name, c.slug, c.icon, q.difficulty, c.sort_order
		 ORDER BY c.sort_order, q.difficulty`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.CoverageRow
	for rows.Next() {
		var row models.CoverageRow
		if err := rows.Scan(&row.CategoryID, &row.CategoryName, &row.CategoryIcon,
			&row.Difficulty, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------ duplicates --

// DefaultSimilarity is the fallback when a caller passes nothing usable. It
// matches the sweep's own floor, which is the loosest number anything in the
// application treats as "worth a look".
const DefaultSimilarity = 0.55

// MarkPairDistinct records that a moderator has read both questions and judged
// them different. The pair stops being raised until one of them changes.
func (r *Repo) MarkPairDistinct(ctx context.Context, leftID, rightID int, by uuid.UUID) error {
	if leftID == rightID {
		return ErrForbidden
	}
	if leftID > rightID {
		leftID, rightID = rightID, leftID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO question_duplicate_verdicts (left_id, right_id, verdict, decided_by)
		VALUES ($1, $2, 'distinct', $3)
		ON CONFLICT (left_id, right_id) DO UPDATE
		   SET decided_by = EXCLUDED.decided_by, decided_at = now()`,
		leftID, rightID, by)
	return err
}

// ForgetPairVerdict puts a pair back on the list, for a decision taken by
// mistake.
func (r *Repo) ForgetPairVerdict(ctx context.Context, leftID, rightID int) error {
	if leftID > rightID {
		leftID, rightID = rightID, leftID
	}
	_, err := r.pool.Exec(ctx,
		`DELETE FROM question_duplicate_verdicts WHERE left_id = $1 AND right_id = $2`,
		leftID, rightID)
	return err
}

// NearDuplicates finds question prompts that are textually similar, using the
// trigram index so the sweep stays indexable instead of O(n²).
//
// One language at a time. A pair of questions written in three languages
// surfaces three times otherwise — the same two ids, the same verdict, once per
// language they share — and a list that repeats itself three times over is a
// list nobody reads to the end. Which language is a choice for the screen; the
// comparison behind each pair still carries all of them.
func (r *Repo) NearDuplicates(ctx context.Context, threshold float64, limit int,
	locale string) ([]*models.DuplicatePair, error) {

	if threshold <= 0 {
		threshold = DefaultSimilarity
	}

	// In a transaction for the sake of one setting. The `%` operator picks its
	// candidates by pg_trgm.similarity_threshold, which defaults to 0.3 — a long
	// way below what the WHERE then keeps — so every pair between the two was
	// fetched, scored and thrown away. On six thousand translations that is ten
	// seconds of work to return sixty rows. Telling the operator the number the
	// query actually filters by takes it to about one second.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT set_config('pg_trgm.similarity_threshold', $1, true)`,
		strconv.FormatFloat(threshold, 'f', -1, 64)); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT a.question_id, b.question_id, a.locale, a.prompt, b.prompt,
		       similarity(a.prompt, b.prompt) AS score
		  FROM question_translations a
		  JOIN questions qa ON qa.id = a.question_id AND qa.author_id IS NULL
		  JOIN question_translations b
		    ON b.locale = a.locale
		   AND b.question_id > a.question_id
		   AND b.prompt % a.prompt
		 WHERE similarity(a.prompt, b.prompt) >= $1
		   AND ($3 = '' OR a.locale = $3)
		   -- Not what somebody has already read and called different.
		   AND NOT EXISTS (
		       SELECT 1 FROM question_duplicate_verdicts v
		        WHERE v.left_id  = least(a.question_id, b.question_id)
		          AND v.right_id = greatest(a.question_id, b.question_id))
		 ORDER BY score DESC
		 LIMIT $2`, threshold, limit, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.DuplicatePair
	for rows.Next() {
		var d models.DuplicatePair
		if err := rows.Scan(&d.LeftID, &d.RightID, &d.Locale,
			&d.LeftPrompt, &d.RightPrompt, &d.Score); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// IntegrityIssues collects the content problems that are worth an operator's
// attention but are not hard database constraints.
func (r *Repo) IntegrityIssues(ctx context.Context) ([]*models.IntegrityIssue, error) {
	checks := []struct {
		kind string
		sql  string
	}{
		{"missing_locale", `
			SELECT q.id, 'question ' || q.id || ' has only ' ||
			       count(t.locale) || ' of 3 locales'
			  FROM questions q
			  LEFT JOIN question_translations t ON t.question_id = q.id
			 GROUP BY q.id HAVING count(t.locale) < 3`},

		{"duplicate_choice", `
			SELECT t.question_id,
			       'duplicate answer choice in ' || t.locale || ': ' || dup.c
			  FROM question_translations t
			  CROSS JOIN LATERAL (
			        SELECT c, count(*) AS n FROM unnest(t.choices) AS c
			         GROUP BY c HAVING count(*) > 1 LIMIT 1
			  ) dup`},

		{"choice_count", `
			SELECT question_id,
			       'locale ' || locale || ' has ' ||
			       COALESCE(array_length(choices, 1), 0) || ' choices, expected 4'
			  FROM question_translations
			 WHERE COALESCE(array_length(choices, 1), 0) <> 4`},

		{"empty_prompt", `
			SELECT question_id, 'empty prompt in ' || locale
			  FROM question_translations WHERE btrim(prompt) = ''`},

		{"correct_index", `
			SELECT q.id, 'correct_index ' || q.correct_index || ' is out of range'
			  FROM questions q WHERE q.correct_index < 0 OR q.correct_index > 3`},

		{"no_explanation", `
			SELECT question_id, 'no explanation in ' || locale
			  FROM question_translations WHERE btrim(explanation) = ''`},

		{"orphan_category", `
			SELECT q.id, 'category is inactive'
			  FROM questions q JOIN categories c ON c.id = q.category_id
			 WHERE q.is_active AND NOT c.is_active`},
	}

	var out []*models.IntegrityIssue
	for _, check := range checks {
		rows, err := r.pool.Query(ctx, check.sql)
		if err != nil {
			return nil, fmt.Errorf("integrity check %s: %w", check.kind, err)
		}
		for rows.Next() {
			issue := &models.IntegrityIssue{Kind: check.kind}
			if err := rows.Scan(&issue.QuestionID, &issue.Detail); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, issue)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SimilarPrompts finds prompts that read like this one, at or above minScore.
//
// The threshold is the caller's, and it is applied twice on purpose: once as
// pg_trgm's own candidate filter and once in the WHERE. `%` decides what the
// index hands back, and it answers to pg_trgm.similarity_threshold — 0.3 by
// default. Left alone, a caller asking for 0.85 got every pair from 0.3
// upwards fetched, scored and thrown away: 566 rows to keep 14 on the real
// bank, 11.6 ms where 1.3 ms would do, once per row per language of an import.
func (r *Repo) SimilarPrompts(ctx context.Context, prompt, locale string, excludeID int, limit int, minScore float64) ([]*models.DuplicatePair, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, nil
	}
	if minScore <= 0 || minScore > 1 {
		minScore = DefaultSimilarity
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT set_config('pg_trgm.similarity_threshold', $1, true)`,
		strconv.FormatFloat(minScore, 'f', -1, 64)); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT question_id, prompt, similarity(prompt, $1) AS score
		  FROM question_translations
		 WHERE locale = $2
		   AND question_id <> $3
		   AND prompt % $1
		   AND similarity(prompt, $1) >= $5
		 ORDER BY score DESC
		 LIMIT $4`, prompt, locale, excludeID, limit, minScore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.DuplicatePair
	for rows.Next() {
		var d models.DuplicatePair
		d.Locale = locale
		d.LeftPrompt = prompt
		if err := rows.Scan(&d.RightID, &d.RightPrompt, &d.Score); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// ExistingQuestionIDs reports which of these ids the bank already holds.
//
// The importer asks "does this row update something?" once per row, and used
// to answer it with QuestionDraftByID — two queries, and every translation of
// the question loaded, to produce a yes or a no.
func (r *Repo) ExistingQuestionIDs(ctx context.Context, ids []int) (map[int]bool, error) {
	out := make(map[int]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id FROM questions WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// PromptProbe is one prompt to look for, tagged with the caller's own key so
// the answers can be matched back up.
type PromptProbe struct {
	Key       int // the caller's identifier — the importer passes the file line
	Prompt    string
	ExcludeID int // a question that cannot be its own duplicate
}

// BestMatches finds, for every probe at once, the closest prompt in one
// language at or above minScore.
//
// One round trip for a whole file instead of one per row. The import preview
// ran the single-prompt query inside two nested loops — every row, every
// language — which on a 300-row trilingual file is 900 trigram probes and
// about ten seconds, and then ran the identical work again on apply.
//
// Absent from the result means nothing was close enough, which is the common
// case and is why this returns a map rather than a row per probe.
func (r *Repo) BestMatches(ctx context.Context, probes []PromptProbe, locale string,
	minScore float64) (map[int]*models.DuplicatePair, error) {

	out := map[int]*models.DuplicatePair{}
	if len(probes) == 0 {
		return out, nil
	}
	if minScore <= 0 || minScore > 1 {
		minScore = DefaultSimilarity
	}

	keys := make([]int, len(probes))
	prompts := make([]string, len(probes))
	excludes := make([]int, len(probes))
	for i, p := range probes {
		keys[i], prompts[i], excludes[i] = p.Key, strings.TrimSpace(p.Prompt), p.ExcludeID
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Same reason as SimilarPrompts: `%` answers to this setting, and left at
	// its default it hands back everything from 0.3 upwards for the query to
	// score and discard.
	if _, err := tx.Exec(ctx,
		`SELECT set_config('pg_trgm.similarity_threshold', $1, true)`,
		strconv.FormatFloat(minScore, 'f', -1, 64)); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT v.key, m.question_id, m.prompt, m.score
		  FROM unnest($1::int[], $2::text[], $3::int[]) AS v(key, prompt, exclude_id)
		  CROSS JOIN LATERAL (
		        SELECT t.question_id, t.prompt, similarity(t.prompt, v.prompt) AS score
		          FROM question_translations t
		         WHERE t.locale = $4
		           AND t.question_id <> v.exclude_id
		           AND t.prompt % v.prompt
		           AND similarity(t.prompt, v.prompt) >= $5
		         ORDER BY score DESC
		         LIMIT 1
		  ) m
		 WHERE v.prompt <> ''`, keys, prompts, excludes, locale, minScore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key int
		d := &models.DuplicatePair{Locale: locale}
		if err := rows.Scan(&key, &d.RightID, &d.RightPrompt, &d.Score); err != nil {
			return nil, err
		}
		out[key] = d
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- imports --

func (r *Repo) RecordImport(ctx context.Context, imp *models.ImportRun) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO question_imports
			(actor_id, filename, format, committed, total, added, updated, skipped, rejected, report)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`,
		imp.ActorID, imp.Filename, imp.Format, imp.Committed, imp.Total,
		imp.Added, imp.Updated, imp.Skipped, imp.Rejected, imp.Report,
	).Scan(&imp.ID, &imp.CreatedAt)
}

func (r *Repo) RecentImports(ctx context.Context, limit int) ([]*models.ImportRun, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT i.id, i.actor_id, COALESCE(u.username, '—'), i.filename, i.format,
		       i.committed, i.total, i.added, i.updated, i.skipped, i.rejected, i.created_at
		  FROM question_imports i
		  LEFT JOIN users u ON u.id = i.actor_id
		 ORDER BY i.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.ImportRun
	for rows.Next() {
		var i models.ImportRun
		if err := rows.Scan(&i.ID, &i.ActorID, &i.ActorUsername, &i.Filename,
			&i.Format, &i.Committed, &i.Total, &i.Added, &i.Updated,
			&i.Skipped, &i.Rejected, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &i)
	}
	return out, rows.Err()
}

// PurgeAuditOlderThan keeps the trail from growing without bound.
func (r *Repo) PurgeAuditOlderThan(ctx context.Context, d time.Duration) (int64, error) {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM admin_audit WHERE created_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(d.Seconds())))
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

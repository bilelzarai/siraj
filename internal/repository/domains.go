package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// The level above a category. Everything here mirrors categories.go function
// for function on purpose: the two are the same shape of thing one level
// apart, and a reader who knows one should not have to learn the other.
//
// What differs is who may write them. Reshaping the taxonomy is structural, so
// domains are admin-only; writing a category inside one is content work and
// stays with moderators (D11).

// Domains lists the active domains with names resolved for locale, falling
// back to Arabic. This is the player's list — the one above the category row
// on the setup screen — so a retired domain is absent, exactly as a retired
// category is.
func (r *Repo) Domains(ctx context.Context, locale string) ([]*models.Domain, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.slug, d.icon, d.color, d.sort_order, d.is_active,
		       COALESCE(t.name, fb.name, d.slug),
		       COALESCE(t.description, fb.description, '')
		  FROM domains d
		  LEFT JOIN domain_translations t  ON t.domain_id  = d.id AND t.locale  = $1 AND NOT t.needs_review
		  LEFT JOIN domain_translations fb ON fb.domain_id = d.id AND fb.locale = 'ar' AND NOT fb.needs_review
		 WHERE d.is_active
		 ORDER BY d.sort_order, d.id`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Domain
	for rows.Next() {
		var d models.Domain
		if err := rows.Scan(&d.ID, &d.Slug, &d.Icon, &d.Color, &d.SortOrder,
			&d.IsActive, &d.Name, &d.Description); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// AdminDomains lists every domain, active or not, with how many categories and
// questions hang below each — the two numbers that decide whether a domain can
// be deleted and whether retiring it would empty the game.
func (r *Repo) AdminDomains(ctx context.Context, locale string) ([]*models.AdminDomain, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.slug, d.icon, d.color, d.sort_order, d.is_active,
		       COALESCE(t.name, fb.name, d.slug),
		       COALESCE(t.description, fb.description, ''),
		       (SELECT count(*) FROM categories c WHERE c.domain_id = d.id),
		       (SELECT count(*) FROM questions q
		          JOIN categories c ON c.id = q.category_id
		         WHERE c.domain_id = d.id),
		       (SELECT count(*) FROM questions q
		          JOIN categories c ON c.id = q.category_id
		         WHERE c.domain_id = d.id AND q.is_active AND c.is_active),
		       ARRAY(SELECT x.locale FROM domain_translations x
		              WHERE x.domain_id = d.id ORDER BY x.locale)
		  FROM domains d
		  LEFT JOIN domain_translations t  ON t.domain_id  = d.id AND t.locale  = $1 AND NOT t.needs_review
		  LEFT JOIN domain_translations fb ON fb.domain_id = d.id AND fb.locale = 'ar' AND NOT fb.needs_review
		 ORDER BY d.sort_order, d.id`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.AdminDomain
	for rows.Next() {
		var d models.AdminDomain
		if err := rows.Scan(&d.ID, &d.Slug, &d.Icon, &d.Color, &d.SortOrder,
			&d.IsActive, &d.Name, &d.Description,
			&d.Categories, &d.Questions, &d.ActiveQuestions, &d.PresentLocales); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// DomainDraft loads one domain with every language it has been written in,
// which is what the edit form is built from.
func (r *Repo) DomainDraft(ctx context.Context, id int) (*models.DomainDraft, error) {
	d := &models.DomainDraft{Names: map[string]models.NameDraft{}}

	err := r.pool.QueryRow(ctx, `
		SELECT id, slug, icon, color, sort_order, is_active
		  FROM domains WHERE id = $1`, id).
		Scan(&d.ID, &d.Slug, &d.Icon, &d.Color, &d.SortOrder, &d.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT locale, name, description, source, needs_review
		  FROM domain_translations WHERE domain_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var locale string
		var n models.NameDraft
		if err := rows.Scan(&locale, &n.Name, &n.Description, &n.Source, &n.NeedsReview); err != nil {
			return nil, err
		}
		d.Names[locale] = n
	}
	return d, rows.Err()
}

// UpsertDomain writes a domain and the languages supplied with it, in one
// transaction so a domain can never be stored without the name that makes it
// readable. A slug already in use comes back as ErrConflict, which the form
// reports against the field rather than as a failure of the whole page.
func (r *Repo) UpsertDomain(ctx context.Context, d *models.DomainDraft) (int, error) {
	var id int

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		if d.ID > 0 {
			err = tx.QueryRow(ctx, `
				UPDATE domains
				   SET slug = $2, icon = $3, color = $4,
				       sort_order = $5, is_active = $6, updated_at = now()
				 WHERE id = $1
				RETURNING id`,
				d.ID, d.Slug, d.Icon, d.Color, d.SortOrder, d.IsActive).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
		} else {
			err = tx.QueryRow(ctx, `
				INSERT INTO domains (slug, icon, color, sort_order, is_active, created_by)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING id`,
				d.Slug, d.Icon, d.Color, d.SortOrder, d.IsActive, createdBy(d)).Scan(&id)
		}
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: slug", ErrConflict)
		}
		if err != nil {
			return err
		}

		for locale, n := range d.Names {
			if strings.TrimSpace(n.Name) == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO domain_translations
					(domain_id, locale, name, description, source, needs_review)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (domain_id, locale) DO UPDATE SET
					name         = EXCLUDED.name,
					description  = EXCLUDED.description,
					source       = EXCLUDED.source,
					needs_review = EXCLUDED.needs_review`,
				id, locale, n.Name, n.Description, n.Source, n.NeedsReview); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// SetDomainActive retires a domain or brings it back. Retiring is the
// reversible half of what the delete button does, and it reaches further than
// retiring a category: every category under it stops being offered and every
// question under those stops being drawn, which is the whole point — a subject
// area that is half built must not reach a player.
func (r *Repo) SetDomainActive(ctx context.Context, id int, active bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE domains SET is_active = $2, updated_at = now() WHERE id = $1`, id, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDomain removes an empty domain.
//
// Only an empty one, for the same reason DeleteCategory refuses a full one,
// one level further up: the key is RESTRICT since migration 0034, so the
// database would refuse it anyway — this is what turns that refusal into a
// sentence the screen can show. The count is taken inside the transaction, so
// a category filed under it a moment earlier is not missed.
func (r *Repo) DeleteDomain(ctx context.Context, id int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(ctx,
			`SELECT count(*) FROM categories WHERE domain_id = $1`, id).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: domain holds %d categor%s", ErrConflict, n, plural(n, "y", "ies"))
		}
		tag, err := tx.Exec(ctx, `DELETE FROM domains WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// createdBy keeps a zero UUID out of the column: the key is ON DELETE SET
// NULL, and a row claiming to be created by the nil user is worse than one
// admitting it does not know.
func createdBy(d *models.DomainDraft) any {
	if d.CreatedBy == uuid.Nil {
		return nil
	}
	return d.CreatedBy
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// DomainPlaysToday is how many rounds were finished in each subject area in
// the last day, keyed by domain id.
//
// Counted through the questions that were answered rather than through
// game_sessions.domain_id: a round drawn from one category records that
// category and no domain, so counting the column would report zero for every
// subject area a player reached the ordinary way. What the card is reporting
// is how much play the area is carrying, and that is answers to its questions.
func (r *Repo) DomainPlaysToday(ctx context.Context) (map[int]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.domain_id, count(DISTINCT ga.session_id)::int
		  FROM game_answers ga
		  JOIN questions q  ON q.id = ga.question_id
		  JOIN categories c ON c.id = q.category_id
		 WHERE ga.answered_at > now() - interval '1 day'
		 GROUP BY c.domain_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]int{}
	for rows.Next() {
		var id, n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// The admin side of the taxonomy. Categories used to arrive only through the
// seed migration, which meant adding one — or correcting a name read by every
// player on the setup screen — was a schema change and a deploy. These are the
// queries behind the screen that does it instead.
//
// Reading stays in content.go: that side is the player's, filtered to what is
// active and resolved for one locale. This side is the editor's, so it shows
// the retired rows too and keeps every language side by side.

// AdminCategories lists every category, active or not, with what is filed
// under each. Names resolve for locale and fall back to Arabic, exactly as the
// player's list does, so the admin reads the same name the player does.
// It carries the domain each category sits in, resolved the same way, and
// orders by domain first: a list of categories that does not say which subject
// area each belongs to stops being readable the moment there is more than one.
// Retired domains are included — this is the editor's list, and a category
// under a retired domain is exactly what somebody needs to find.
func (r *Repo) AdminCategories(ctx context.Context, locale string) ([]*models.AdminCategory, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.slug, c.icon, c.color, c.sort_order, c.is_active,
		       COALESCE(t.name, fb.name, c.slug),
		       COALESCE(t.description, fb.description, ''),
		       (SELECT count(*) FROM questions q WHERE q.category_id = c.id),
		       (SELECT count(*) FROM questions q WHERE q.category_id = c.id AND q.is_active),
		       ARRAY(SELECT x.locale FROM category_translations x
		              WHERE x.category_id = c.id ORDER BY x.locale),
		       d.id, d.slug, COALESCE(dt.name, dfb.name, d.slug), d.is_active
		  FROM categories c
		  JOIN domains d ON d.id = c.domain_id
		  LEFT JOIN category_translations t  ON t.category_id  = c.id AND t.locale  = $1 AND NOT t.needs_review
		  LEFT JOIN category_translations fb ON fb.category_id = c.id AND fb.locale = 'ar' AND NOT fb.needs_review
		  LEFT JOIN domain_translations dt   ON dt.domain_id   = d.id AND dt.locale  = $1 AND NOT dt.needs_review
		  LEFT JOIN domain_translations dfb  ON dfb.domain_id  = d.id AND dfb.locale = 'ar' AND NOT dfb.needs_review
		 ORDER BY d.sort_order, c.sort_order, c.id`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.AdminCategory
	for rows.Next() {
		var c models.AdminCategory
		if err := rows.Scan(&c.ID, &c.Slug, &c.Icon, &c.Color, &c.SortOrder,
			&c.IsActive, &c.Name, &c.Description,
			&c.Questions, &c.ActiveQuestions, &c.PresentLocales,
			&c.DomainID, &c.DomainSlug, &c.DomainName, &c.DomainActive); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// CategoryDraft loads one category with every language it has been written in,
// which is what the edit form is built from.
func (r *Repo) CategoryDraft(ctx context.Context, id int) (*models.CategoryDraft, error) {
	d := &models.CategoryDraft{Names: map[string]models.NameDraft{}}

	err := r.pool.QueryRow(ctx, `
		SELECT id, slug, icon, color, sort_order, is_active, domain_id
		  FROM categories WHERE id = $1`, id).
		Scan(&d.ID, &d.Slug, &d.Icon, &d.Color, &d.SortOrder, &d.IsActive, &d.DomainID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT locale, name, description, source, needs_review
		  FROM category_translations WHERE category_id = $1`, id)
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

// UpsertCategory writes a category and the languages supplied with it, in one
// transaction so a category can never be stored without the name that makes it
// readable. A slug already in use comes back as ErrConflict, which the form
// reports against the field rather than as a failure of the whole page.
func (r *Repo) UpsertCategory(ctx context.Context, d *models.CategoryDraft) (int, error) {
	var id int

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		if d.ID > 0 {
			err = tx.QueryRow(ctx, `
				UPDATE categories
				   SET slug = $2, icon = $3, color = $4,
				       sort_order = $5, is_active = $6, domain_id = $7
				 WHERE id = $1
				RETURNING id`,
				d.ID, d.Slug, d.Icon, d.Color, d.SortOrder, d.IsActive, d.DomainID).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
		} else {
			err = tx.QueryRow(ctx, `
				INSERT INTO categories (slug, icon, color, sort_order, is_active, domain_id)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING id`,
				d.Slug, d.Icon, d.Color, d.SortOrder, d.IsActive, d.DomainID).Scan(&id)
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
				INSERT INTO category_translations
					(category_id, locale, name, description, source, needs_review)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (category_id, locale) DO UPDATE SET
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

// SetCategoryActive retires a category or brings it back. Retiring is the
// reversible half of what the delete button does: the questions stay, and stop
// being dealt, so the decision can be taken back the next morning.
func (r *Repo) SetCategoryActive(ctx context.Context, id int, active bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE categories SET is_active = $2 WHERE id = $1`, id, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCategory removes an empty category.
//
// Only an empty one. The key was ON DELETE CASCADE into questions, and from
// questions into game_answers, so deleting a full category would have taken
// the bank and every player's history with it on one press; migration 0034
// made it RESTRICT, and the database refuses it now as well. This is what
// turns that refusal into a sentence a screen can show. The count is taken
// inside the transaction, so a question filed under it a moment earlier is not
// missed.
func (r *Repo) DeleteCategory(ctx context.Context, id int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(ctx,
			`SELECT count(*) FROM questions WHERE category_id = $1`, id).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: category holds %d question(s)", ErrConflict, n)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM categories WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ReorderCategories writes a new running order from a list of ids.
//
// The order is positional: the caller sends the ids in the order they should
// appear and the row's place in that list becomes its sort_order. Sending
// numbers instead would mean the screen had to invent gap-free values itself,
// and two categories dragged in the same minute would collide on one.
//
// One statement rather than a loop. A reorder is a single decision, so a crash
// halfway through it must not leave half the list renumbered — and every row
// is being rewritten anyway, so there is nothing to be gained by sending them
// one at a time. Ids that are not categories are ignored rather than refused:
// the list comes from a screen that may have been open while somebody else
// deleted one of them.
func (r *Repo) ReorderCategories(ctx context.Context, ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE categories c
		   SET sort_order = o.position
		  FROM unnest($1::int[]) WITH ORDINALITY AS o(id, position)
		 WHERE c.id = o.id AND c.sort_order <> o.position`, ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

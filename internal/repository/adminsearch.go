package repository

import (
	"context"
	"strings"

	"github.com/bilelzarai/siraj/internal/models"
)

// The admin top bar's one search field.
//
// Every admin screen already searches its own list, which is the right tool
// once you know which screen to be on. It is the wrong tool for the thing
// admins actually start with — a username, a question id, a ticket subject
// somebody quoted at them — because answering that means guessing which of
// eleven screens the thing lives on first.

// AdminSearchResults is one answer per area, each capped, so the drop-down
// shows a few of everything rather than fifty of whichever matched most.
type AdminSearchResults struct {
	Users     []*models.AdminSearchHit
	Questions []*models.AdminSearchHit
	Tickets   []*models.AdminSearchHit
}

// Empty reports whether nothing matched anywhere.
func (r AdminSearchResults) Empty() bool {
	return len(r.Users) == 0 && len(r.Questions) == 0 && len(r.Tickets) == 0
}

// AdminSearch looks one query up across the three things an admin searches for
// by name. Each area is a separate statement: they match different columns and
// a UNION would force one shape onto all three and sort the areas into each
// other.
//
// Tickets are included for a moderator as well as an admin — the support inbox
// is moderator work — but users are not, because the user directory is behind
// RequireAdmin, and a search that answered from a screen you cannot open would
// be a way to read it.
func (r *Repo) AdminSearch(ctx context.Context, query, locale string,
	includeUsers bool, limit int) (*AdminSearchResults, error) {

	query = strings.TrimSpace(query)
	out := &AdminSearchResults{}
	if query == "" {
		return out, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}

	if includeUsers {
		rows, err := r.pool.Query(ctx, `
			SELECT id::text, username, display_name, role::text, status
			  FROM users
			 WHERE NOT is_temporary
			   AND (username ILIKE '%' || $1 || '%'
			     OR display_name ILIKE '%' || $1 || '%'
			     OR COALESCE(email::text, '') ILIKE '%' || $1 || '%')
			 ORDER BY username
			 LIMIT $2`, query, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h models.AdminSearchHit
			var role, status string
			if err := rows.Scan(&h.ID, &h.Subtitle, &h.Title, &role, &status); err != nil {
				rows.Close()
				return nil, err
			}
			if h.Title == "" {
				h.Title = h.Subtitle
			}
			h.Kind, h.Badge = "user", role
			if status != models.StatusUserActive {
				h.Badge = status
			}
			h.Subtitle = "@" + h.Subtitle
			h.Href = "/admin/users?q=" + h.Subtitle[1:]
			out.Users = append(out.Users, &h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// By prompt in any language, and by id: "#412" is how the other admin
	// screens refer to a question, so it has to be something you can paste in
	// here. A query that is not a number simply never matches the id branch.
	rows, err := r.pool.Query(ctx, `
		SELECT q.id::text, COALESCE(t.prompt, any_t.prompt, ''),
		       COALESCE(ct.name, c.slug), q.is_active
		  FROM questions q
		  JOIN categories c ON c.id = q.category_id
		  LEFT JOIN category_translations ct ON ct.category_id = c.id AND ct.locale = $2
		  LEFT JOIN question_translations t  ON t.question_id  = q.id AND t.locale  = $2
		  LEFT JOIN LATERAL (
		        SELECT prompt FROM question_translations
		         WHERE question_id = q.id ORDER BY locale LIMIT 1
		  ) any_t ON true
		 WHERE q.author_id IS NULL
		   AND (q.id::text = btrim($1, '#')
		     OR EXISTS (SELECT 1 FROM question_translations st
		                 WHERE st.question_id = q.id
		                   AND st.prompt ILIKE '%' || $1 || '%'))
		 ORDER BY q.id::text = btrim($1, '#') DESC, q.id DESC
		 LIMIT $3`, query, locale, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h models.AdminSearchHit
		var active bool
		if err := rows.Scan(&h.ID, &h.Title, &h.Subtitle, &active); err != nil {
			rows.Close()
			return nil, err
		}
		h.Kind = "question"
		if !active {
			h.Badge = "retired"
		}
		h.Href = "/admin/questions/" + h.ID + "/edit"
		out.Questions = append(out.Questions, &h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `
		SELECT t.id::text, t.subject, u.username, t.status::text
		  FROM support_tickets t
		  JOIN users u ON u.id = t.user_id
		 WHERE t.subject ILIKE '%' || $1 || '%'
		    OR u.username ILIKE '%' || $1 || '%'
		 ORDER BY t.last_message_at DESC
		 LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var h models.AdminSearchHit
		if err := rows.Scan(&h.ID, &h.Title, &h.Subtitle, &h.Badge); err != nil {
			return nil, err
		}
		h.Kind = "ticket"
		h.Subtitle = "@" + h.Subtitle
		h.Href = "/admin/support/" + h.ID
		out.Tickets = append(out.Tickets, &h)
	}
	return out, rows.Err()
}

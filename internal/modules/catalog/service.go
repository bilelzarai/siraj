// Package catalog: domains, categories and "Your domains".
//
// Product rule (see CLAUDE.md): the player app works inside the player's own domains.
// Lists default to scope=mine; scope=all is only for Explore, Add a domain and onboarding.
// A player always keeps at least one domain.
package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/view"
	"siraj/web/templates/components"
)

const MaxUserDomains = 50

var (
	ErrLastDomain  = apperr.Conflict("last_domain", "Keep at least one domain")
	ErrIslamicOnly = apperr.Conflict("islamic_only", "Islamic content only is on: turn it off in Settings to add general domains")
)

type Service struct{ db *db.DB }

func NewService(d *db.DB) *Service { return &Service{db: d} }

type Scope string

const (
	ScopeMine Scope = "mine"
	ScopeAll  Scope = "all"
)

type ListInput struct {
	// Alpha sorts the catalog A–Z, the way the Explore and Add sheets list it (O01).
	Alpha bool
	// MineFirst lifts the domains already on the player's home to the top of the
	// list. It sorts in the query, not on the page, so a domain the player picked
	// cannot be left stranded on page two (O02).
	MineFirst bool
	Who       *authctx.Principal // nil = anonymous
	Scope     Scope
	Q         string
	Lang      string
	Page      int
	PerPage   int
}

func (s *Service) ListDomains(ctx context.Context, in ListInput) ([]sqlc.ListDomainsRow, int, error) {
	if in.Scope == "" {
		in.Scope = ScopeMine
	}
	if in.Scope != ScopeMine && in.Scope != ScopeAll {
		return nil, 0, apperr.Invalid(map[string]string{"scope": "mine or all"})
	}
	if in.Scope == ScopeMine && in.Who == nil {
		return nil, 0, apperr.Unauthorized("scope=mine needs a player; use scope=all to browse")
	}
	var uid uuid.NullUUID
	islamicOnly := false
	if in.Who != nil {
		uid = uuid.NullUUID{UUID: in.Who.ID, Valid: true}
		islamicOnly = in.Who.IslamicOnly
	}
	rows, err := s.db.Q.ListDomains(ctx, sqlc.ListDomainsParams{
		Lang: in.Lang, UserID: uid, OnlyMine: in.Scope == ScopeMine, IslamicOnly: islamicOnly,
		Q: in.Q, Alpha: in.Alpha, MineFirst: in.MineFirst,
		Lim: int32(in.PerPage), Off: int32((in.Page - 1) * in.PerPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountDomains(ctx, sqlc.CountDomainsParams{
		Lang: in.Lang, UserID: uid, OnlyMine: in.Scope == ScopeMine, IslamicOnly: islamicOnly, Q: in.Q,
	})
	return rows, int(total), err
}

func (s *Service) GetDomain(ctx context.Context, who *authctx.Principal, id int64, lang string) (sqlc.GetDomainRow, error) {
	var uid uuid.NullUUID
	if who != nil {
		uid = uuid.NullUUID{UUID: who.ID, Valid: true}
	}
	d, err := s.db.Q.GetDomain(ctx, sqlc.GetDomainParams{UserID: uid, Lang: lang, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return d, apperr.NotFound("domain")
	}
	if err == nil && who != nil && who.IslamicOnly && d.Family != sqlc.DomainFamilyIslamic {
		return d, apperr.NotFound("domain") // hidden in Islamic-only mode
	}
	return d, err
}

func (s *Service) Categories(ctx context.Context, who *authctx.Principal, domainID int64, lang string) ([]sqlc.ListCategoriesRow, error) {
	if _, err := s.GetDomain(ctx, who, domainID, lang); err != nil {
		return nil, err
	}
	return s.db.Q.ListCategories(ctx, sqlc.ListCategoriesParams{Lang: lang, DomainID: domainID})
}

// MyDomains = endpoint 114: the player's domains, in their order.
func (s *Service) MyDomains(ctx context.Context, who *authctx.Principal, lang string) ([]sqlc.ListDomainsRow, error) {
	rows, _, err := s.ListDomains(ctx, ListInput{Who: who, Scope: ScopeMine, Lang: lang, Page: 1, PerPage: MaxUserDomains})
	return rows, err
}

// ReplaceMyDomains = endpoint 115 (onboarding, Manage): replaces the list and its order.
func (s *Service) ReplaceMyDomains(ctx context.Context, who *authctx.Principal, ids []int64, source sqlc.UserDomainSource) error {
	if err := ValidateDomainIDs(ids); err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.LockUser(ctx, who.ID); err != nil {
			return err
		}
		n, err := q.CountPublishedDomains(ctx, ids)
		if err != nil {
			return err
		}
		if int(n) != len(ids) {
			return apperr.Invalid(map[string]string{"domain_ids": "one or more domains don't exist"})
		}
		if who.IslamicOnly {
			for _, id := range ids {
				if fam, err := q.DomainFamily(ctx, id); err != nil {
					return err
				} else if fam != sqlc.DomainFamilyIslamic {
					return ErrIslamicOnly
				}
			}
		}
		if err := q.DeleteAllUserDomains(ctx, who.ID); err != nil {
			return err
		}
		for i, id := range ids {
			if err := q.InsertUserDomain(ctx, sqlc.InsertUserDomainParams{UserID: who.ID, DomainID: id, Position: int32(i + 1), Source: source}); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddMyDomain = endpoint 116. Also called when a player starts a game in a domain that isn't theirs (source=played).
// Returns true when the domain was added, false when it was already there.
func (s *Service) AddMyDomain(ctx context.Context, who *authctx.Principal, id int64, source sqlc.UserDomainSource) (bool, error) {
	var added bool
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.LockUser(ctx, who.ID); err != nil {
			return err
		}
		fam, err := q.DomainFamily(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("domain")
		} else if err != nil {
			return err
		}
		if who.IslamicOnly && fam != sqlc.DomainFamilyIslamic {
			return ErrIslamicOnly
		}
		count, err := q.CountUserDomains(ctx, who.ID)
		if err != nil {
			return err
		}
		if count >= MaxUserDomains {
			return apperr.Conflict("too_many_domains", fmt.Sprintf("You can follow up to %d domains", MaxUserDomains))
		}
		n, err := q.AddUserDomain(ctx, sqlc.AddUserDomainParams{UserID: who.ID, DomainID: id, Source: source})
		added = n > 0
		return err
	})
	return added, err
}

// RemoveMyDomain = endpoint 117. Refused for the last domain.
func (s *Service) RemoveMyDomain(ctx context.Context, who *authctx.Principal, id int64) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if err := q.LockUser(ctx, who.ID); err != nil {
			return err
		}
		count, err := q.CountUserDomains(ctx, who.ID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastDomain
		}
		n, err := q.DeleteUserDomain(ctx, sqlc.DeleteUserDomainParams{UserID: who.ID, DomainID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("domain in your list")
		}
		return nil
	})
}

// ValidateDomainIDs checks a replacement list before touching the database.
func ValidateDomainIDs(ids []int64) error {
	if len(ids) == 0 {
		return apperr.Invalid(map[string]string{"domain_ids": "pick at least one domain"})
	}
	if len(ids) > MaxUserDomains {
		return apperr.Invalid(map[string]string{"domain_ids": fmt.Sprintf("%d domains at most", MaxUserDomains)})
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return apperr.Invalid(map[string]string{"domain_ids": "ids must be positive"})
		}
		if seen[id] {
			return apperr.Invalid(map[string]string{"domain_ids": "each domain only once"})
		}
		seen[id] = true
	}
	return nil
}

// ViewDomains turns catalog rows into the view model the pages and components use.
// It lives here so every screen shows a domain the same way (BR-02, BR-03).
func ViewDomains(rows []sqlc.ListDomainsRow) []view.Domain {
	out := make([]view.Domain, len(rows))
	for i, r := range rows {
		out[i] = view.Domain{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description,
			Family: string(r.Family), Icon: r.Icon, Color: r.Color, Motif: r.Motif,
			IsFeatured: r.IsFeatured, IsNew: r.IsNew, IsMine: r.IsMine,
			CategoryCount: r.CategoryCount, QuestionCount: r.QuestionCount,
		}
	}
	return out
}

// ViewDomain is the single-domain version, for the domain page and the game screens.
func ViewDomain(r sqlc.GetDomainRow) view.Domain {
	return view.Domain{
		ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description,
		Family: string(r.Family), Icon: r.Icon, Color: r.Color, Motif: r.Motif,
		IsFeatured: r.IsFeatured, IsNew: r.IsNew, IsMine: r.IsMine,
		CategoryCount: r.CategoryCount, QuestionCount: r.QuestionCount,
	}
}

// CategoriesOf loads the categories of several domains at once, keyed by domain id.
// Every screen that shows category chips or a category grid goes through it.
func (s *Service) CategoriesOf(ctx context.Context, lang string, domainIDs []int64) (map[int64][]components.Category, error) {
	out := map[int64][]components.Category{}
	if len(domainIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Q.ListCategoriesForDomains(ctx, sqlc.ListCategoriesForDomainsParams{Lang: lang, DomainIds: domainIDs})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DomainID] = append(out[r.DomainID], components.Category{
			ID: r.ID, DomainID: r.DomainID, Slug: r.Slug, Name: r.Name, Icon: r.Icon,
			Color: r.Color, QuestionCount: r.QuestionCount,
		})
	}
	return out, nil
}

// Category is endpoint 20: one category, refused when its domain is hidden by
// Islamic-only mode, the same rule the domain itself follows (D-05).
func (s *Service) Category(ctx context.Context, who *authctx.Principal, id int64, lang string) (sqlc.GetCategoryRow, error) {
	var uid uuid.NullUUID
	if who != nil {
		uid = uuid.NullUUID{UUID: who.ID, Valid: true}
	}
	row, err := s.db.Q.GetCategory(ctx, sqlc.GetCategoryParams{UserID: uid, Lang: lang, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, apperr.NotFound("category")
	}
	if err == nil && who != nil && who.IslamicOnly && row.DomainFamily != sqlc.DomainFamilyIslamic {
		return row, apperr.NotFound("category")
	}
	return row, err
}

// SearchResult is endpoint 21: what a search box offers, domains and categories
// together, inside the scope the player asked for.
type SearchResult struct {
	Domains    []sqlc.ListDomainsRow
	Categories []sqlc.SearchCategoriesRow
}

// Search runs the one query behind the search screen and the API. scope=mine stays
// inside the player's domains (DM-01); scope=all is Explore.
func (s *Service) Search(ctx context.Context, who *authctx.Principal, scope Scope, q, lang string, limit int) (SearchResult, error) {
	var out SearchResult
	if scope == "" {
		scope = ScopeMine
		if who == nil {
			scope = ScopeAll
		}
	}
	if scope != ScopeMine && scope != ScopeAll {
		return out, apperr.Invalid(map[string]string{"scope": "mine or all"})
	}
	if scope == ScopeMine && who == nil {
		return out, apperr.Unauthorized("scope=mine needs a player; use scope=all to browse")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	domains, _, err := s.ListDomains(ctx, ListInput{
		Who: who, Scope: scope, Q: q, Lang: lang, Page: 1, PerPage: limit, Alpha: true,
	})
	if err != nil {
		return out, err
	}
	out.Domains = domains

	var uid uuid.NullUUID
	islamicOnly := false
	if who != nil {
		uid = uuid.NullUUID{UUID: who.ID, Valid: true}
		islamicOnly = who.IslamicOnly
	}
	out.Categories, err = s.db.Q.SearchCategories(ctx, sqlc.SearchCategoriesParams{
		Lang: lang, UserID: uid, OnlyMine: scope == ScopeMine, IslamicOnly: islamicOnly,
		Q: q, Lim: int32(limit),
	})
	return out, err
}

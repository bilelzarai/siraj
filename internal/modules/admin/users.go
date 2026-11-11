package admin

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/platform/apperr"
)

// A02b, the user drawer: everything the console knows about one account, and the
// few things staff may change on it — the role, the player's own settings and the
// domains their app shows (DM-01, D-05).

// UserDetail is the drawer's data.
type UserDetail struct {
	Row       sqlc.AdminUserDetailRow
	Accuracy  int
	Streak    int
	Domains   []sqlc.AdminUserDomainsRow
	Top       []sqlc.TopCategoriesForUserRow
	Sanctions []sqlc.UserSanctionsRow
	// Catalog is every published domain, so the drawer can offer the ones this
	// player does not follow yet.
	Catalog []sqlc.AdminDomainsRow
}

func (s *Service) UserDetail(ctx context.Context, id uuid.UUID, lang string) (UserDetail, error) {
	var out UserDetail
	row, err := s.db.Q.AdminUserDetail(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, apperr.NotFound("user")
	}
	if err != nil {
		return out, err
	}
	out.Row = row
	if row.Answered > 0 {
		out.Accuracy = int(row.Correct) * 100 / int(row.Answered)
	}
	if progress, err := s.db.Q.MyProgress(ctx, id); err == nil {
		out.Streak = int(progress.Streak)
	}
	if out.Domains, err = s.db.Q.AdminUserDomains(ctx, sqlc.AdminUserDomainsParams{UserID: id, Lang: lang}); err != nil {
		return out, err
	}
	out.Top, err = s.db.Q.TopCategoriesForUser(ctx, sqlc.TopCategoriesForUserParams{
		UserID: id, Lang: lang, Lim: 3,
	})
	if err != nil {
		return out, err
	}
	if out.Sanctions, err = s.db.Q.UserSanctions(ctx, id); err != nil {
		return out, err
	}
	out.Catalog, err = s.db.Q.AdminDomains(ctx, sqlc.AdminDomainsParams{Lang: lang})
	return out, err
}

// UserConfig is what the drawer's form posts: the staff-editable part of an account.
type UserConfig struct {
	UserID      uuid.UUID
	Role        string
	Lang        string
	Theme       string
	DailyGoal   int32
	IslamicOnly bool
	// DomainIDs replaces the player's domains, in the order given. Empty leaves them
	// alone, because "at least one domain" must stay true (DM-03).
	DomainIDs []int64
}

// SaveUserConfig writes the whole drawer in one transaction, with one audit row for
// the change (AD-03). The rules the player app relies on are kept here too: a player
// always keeps at least one domain, and only the five known roles exist.
func (s *Service) SaveUserConfig(ctx context.Context, a Actor, in UserConfig) error {
	role, err := parseRole(in.Role)
	if err != nil {
		return err
	}
	if !i18n.Known(in.Lang) {
		return apperr.Invalid(map[string]string{"lang": "ar, en or fr"})
	}
	switch in.Theme {
	case "light", "dark", "system":
	default:
		in.Theme = "system"
	}
	if in.DailyGoal < 1 || in.DailyGoal > 20 {
		return apperr.Invalid(map[string]string{"daily_goal": "between 1 and 20"})
	}
	if len(in.DomainIDs) == 0 {
		return apperr.Invalid(map[string]string{"domains": "a player keeps at least one domain"})
	}
	if in.UserID == a.ID && role != sqlc.UserRoleAdmin {
		// Nobody locks themselves out of the console they are standing in.
		return apperr.Conflict("own_role", "You cannot take your own admin role away")
	}

	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		user, err := q.GetUser(ctx, in.UserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("user")
		}
		if err != nil {
			return err
		}
		if user.Role != role {
			if err := q.SetUserRole(ctx, sqlc.SetUserRoleParams{Role: role, ID: in.UserID}); err != nil {
				return err
			}
		}
		if user.Lang != in.Lang {
			if err := q.AdminSetUserLang(ctx, sqlc.AdminSetUserLangParams{Lang: in.Lang, ID: in.UserID}); err != nil {
				return err
			}
		}
		if err := q.AdminSetUserSettings(ctx, sqlc.AdminSetUserSettingsParams{
			UserID: in.UserID, Theme: in.Theme, DailyGoal: in.DailyGoal, IslamicOnly: in.IslamicOnly,
		}); err != nil {
			return err
		}
		if err := q.AdminReplaceUserDomains(ctx, in.UserID); err != nil {
			return err
		}
		for i, id := range in.DomainIDs {
			if err := q.AdminAddUserDomain(ctx, sqlc.AdminAddUserDomainParams{
				UserID: in.UserID, DomainID: id, Position: int32(i + 1),
				Source: sqlc.UserDomainSourceAdded,
			}); err != nil {
				return err
			}
		}
		return audit(ctx, q, a, "user.configure", "user", in.UserID.String(), in)
	})
}

func parseRole(v string) (sqlc.UserRole, error) {
	switch sqlc.UserRole(strings.ToLower(strings.TrimSpace(v))) {
	case sqlc.UserRoleAdmin:
		return sqlc.UserRoleAdmin, nil
	case sqlc.UserRoleModerator:
		return sqlc.UserRoleModerator, nil
	case sqlc.UserRoleSupport:
		return sqlc.UserRoleSupport, nil
	case sqlc.UserRoleTranslator:
		return sqlc.UserRoleTranslator, nil
	case sqlc.UserRolePlayer, "":
		return sqlc.UserRolePlayer, nil
	}
	return "", apperr.Invalid(map[string]string{"role": "player, translator, moderator, support or admin"})
}

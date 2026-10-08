package handlers

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/views"
)

// Accounts: who exists, what they may do, and taking it away.
func (h *Handlers) AdminUsers(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	paging := h.paging(w, r)
	filter := adminUserFilterFrom(r)
	filter.Limit, filter.Offset = paging.Size, paging.Offset()

	users, total, err := h.repo.AdminUsers(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging = paging.withTotal(total)
	countries, _ := h.repo.CountriesInUse(r.Context())
	counts, err := h.repo.UserCounts(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	confirmDelete := ""
	if id, ok := parseUUID(r.URL.Query().Get("confirm")); ok {
		confirmDelete = id.String()
	}

	h.render(w, r, http.StatusOK, views.AdminUsers(c, chrome, views.AdminUsersData{
		Users:           users,
		Total:           total,
		Pager:           pagerFor(paging),
		Query:           filter.Query,
		Kind:            filter.Kind,
		Counts:          counts,
		Status:          filter.Status,
		Country:         filter.Country,
		Countries:       countries,
		Sorting: views.SortState{
			Sort: filter.Sort, Path: "/admin/users", Query: r.URL.Query(),
		},
		ConfirmDeleteID: confirmDelete,
	}))
}

// AdminUserAction applies role, suspension and deletion from one route.
func (h *Handlers) AdminUserAction(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	action := chi.URLParam(r, "action")

	// An admin must not be able to strip or suspend their own access and
	// lock themselves out mid-session.
	if id == c.User.ID && action != "reinstate" {
		h.flash(w, "error", c.T("admin.users.notYourself"))
		redirect(w, r, "/admin/users")
		return
	}

	target, err := h.repo.AdminUser(r.Context(), id)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	switch action {
	case "role":
		role := validRole(r.PostFormValue("role"))
		if role == "" {
			h.flash(w, "error", c.T("admin.users.badRole"))
			break
		}
		err = h.repo.SetUserRole(r.Context(), id, role)
		if err == nil {
			h.auditChange(r, "user.role", "user", id.String(), target.Username,
				map[string]any{"role": target.Role},
				map[string]any{"role": role})
			h.flash(w, "success", c.T("admin.users.roleChanged", target.Username,
				c.T(models.RoleLabelKey(role))))
		}

	case "suspend":
		reason := clip(strings.TrimSpace(r.PostFormValue("reason")), 200)
		err = h.repo.SetUserStatus(r.Context(), id, models.StatusUserSuspended, reason)
		if err == nil {
			// Revoking their sessions makes the suspension take effect now
			// rather than whenever their cookie happens to expire.
			_ = h.repo.DeleteOtherSessions(r.Context(), id, "")
			h.auditChange(r, "user.suspend", "user", id.String(), target.Username,
				map[string]any{"status": target.Status},
				map[string]any{"status": models.StatusUserSuspended, "reason": reason})
			h.flash(w, "success", c.T("admin.users.suspended.done", target.Username))
		}

	case "reinstate":
		err = h.repo.SetUserStatus(r.Context(), id, models.StatusUserActive, "")
		if err == nil {
			h.auditChange(r, "user.reinstate", "user", id.String(), target.Username,
				map[string]any{"status": target.Status, "reason": target.SuspendedReason},
				map[string]any{"status": models.StatusUserActive})
			h.flash(w, "success", c.T("admin.users.reinstated", target.Username))
		}

	case "delete":
		// Deletion is a cascade across every table the account touches and
		// cannot be undone. The confirmation has to be something the admin
		// supplies, so the first press opens an inline box asking them to type
		// the username — a hidden field pre-filled with the answer, which is
		// what this used to be, confirms nothing.
		typed := strings.TrimSpace(r.PostFormValue("confirm"))
		if typed == "" {
			redirect(w, r, "/admin/users?confirm="+id.String())
			return
		}
		if !strings.EqualFold(typed, target.Username) {
			h.flash(w, "error", c.T("admin.users.confirmMismatch"))
			redirect(w, r, "/admin/users?confirm="+id.String())
			return
		}
		err = h.repo.DeleteUserAsAdmin(r.Context(), id)
		if err == nil {
			h.auditChange(r, "user.delete", "user", id.String(), target.Username,
				map[string]any{"account": target.Username, "role": target.Role}, nil)
			h.flash(w, "success", c.T("admin.users.deleted", target.Username))
		}

	default:
		h.NotFound(w, r)
		return
	}

	if errors.Is(err, repository.ErrLastAdmin) {
		h.flash(w, "error", c.T("admin.users.lastAdmin"))
	} else if err != nil {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, "/admin/users")
}

// validRole keeps anything that is not a role out of the SQL, and reads the
// list from models so a role added there is assignable here without a second
// edit nobody remembers to make.
func validRole(role string) string {
	if slices.Contains(models.AssignableRoles, role) {
		return role
	}
	return ""
}

func validStatus(status string) string {
	switch status {
	case models.StatusUserActive, models.StatusUserSuspended:
		return status
	default:
		return ""
	}
}

// panelStrengthFloor is how many answers a category needs before its accuracy
// is worth ranking. Two right out of two is a hundred per cent and says
// nothing about a player; it would outrank a category they have learned.
const panelStrengthFloor = 10

// AdminUserPanel is the drawer the directory's eye icon opens: who this
// account is, how they play, and what may be done about them.
//
// A fragment, fetched when it is opened. Rendering it with the list would be
// fifty copies in every page, most never looked at — and the strongest
// categories underneath are an aggregate per player, so it would be fifty
// aggregates computed to show one.
func (h *Handlers) AdminUserPanel(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	user, err := h.repo.AdminUser(r.Context(), id)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}

	// Thin evidence is left out rather than ranked, so a player with little
	// play simply has no strongest categories instead of a misleading list.
	strongest, err := h.repo.PlayerStrengths(r.Context(), id, c.Locale, panelStrengthFloor, 4)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// What the console has done to this account. Read from the trail rather
	// than from a column: the account carries its current state, and what the
	// panel is showing is the history that led to it.
	history, err := h.repo.AuditPage(r.Context(), repository.AuditFilter{
		Query: id.String(), Limit: 6,
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.renderFragment(w, r, views.AdminUserPanel(c, views.AdminUserPanelData{
		User:      user,
		Strongest: strongest,
		Recent:    history,
	}))
}

package handlers

import (
	"errors"
	"net/http"
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
	filter := repository.AdminUserFilter{
		Query:   strings.TrimSpace(r.URL.Query().Get("q")),
		Role:    validRole(r.URL.Query().Get("role")),
		Status:  validStatus(r.URL.Query().Get("status")),
		Country: strings.TrimSpace(r.URL.Query().Get("country")),
		Limit:   paging.Size,
		Offset:  paging.Offset(),
	}

	users, total, err := h.repo.AdminUsers(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	paging = paging.withTotal(total)
	countries, _ := h.repo.CountriesInUse(r.Context())

	confirmDelete := ""
	if id, ok := parseUUID(r.URL.Query().Get("confirm")); ok {
		confirmDelete = id.String()
	}

	h.render(w, r, http.StatusOK, views.AdminUsers(c, chrome, views.AdminUsersData{
		Users:           users,
		Total:           total,
		Pager:           pagerFor(paging),
		Query:           filter.Query,
		Role:            filter.Role,
		Status:          filter.Status,
		Country:         filter.Country,
		Countries:       countries,
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
			h.audit(r, "user.role", "user", id.String(),
				map[string]any{"from": target.Role, "to": role})
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
			h.audit(r, "user.suspend", "user", id.String(), map[string]any{"reason": reason})
			h.flash(w, "success", c.T("admin.users.suspended.done", target.Username))
		}

	case "reinstate":
		err = h.repo.SetUserStatus(r.Context(), id, models.StatusUserActive, "")
		if err == nil {
			h.audit(r, "user.reinstate", "user", id.String(), nil)
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
			h.audit(r, "user.delete", "user", id.String(),
				map[string]any{"username": target.Username})
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

func validRole(role string) string {
	switch role {
	case models.RolePlayer, models.RoleModerator, models.RoleAdmin:
		return role
	default:
		return ""
	}
}

func validStatus(status string) string {
	switch status {
	case models.StatusUserActive, models.StatusUserSuspended:
		return status
	default:
		return ""
	}
}

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/views"
)

// The level above a category. Until this screen existed the taxonomy was one
// level deep and every category was Islamic, so there was nowhere to say that
// Football and Qur'an are different kinds of subject. This is where an admin
// says it — without a deploy, because a subject area is a row.
//
// Everything here mirrors admin_categories.go on purpose. What differs is the
// role: reshaping the taxonomy is structural, so these routes are admin-only
// (D11), while writing a category inside one stays moderator work. The gate is
// on the route, not in here.

// AdminDomains lists the subject areas, retired ones included.
func (h *Handlers) AdminDomains(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	domains, err := h.repo.AdminDomains(r.Context(), c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// What is filed under each one, so a card can list its categories rather
	// than only count them — an area with eight categories and one with eight
	// read identically as a number and not at all as a list.
	categories, err := h.repo.Categories(r.Context(), c.Locale, 0)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	plays, err := h.repo.DomainPlaysToday(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.AdminDomains(c, chrome, views.AdminDomainsData{
		Domains:    domains,
		Categories: categories,
		PlaysToday: plays,
	}))
}

// AdminDomainForm serves the blank form and the edit form, which are the same
// form.
func (h *Handlers) AdminDomainForm(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	d := views.AdminDomainFormData{
		Icon:      models.DefaultDomainIcon,
		Color:     models.DefaultDomainColor,
		IsActive:  true,
		SortOrder: 0,
		Locales:   map[string]views.LocaleForm{},
		Errors:    map[string]string{},
	}

	if raw := chi.URLParam(r, "id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			h.NotFound(w, r)
			return
		}
		draft, err := h.repo.DomainDraft(r.Context(), id)
		if err != nil {
			h.notFoundOrError(w, r, err)
			return
		}
		d = domainFormFromDraft(draft)
	} else {
		// A new domain goes at the end of the list rather than sharing
		// position 0 with whatever is already there.
		if existing, err := h.repo.AdminDomains(r.Context(), c.Locale); err == nil {
			for _, dom := range existing {
				if dom.SortOrder >= d.SortOrder {
					d.SortOrder = dom.SortOrder + 1
				}
			}
		}
	}

	h.render(w, r, http.StatusOK, views.AdminDomainForm(c, chrome, d))
}

// AdminDomainSave writes the form. Create and update go through one path, so
// neither can be validated differently from the other.
func (h *Handlers) AdminDomainSave(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	d := views.AdminDomainFormData{
		ID:        intParam(r, "id", 0),
		Slug:      strings.ToLower(strings.TrimSpace(r.PostFormValue("slug"))),
		Icon:      clip(strings.TrimSpace(r.PostFormValue("icon")), maxTaxonomyIcon),
		Color:     strings.TrimSpace(r.PostFormValue("color")),
		SortOrder: intParam(r, "sort_order", 0),
		IsActive:  r.PostFormValue("is_active") == "1",
		Locales:   map[string]views.LocaleForm{},
		Errors:    map[string]string{},
	}

	// Every shipped language is read; one left blank is simply not part of
	// this submission, which is how a domain gets named one language at a time
	// instead of all three or nothing.
	for _, loc := range i18n.Supported {
		d.Locales[loc.Code] = views.LocaleForm{
			Name: clip(strings.TrimSpace(
				r.PostFormValue("name_"+loc.Code)), maxTaxonomyName),
			Description: clip(strings.TrimSpace(
				r.PostFormValue("description_"+loc.Code)), maxTaxonomyDesc),
		}
	}

	names := map[string]models.NameDraft{}
	for code, form := range d.Locales {
		if form.Blank() {
			continue
		}
		if form.Name == "" {
			d.Errors[code] = c.T("admin.domain.needsName")
			continue
		}
		// A human typed this. needs_review exists for machine output and bulk
		// imports, and a domain whose name is held for review falls back to
		// Arabic for everybody — so a name written here is live immediately.
		names[code] = models.NameDraft{
			Name:        form.Name,
			Description: form.Description,
			Source:      "human",
			NeedsReview: false,
		}
	}

	switch {
	case d.Slug == "":
		d.Errors["slug"] = c.T("admin.domain.slugRequired")
	case len(d.Slug) > maxTaxonomySlug:
		d.Errors["slug"] = c.T("admin.domain.slugTooLong", maxTaxonomySlug)
	case !slugPattern.MatchString(d.Slug):
		d.Errors["slug"] = c.T("admin.domain.slugShape")
	}
	if d.Icon == "" {
		d.Icon = models.DefaultDomainIcon
	}
	if !models.ValidHexColor(d.Color) {
		d.Errors["color"] = c.T("admin.domain.badColor")
	}
	if d.SortOrder < 0 || d.SortOrder > maxTaxonomyOrder {
		d.Errors["sort_order"] = c.T("admin.domain.badOrder", maxTaxonomyOrder)
	}
	// Arabic is the fallback every other language resolves to, so a domain
	// without it is one that reads as its slug for anybody whose language is
	// missing. It is the one name that is not optional.
	if _, ok := names[i18n.DefaultLocale]; !ok {
		d.Error = c.T("admin.domain.needsFallback")
	}

	if d.Error != "" || len(d.Errors) > 0 {
		h.render(w, r, http.StatusUnprocessableEntity,
			views.AdminDomainForm(c, chrome, d))
		return
	}

	draft := &models.DomainDraft{
		ID:        d.ID,
		Slug:      d.Slug,
		Icon:      d.Icon,
		Color:     d.Color,
		SortOrder: d.SortOrder,
		IsActive:  d.IsActive,
		Names:     names,
	}
	// Who added a subject area is worth keeping: it is a structural decision,
	// and the column exists for exactly this. Only on create — an edit does
	// not rewrite who made it.
	if draft.ID == 0 {
		if user := userFrom(r); user != nil {
			draft.CreatedBy = user.ID
		}
	}

	id, err := h.repo.UpsertDomain(r.Context(), draft)
	switch {
	case errors.Is(err, repository.ErrConflict):
		d.Errors["slug"] = c.T("admin.domain.slugTaken")
		h.render(w, r, http.StatusUnprocessableEntity,
			views.AdminDomainForm(c, chrome, d))
		return
	case errors.Is(err, repository.ErrNotFound):
		h.NotFound(w, r)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}

	action := "domain.create"
	if draft.ID > 0 {
		action = "domain.update"
	}
	h.audit(r, action, "domain", strconv.Itoa(id), map[string]any{
		"slug":    draft.Slug,
		"active":  draft.IsActive,
		"locales": len(names),
	})
	h.flash(w, "success", c.T("admin.domain.saved", names[i18n.DefaultLocale].Name))
	redirect(w, r, "/admin/domains")
}

// AdminDomainAction is retire and restore.
//
// Delete is not here. It is routed separately so the shape matches the
// category screen, where that separation carries a different role gate — and
// because that route names the action in its path, there is no {action}
// parameter on it to read.
func (h *Handlers) AdminDomainAction(w http.ResponseWriter, r *http.Request) {
	h.domainAction(w, r, chi.URLParam(r, "action"))
}

// AdminDomainDelete removes an empty subject area.
func (h *Handlers) AdminDomainDelete(w http.ResponseWriter, r *http.Request) {
	h.domainAction(w, r, "delete")
}

func (h *Handlers) domainAction(w http.ResponseWriter, r *http.Request, action string) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}

	draft, err := h.repo.DomainDraft(r.Context(), id)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	name := draft.Names[i18n.DefaultLocale].Name
	if name == "" {
		name = draft.Slug
	}

	switch action {
	case "retire":
		err = h.repo.SetDomainActive(r.Context(), id, false)
		if err == nil {
			h.auditChange(r, "domain.retire", "domain", strconv.Itoa(id), name,
				map[string]any{"state": "live"},
				map[string]any{"state": "retired"})
			h.flash(w, "success", c.T("admin.domain.retired", name))
		}

	case "restore":
		err = h.repo.SetDomainActive(r.Context(), id, true)
		if err == nil {
			h.auditChange(r, "domain.restore", "domain", strconv.Itoa(id), name,
				map[string]any{"state": "retired"},
				map[string]any{"state": "live"})
			h.flash(w, "success", c.T("admin.domain.restored", name))
		}

	case "delete":
		// The key is RESTRICT, so a domain still holding categories is refused
		// by the database as well as here. Retiring it is what the admin
		// wanted in that case, and the message says so rather than reporting a
		// failure.
		err = h.repo.DeleteDomain(r.Context(), id)
		switch {
		case errors.Is(err, repository.ErrConflict):
			h.flash(w, "error", c.T("admin.domain.notEmpty", name))
			err = nil
		case err == nil:
			h.audit(r, "domain.delete", "domain", strconv.Itoa(id),
				map[string]any{"slug": draft.Slug})
			h.flash(w, "success", c.T("admin.domain.deleted", name))
		}

	default:
		h.NotFound(w, r)
		return
	}

	if err != nil {
		h.serverError(w, r, fmt.Errorf("domain action: %w", err))
		return
	}
	redirect(w, r, "/admin/domains")
}

// domainFormFromDraft fills the edit form from what is stored.
func domainFormFromDraft(d *models.DomainDraft) views.AdminDomainFormData {
	form := views.AdminDomainFormData{
		ID:        d.ID,
		Slug:      d.Slug,
		Icon:      d.Icon,
		Color:     d.Color,
		SortOrder: d.SortOrder,
		IsActive:  d.IsActive,
		Locales:   map[string]views.LocaleForm{},
		Errors:    map[string]string{},
	}
	for locale, n := range d.Names {
		form.Locales[locale] = views.LocaleForm{
			Name:        n.Name,
			Description: n.Description,
			Source:      n.Source,
			NeedsReview: n.NeedsReview,
		}
	}
	return form
}

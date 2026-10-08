package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/views"
)

// The taxonomy screen. Categories reached the bank through the seed migration
// and nothing else, so adding one — or fixing a name eight thousand players
// read on the setup screen — meant editing SQL and deploying. This is the same
// work done from the admin area, with the same rules the question bank uses:
// moderators write content, only an admin destroys it.

// slugPattern is what a category slug may be. It goes in URLs and query
// strings and is matched by the importer, so it stays to the lowercase ASCII
// alphabet rather than accepting whatever was typed.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Both levels of the taxonomy are bound by the same limits: they are the same
// kind of row, written through the same shape of form, read in the same places.
// One set of constants on the server, so the two levels cannot drift into
// accepting different things.
//
// The templates carry the same numbers again as maxlength attributes, because a
// view cannot see a handler's constants. That copy is a hint to the browser and
// nothing else — these are what actually refuse a value.
const (
	maxTaxonomySlug  = 40
	maxTaxonomyName  = 80
	maxTaxonomyDesc  = 160
	maxTaxonomyIcon  = 8
	maxTaxonomyOrder = 999
)

// AdminCategories lists the taxonomy, retired rows included.
func (h *Handlers) AdminCategories(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	categories, err := h.repo.AdminCategories(r.Context(), c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK,
		views.AdminCategories(c, chrome, views.AdminCategoriesData{Categories: categories}))
}

// AdminCategoryForm serves the blank form and the edit form, which are the
// same form.
func (h *Handlers) AdminCategoryForm(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	d := views.AdminCategoryFormData{
		Icon:      models.DefaultCategoryIcon,
		Color:     models.DefaultCategoryColor,
		IsActive:  true,
		SortOrder: 0,
		Locales:   map[string]views.LocaleForm{},
		Errors:    map[string]string{},
	}

	// What there is to file it under. Active domains, plus — when editing —
	// whichever one this category is already in, even if that one has since
	// been retired. Without the exception the select would not contain the
	// category's own domain, nothing would be selected, the browser would
	// preselect the first option, and saving an unrelated edit would silently
	// move the category to another subject area.
	keep := 0
	if raw := chi.URLParam(r, "id"); raw != "" {
		if id, err := strconv.Atoi(raw); err == nil && id > 0 {
			if draft, err := h.repo.CategoryDraft(r.Context(), id); err == nil {
				keep = draft.DomainID
			}
		}
	}
	domains, err := h.domainOptions(r.Context(), c.Locale, keep)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d.Domains = domains

	if raw := chi.URLParam(r, "id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			h.NotFound(w, r)
			return
		}
		draft, err := h.repo.CategoryDraft(r.Context(), id)
		if err != nil {
			h.notFoundOrError(w, r, err)
			return
		}
		d = categoryFormFromDraft(draft)
		d.Domains = domains
	} else {
		// A new category goes at the end of the list rather than sharing
		// position 0 with whatever is already there.
		if existing, err := h.repo.AdminCategories(r.Context(), c.Locale); err == nil {
			for _, cat := range existing {
				if cat.SortOrder >= d.SortOrder {
					d.SortOrder = cat.SortOrder + 1
				}
			}
		}
		// One subject area is not a decision; several is. Preselecting the
		// only one there is saves a click without ever choosing for somebody.
		if len(domains) == 1 {
			d.DomainID = domains[0].ID
		}
	}

	h.render(w, r, http.StatusOK, views.AdminCategoryForm(c, chrome, d))
}

// AdminCategorySave writes the form. Create and update go through one path, so
// neither can be validated differently from the other.
func (h *Handlers) AdminCategorySave(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	d := views.AdminCategoryFormData{
		ID:        intParam(r, "id", 0),
		DomainID:  intParam(r, "domain_id", 0),
		Slug:      strings.ToLower(strings.TrimSpace(r.PostFormValue("slug"))),
		Icon:      clip(strings.TrimSpace(r.PostFormValue("icon")), maxTaxonomyIcon),
		Color:     strings.TrimSpace(r.PostFormValue("color")),
		SortOrder: intParam(r, "sort_order", 0),
		IsActive:  r.PostFormValue("is_active") == "1",
		Locales:   map[string]views.LocaleForm{},
		Errors:    map[string]string{},
	}

	// Every shipped language is read; one left blank is simply not part of
	// this submission, which is how a category gets named one language at a
	// time instead of all three or nothing.
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
			d.Errors[code] = c.T("admin.category.needsName")
			continue
		}
		// A human typed this. needs_review exists for machine output and bulk
		// imports, and a category whose name is held for review falls back to
		// Arabic for everybody — so a name written here is live immediately.
		names[code] = models.NameDraft{
			Name:        form.Name,
			Description: form.Description,
			Source:      "human",
			NeedsReview: false,
		}
	}

	// The form is re-rendered on every refusal below, so the list it needs has
	// to be loaded before the first one, not after the last. On an edit the
	// category's current domain belongs in the list even if it is retired —
	// see the form handler — or editing anything else about such a category
	// would move it.
	keep := 0
	if d.ID > 0 {
		if stored, err := h.repo.CategoryDraft(r.Context(), d.ID); err == nil {
			keep = stored.DomainID
		}
	}
	domains, err := h.domainOptions(r.Context(), c.Locale, keep)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d.Domains = domains

	// Checked against what is offered rather than trusted: the select is
	// markup, and a posted id naming no reachable domain would otherwise reach
	// the foreign key as a 500 instead of a sentence on the field.
	named := false
	for _, dom := range domains {
		if dom.ID == d.DomainID {
			named = true
		}
	}
	switch {
	case d.DomainID <= 0:
		d.Errors["domain_id"] = c.T("admin.category.domainRequired")
	case !named:
		d.Errors["domain_id"] = c.T("admin.category.badDomain")
	}

	switch {
	case d.Slug == "":
		d.Errors["slug"] = c.T("admin.category.slugRequired")
	case len(d.Slug) > maxTaxonomySlug:
		d.Errors["slug"] = c.T("admin.category.slugTooLong", maxTaxonomySlug)
	case !slugPattern.MatchString(d.Slug):
		d.Errors["slug"] = c.T("admin.category.slugShape")
	}
	if d.Icon == "" {
		d.Icon = models.DefaultCategoryIcon
	}
	if !models.ValidHexColor(d.Color) {
		d.Errors["color"] = c.T("admin.category.badColor")
	}
	if d.SortOrder < 0 || d.SortOrder > maxTaxonomyOrder {
		d.Errors["sort_order"] = c.T("admin.category.badOrder", maxTaxonomyOrder)
	}
	// Arabic is the fallback every other language resolves to, so a category
	// without it is one that reads as its slug for anybody whose language is
	// missing. It is the one name that is not optional.
	if _, ok := names[i18n.DefaultLocale]; !ok {
		d.Error = c.T("admin.category.needsFallback")
	}

	if d.Error != "" || len(d.Errors) > 0 {
		h.render(w, r, http.StatusUnprocessableEntity,
			views.AdminCategoryForm(c, chrome, d))
		return
	}

	draft := &models.CategoryDraft{
		ID:        d.ID,
		DomainID:  d.DomainID,
		Slug:      d.Slug,
		Icon:      d.Icon,
		Color:     d.Color,
		SortOrder: d.SortOrder,
		IsActive:  d.IsActive,
		Names:     names,
	}

	id, err := h.repo.UpsertCategory(r.Context(), draft)
	switch {
	case errors.Is(err, repository.ErrConflict):
		d.Errors["slug"] = c.T("admin.category.slugTaken")
		h.render(w, r, http.StatusUnprocessableEntity,
			views.AdminCategoryForm(c, chrome, d))
		return
	case errors.Is(err, repository.ErrNotFound):
		h.NotFound(w, r)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}

	action := "category.create"
	if draft.ID > 0 {
		action = "category.update"
	}
	h.audit(r, action, "category", strconv.Itoa(id), map[string]any{
		"slug":    draft.Slug,
		"domain":  draft.DomainID,
		"active":  draft.IsActive,
		"locales": len(names),
	})
	h.flash(w, "success", c.T("admin.category.saved", names[i18n.DefaultLocale].Name))
	redirect(w, r, "/admin/categories")
}

// AdminCategoryAction is retire and restore, which are content work.
//
// Delete is not here. It is routed separately so the role gate sits on the
// route rather than inside a switch — and because that route names the action
// in its path, there is no {action} parameter on it to read.
func (h *Handlers) AdminCategoryAction(w http.ResponseWriter, r *http.Request) {
	h.categoryAction(w, r, chi.URLParam(r, "action"))
}

// AdminCategoryDelete is the admin-only half.
func (h *Handlers) AdminCategoryDelete(w http.ResponseWriter, r *http.Request) {
	h.categoryAction(w, r, "delete")
}

func (h *Handlers) categoryAction(w http.ResponseWriter, r *http.Request, action string) {
	c, _ := h.adminCtx(w, r)

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		h.NotFound(w, r)
		return
	}

	draft, err := h.repo.CategoryDraft(r.Context(), id)
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
		err = h.repo.SetCategoryActive(r.Context(), id, false)
		if err == nil {
			h.auditChange(r, "category.retire", "category", strconv.Itoa(id), name,
				map[string]any{"state": "live"},
				map[string]any{"state": "retired"})
			h.flash(w, "success", c.T("admin.category.retired", name))
		}

	case "restore":
		err = h.repo.SetCategoryActive(r.Context(), id, true)
		if err == nil {
			h.auditChange(r, "category.restore", "category", strconv.Itoa(id), name,
				map[string]any{"state": "retired"},
				map[string]any{"state": "live"})
			h.flash(w, "success", c.T("admin.category.restored", name))
		}

	case "delete":
		// The key is RESTRICT since migration 0034, so a category still
		// holding questions is refused by the database as well as by the
		// repository. Retiring it is what the admin wanted in that case, and
		// the message says so rather than reporting a failure.
		err = h.repo.DeleteCategory(r.Context(), id)
		switch {
		case errors.Is(err, repository.ErrConflict):
			h.flash(w, "error", c.T("admin.category.notEmpty", name))
			err = nil
		case err == nil:
			h.audit(r, "category.delete", "category", strconv.Itoa(id),
				map[string]any{"slug": draft.Slug})
			h.flash(w, "success", c.T("admin.category.deleted", name))
		}

	default:
		h.NotFound(w, r)
		return
	}

	if err != nil {
		h.serverError(w, r, fmt.Errorf("category action: %w", err))
		return
	}
	redirect(w, r, "/admin/categories")
}

// domainOptions is what the category form may file a category under: every
// active subject area, plus `keep` if it names one that is not active.
//
// The exception is the whole point. A domain can be retired after categories
// have been filed under it, and those categories still need editing — their
// names corrected, their questions moved — without the form quietly relocating
// them to the first subject area in the list.
func (h *Handlers) domainOptions(ctx context.Context, locale string, keep int) ([]*models.Domain, error) {
	domains, err := h.repo.Domains(ctx, locale)
	if err != nil {
		return nil, err
	}
	if keep <= 0 {
		return domains, nil
	}
	for _, dom := range domains {
		if dom.ID == keep {
			return domains, nil
		}
	}

	all, err := h.repo.AdminDomains(ctx, locale)
	if err != nil {
		return nil, err
	}
	for _, dom := range all {
		if dom.ID == keep {
			retired := dom.Domain
			domains = append(domains, &retired)
			break
		}
	}
	return domains, nil
}

// categoryFormFromDraft fills the edit form from what is stored.
func categoryFormFromDraft(d *models.CategoryDraft) views.AdminCategoryFormData {
	form := views.AdminCategoryFormData{
		ID:        d.ID,
		DomainID:  d.DomainID,
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

// AdminCategoryReorder writes a new running order for the category list.
//
// The order players see is sort_order, which the category form could only set
// one row at a time as a number the admin had to work out themselves. This
// takes the list as it was arranged on screen and makes the arrangement the
// order, which is the only way a running order of a dozen categories is
// actually editable.
//
// Moderator, not admin: this is the same content work as naming a category,
// and nothing is destroyed — every id in the list keeps its row and its
// questions, only the order changes.
func (h *Handlers) AdminCategoryReorder(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	// One id per form value, in order. Repeated keys rather than a delimited
	// string so the browser does the encoding and a slug containing the
	// delimiter can never split an id in half.
	raw := r.PostForm["id"]
	ids := make([]int, 0, len(raw))
	seen := map[int]bool{}
	for _, value := range raw {
		id, err := strconv.Atoi(strings.TrimSpace(value))
		// A duplicate id would give one row two positions, and the last write
		// would win silently. Refuse the whole list instead: a reorder that
		// arrives malformed is a bug in the sender, not a user mistake.
		if err != nil || id <= 0 || seen[id] {
			h.NotFound(w, r)
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		h.NotFound(w, r)
		return
	}

	moved, err := h.repo.ReorderCategories(r.Context(), ids)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if moved > 0 {
		h.audit(r, "category.reorder", "category", "", map[string]any{
			"order": ids, "moved": moved,
		})
		h.flash(w, "success", c.T("admin.category.reordered", moved))
	}
	redirect(w, r, backTo(r, "/admin/categories"))
}

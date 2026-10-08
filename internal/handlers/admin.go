package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// Shared admin plumbing: the badge counts every admin page carries, the audit
// helper, and the small validators.
const adminPageSize = 40

// audit records a privileged action. Failure to log is reported but never
// blocks the action the admin actually asked for.
func (h *Handlers) audit(r *http.Request, action, kind, id string, detail map[string]any) {
	user := userFrom(r)
	if user == nil {
		return
	}
	entry := models.AuditEntry{
		ActorID:       &user.ID,
		ActorUsername: user.Username,
		Action:        action,
		TargetKind:    kind,
		TargetID:      id,
		Detail:        detail,
		IP:            service.ClientKey(r),
	}
	if entry.Detail == nil {
		entry.Detail = map[string]any{}
	}
	if err := h.repo.Audit(r.Context(), entry); err != nil {
		slogError(r, fmt.Errorf("audit %s: %w", action, err))
	}
}

// auditChange records a privileged action together with what it changed.
//
// The trail has carried a jsonb detail column since 0003 and every writer put
// loose keys in it, so the audit screen could say that a category was retired
// but never what it was retired from — which is the question somebody reading
// the log a week later is actually asking. This writes the pair under the two
// names the screen reads, beside a label so the row can name its target
// instead of printing a bare id.
//
// before and after hold the fields that moved and nothing else. A full
// snapshot of the row would bury the one line that changed, and the trail is
// read by people, not diffed by machines.
func (h *Handlers) auditChange(r *http.Request, action, kind, id, label string,
	before, after map[string]any) {

	detail := map[string]any{}
	if label != "" {
		detail["label"] = label
	}
	if len(before) > 0 {
		detail["before"] = before
	}
	if len(after) > 0 {
		detail["after"] = after
	}
	h.audit(r, action, kind, id, detail)
}

// adminCtx adds the admin-only badge counts to the normal view context.
func (h *Handlers) adminCtx(w http.ResponseWriter, r *http.Request) (views.Ctx, views.AdminChrome) {
	c := h.viewCtx(w, r)
	// The admin area answers 404 rather than 403 so it does not advertise what
	// it is withholding — and a navigation entry that 404s on click hands that
	// back. The three admin-only destinations are offered to admins only.
	chrome := views.AdminChrome{Path: r.URL.Path, IsAdmin: c.User != nil && c.User.IsAdmin()}
	if n, err := h.repo.PendingReviewCount(r.Context()); err == nil {
		chrome.PendingReview = n
	}
	if counts, err := h.repo.TicketCounts(r.Context(), c.User.ID, true); err == nil {
		chrome.AwaitingSupport = counts.AwaitingReply
	}
	if n, err := h.repo.CountPoorlyRated(r.Context(),
		models.PoorRatingThreshold, models.MinRatingVotes); err == nil {
		chrome.PoorlyRated = n
	}
	return c, chrome
}

// ------------------------------------------------------------- dashboard --

// ------------------------------------------------------------------ users --

func (h *Handlers) AdminNewUserForm(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)
	h.render(w, r, http.StatusOK, views.AdminNewUser(c, chrome, views.AdminNewUserData{
		Role:   models.RolePlayer,
		Locale: c.Locale,
	}))
}

// AdminCreateUser is the admin-side account creation path: same validation as
// public registration, plus a role chosen by the admin.
func (h *Handlers) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	form := views.AdminNewUserData{
		Username:    strings.TrimSpace(r.PostFormValue("username")),
		Email:       strings.TrimSpace(r.PostFormValue("email")),
		DisplayName: strings.TrimSpace(r.PostFormValue("display_name")),
		Country:     clip(strings.TrimSpace(r.PostFormValue("country")), 60),
		Role:        validRole(r.PostFormValue("role")),
		Locale:      r.PostFormValue("locale"),
	}
	if form.Role == "" {
		form.Role = models.RolePlayer
	}
	if !i18n.IsSupported(form.Locale) {
		form.Locale = c.Locale
	}

	password := r.PostFormValue("password")

	user, err := h.auth.RegisterAs(r.Context(), service.RegisterInput{
		Username:        form.Username,
		Email:           form.Email,
		DisplayName:     form.DisplayName,
		Password:        password,
		PasswordConfirm: password,
		Locale:          form.Locale,
	}, form.Role, form.Country)

	if err != nil {
		var fes *service.FieldErrors
		var fe *service.FieldError
		switch {
		case errors.As(err, &fes):
			form.Errors = fes.Map(c.T)
		case errors.As(err, &fe):
			if fe.Field == "" {
				form.Error = c.T(fe.Key, fe.Args...)
			} else {
				form.Errors = map[string]string{fe.Field: c.T(fe.Key, fe.Args...)}
			}
		default:
			h.serverError(w, r, err)
			return
		}
		h.render(w, r, http.StatusUnprocessableEntity,
			views.AdminNewUser(c, chrome, form))
		return
	}

	h.audit(r, "user.create", "user", user.ID.String(), map[string]any{
		"username": user.Username, "role": user.Role,
	})
	h.flash(w, "success", c.T("admin.users.created", user.Username))
	redirect(w, r, "/admin/users")
}

// -------------------------------------------------------------- questions --

// ------------------------------------------------------- question editing --

func questionFormFromDraft(draft *models.QuestionDraft) views.AdminQuestionFormData {
	d := views.AdminQuestionFormData{
		ID:           draft.ID,
		CategoryID:   draft.CategoryID,
		Difficulty:   draft.Difficulty,
		Points:       draft.Points,
		CorrectIndex: draft.CorrectIndex,
		Source:       draft.Source,
		IsActive:     draft.IsActive,
		Locales:      map[string]views.QuestionLocaleForm{},
	}
	for locale, t := range draft.Translations {
		choices := make([]string, 4)
		copy(choices, t.Choices)
		d.Locales[locale] = views.QuestionLocaleForm{
			Prompt:      t.Prompt,
			Choices:     choices,
			Explanation: t.Explanation,
			Source:      t.Source,
			NeedsReview: t.NeedsReview,
		}
	}
	return d
}

// ----------------------------------------------------------------- import --

// duplicateDecisions reads what the admin chose about each flagged row, as
// "dup:<line>=duplicate|nothing|anyway".
//
// Keyed by line rather than by question id because a row the importer refuses
// may have no id yet, and because the line is what the admin is looking at in
// the report.
func duplicateDecisions(r *http.Request) map[int]string {
	out := map[int]string{}
	for key, values := range r.PostForm {
		if !strings.HasPrefix(key, "dup:") || len(values) == 0 {
			continue
		}
		line, err := strconv.Atoi(strings.TrimPrefix(key, "dup:"))
		if err != nil || line <= 0 {
			continue
		}
		if decision := service.ValidDecision(values[0]); decision != service.DecideNone {
			out[line] = decision
		}
	}
	return out
}

// AdminDuplicateVerdict records what a moderator decided about one pair.
//
// Two verdicts, because there are two useful answers. "Different questions"
// takes the pair off the list for good; "retire this one" deactivates the
// weaker of the two, which is what the bank already understands and what keeps
// every player's history intact. Deleting is deliberately not offered here — it
// cascades into answered rounds, and the question editor already refuses it
// once for exactly that reason.
func (h *Handlers) AdminDuplicateVerdict(w http.ResponseWriter, r *http.Request) {
	c, _ := h.adminCtx(w, r)

	left, errLeft := strconv.Atoi(r.PostFormValue("left"))
	right, errRight := strconv.Atoi(r.PostFormValue("right"))
	if errLeft != nil || errRight != nil || left <= 0 || right <= 0 || left == right {
		h.NotFound(w, r)
		return
	}
	back := backTo(r, "/admin/integrity")

	switch r.PostFormValue("verdict") {
	case "distinct":
		if err := h.repo.MarkPairDistinct(r.Context(), left, right, c.User.ID); err != nil {
			h.serverError(w, r, err)
			return
		}
		h.audit(r, "duplicate.distinct", "question", fmt.Sprint(left),
			map[string]any{"other": right})
		h.flash(w, "success", c.T("admin.integrity.markedDistinct", left, right))

	case "retire":
		// Which of the two goes is the moderator's call, not a guess.
		victim, err := strconv.Atoi(r.PostFormValue("retire"))
		if err != nil || (victim != left && victim != right) {
			h.NotFound(w, r)
			return
		}
		if err := h.repo.SetQuestionActive(r.Context(), victim, false); err != nil {
			h.serverError(w, r, err)
			return
		}
		// Retiring settles the pair as well: an inactive question is not going
		// to be drawn, so the two are no longer a duplicate anybody can meet.
		if err := h.repo.MarkPairDistinct(r.Context(), left, right, c.User.ID); err != nil {
			h.serverError(w, r, err)
			return
		}
		h.audit(r, "duplicate.retire", "question", fmt.Sprint(victim),
			map[string]any{"kept": left + right - victim})
		h.flash(w, "success", c.T("admin.integrity.retired", victim))

	default:
		h.NotFound(w, r)
		return
	}

	// The sweep is held for a few minutes; a decision has to show immediately.
	h.duplicates.Invalidate()
	redirect(w, r, back)
}

// ----------------------------------------------------- review & integrity --

// pickTranslationSource chooses which existing locale to translate from.
//
// Arabic first: it is the language the bank is authored in and the one every
// content query falls back to, so it is the text least likely to be itself a
// translation. Never translate from a locale that is still awaiting review, or
// a machine rendering becomes the source for the next machine rendering.
func pickTranslationSource(draft *models.QuestionDraft, target string) (*models.TranslationDraft, string) {
	if t, ok := draft.Translations[i18n.DefaultLocale]; ok && !t.NeedsReview && target != i18n.DefaultLocale {
		return &t, i18n.DefaultLocale
	}
	for _, loc := range i18n.Supported {
		if loc.Code == target {
			continue
		}
		if t, ok := draft.Translations[loc.Code]; ok && !t.NeedsReview {
			return &t, loc.Code
		}
	}
	return nil, ""
}

// ------------------------------------------------------ comment moderation --

// ---------------------------------------------------------------- helpers --

func pageCount(total, size int) int {
	if size <= 0 {
		return 1
	}
	n := (total + size - 1) / size
	if n < 1 {
		return 1
	}
	return n
}

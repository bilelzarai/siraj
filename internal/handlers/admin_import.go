package handlers

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// Bulk upload: preview, decide, apply.
func (h *Handlers) AdminImportForm(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)
	recent, _ := h.repo.RecentImports(r.Context(), 10)
	h.render(w, r, http.StatusOK, views.AdminImport(c, chrome, views.AdminImportData{
		Recent: recent,
	}))
}

// AdminImportTemplate hands back a filled-in CSV so the format is obvious.
func (h *Handlers) AdminImportTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="questions-template.csv"`)
	// A BOM makes Excel open UTF-8 Arabic correctly instead of as mojibake.
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	_, _ = w.Write([]byte(service.ImportTemplateCSV()))
}

// AdminImportUpload parses the file and either previews or commits it. The
// preview and the commit run the same code path, so what an admin approves is
// exactly what gets written.
//
// The file is read once, on the preview, and held server-side. Applying it
// then needs only its token — no second trip through the file dialog, and no
// way to approve one file and apply another, because there is only ever the
// one upload.
func (h *Handlers) AdminImportUpload(w http.ResponseWriter, r *http.Request) {
	c, chrome := h.adminCtx(w, r)

	var (
		raw      []byte
		filename string
		format   string
		token    = strings.TrimSpace(r.PostFormValue("stash"))
	)

	if name, fmtName, data, ok := h.stash.Get(token, c.User.ID); ok {
		filename, format, raw = name, fmtName, data
	} else {
		token = ""
		file, header, err := r.FormFile("file")
		if err != nil {
			// Either nothing was attached, or the preview being applied has
			// aged out of the stash. Both end in the same place: ask for the
			// file, rather than pretending an empty import happened.
			h.flash(w, "error", c.T("admin.import.noFile"))
			redirect(w, r, "/admin/questions/import")
			return
		}
		defer file.Close()

		raw, err = io.ReadAll(io.LimitReader(file, service.MaxImportBytes+1))
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		filename = clip(header.Filename, 120)
		format = strings.ToLower(strings.TrimSpace(r.PostFormValue("format")))
		if format == "" {
			if strings.HasSuffix(strings.ToLower(filename), ".csv") {
				format = "csv"
			} else {
				format = "json"
			}
		}
	}

	records, rejected, err := h.importer.Parse(bytes.NewReader(raw), format)
	if err != nil {
		recent, _ := h.repo.RecentImports(r.Context(), 10)
		h.render(w, r, http.StatusUnprocessableEntity,
			views.AdminImport(c, chrome, views.AdminImportData{
				Recent: recent,
				Error:  err.Error(),
			}))
		return
	}

	// Committing is only ever the second press on a file already previewed, so
	// a commit arriving without a stash token has no preview behind it.
	commit := r.PostFormValue("commit") == "1" && token != ""
	actor := c.User.ID

	run, err := h.importer.Run(r.Context(), records, rejected, &actor,
		filename, format, commit, duplicateDecisions(r))
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	if token == "" {
		token = h.stash.Put(c.User.ID, filename, format, raw)
	}

	// Only while the file is still a proposal: the panels exist to answer the
	// question the preview asks, and once the import is applied there is nothing
	// left to decide. Failing to build them is not worth losing the report over,
	// so the preview renders without them.
	// Logged rather than swallowed: the panels going missing looks exactly like
	// the feature not existing, and an admin deciding duplicates without them
	// would have no reason to suspect anything went wrong.
	var compare []*models.QuestionCompare
	if !commit {
		if compare, err = h.comparer.Import(r.Context(), records, run); err != nil {
			slog.ErrorContext(r.Context(), "import comparison failed",
				"file", filename, "error", err)
			compare = nil
		}
	}

	if commit {
		h.audit(r, "question.import", "import", run.ID.String(), map[string]any{
			"file": filename, "added": run.Added,
			"updated": run.Updated, "rejected": run.Rejected,
		})
		h.flash(w, "success", c.T("admin.import.committed", run.Added, run.Updated))

		// Applied, so the held upload has nothing left to do. Keeping it would
		// leave an Apply button on screen that writes the same file again.
		h.stash.Drop(token)
		token = ""
	}

	recent, _ := h.repo.RecentImports(r.Context(), 10)
	h.render(w, r, http.StatusOK, views.AdminImport(c, chrome, views.AdminImportData{
		Recent:   recent,
		Result:   run,
		Preview:  !commit,
		Stash:    token,
		Filename: filename,
		Compare:  compare,
	}))
}

package handlers_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

func adminQuestionsAll() repository.AdminQuestionFilter {
	return repository.AdminQuestionFilter{Locale: "en", Limit: 1}
}

// Upload, preview, apply. The only multi-step flow in the console, and the one
// whose middle step holds state the second request has to find again — so a
// redesign that loses a hidden field turns "apply" into a second upload with
// no file attached.
func TestImportPreviewThenApply(t *testing.T) {
	a := newApp(t)
	a.register("importer")
	a.promote("importer", models.RoleAdmin)

	status, body := a.get("/admin/questions/import")
	if status != 200 {
		t.Fatalf("the import screen → %d", status)
	}
	// The template the screen offers has to be the shape the parser reads.
	if !strings.Contains(body, "/admin/questions/import/template.csv") {
		t.Error("the screen does not offer its own template")
	}
	header := csvHeader(t, a)

	// The template's own column order, read from the header above. The id is
	// a grouping key inside the file, not a database id — rows sharing one
	// are the same question in different languages — so a new question just
	// needs a positive number nothing else in the file is using.
	csv := header + "\n" +
		"1,quran,1,10,0,ar,سؤال الاستيراد الأول,أ,ب,ج,د,,\n" +
		"2,quran,2,20,1,ar,سؤال الاستيراد الثاني,أ,ب,ج,د,,\n"

	// ---- step one: preview, which must not write anything
	before := countQuestions(t, a)
	status, preview := a.upload("/admin/questions/import", "import.csv", csv, nil)
	if status != 200 {
		t.Fatalf("the preview → %d", status)
	}
	if after := countQuestions(t, a); after != before {
		t.Errorf("the preview wrote %d questions; it must only report", after-before)
	}

	// ---- the preview holds the parsed file for the apply step
	stash := regexp.MustCompile(`name="stash" value="([^"]+)"`).FindStringSubmatch(preview)
	if stash == nil {
		t.Fatalf("the preview offers no way to apply itself:\n%s", trim(preview))
	}
	if !strings.Contains(preview, `name="commit"`) {
		t.Error("the preview has no apply button")
	}

	// ---- step two: apply, carrying only what the preview rendered
	status, _ = a.upload("/admin/questions/import", "", "", map[string]string{
		"stash": stash[1], "commit": "1", "format": "csv",
	})
	if status != http.StatusSeeOther && status != 200 {
		t.Fatalf("applying the preview → %d", status)
	}
	if after := countQuestions(t, a); after != before+2 {
		t.Errorf("the apply added %d questions, want 2", after-before)
	}

	// ---- and it is in the trail, with what it did
	entries, err := a.repo.AuditPage(t.Context(), auditAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Action == "question.import" {
			return
		}
	}
	t.Error("an import left no entry in the trail")
}

func csvHeader(t *testing.T, a *app) string {
	t.Helper()
	status, body := a.get("/admin/questions/import/template.csv")
	if status != 200 {
		t.Fatalf("the template → %d", status)
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimPrefix(body, "\xef\xbb\xbf"), "\n", 2)[0])
}

func countQuestions(t *testing.T, a *app) int {
	t.Helper()
	_, total, err := a.repo.AdminQuestions(t.Context(), adminQuestionsAll())
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func trim(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

// upload posts a multipart form, which is the only way the import screen can
// be driven: its first step carries a file and the rest of the form with it.
func (a *app) upload(path, filename, content string, fields map[string]string) (int, string) {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("csrf_token", a.csrf())
	if filename != "" {
		part, err := w.CreateFormFile("file", filename)
		if err != nil {
			a.t.Fatal(err)
		}
		_, _ = part.Write([]byte(content))
		_ = w.WriteField("format", "csv")
	}
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, a.server.URL+path, &buf)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	return res.StatusCode, readAll(res)
}

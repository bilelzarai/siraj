package handlers_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// The console in Arabic. The bank is authored in Arabic and it is the default
// locale, so a right-to-left console is the common case here and not an edge.
func TestAdminMirrorsInArabic(t *testing.T) {
	a := newApp(t)
	a.register("rtlreader")
	a.promote("rtlreader", models.RoleAdmin)
	if status, _ := a.post("/settings/locale", url.Values{"locale": {"ar"}}); status != http.StatusSeeOther {
		t.Fatalf("switching to Arabic did not redirect")
	}

	for name, path := range adminPaths {
		status, body := a.get(path)
		if status != 200 {
			t.Errorf("%s → %d", name, status)
			continue
		}
		if !strings.Contains(body, `dir="rtl"`) {
			t.Errorf("%-11s does not render right-to-left", name)
		}
		if !strings.Contains(body, `lang="ar"`) {
			t.Errorf("%-11s does not declare Arabic", name)
		}
		// A physical direction written into a style attribute does not mirror.
		// The kit is built on logical properties; anything added by hand here
		// has to be too, or the console is subtly wrong in half its languages.
		for _, m := range regexp.MustCompile(`style="([^"]*)"`).FindAllStringSubmatch(body, -1) {
			decl := m[1]
			for _, physical := range []string{"margin-left", "margin-right",
				"padding-left", "padding-right", "border-left", "border-right",
				"left:", "right:", "text-align:left", "text-align:right"} {
				if strings.Contains(decl, physical) {
					t.Errorf("%-11s has a physical %q in a style attribute: %q",
						name, physical, decl)
				}
			}
		}
	}
}

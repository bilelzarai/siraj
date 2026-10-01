package i18n

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// Every shipped locale must define every key the fallback defines, otherwise
// a user silently sees another language mid-page.
func TestCatalogsAreComplete(t *testing.T) {
	b, err := New(DefaultLocale)
	if err != nil {
		t.Fatalf("loading catalogs: %v", err)
	}

	for _, loc := range Supported {
		missing := b.MissingKeys(loc.Code)
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("locale %q is missing %d key(s): %v", loc.Code, len(missing), missing)
		}
	}
}

// A key present only in a non-fallback catalog is dead weight and usually a
// typo, so check the reverse direction too.
func TestNoExtraKeys(t *testing.T) {
	b, err := New(DefaultLocale)
	if err != nil {
		t.Fatal(err)
	}

	base := b.catalogs[DefaultLocale]
	for _, loc := range Supported {
		if loc.Code == DefaultLocale {
			continue
		}
		for key := range b.catalogs[loc.Code] {
			if _, ok := base[key]; !ok {
				t.Errorf("locale %q defines %q, which the fallback catalog does not", loc.Code, key)
			}
		}
	}
}

// Format verbs must line up across locales, or Sprintf renders %!d(MISSING).
func TestFormatVerbsMatchAcrossLocales(t *testing.T) {
	b, err := New(DefaultLocale)
	if err != nil {
		t.Fatal(err)
	}

	base := b.catalogs[DefaultLocale]
	for _, loc := range Supported {
		if loc.Code == DefaultLocale {
			continue
		}
		for key, baseVal := range base {
			other, ok := b.catalogs[loc.Code][key]
			if !ok {
				continue // reported by TestCatalogsAreComplete
			}
			if got, want := countVerbs(other), countVerbs(baseVal); got != want {
				t.Errorf("%s/%s has %d format verb(s), fallback has %d\n  %s\n  %s",
					loc.Code, key, got, want, baseVal, other)
			}
		}
	}
}

func countVerbs(s string) int {
	n := 0
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '%' {
			continue
		}
		if s[i+1] == '%' {
			i++ // an escaped literal percent
			continue
		}
		n++
	}
	return n
}

func TestTranslateFallsBack(t *testing.T) {
	b, err := New("en")
	if err != nil {
		t.Fatal(err)
	}

	if got := b.T("en", "nav.home"); got != "Home" {
		t.Errorf("nav.home = %q, want Home", got)
	}
	// An unsupported locale falls back to the configured default.
	if got := b.T("de", "nav.home"); got != "Home" {
		t.Errorf("unknown locale should fall back, got %q", got)
	}
	// An unknown key surfaces as itself rather than as an empty string.
	if got := b.T("en", "does.not.exist"); got != "does.not.exist" {
		t.Errorf("unknown key = %q, want the key echoed back", got)
	}
}

func TestTranslateWithArguments(t *testing.T) {
	b, _ := New("en")
	got := b.T("en", "home.greeting", "Malek")
	if !strings.Contains(got, "Malek") {
		t.Errorf("greeting %q should contain the name", got)
	}
}

func TestPrinterDirection(t *testing.T) {
	b, _ := New("ar")

	if p := b.Printer("ar"); !p.IsRTL() || p.Dir() != "rtl" {
		t.Error("Arabic must render right-to-left")
	}
	for _, code := range []string{"en", "fr"} {
		if p := b.Printer(code); p.IsRTL() {
			t.Errorf("%s must render left-to-right", code)
		}
	}
	// An unsupported request falls back to the bundle default.
	if p := b.Printer("zz"); p.Locale() != "ar" {
		t.Errorf("unsupported locale resolved to %q, want the fallback ar", p.Locale())
	}
}

func TestNegotiatePriority(t *testing.T) {
	newReq := func(query, cookie, accept string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/"+query, nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "locale", Value: cookie})
		}
		if accept != "" {
			r.Header.Set("Accept-Language", accept)
		}
		return r
	}

	cases := []struct {
		name       string
		query      string
		userLocale string
		cookie     string
		accept     string
		want       string
	}{
		{"query wins over everything", "?lang=fr", "en", "ar", "en-GB", "fr"},
		{"user preference beats cookie", "", "en", "ar", "fr", "en"},
		{"cookie beats Accept-Language", "", "", "fr", "en-GB", "fr"},
		{"Accept-Language is the last signal", "", "", "", "fr-FR,fr;q=0.9", "fr"},
		{"unsupported query is ignored", "?lang=de", "", "en", "", "en"},
		{"nothing at all falls back", "", "", "", "", "ar"},
		{"unknown Accept-Language falls back", "", "", "", "ja-JP", "ar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Negotiate(newReq(tc.query, tc.cookie, tc.accept), tc.userLocale, "ar")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsSupported(t *testing.T) {
	for _, code := range []string{"ar", "en", "fr"} {
		if !IsSupported(code) {
			t.Errorf("%q should be supported", code)
		}
	}
	for _, code := range []string{"", "de", "ar-EG", "AR"} {
		if IsSupported(code) {
			t.Errorf("%q should not be supported", code)
		}
	}
}

func TestPrinterDuration(t *testing.T) {
	p := (&Bundle{catalogs: map[string]map[string]string{"en": {
		"common.seconds": "s",
	}}, fallback: "en"}).Printer("en")

	if got := p.Duration(4200); got != "4s" {
		t.Errorf("Duration(4200) = %q, want 4s", got)
	}
	if got := p.Duration(125000); got != "2:05" {
		t.Errorf("Duration(125000) = %q, want 2:05", got)
	}
}

// models.ShippedLocales is the same number as len(Supported), duplicated there
// so the models package can stay free of every import. If a language is added
// or removed this is what notices.
func TestShippedLocalesMatchesTheCatalogList(t *testing.T) {
	if models.ShippedLocales != len(Supported) {
		t.Errorf("models.ShippedLocales = %d, but %d locales are shipped; update the constant",
			models.ShippedLocales, len(Supported))
	}
}

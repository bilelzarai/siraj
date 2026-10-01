package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localeFS embed.FS

// Locale describes one supported language.
type Locale struct {
	Code        string
	Name        string // endonym, shown in the switcher
	EnglishName string
	Dir         string // "rtl" or "ltr"
	Flag        string
}

// Supported is ordered as the language switcher renders it.
var Supported = []Locale{
	{Code: "ar", Name: "العربية", EnglishName: "Arabic", Dir: "rtl", Flag: "🇸🇦"},
	{Code: "en", Name: "English", EnglishName: "English", Dir: "ltr", Flag: "🇬🇧"},
	{Code: "fr", Name: "Français", EnglishName: "French", Dir: "ltr", Flag: "🇫🇷"},
}

const DefaultLocale = "ar"

var matcher = language.NewMatcher([]language.Tag{
	language.Arabic,
	language.English,
	language.French,
})

// Bundle holds every catalog in memory. It is read-only after New.
type Bundle struct {
	catalogs map[string]map[string]string
	fallback string
}

// New loads all embedded catalogs and verifies the fallback exists.
func New(fallback string) (*Bundle, error) {
	if !IsSupported(fallback) {
		fallback = DefaultLocale
	}

	b := &Bundle{
		catalogs: make(map[string]map[string]string, len(Supported)),
		fallback: fallback,
	}

	for _, loc := range Supported {
		raw, err := localeFS.ReadFile("locales/" + loc.Code + ".json")
		if err != nil {
			return nil, fmt.Errorf("read catalog %s: %w", loc.Code, err)
		}
		var catalog map[string]string
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return nil, fmt.Errorf("parse catalog %s: %w", loc.Code, err)
		}
		b.catalogs[loc.Code] = catalog
	}
	return b, nil
}

// MissingKeys reports keys present in the fallback catalog but absent from
// another locale. Used by the startup self-check and by tests.
func (b *Bundle) MissingKeys(locale string) []string {
	base := b.catalogs[b.fallback]
	target, ok := b.catalogs[locale]
	if !ok {
		return nil
	}
	var missing []string
	for k := range base {
		if _, ok := target[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

// T resolves a key for a locale, falling back to the default catalog and
// finally to the key itself, so a missing string is visible but not fatal.
func (b *Bundle) T(locale, key string, args ...any) string {
	if catalog, ok := b.catalogs[locale]; ok {
		if v, ok := catalog[key]; ok {
			return format(v, args)
		}
	}
	if catalog, ok := b.catalogs[b.fallback]; ok {
		if v, ok := catalog[key]; ok {
			return format(v, args)
		}
	}
	return key
}

func format(s string, args []any) string {
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Printer binds a locale so templates can call p.T("key") directly.
type Printer struct {
	bundle *Bundle
	locale string
}

func (b *Bundle) Printer(locale string) *Printer {
	if !IsSupported(locale) {
		locale = b.fallback
	}
	return &Printer{bundle: b, locale: locale}
}

func (p *Printer) T(key string, args ...any) string { return p.bundle.T(p.locale, key, args...) }
func (p *Printer) Locale() string                   { return p.locale }
func (p *Printer) Dir() string                      { return DirOf(p.locale) }
func (p *Printer) IsRTL() bool                      { return DirOf(p.locale) == "rtl" }

// RelativeTime renders a short, localised "time ago" label.
func (p *Printer) RelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return p.T("common.justNow")
	case d < time.Hour:
		return p.T("common.minutesAgo", int(d.Minutes()))
	case d < 24*time.Hour:
		return p.T("common.hoursAgo", int(d.Hours()))
	default:
		return p.T("common.daysAgo", int(d.Hours()/24))
	}
}

// Date renders a locale-appropriate short date.
func (p *Printer) Date(t time.Time) string {
	switch p.locale {
	case "ar":
		return t.Format("2006/01/02")
	case "fr":
		return t.Format("02/01/2006")
	default:
		return t.Format("Jan 2, 2006")
	}
}

// DateTime renders date plus 24-hour clock.
func (p *Printer) DateTime(t time.Time) string {
	return p.Date(t) + " · " + t.Format("15:04")
}

// Duration renders a compact m:ss or Ns label.
func (p *Printer) Duration(ms int) string {
	secs := ms / 1000
	if secs < 60 {
		return fmt.Sprintf("%d%s", secs, p.T("common.seconds"))
	}
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// IsSupported reports whether a locale code is one we ship.
func IsSupported(code string) bool {
	for _, l := range Supported {
		if l.Code == code {
			return true
		}
	}
	return false
}

// DirOf returns the writing direction for a locale.
func DirOf(code string) string {
	for _, l := range Supported {
		if l.Code == code {
			return l.Dir
		}
	}
	return "ltr"
}

// FromAcceptLanguage picks the best supported locale for a request header.
func FromAcceptLanguage(header string) string {
	if header == "" {
		return ""
	}
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil || len(tags) == 0 {
		return ""
	}
	_, idx, conf := matcher.Match(tags...)
	if conf == language.No {
		return ""
	}
	if idx < 0 || idx >= len(Supported) {
		return ""
	}
	return Supported[idx].Code
}

// Negotiate resolves the locale for a request, in priority order:
// explicit query param, signed-in user preference, cookie, Accept-Language.
func Negotiate(r *http.Request, userLocale string, fallback string) string {
	if q := strings.TrimSpace(r.URL.Query().Get("lang")); IsSupported(q) {
		return q
	}
	if IsSupported(userLocale) {
		return userLocale
	}
	if c, err := r.Cookie("locale"); err == nil && IsSupported(c.Value) {
		return c.Value
	}
	if code := FromAcceptLanguage(r.Header.Get("Accept-Language")); code != "" {
		return code
	}
	if IsSupported(fallback) {
		return fallback
	}
	return DefaultLocale
}

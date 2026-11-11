// Package i18n holds every user-visible string in ar / en / fr (NF-06, LG-01).
//
// A key is flat ("home.greeting"), values may carry {name}-style placeholders.
// English is the fallback: a missing key in ar or fr falls back to en, and a key missing
// everywhere returns the key itself so it shows up in review instead of crashing a page.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed locales/*.json
var files embed.FS

// Langs are the languages of the app, in menu order.
var Langs = []string{"ar", "en", "fr"}

// Dir is the writing direction of a language (LG-02).
func Dir(lang string) string {
	if lang == "ar" {
		return "rtl"
	}
	return "ltr"
}

// Native is the language's own name, for the language menu.
var Native = map[string]string{"ar": "العربية", "en": "English", "fr": "Français"}

// English is the language's English name, under its own name on the onboarding
// step so a reader of any script recognises it (AU-04).
var English = map[string]string{"ar": "Arabic", "en": "English", "fr": "French"}

// Short is the two-letter badge shown in the top bar.
var Short = map[string]string{"ar": "ع", "en": "EN", "fr": "FR"}

// Known reports whether lang is one of the three supported languages.
func Known(lang string) bool {
	for _, l := range Langs {
		if l == lang {
			return true
		}
	}
	return false
}

// Fallback is the language used when a string or a request language is missing.
const Fallback = "en"

type Bundle struct {
	strings map[string]map[string]string // lang -> key -> value
}

// Load reads the three locale files. It fails when a file is missing or invalid, so a
// broken locale never reaches production.
func Load() (*Bundle, error) {
	b := &Bundle{strings: map[string]map[string]string{}}
	for _, lang := range Langs {
		raw, err := files.ReadFile("locales/" + lang + ".json")
		if err != nil {
			return nil, fmt.Errorf("i18n: %w", err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("i18n %s.json: %w", lang, err)
		}
		b.strings[lang] = m
	}
	return b, nil
}

// MissingKeys lists the keys that exist in English but not in lang. The test in
// i18n_test.go fails when it is not empty, which is the CI check asked for in 04-TODO.md.
func (b *Bundle) MissingKeys(lang string) []string {
	var missing []string
	for k := range b.strings[Fallback] {
		if _, ok := b.strings[lang][k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

// ExtraKeys lists keys of lang that English does not have (usually a typo).
func (b *Bundle) ExtraKeys(lang string) []string {
	var extra []string
	for k := range b.strings[lang] {
		if _, ok := b.strings[Fallback][k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return extra
}

// Keys of the English file, sorted (used by tests and the admin translation screens).
func (b *Bundle) Keys() []string {
	keys := make([]string, 0, len(b.strings[Fallback]))
	for k := range b.strings[Fallback] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Printer is the per-request view of the bundle: one language, with a fallback.
type Printer struct {
	Lang   string
	bundle *Bundle
}

// For returns a printer for lang (unknown languages get English).
func (b *Bundle) For(lang string) *Printer {
	if !Known(lang) {
		lang = Fallback
	}
	return &Printer{Lang: lang, bundle: b}
}

// T translates a key. Pairs of arguments replace {placeholders}:
//
//	p.T("home.hello", "name", "Aisha")
func (p *Printer) T(key string, args ...any) string {
	s, ok := p.bundle.strings[p.Lang][key]
	if !ok || s == "" {
		s, ok = p.bundle.strings[Fallback][key]
		if !ok || s == "" {
			return key
		}
	}
	if len(args) == 0 {
		return s
	}
	repl := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		repl = append(repl, "{"+fmt.Sprint(args[i])+"}", fmt.Sprint(args[i+1]))
	}
	return strings.NewReplacer(repl...).Replace(s)
}

// Has reports whether the key exists at all (used by screens with optional copy).
func (p *Printer) Has(key string) bool {
	if _, ok := p.bundle.strings[p.Lang][key]; ok {
		return true
	}
	_, ok := p.bundle.strings[Fallback][key]
	return ok
}

// Dir is the direction of the printer's language.
func (p *Printer) Dir() string { return Dir(p.Lang) }

// IsRTL is a convenience for templates.
func (p *Printer) IsRTL() bool { return p.Lang == "ar" }

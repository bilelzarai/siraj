package auth

import (
	"regexp"
	"strings"
)

// The phone side of the V5 sign-in flow: the country list the picker shows and the
// rules that turn what a player types into one E.164 number.

// Country is one line of the picker: the flag letters, the dialling code and how
// many digits a national number has there (the design's rule — Tunisia and Qatar 8,
// France 9, and so on).
type Country struct {
	Code   string // ISO 3166-1 alpha-2, e.g. TN
	Name   string // English name; the picker shows it translated
	Dial   string // +216
	Digits []int  // allowed national lengths
}

// Countries is the list in the order the design shows it: the ones Sirāj is played
// in first, then the rest alphabetically.
var Countries = []Country{
	{"TN", "Tunisia", "+216", []int{8}},
	{"QA", "Qatar", "+974", []int{8}},
	{"SA", "Saudi Arabia", "+966", []int{9}},
	{"AE", "United Arab Emirates", "+971", []int{9}},
	{"MA", "Morocco", "+212", []int{9}},
	{"DZ", "Algeria", "+213", []int{9}},
	{"EG", "Egypt", "+20", []int{10}},
	{"KW", "Kuwait", "+965", []int{8}},
	{"BH", "Bahrain", "+973", []int{8}},
	{"OM", "Oman", "+968", []int{8}},
	{"JO", "Jordan", "+962", []int{9}},
	{"LB", "Lebanon", "+961", []int{7, 8}},
	{"LY", "Libya", "+218", []int{9}},
	{"TR", "Türkiye", "+90", []int{10}},
	{"FR", "France", "+33", []int{9}},
	{"BE", "Belgium", "+32", []int{8, 9}},
	{"DE", "Germany", "+49", []int{10, 11}},
	{"IT", "Italy", "+39", []int{9, 10}},
	{"ES", "Spain", "+34", []int{9}},
	{"GB", "United Kingdom", "+44", []int{10}},
	{"US", "United States", "+1", []int{10}},
	{"CA", "Canada", "+1", []int{10}},
	{"MY", "Malaysia", "+60", []int{9, 10}},
	{"ID", "Indonesia", "+62", []int{9, 10, 11}},
	{"PK", "Pakistan", "+92", []int{10}},
	{"IN", "India", "+91", []int{10}},
}

// DefaultCountry is the one the picker opens on.
const DefaultCountry = "TN"

// CountryByCode finds a country, falling back to the default rather than refusing.
func CountryByCode(code string) Country {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, c := range Countries {
		if c.Code == code {
			return c
		}
	}
	return CountryByCode(DefaultCountry)
}

var digitsOnly = regexp.MustCompile(`\D`)

// NormalisePhone turns a country and whatever was typed into E.164, and says whether
// it is a number that country could have. Everything downstream — the code, the
// account, the lookup — uses only this form.
func NormalisePhone(country, typed string) (string, bool) {
	c := CountryByCode(country)
	national := digitsOnly.ReplaceAllString(typed, "")
	dial := strings.TrimPrefix(c.Dial, "+")

	// A number pasted with its country code, with or without the +.
	national = strings.TrimPrefix(national, "00"+dial)
	if strings.HasPrefix(national, dial) && len(national) > len(dial) {
		if !allowedLength(c, len(national)) {
			national = strings.TrimPrefix(national, dial)
		}
	}
	// A national number written with its trunk zero.
	if len(national) > 0 && national[0] == '0' && !allowedLength(c, len(national)) {
		national = strings.TrimPrefix(national, "0")
	}
	if !allowedLength(c, len(national)) {
		return "", false
	}
	return c.Dial + national, true
}

func allowedLength(c Country, n int) bool {
	for _, want := range c.Digits {
		if n == want {
			return true
		}
	}
	return false
}

// MaskPhone is how a number is shown back in a message: the country code and the
// last two digits, so a player recognises it without it being readable over a
// shoulder. The design prints the whole number on the code screen, which is the one
// place the player has just typed it themselves.
func MaskPhone(e164 string) string {
	if len(e164) < 6 {
		return e164
	}
	return e164[:4] + strings.Repeat("·", len(e164)-6) + e164[len(e164)-2:]
}

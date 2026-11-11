package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// Passwords are hashed with argon2id, as the architecture doc asks. The parameters and
// the salt travel inside the stored string, so they can be raised later without a migration:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLength is the rule shown on the sign-up screen (AU-02).
const MinPasswordLength = 8

func hashPassword(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// checkPassword is constant-time and never tells which part failed.
func checkPassword(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// HashPassword is hashPassword for the tools that create accounts outside a request
// (cmd/seed), so there is still only one place that decides how a password is stored.
func HashPassword(plain string) (string, error) { return hashPassword(plain) }

// The five checks the create-account screen lists under the field — 8 chars, A-Z,
// a-z, 123, @#$ — and the Weak / Medium / Strong word beside the bar. The screen
// shows them live in Alpine; this is the same rule on the server, which is the one
// that decides (NF-04).

// PasswordRule is one of the five checks, named by the key the screen uses.
type PasswordRule string

const (
	RuleLength PasswordRule = "length"
	RuleUpper  PasswordRule = "upper"
	RuleLower  PasswordRule = "lower"
	RuleDigit  PasswordRule = "digit"
	RuleSymbol PasswordRule = "symbol"
)

// PasswordRules is the list in the order the screen prints it.
var PasswordRules = []PasswordRule{RuleLength, RuleUpper, RuleLower, RuleDigit, RuleSymbol}

// PasswordChecks says which of the five a password meets.
func PasswordChecks(plain string) map[PasswordRule]bool {
	met := map[PasswordRule]bool{RuleLength: len([]rune(plain)) >= MinPasswordLength}
	for _, r := range plain {
		switch {
		case r >= 'A' && r <= 'Z':
			met[RuleUpper] = true
		case r >= 'a' && r <= 'z':
			met[RuleLower] = true
		case r >= '0' && r <= '9':
			met[RuleDigit] = true
		case !unicode.IsSpace(r) && !unicode.IsLetter(r) && !unicode.IsDigit(r):
			met[RuleSymbol] = true
		}
	}
	return met
}

// PasswordStrength is weak, medium or strong — the word the screen prints.
func PasswordStrength(plain string) string {
	n := 0
	for _, met := range PasswordChecks(plain) {
		if met {
			n++
		}
	}
	switch {
	case n >= len(PasswordRules):
		return "strong"
	case n >= 3:
		return "medium"
	default:
		return "weak"
	}
}

// ValidateNewPassword is the rule a brand-new account's password has to pass: all
// five checks, which is what unlocks the button on the screen.
func ValidateNewPassword(plain string) []PasswordRule {
	var missing []PasswordRule
	met := PasswordChecks(plain)
	for _, rule := range PasswordRules {
		if !met[rule] {
			missing = append(missing, rule)
		}
	}
	return missing
}

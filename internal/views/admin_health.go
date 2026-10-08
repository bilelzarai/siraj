package views

import (
	"github.com/bilelzarai/siraj/internal/models"
)

// Content health's reading of the bank.
//
// The screen's job is one sentence — is the bank sound — and the issue list
// underneath is the detail behind it. A list alone cannot say whether a
// hundred missing explanations is a crisis or a Tuesday, which is what the
// score and the checks are for.

// HealthCheck is one named thing that is or is not true of the bank, with the
// proportion of it that passes.
type HealthCheck struct {
	// Key is the catalogue key for the sentence; Detail its second line.
	Key, DetailKey string
	// Level is "ok", "warn" or "err", which decides the icon and the colour.
	Level string
	// Share is how much of the bank passes this check, as a percentage, and
	// Failing how many rows do not.
	Share   int
	Failing int
	// Action is where to go and fix it, empty for a check that is passing.
	Action    string
	ActionKey string
}

// healthWeights is how much each kind of problem costs the score.
//
// A missing translation is the most expensive because it removes the question
// from a third of players outright; a broken question is next because it is
// wrong for everybody who draws it; a thin explanation is a quality problem
// and costs least. The numbers are a judgement, not a measurement, which is
// why they are named here rather than scattered through the arithmetic.
var healthWeights = map[string]int{
	"missing_locale":   5,
	"empty_prompt":     5,
	"correct_index":    5,
	"duplicate_choice": 3,
	"orphan_category":  3,
	"no_explanation":   1,
	"choice_count":     5,
}

// HealthReport is what the screen shows: a score out of a hundred, the counts
// behind it, and the checks it is made of.
type HealthReport struct {
	Score                     int
	Errors, Warnings, Passing int
	Checks                    []HealthCheck
}

// BuildHealth turns the integrity issues and the duplicate sweep into the
// screen's reading.
//
// The score is a weighted share of questions without a problem, not a count of
// problems: ten faults in a bank of two thousand and ten in a bank of twenty
// are the same number and not the same bank.
func BuildHealth(issues []*models.IntegrityIssue, duplicates, questions int) HealthReport {
	byKind := map[string]int{}
	for _, issue := range issues {
		byKind[issue.Kind]++
	}

	// Weighted faults against the worst the bank could be. A bank with no
	// questions scores nothing rather than dividing by none.
	penalty, worst := 0, 0
	for kind, weight := range healthWeights {
		penalty += byKind[kind] * weight
		worst += questions * weight
	}
	score := 0
	if worst > 0 {
		score = 100 - (penalty * 100 / worst)
	}
	if score < 0 {
		score = 0
	}

	report := HealthReport{Score: score}
	for _, spec := range []struct {
		kind, key, detail, action, actionKey string
		severe                               bool
	}{
		{"missing_locale", "admin.health.missingLocale", "admin.health.missingLocaleDetail",
			"/admin/review", "admin.health.fixNow", true},
		{"empty_prompt", "admin.health.emptyPrompt", "admin.health.emptyPromptDetail",
			"/admin/questions", "admin.health.review", true},
		{"correct_index", "admin.health.correctIndex", "admin.health.correctIndexDetail",
			"/admin/questions", "admin.health.review", true},
		{"duplicate_choice", "admin.health.duplicateChoice", "admin.health.duplicateChoiceDetail",
			"/admin/questions", "admin.health.review", true},
		{"orphan_category", "admin.health.orphanCategory", "admin.health.orphanCategoryDetail",
			"/admin/categories", "admin.health.review", false},
		{"no_explanation", "admin.health.noExplanation", "admin.health.noExplanationDetail",
			"/admin/questions", "admin.health.review", false},
	} {
		failing := byKind[spec.kind]
		check := HealthCheck{
			Key: spec.key, DetailKey: spec.detail, Failing: failing,
			Share: shareOf(questions-failing, questions),
		}
		switch {
		case failing == 0:
			check.Level = "ok"
		case spec.severe:
			check.Level = "err"
			check.Action, check.ActionKey = spec.action, spec.actionKey
		default:
			check.Level = "warn"
			check.Action, check.ActionKey = spec.action, spec.actionKey
		}
		report.Checks = append(report.Checks, check)
	}

	// The duplicate sweep is its own check: it is not a fault in one question
	// but a relationship between two, so it is counted in pairs.
	dup := HealthCheck{
		Key: "admin.health.duplicates", DetailKey: "admin.health.duplicatesDetail",
		Failing: duplicates, Share: shareOf(questions-duplicates*2, questions),
	}
	if duplicates == 0 {
		dup.Level = "ok"
	} else {
		dup.Level = "warn"
		dup.Action, dup.ActionKey = "/admin/integrity#duplicates", "admin.health.comparePairs"
	}
	report.Checks = append(report.Checks, dup)

	for _, check := range report.Checks {
		switch check.Level {
		case "err":
			report.Errors++
		case "warn":
			report.Warnings++
		default:
			report.Passing++
		}
	}
	return report
}

// shareOf is a proportion as a percentage, floored at zero and ceilinged at a
// hundred — a check can report more failing rows than there are questions when
// one question fails it in two languages.
func shareOf(good, total int) int {
	if total <= 0 {
		return 100
	}
	pct := good * 100 / total
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// healthVerdictKey is the sentence beside the score. Three bands rather than a
// number restated: "86 out of 100" is already on the ring, and what a reader
// wants next is whether that is fine.
func healthVerdictKey(h HealthReport) string {
	switch {
	case h.Errors > 0:
		return "admin.health.verdict.errors"
	case h.Warnings > 0:
		return "admin.health.verdict.warnings"
	default:
		return "admin.health.verdict.clean"
	}
}

func healthSeverity(level string) string {
	switch level {
	case "err":
		return "err"
	case "warn":
		return "warn"
	default:
		return "ok"
	}
}

func healthIcon(level string) string {
	switch level {
	case "err":
		return "i-x-circle"
	case "warn":
		return "i-alert"
	default:
		return "i-check-circle"
	}
}

// localeList names the languages the bank ships in, in their own languages.
func localeList(c Ctx) string {
	out := ""
	for i, loc := range c.Locales() {
		if i > 0 {
			out += ", "
		}
		out += loc.Name
	}
	return out
}

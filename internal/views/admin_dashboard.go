package views

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
)

// The dashboard's small decisions: how a change is described, how a day is
// labelled, and how one audit line is turned into a sentence.

// Delta is one figure's movement against the period before it.
//
// Known is separate from a zero change because the two mean different things:
// nothing happened last week and nothing changed since last week are not the
// same report, and a screen that draws them identically is lying about one of
// them.
type Delta struct {
	Known bool
	Up    bool
	Flat  bool
	Label string
}

// Class is the colour: green for up, red for down, neither for flat. Note that
// "up" is not always good — this is the arrow, and the reader knows whether
// more of the thing they are looking at is welcome.
func (d Delta) Class() string {
	switch {
	case d.Flat:
		return ""
	case d.Up:
		return "up"
	default:
		return "down"
	}
}

func (d Delta) Icon() string {
	switch {
	case d.Flat:
		return "i-sort"
	case d.Up:
		return "i-trend-up"
	default:
		return "i-trend-down"
	}
}

// delta describes a count against its previous value, as a percentage.
//
// A prior of zero has no percentage — everything is an infinite rise from
// nothing — so it reports unknown rather than a number that cannot be read.
func delta(now, prior int) Delta {
	if prior <= 0 {
		return Delta{}
	}
	if now == prior {
		return Delta{Known: true, Flat: true, Label: "0%"}
	}
	change := float64(now-prior) / float64(prior) * 100
	up := change > 0
	if change < 0 {
		change = -change
	}
	return Delta{Known: true, Up: up, Label: fmt.Sprintf("%.1f%%", change)}
}

// accuracyDelta is the same idea in points rather than per cent.
//
// Accuracy is already a percentage, and "accuracy is up 4%" is ambiguous
// between four points and four per cent of sixty-seven. Points is the honest
// unit and the one the design's label uses.
func accuracyDelta(d *models.DashboardInsight) Delta {
	if d.AnswersPriorWeek <= 0 || d.AnswersThisWeek <= 0 {
		return Delta{}
	}
	points := d.Accuracy() - d.PriorAccuracy()
	if points == 0 {
		return Delta{Known: true, Flat: true, Label: "0"}
	}
	up := points > 0
	if points < 0 {
		points = -points
	}
	return Delta{Known: true, Up: up, Label: fmt.Sprintf("%d pts", points)}
}

// dashDayLabel is a bar's caption: the day of the month, and "today" for the
// last one.
//
// The number alone, not the date. Fourteen full dates in a row is a wall of
// text where a sequence was wanted, and a month abbreviation written here
// would be English on an Arabic screen — the catalogue has no month names and
// inventing them for a chart caption is the wrong place to start. The whole
// localised date is on each bar's title and in the chart's description, which
// is where a reader who needs it goes.
func dashDayLabel(c Ctx, day time.Time, today bool) string {
	if today {
		return c.T("admin.dash.today")
	}
	return fmt.Sprint(day.Day())
}

// dashChartSummary is what the chart says to a reader who cannot see it. A
// row of bars is an image, and the useful description of it is the shape:
// where it started, where it ended, and its highest day.
func dashChartSummary(c Ctx, d *models.DashboardInsight) string {
	if len(d.Days) == 0 {
		return c.T("admin.dash.noGames")
	}
	return c.T("admin.dash.chartSummary",
		len(d.Days), d.Days[0].Games, d.Days[len(d.Days)-1].Games, d.PeakGames())
}

// catColour passes a category's own colour into the tile.
//
// The one place a colour is written into markup rather than taken from a
// token, because it is the category's own property and the admin chose it. An
// empty or malformed value is dropped rather than written, so a bad row cannot
// put arbitrary text into a style attribute.
func catColour(hex string) string {
	if !validHexColour(hex) {
		return ""
	}
	return "--c:" + hex
}

// validHexColour accepts exactly #rgb and #rrggbb. Anything else — a name, a
// function, a semicolon — is refused.
func validHexColour(s string) bool {
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// languageBar is the colour of one language's share bar. Three tokens cycled,
// so the bars are distinguishable from each other without the language being
// assigned a meaning by its colour.
func languageBar(i int) string {
	tokens := []string{"var(--brand)", "var(--info)", "var(--lamp)"}
	return tokens[i%len(tokens)]
}

// localeName is a language's name in its own language, which is how a reader
// recognises their own.
func localeName(c Ctx, code string) string {
	for _, loc := range i18n.Supported {
		if loc.Code == code {
			return loc.Name
		}
	}
	return code
}

// auditPhrase turns one recorded action into a sentence a reader understands.
//
// Composed from the verb and the thing it acted on rather than from a key per
// action. Every action is already "<kind>.<verb>" — category.retire,
// translation.approve, support.triage — so thirty actions need twenty verbs
// and eight kinds between them, and the next action added to the codebase gets
// a readable line without a catalogue change as long as its verb is one of
// these.
//
// A verb nobody has named falls back to the raw action string. That is
// deliberate: a technical name in the trail is readable and tells whoever
// added the action which key to write, where a blank cell or a bare
// "admin.audit.verb.frobnicate" tells the reader nothing at all.
func auditPhrase(c Ctx, e *models.AuditEntry) string {
	verb, kind := auditParts(e.Action)

	verbKey := "admin.audit.verb." + verb
	phrase := c.T(verbKey)
	if phrase == verbKey {
		return e.Action
	}

	kindKey := "admin.audit.kind." + kind
	if noun := c.T(kindKey); noun != kindKey {
		return c.T("admin.audit.phrase", phrase, noun)
	}
	return phrase
}

// auditParts splits "<kind>.<verb>" into its halves.
//
// The verb is everything after the first dot, so "question.bulk.activate" is
// the verb "bulk.activate" rather than "bulk" — those are different actions
// and they read differently.
func auditParts(action string) (verb, kind string) {
	if i := strings.Index(action, "."); i > 0 {
		return action[i+1:], action[:i]
	}
	return action, ""
}

// auditTone and auditIcon give one trail line its dot. Grouped by what the
// action did rather than by what it acted on: a reader scanning the timeline
// is looking for the destructive ones.
func auditTone(e *models.AuditEntry) string {
	switch {
	case strings.HasSuffix(e.Action, ".delete"), strings.Contains(e.Action, "suspend"),
		strings.HasSuffix(e.Action, ".reject"):
		return "c-danger"
	case strings.HasSuffix(e.Action, ".approve"), strings.HasSuffix(e.Action, ".create"),
		strings.HasSuffix(e.Action, ".restore"):
		return "c-brand"
	case strings.HasSuffix(e.Action, ".retire"), strings.HasSuffix(e.Action, ".changes"):
		return "c-lamp"
	default:
		return "c-info"
	}
}

func auditIcon(e *models.AuditEntry) string {
	switch {
	case strings.HasSuffix(e.Action, ".delete"):
		return "i-trash"
	case strings.HasSuffix(e.Action, ".approve"):
		return "i-check"
	case strings.HasSuffix(e.Action, ".reject"):
		return "i-x-circle"
	case strings.HasSuffix(e.Action, ".retire"):
		return "i-archive"
	case strings.HasSuffix(e.Action, ".restore"):
		return "i-restore"
	case strings.Contains(e.Action, "suspend"):
		return "i-ban"
	case strings.HasPrefix(e.Action, "support."):
		return "i-reply"
	case strings.HasPrefix(e.Action, "translation."):
		return "i-languages"
	case strings.HasSuffix(e.Action, ".import"):
		return "i-upload"
	case strings.HasSuffix(e.Action, ".export"):
		return "i-download"
	case strings.HasSuffix(e.Action, ".reorder"):
		return "i-sort"
	default:
		return "i-edit"
	}
}

// ---------------------------------------------------------------- audit --

// auditWindowTab is one of the trail's time filters, named by the value the
// handler's auditWindows map understands. The two have to agree, so the tabs
// are written from the same strings rather than from labels.
type auditWindowTab struct {
	Value    string
	LabelKey string
}

var auditWindowTabs = []auditWindowTab{
	{Value: "", LabelKey: "admin.audit.allTime"},
	{Value: "24h", LabelKey: "admin.audit.window24h"},
	{Value: "7d", LabelKey: "admin.audit.window7d"},
	{Value: "30d", LabelKey: "admin.audit.window30d"},
}

// FilterQuery is the trail's current filter as a query string, so the export
// downloads what is on screen.
func (d AdminAuditData) FilterQuery() string {
	v := url.Values{}
	for name, value := range map[string]string{
		"q": d.Query, "actor": d.Actor, "area": d.Area, "window": d.Window,
	} {
		if value != "" {
			v.Set(name, value)
		}
	}
	return v.Encode()
}

// auditDayGroup is one calendar day of the trail.
type auditDayGroup struct {
	Label   string
	Entries []*models.AuditEntry
}

// auditByDay splits the page into day groups, keeping the order the query
// returned.
//
// Grouped in the view rather than in SQL because the grouping is a reading
// aid, not a fact about the data: the same rows read as one list for a machine
// and as "today, yesterday, Tuesday" for a person.
func auditByDay(c Ctx, entries []*models.AuditEntry) []auditDayGroup {
	var out []auditDayGroup
	for _, e := range entries {
		label := auditDayLabel(c, e.CreatedAt)
		if len(out) == 0 || out[len(out)-1].Label != label {
			out = append(out, auditDayGroup{Label: label})
		}
		out[len(out)-1].Entries = append(out[len(out)-1].Entries, e)
	}
	return out
}

// auditDayLabel names a day: "today" and "yesterday" by those names, anything
// older by its date. Relative names are what a reader actually holds in their
// head about the last two days and nothing further back.
func auditDayLabel(c Ctx, t time.Time) string {
	today := time.Now().Truncate(24 * time.Hour)
	day := t.Truncate(24 * time.Hour)
	switch {
	case day.Equal(today):
		return c.T("admin.audit.today")
	case day.Equal(today.AddDate(0, 0, -1)):
		return c.T("admin.audit.yesterday")
	default:
		return c.Tr.Date(t)
	}
}

// auditClock is the time of day one entry happened, which is all the time
// column needs once the rows are grouped by date.
func auditClock(t time.Time) string {
	return t.Local().Format("15:04")
}

// auditFieldName is a changed field's label. Falls back to the stored key,
// which is a readable word already — the fields are named "status", "role",
// "state" — so an unlabelled one is informative rather than blank.
func auditFieldName(c Ctx, field string) string {
	key := "admin.audit.field." + field
	if label := c.T(key); label != key {
		return label
	}
	return field
}

// ---------------------------------------------------------------- review --

// translatorCalloutClass styles the banner that says whether a provider is
// configured. Not an alarm either way: with no provider the queue still works,
// a human simply writes the translation instead of correcting a machine's.
func translatorCalloutClass(available bool) string {
	if available {
		return ""
	}
	return "warn"
}

// reviewNoteWho attributes a request. An account that has since been deleted
// leaves the note and no name, and the note is still the useful half.
func reviewNoteWho(c Ctx, note *models.ReviewNote) string {
	when := ""
	if note.NotedAt != nil {
		when = c.Tr.RelativeTime(*note.NotedAt)
	}
	switch {
	case note.NotedBy != "" && when != "":
		return c.T("admin.review.notedBy", note.NotedBy, when)
	case note.NotedBy != "":
		return note.NotedBy
	default:
		return when
	}
}

// ratingShare is a star average as a percentage of the scale, which is what
// fills the rated-poorly bar.
//
// The design's bar is a thumbs-up/down split and this bank is rated one to
// five, so there is no up-or-down to draw. The average against the top of the
// scale is the same quantity the alert threshold is set against, which keeps
// the bar and the reason the row is on the screen in agreement.
func ratingShare(average float64) int {
	pct := int(average / float64(models.MaxStars) * 100)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// integrityBadge colours one content problem by how wrong it is.
//
// The two groups are different urgencies, not different severities of the same
// thing. A missing language or a missing explanation is work not done yet: the
// question is correct as far as it goes and a player simply gets the fallback.
// A duplicated choice, an empty prompt or a correct_index pointing past the
// end of the list is a question that is broken for whoever draws it — the kinds
// are the ones IntegrityIssues reports, so the split is over that list and not
// over a guess.
func integrityBadge(kind string) string {
	switch kind {
	case "missing_locale", "no_explanation", "orphan_category":
		return "badge-info"
	default:
		return "badge-danger"
	}
}

// ---------------------------------------------------------------- support --

// ticketKindIcon is the sprite symbol for one topic.
//
// The player's side of support keeps the emoji — a topic picker is friendlier
// with them, and that screen is not part of this design. The console uses the
// stroke icons the rest of its chrome is drawn with, so a chip row sits level
// with the buttons beside it instead of being a row of mismatched glyphs.
func ticketKindIcon(kind string) string {
	switch kind {
	case models.TicketSuggestion:
		return "i-lightbulb"
	case models.TicketQuestion:
		return "i-question"
	case models.TicketBug:
		return "i-bug"
	case models.TicketAccount:
		return "i-key"
	case models.TicketAbuse:
		return "i-flag"
	default:
		return "i-comment"
	}
}

// ticketStatusBadge and ticketPriorityBadge map a ticket's two states onto the
// kit's badges. Open is the live state here: a ticket that is open is one the
// queue is still carrying, which is what the green dot means everywhere else
// in this console.
func ticketStatusBadge(status string) string {
	switch status {
	case models.TicketOpen:
		return "badge-live"
	case models.TicketInProgress, models.TicketWaitingUser:
		return "badge-pending"
	case models.TicketResolved:
		return "badge-info"
	default:
		return "badge-retired"
	}
}

func ticketPriorityBadge(priority string) string {
	switch priority {
	case models.PriorityUrgent, models.PriorityHigh:
		return "badge-danger"
	case models.PriorityLow:
		return "badge-info"
	default:
		return "badge-lamp"
	}
}

// describeDevice reduces a browser string to the one line staff need.
//
// Deliberately coarse. A full user-agent is forty characters of version
// numbers around two useful facts — roughly what kind of machine, and roughly
// which browser — and the answer to "it does not work on my phone" is the
// first of those. The raw string is kept beside this in the markup for anybody
// who needs the rest, so being approximate here costs nothing.
//
// Order matters in both passes: Edge's string contains "Chrome", Chrome's
// contains "Safari", and iPad's contains "Macintosh" in desktop mode — so the
// more specific name has to be tested first or every browser reads as Safari.
func describeDevice(agent string) string {
	platform := "" // unknown rather than guessed
	for _, probe := range []struct{ needle, name string }{
		{"iPhone", "iPhone"},
		{"iPad", "iPad"},
		{"Android", "Android"},
		{"Windows", "Windows"},
		{"Macintosh", "Mac"},
		{"CrOS", "ChromeOS"},
		{"Linux", "Linux"},
	} {
		if strings.Contains(agent, probe.needle) {
			platform = probe.name
			break
		}
	}

	browser := ""
	for _, probe := range []struct{ needle, name string }{
		{"Edg/", "Edge"},
		{"OPR/", "Opera"},
		{"SamsungBrowser", "Samsung Internet"},
		{"Firefox", "Firefox"},
		{"Chrome", "Chrome"},
		{"Safari", "Safari"},
	} {
		if strings.Contains(agent, probe.needle) {
			browser = probe.name
			break
		}
	}

	switch {
	case platform != "" && browser != "":
		return browser + " · " + platform
	case browser != "":
		return browser
	case platform != "":
		return platform
	default:
		// Something this does not recognise. Saying so is better than
		// printing a guess, and the raw string is right underneath.
		return ""
	}
}

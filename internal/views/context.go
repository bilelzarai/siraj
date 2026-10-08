package views

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/service"
)

// Ctx is the per-request bundle every template receives. It carries just
// enough to render the chrome without another database round trip.
type Ctx struct {
	Tr   *i18n.Printer
	User *models.User
	// Acting is the player the next move belongs to, which is the signed-in
	// user unless a device is being shared and somebody else is holding it.
	//
	// Every screen that asks "is it my turn" has to ask it about this rather
	// than about User, or a match played on one phone shows the host's turn to
	// whoever is sitting there.
	Acting *models.User
	CSRF   string
	Path   string
	Locale string
	Dir    string
	Theme  string
	AssetV string
	// Assets resolves a bundler entry to the URL the page links. Declared as
	// an interface here rather than importing the build package, so the view
	// layer keeps depending on nothing but models and i18n.
	Assets AssetLinks

	UnreadMessages    int
	PendingChallenges int
	FriendRequests    int
	SupportUnread     int
	Notifications     int

	Flash *Flash
}

type Flash struct {
	Kind string // "success" | "error" | "info" | "warning"
	Text string
}

func (c Ctx) IsAuthed() bool { return c.User != nil }

// IsGuest reports a player with no account behind them, for the screens that
// offer an account rather than pretending everything is available.
func (c Ctx) IsGuest() bool { return c.User != nil && c.User.IsGuest() }

// Seated is the id this device should be asked about for one match.
//
// The rule itself is models.Challenge.WhoActs, and this is only the view's way
// of reaching it: the handler that answers a card's buttons calls the same
// function on the same match, so the two cannot disagree about who pressed it.
func (c Ctx) Seated(ch matchView) uuid.UUID {
	seat, account := uuid.Nil, uuid.Nil
	if c.Acting != nil {
		seat = c.Acting.ID
	}
	if c.User != nil {
		account = c.User.ID
	}
	if ch == nil {
		return account
	}
	return ch.WhoActs(seat, account)
}

// matchView is the little a Ctx needs to know about a match to answer that.
type matchView interface {
	WhoActs(seat, account uuid.UUID) uuid.UUID
}

func (c Ctx) IsRTL() bool { return c.Dir == "rtl" }

// Active reports whether a nav destination matches the current path.
func (c Ctx) Active(prefix string) bool {
	if prefix == "/" {
		return c.Path == "/" || c.Path == "/app"
	}
	return strings.HasPrefix(c.Path, prefix)
}

func (c Ctx) AriaCurrent(prefix string) string {
	if c.Active(prefix) {
		return "page"
	}
	return "false"
}

// AssetLinks answers where a built asset lives. The dev server and the build
// both satisfy it; a tree with neither answers "not found", and the page then
// renders without that asset rather than refusing to answer at all.
type AssetLinks interface {
	Script(entry string) (string, bool)
	Stylesheets(entry string) []string
	Copied(name string) string
	DevClient() string
}

// Script is the URL of a bundled entry — "js/app.js" — or false when nothing
// built it.
func (c Ctx) Script(entry string) (string, bool) {
	if c.Assets == nil {
		return "", false
	}
	return c.Assets.Script(entry)
}

// Stylesheets are the sheets belonging to an entry. Empty is a legitimate
// answer: the dev server delivers styles through the module graph.
func (c Ctx) Stylesheets(entry string) []string {
	if c.Assets == nil {
		return nil
	}
	return c.Assets.Stylesheets(entry)
}

// DevClient is the dev server's own module, the one that performs the
// replacement when a source file changes. Empty on every deployment.
func (c Ctx) DevClient() string {
	if c.Assets == nil {
		return ""
	}
	return c.Assets.DevClient()
}

// Copied is a file the bundler copies verbatim rather than naming — boot.js,
// which has to stay a classic blocking script.
func (c Ctx) Copied(name string) string {
	if c.Assets == nil {
		return ""
	}
	return c.Assets.Copied(name)
}

// Asset appends the build's content hash to a static URL, so a stylesheet or
// script change is picked up immediately instead of being served from cache.
func (c Ctx) Asset(path string) string {
	if c.AssetV == "" {
		return path
	}
	return path + "?v=" + c.AssetV
}

// ThemeOrder is the cycle the appearance button walks. The button always
// announces the theme it will switch *to*, so pressing it is predictable.
var ThemeOrder = []string{"system", "light", "dark"}

// NextTheme returns the setting one press from the current one.
func NextTheme(current string) string {
	if current == "" {
		current = "system"
	}
	for i, t := range ThemeOrder {
		if t == current {
			return ThemeOrder[(i+1)%len(ThemeOrder)]
		}
	}
	return ThemeOrder[0]
}

// CurrentTheme normalises the stored value for display.
func (c Ctx) CurrentTheme() string {
	if c.Theme == "" {
		return "system"
	}
	return c.Theme
}

// ThemeKey maps a setting to its translation key.
func ThemeKey(theme string) string { return "settings.theme." + theme }

// T looks up a translation. It is a method rather than a bare printer field so
// templates read c.T("nav.home") instead of c.T("nav.home"); the printer
// itself stays reachable as c.Tr for dates and relative times.
func (c Ctx) T(key string, args ...any) string { return c.Tr.T(key, args...) }

// ThemeLabel is what the appearance button says while `theme` is the one in
// force. It names the applied theme first — the question the icon alone left
// open — and then the theme the next press gives you.
func ThemeLabel(c Ctx, theme string) string {
	return c.T("settings.theme.current", c.T(ThemeKey(theme))) + " · " +
		c.T("settings.theme.switchTo", c.T(ThemeKey(NextTheme(theme))))
}

// Locales exposes the switcher list to templates.
func (c Ctx) Locales() []i18n.Locale { return i18n.Supported }

// NewRequestKey stamps a form so that submitting it twice creates one thing.
//
// A fresh key per rendering: reloading the page is a genuine second attempt and
// must not be mistaken for a repeat of the first, while a resent POST carries
// the key it was rendered with and is recognised as the repeat it is.
func NewRequestKey() string { return uuid.NewString() }

// ---------------------------------------------------------------- avatars --

// avatarPalette holds gradient pairs; a stable hash of the seed picks one so
// a person keeps the same colours everywhere they appear.
var avatarPalette = [][2]string{
	{"#0ea5a4", "#065f46"},
	{"#8b5cf6", "#5b21b6"},
	{"#f59e0b", "#b45309"},
	{"#3b82f6", "#1e40af"},
	{"#ec4899", "#9d174d"},
	{"#14b8a6", "#0f766e"},
	{"#6366f1", "#3730a3"},
	{"#22c55e", "#15803d"},
	{"#ef4444", "#991b1b"},
	{"#06b6d4", "#155e75"},
}

// AvatarStyle produces the inline custom properties the .avatar class reads.
// AvatarStyle turns a seed into what to paint.
//
// The seed is a tagged value, not only a hash input: "photo:<id>" means the
// person has uploaded a picture and the gradient is just what sits behind it
// while that loads. Overloading this one column is what keeps a profile photo
// from having to be threaded through twenty-one queries that already select
// avatar_seed and nothing more.
func AvatarStyle(seed string) string {
	pair := avatarPalette[hashIndex(seed, len(avatarPalette))]
	style := fmt.Sprintf("--a1:%s;--a2:%s", pair[0], pair[1])
	if id, ok := AvatarPhoto(seed); ok {
		// Unquoted on purpose: this string goes into a style attribute that
		// templ escapes, and the quotes came back out the other side as
		// &#39; — valid HTML and invalid CSS. A uuid path needs none.
		style += fmt.Sprintf(";--photo:url(/files/%s)", id)
	}
	return style
}

// AvatarPhotoPrefix marks a seed that names an uploaded picture.
const AvatarPhotoPrefix = "photo:"

// AvatarPhoto reads the attachment id out of a seed, if there is one.
func AvatarPhoto(seed string) (string, bool) {
	if !strings.HasPrefix(seed, AvatarPhotoPrefix) {
		return "", false
	}
	id := strings.TrimPrefix(seed, AvatarPhotoPrefix)
	if _, err := uuid.Parse(id); err != nil {
		return "", false
	}
	return id, true
}

// HasAvatarPhoto is the same question asked from a template.
func HasAvatarPhoto(seed string) bool {
	_, ok := AvatarPhoto(seed)
	return ok
}

func hashIndex(seed string, n int) int {
	if seed == "" {
		seed = "default"
	}
	sum := sha256.Sum256([]byte(seed))
	return int(sum[0]) % n
}

// AvatarSeedFor falls back to the username when no seed was stored.
func AvatarSeedFor(id uuid.UUID, seed, username string) string {
	if seed != "" {
		return seed
	}
	if username != "" {
		return username
	}
	return id.String()
}

// ------------------------------------------------------------------ misc --

// Initial returns the single display letter for an avatar.
func Initial(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return "?"
	}
	return strings.ToUpper(string(runes[0]))
}

// AnswerKey labels a choice A–D, or ا–د in Arabic.
func AnswerKey(locale string, i int) string {
	if locale == "ar" {
		keys := []string{"أ", "ب", "ج", "د"}
		if i >= 0 && i < len(keys) {
			return keys[i]
		}
		return "?"
	}
	if i < 0 || i > 25 {
		return "?"
	}
	return string(rune('A' + i))
}

// Percent guards against divide-by-zero in templates.
func Percent(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 100 / total
}

// BarHeight scales a value to a 0–100 percentage for the activity sparkline.
func BarHeight(value, max int) int {
	if max <= 0 || value <= 0 {
		return 3
	}
	h := value * 100 / max
	if h < 6 {
		return 6
	}
	if h > 100 {
		return 100
	}
	return h
}

// DayLabel renders the weekday initial for the activity chart.
func DayLabel(locale string, t time.Time) string {
	switch locale {
	case "ar":
		return [...]string{"ح", "ن", "ث", "ر", "خ", "ج", "س"}[int(t.Weekday())]
	case "fr":
		return [...]string{"D", "L", "M", "M", "J", "V", "S"}[int(t.Weekday())]
	default:
		return [...]string{"S", "M", "T", "W", "T", "F", "S"}[int(t.Weekday())]
	}
}

// MissingLocales names the shipped languages a piece of content does not have
// yet, given the ones it does. It wraps the service-level helper so the review
// screen and any future content tooling answer the question the same way.
func MissingLocales(present []string) []string {
	have := make(map[string]bool, len(present))
	for _, code := range present {
		have[code] = true
	}
	return service.MissingLocales(have)
}

// ContentDir returns the writing direction of stored content, which is not
// always the page direction: a round keeps the language it started in, so an
// English question can be shown inside an Arabic interface. Marking the
// content's own direction stops the bidi algorithm from reordering it.
func ContentDir(locale string) string { return i18n.DirOf(locale) }

// ModeKey maps a stored game mode to its translation key.
func ModeKey(mode string) string { return "history.mode." + mode }

// DifficultyKey maps 0–3 to its translation key.
func DifficultyKey(d int) string {
	if d < 1 || d > 3 {
		return "game.difficulty.any"
	}
	return fmt.Sprintf("game.difficulty.%d", d)
}

// RankClass styles the top three leaderboard positions.
func RankClass(rank int) string {
	if rank >= 1 && rank <= 3 {
		return fmt.Sprintf("lb-rank lb-rank--%d", rank)
	}
	return "lb-rank"
}

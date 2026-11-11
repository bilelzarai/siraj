// Package view carries everything a page needs from the request into the templates:
// the translator, the asset URLs, the viewer, their domains and the domain in focus.
//
// Templates never reach for the database or the Gin context: a handler loads what the
// screen needs, puts it in Ctx (middleware.Shell does the shared part) and renders.
package view

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/platform/authctx"
)

// Domain is a domain as the shell and the cards need it (BR-02, BR-03).
type Domain struct {
	ID            int64
	Slug          string
	Name          string
	Description   string
	Family        string // islamic | general
	Icon          string
	Color         string
	Motif         string
	IsFeatured    bool
	IsNew         bool
	IsMine        bool
	CategoryCount int32
	QuestionCount int32
	Mastery       int32
}

func (d Domain) IsIslamic() bool { return d.Family == "islamic" }

// Counts are the badges on the navigation (CH-11, MS-01).
type Counts struct {
	Challenges int
	Messages   int
	Notifs     int
}

// Ctx is the per-request view model shared by every page.
type Ctx struct {
	T      *i18n.Printer
	Assets *render.Assets
	Lang   string
	Dir    string
	Theme  string // light | dark | system
	CSRF   string
	Path   string
	// Back is where a control that reloads the whole page (the language menu) must
	// return to. It defaults to the path; a screen that holds a position of its own —
	// a running game — sets it so the reload lands on the very same question (LG-01).
	Back string
	Dev  bool

	User        *authctx.Principal // nil = signed-out visitor
	DisplayName string
	Username    string
	Level       int32
	XP          int32
	XPNext      int32
	IslamicOnly bool

	MyDomains []Domain
	Focus     *Domain // the domain in focus re-colours the screen (BR-02)
	Counts    Counts

	// Flash is a one-off message shown at the top of the page after a redirect.
	Flash string
}

const key = "siraj.view"

func Set(c *gin.Context, v *Ctx) { c.Set(key, v) }

// From returns the request's view context. It is always present on web routes
// (middleware.Shell), so pages can use it without checking.
func From(c *gin.Context) *Ctx {
	if v, ok := c.Get(key); ok {
		if ctx, ok := v.(*Ctx); ok {
			return ctx
		}
	}
	return &Ctx{}
}

// Clone copies the context so a handler can set the domain in focus for one page only.
func (v *Ctx) Clone() *Ctx {
	cp := *v
	return &cp
}

// WithFocus returns a copy of the context with the domain in focus set.
func (v *Ctx) WithFocus(d *Domain) *Ctx {
	cp := v.Clone()
	cp.Focus = d
	return cp
}

func (v *Ctx) SignedIn() bool { return v.User != nil }
func (v *Ctx) IsGuest() bool  { return v.User != nil && v.User.Kind == "guest" }
func (v *Ctx) IsMember() bool { return v.User != nil && v.User.Kind == "member" }
func (v *Ctx) IsStaff() bool  { return v.User.IsStaff() }
func (v *Ctx) IsRTL() bool    { return v.Lang == "ar" }

// Tr is the translator, shortened for templates: ctx.Tr("nav.home").
func (v *Ctx) Tr(key string, args ...any) string {
	if v.T == nil {
		return key
	}
	return v.T.T(key, args...)
}

// Has answers whether a key is translated at all. The audit log needs it: an action
// and an area are free text written by whatever recorded them, so the screen shows the
// words when it knows them and the recorded value when it does not, rather than
// printing a raw key at a reader.
func (v *Ctx) Has(key string) bool {
	if v.T == nil {
		return false
	}
	return v.T.Has(key)
}

// Name is what the top bar shows for the viewer.
func (v *Ctx) Name() string {
	if v.DisplayName != "" {
		return v.DisplayName
	}
	return v.Tr("guest.badge")
}

// Initials for the avatar circle.
func (v *Ctx) Initials() string {
	name := v.Name()
	out := []rune{}
	prevSpace := true
	for _, r := range name {
		if r == ' ' {
			prevSpace = true
			continue
		}
		if prevSpace {
			out = append(out, r)
			prevSpace = false
		}
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

// XPPercent fills the ring around the avatar.
func (v *Ctx) XPPercent() int {
	if v.XPNext <= 0 {
		return 0
	}
	p := int(float64(v.XP) / float64(v.XPNext) * 100)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}

// LockedForGuest lists the sections a guest cannot open (RL-03).
var LockedForGuest = map[string]string{
	"friends":       "guest.locked.what.friends",
	"profile":       "guest.locked.what.profile",
	"achievements":  "guest.locked.what.achievements",
	"notifications": "guest.locked.what.notifications",
	"history":       "guest.locked.what.history",
	"my-questions":  "guest.locked.what.my_questions",
}

// Locked reports whether this viewer may not open the section.
func (v *Ctx) Locked(section string) bool {
	if !v.IsGuest() {
		return false
	}
	_, locked := LockedForGuest[section]
	return locked
}

// NavItem is one entry of the top navigation and the phone tab bar (NF-01).
type NavItem struct {
	Section string // also the route: /home, /play, …
	Icon    string
	Label   string // i18n key
	Tab     bool   // shown in the phone tab bar
}

// Nav is the main navigation, in order.
func Nav() []NavItem {
	return []NavItem{
		{"home", "home", "nav.home", true},
		{"play", "play", "nav.play", true},
		{"challenges", "swords", "nav.challenges", true},
		{"messages", "chat", "nav.messages", true},
		{"friends", "users", "nav.friends", false},
		{"leaderboard", "trophy", "nav.leaderboard", true},
	}
}

// activeFor maps a screen to the navigation entry that stays highlighted.
var activeFor = map[string]string{
	"setup": "play", "results": "play", "room-create": "play", "room-join": "play",
	"lobby": "play", "podium": "play", "duel": "play", "domain": "play", "game": "play",
	"challenge-new": "challenges", "challenge": "challenges",
}

// BackTo is the address a language or theme change returns to.
func (v *Ctx) BackTo() string {
	if v.Back != "" {
		return v.Back
	}
	return v.Path
}

// Section is the current screen's section ("home", "play", …), taken from the path.
func (v *Ctx) Section() string {
	s := v.Path
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			s = s[:i]
			break
		}
	}
	if s == "" {
		return "home"
	}
	return s
}

// ActiveNav is the navigation entry to highlight for the current screen.
func (v *Ctx) ActiveNav() string {
	s := v.Section()
	if a, ok := activeFor[s]; ok {
		return a
	}
	return s
}

// Count is the badge of a navigation entry (0 = no badge).
func (v *Ctx) Count(section string) int {
	if !v.SignedIn() || v.Locked(section) {
		return 0
	}
	switch section {
	case "challenges":
		return v.Counts.Challenges
	case "messages":
		return v.Counts.Messages
	}
	return 0
}

// FocusStyle is the inline style that re-colours the screen for the domain in focus.
// It is the one place a colour reaches the markup, and it comes from domains.color (BR-02).
func (v *Ctx) FocusStyle() string {
	if v.Focus == nil {
		return ""
	}
	return "--d:" + v.Focus.Color
}

// FocusFamily drives the Islamic geometric pattern (BR-03).
func (v *Ctx) FocusFamily() string {
	if v.Focus == nil {
		return ""
	}
	return v.Focus.Family
}

// Greeting switches with Islamic-only mode (DM-09).
func (v *Ctx) Greeting() string {
	if v.IslamicOnly {
		return v.Tr("home.greeting.islamic")
	}
	if v.IsGuest() {
		// A guest is greeted by the first word of their name, so "Guest 4821"
		// reads as "Welcome back, Guest" (RL-05).
		return v.Tr("home.greeting.guest", "name", firstWord(v.Name()))
	}
	return v.Tr("home.greeting.member", "name", v.Name())
}

// WeeklyReset copy also switches with Islamic-only mode (DM-09).
func (v *Ctx) WeeklyReset() string {
	if v.IslamicOnly {
		return v.Tr("home.reset.islamic")
	}
	return v.Tr("home.reset.general")
}

// HasIslamicDomain is true when one of the player's domains is in the Islamic family;
// it drives the Ramadan card (HM-08).
func (v *Ctx) HasIslamicDomain() bool {
	if v.IslamicOnly {
		return true
	}
	for _, d := range v.MyDomains {
		if d.IsIslamic() {
			return true
		}
	}
	return false
}

// firstWord is the greeting's short form of a name.
func firstWord(name string) string {
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}

// Num groups thousands the way the player's language writes them, so 2340 reads
// "2,340" in English and Arabic and "2 340" in French. Digits stay Latin, as the
// design does: the scores line up in the same column in all three languages.
func (v *Ctx) Num(n int64) string {
	sep := ","
	if v.Lang == "fr" {
		sep = " " // narrow no-break space
	}
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits := strconv.FormatInt(n, 10)
	if len(digits) <= 3 {
		return sign + digits
	}
	var b strings.Builder
	lead := len(digits) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(digits[:lead])
	for i := lead; i < len(digits); i += 3 {
		b.WriteString(sep)
		b.WriteString(digits[i : i+3])
	}
	return sign + b.String()
}

package handlers_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The accessibility checks that can be made from the markup alone.
//
// Not a substitute for a real audit — contrast, focus order and screen-reader
// behaviour need a browser and a person. These are the ones a page either
// passes or fails as text, which makes them the ones worth having run on every
// change: an icon button with no name, an image with no alternative, a field
// with no label, a page that does not say what language it is in.

var (
	imgTag      = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	altAttr     = regexp.MustCompile(`(?i)\balt\s*=`)
	buttonTag   = regexp.MustCompile(`(?is)<button\b[^>]*>(.*?)</button>`)
	ariaLabel   = regexp.MustCompile(`(?i)\baria-label\s*=\s*"[^"]+"`)
	ariaLabelBy = regexp.MustCompile(`(?i)\baria-labelledby\s*=\s*"[^"]+"`)
	titleAttr   = regexp.MustCompile(`(?i)\btitle\s*=\s*"[^"]+"`)
	tags        = regexp.MustCompile(`(?s)<[^>]*>`)
	inputTag    = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	typeAttr    = regexp.MustCompile(`(?i)\btype\s*=\s*"([^"]*)"`)
	idAttr      = regexp.MustCompile(`(?i)\bid\s*=\s*"([^"]+)"`)
	hasLang     = regexp.MustCompile(`(?i)<html[^>]*\blang\s*=\s*"[^"]+"`)
	hasDir      = regexp.MustCompile(`(?i)<html[^>]*\bdir\s*=\s*"(ltr|rtl)"`)
)

// everyScreen is the set a signed-in player can reach, in both directions of
// writing, so a fault that only shows under Arabic is caught too.
var everyScreen = []string{
	"/app", "/play", "/challenges", "/challenges/new", "/messages",
	"/friends", "/leaderboard", "/notifications", "/history", "/settings",
	"/support", "/support/new", "/profile/edit", "/my/questions",
}

func TestEveryScreenIsLabelledForAssistiveTech(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 980000, 12)
	a.register("a11y_user")

	// Somebody to see, so the lists are not all empty.
	mate := newAppSharing(t, a)
	mate.register("a11y_mate")
	me, err := a.repo.UserByUsername(t.Context(), "a11y_user")
	if err != nil {
		t.Fatal(err)
	}
	them, err := a.repo.UserByUsername(t.Context(), "a11y_mate")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, me.ID, them.ID)
	a.post("/players", url.Values{"name": {"Zaynab"}})

	for _, locale := range []string{"en", "ar"} {
		for _, screen := range everyScreen {
			page := screen + "?lang=" + locale
			status, body := a.get(page)
			if status != 200 {
				continue // redirects are another test's business
			}

			t.Run(locale+" "+screen, func(t *testing.T) {
				// The document says what language it is and which way it runs.
				if !hasLang.MatchString(body) {
					t.Error("the page does not declare a language")
				}
				if !hasDir.MatchString(body) {
					t.Error("the page does not declare a writing direction")
				}
				if locale == "ar" && !strings.Contains(body, `dir="rtl"`) {
					t.Error("the Arabic page is not right-to-left")
				}

				// Every picture says what it is.
				for _, img := range imgTag.FindAllString(body, -1) {
					if !altAttr.MatchString(img) {
						t.Errorf("an image carries no alt text: %s", clip(img, 90))
					}
				}

				// Every button has a name, whether from its text or an
				// attribute. An icon button with neither is a button a screen
				// reader announces as "button".
				for _, m := range buttonTag.FindAllStringSubmatch(body, -1) {
					whole, inner := m[0], m[1]
					text := strings.TrimSpace(tags.ReplaceAllString(inner, " "))
					named := text != "" ||
						ariaLabel.MatchString(whole) ||
						ariaLabelBy.MatchString(whole) ||
						titleAttr.MatchString(whole)
					if !named {
						t.Errorf("a button has no accessible name: %s", clip(whole, 110))
					}
				}

				// Every field a person types in is either labelled or
				// described. Hidden and structural inputs are not.
				for _, field := range inputTag.FindAllString(body, -1) {
					kind := "text"
					if m := typeAttr.FindStringSubmatch(field); m != nil {
						kind = strings.ToLower(m[1])
					}
					switch kind {
					case "hidden", "submit", "button", "radio", "checkbox":
						continue
					}
					// Not in the accessibility tree at all, and reached
					// through a visible control that is.
					if strings.Contains(field, " hidden") {
						continue
					}
					if ariaLabel.MatchString(field) || ariaLabelBy.MatchString(field) {
						continue
					}
					m := idAttr.FindStringSubmatch(field)
					if m != nil && strings.Contains(body, `for="`+m[1]+`"`) {
						continue
					}
					t.Errorf("a %s field has no label: %s", kind, clip(field, 110))
				}
			})
		}
	}
}

// The round itself is the screen a player spends the most time on, and it is
// built from buttons that are mostly icons.
func TestThePlayScreenIsLabelled(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 981000, 12)
	a.register("a11y_player")

	if status, _ := a.post("/play/start", url.Values{"count": {"5"}}); status != 303 {
		t.Fatal("could not start a round")
	}
	status, body := a.get("/play/round")
	if status != 200 {
		t.Fatalf("the round screen → %d", status)
	}

	for _, m := range buttonTag.FindAllStringSubmatch(body, -1) {
		whole, inner := m[0], m[1]
		text := strings.TrimSpace(tags.ReplaceAllString(inner, " "))
		if text == "" && !ariaLabel.MatchString(whole) &&
			!ariaLabelBy.MatchString(whole) && !titleAttr.MatchString(whole) {
			t.Errorf("a button on the round screen has no accessible name: %s", clip(whole, 110))
		}
	}

	// The clock and the answers are announced, not just drawn.
	for _, want := range []string{"aria-live", "role="} {
		if !strings.Contains(body, want) {
			t.Errorf("the round screen carries no %s at all", want)
		}
	}
}

// The setup form's two halves have to agree on who is who.
//
// The side-pickers find their player by matching data-team-row against a
// checkbox value. If the server ever rendered the two from different sources —
// a username here, an id there — the pickers would silently match nothing and
// the form would quietly stop offering teams. That is a fault with no symptom
// except a button that will not turn on, so it is worth a test.
func TestTheTeamRowsMatchThePlayerCheckboxes(t *testing.T) {
	a := newApp(t)
	seedQuestions(a, 983000, 12)
	a.register("rows_host")

	// Guests at the device, and a friend, so both kinds of key are rendered.
	for _, name := range []string{"Amal", "Bilqis"} {
		if status, _ := a.post("/players", url.Values{"name": {name}}); status != 303 {
			t.Fatalf("adding %s failed", name)
		}
	}
	mate := newAppSharing(t, a)
	mate.register("rows_mate")
	me, err := a.repo.UserByUsername(t.Context(), "rows_host")
	if err != nil {
		t.Fatal(err)
	}
	them, err := a.repo.UserByUsername(t.Context(), "rows_mate")
	if err != nil {
		t.Fatal(err)
	}
	befriendThrough(t, a, me.ID, them.ID)

	rowKey := regexp.MustCompile(`data-team-row="([^"]*)"`)
	boxValue := regexp.MustCompile(`<input type="checkbox" name="(?:opponent|local)" value="([^"]*)"`)

	for _, group := range []string{"device", "friends"} {
		status, body := a.get("/challenges/new?lang=en&group=" + group)
		if status != 200 {
			t.Fatalf("the setup screen for %s → %d", group, status)
		}

		boxes := map[string]bool{}
		for _, m := range boxValue.FindAllStringSubmatch(body, -1) {
			boxes[m[1]] = true
		}
		rows := rowKey.FindAllStringSubmatch(body, -1)
		if len(rows) == 0 {
			t.Errorf("the %s group rendered no side-pickers at all", group)
		}
		for _, m := range rows {
			if !boxes[m[1]] {
				t.Errorf("the %s group has a side-picker for %q with no matching checkbox",
					group, m[1])
			}
		}
	}
}

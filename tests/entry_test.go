package tests

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// The V5 way in: a six-digit code to a phone or an email, the same call for signing
// in and for creating an account, and a guest's points following them in.
func TestCodeSignInCreatesAndReturns(t *testing.T) {
	a := newApp(t)

	// 1. A number nobody has: the code goes out and says so.
	sent := a.post("/api/v1/auth/code", map[string]any{
		"channel": "phone", "country": "TN", "phone": "20 123 456",
	}, "").expect(http.StatusOK).data()
	if sent["destination"] != "+21620123456" {
		t.Fatalf("the number should be normalised to E.164: %v", sent["destination"])
	}
	if sent["exists"] != false {
		t.Errorf("that number has no account yet: %v", sent)
	}
	code, _ := sent["dev_code"].(string)
	if len(code) != 6 {
		t.Fatalf("development should hand back the code, got %q", code)
	}

	// 2. A wrong code costs a try and says how many are left.
	wrong := a.post("/api/v1/auth/code/verify", map[string]any{
		"channel": "phone", "country": "TN", "phone": "20123456", "code": "000000",
	}, "").expect(http.StatusUnprocessableEntity)
	if !strings.Contains(string(wrong.Body), "tries left") {
		t.Errorf("a wrong code should count down the tries: %s", wrong.Body)
	}

	// 3. The right code on a free number asks for a name rather than signing in.
	verified := a.post("/api/v1/auth/code/verify", map[string]any{
		"channel": "phone", "country": "TN", "phone": "20123456", "code": code,
	}, "").expect(http.StatusOK).data()
	if verified["new_account"] != true {
		t.Fatalf("a number nobody has is a new account: %v", verified)
	}

	// 4. Naming it creates the account and signs the player in.
	created := a.post("/api/v1/auth/complete", map[string]any{
		"channel": "phone", "destination": "+21620123456",
		"display_name": "Aisha", "accept_terms": true,
	}, "").expect(http.StatusCreated).data()
	user := created["user"].(map[string]any)
	if user["display_name"] != "Aisha" || user["kind"] != "member" {
		t.Fatalf("the account was not created properly: %v", user)
	}

	// 5. The same number again is a returning player, not a second account.
	a.sleepPastResend(t, "+21620123456")
	again := a.post("/api/v1/auth/code", map[string]any{
		"channel": "phone", "country": "TN", "phone": "20123456",
	}, "").expect(http.StatusOK).data()
	if again["exists"] != true {
		t.Errorf("the number now has an account: %v", again)
	}
	back := a.post("/api/v1/auth/code/verify", map[string]any{
		"channel": "phone", "country": "TN", "phone": "20123456",
		"code": again["dev_code"],
	}, "").expect(http.StatusOK).data()
	if back["new_account"] != false || back["token"] == nil {
		t.Errorf("a known number signs in: %v", back)
	}
	var accounts int
	_ = a.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM users WHERE phone = '+21620123456'").Scan(&accounts)
	if accounts != 1 {
		t.Errorf("one number, one account: %d", accounts)
	}
}

// A guest who signs in keeps everything: the account is the same row, so the points
// are not "moved", they were already theirs (AU-05).
func TestGuestBecomesTheAccount(t *testing.T) {
	a := newApp(t)
	guest := a.guest("Nour")
	me := a.get("/api/v1/me", guest).expect(http.StatusOK).data()
	guestID := me["id"].(string)

	// Give the guest something to lose.
	if _, err := a.DB.Pool.Exec(t.Context(), "UPDATE users SET xp = 120 WHERE id = $1", guestID); err != nil {
		t.Fatal(err)
	}

	sent := a.post("/api/v1/auth/code", map[string]any{
		"channel": "phone", "country": "TN", "phone": "55 444 333",
	}, guest).expect(http.StatusOK).data()
	a.post("/api/v1/auth/code/verify", map[string]any{
		"channel": "phone", "country": "TN", "phone": "55444333", "code": sent["dev_code"],
	}, guest).expect(http.StatusOK)
	created := a.post("/api/v1/auth/complete", map[string]any{
		"channel": "phone", "destination": "+21655444333",
		"display_name": "Nour", "accept_terms": true,
	}, guest).expect(http.StatusCreated).data()

	user := created["user"].(map[string]any)
	if user["id"] != guestID {
		t.Errorf("the guest should become the account, not a second one: %v vs %v", user["id"], guestID)
	}
	if int(user["xp"].(float64)) != 120 {
		t.Errorf("the guest's points should still be there: %v", user["xp"])
	}
	if user["kind"] != "member" {
		t.Errorf("they are a member now: %v", user["kind"])
	}
}

// The rules the design states: a resend brake, a dead code after three tries, and a
// number that does not fit its country.
func TestCodeRules(t *testing.T) {
	a := newApp(t)

	// A number that is not eight digits is refused before anything is sent.
	bad := a.post("/api/v1/auth/code", map[string]any{
		"channel": "phone", "country": "TN", "phone": "2012",
	}, "").expect(http.StatusUnprocessableEntity)
	if bad.errorCode() != "invalid" {
		t.Errorf("a short number is invalid: %s", bad.Body)
	}

	sent := a.post("/api/v1/auth/code", map[string]any{
		"channel": "email", "email": "rules@example.com",
	}, "").expect(http.StatusOK).data()

	// Asking again at once is refused by the server's own timer.
	soon := a.post("/api/v1/auth/code", map[string]any{
		"channel": "email", "email": "rules@example.com",
	}, "").expect(http.StatusConflict)
	if soon.errorCode() != "too_soon" {
		t.Errorf("the resend timer belongs to the server: %s", soon.Body)
	}

	// Three wrong tries and the code is dead, even if it was the right one after.
	for i := 0; i < 3; i++ {
		a.post("/api/v1/auth/code/verify", map[string]any{
			"channel": "email", "email": "rules@example.com", "code": "000000",
		}, "")
	}
	dead := a.post("/api/v1/auth/code/verify", map[string]any{
		"channel": "email", "email": "rules@example.com", "code": sent["dev_code"],
	}, "").expect(http.StatusConflict)
	if code := dead.errorCode(); code != "code_locked" && code != "code_expired" {
		t.Errorf("three tries should kill the code, got %s", dead.Body)
	}
}

// Signing out everywhere closes every session, not just this one.
func TestSignOutEverywhere(t *testing.T) {
	a := newApp(t)
	first := a.member("devices")
	second := a.post("/api/v1/auth/login", map[string]any{
		"login": "devices", "password": "averysafe1", "client": "ios",
	}, "").expect(http.StatusOK).data()["token"].(string)

	if list := a.get("/api/v1/me/devices", first).expect(http.StatusOK).list(); len(list) != 2 {
		t.Errorf("two sessions were opened, the list shows %d", len(list))
	}
	a.post("/api/v1/auth/logout-everywhere", nil, first).expect(http.StatusOK)
	a.get("/api/v1/me", first).expect(http.StatusUnauthorized)
	a.get("/api/v1/me", second).expect(http.StatusUnauthorized)
}

// The entry screen itself: one page, the steps it can show, and the popups over it.
func TestEntryScreensRender(t *testing.T) {
	a := newApp(t)
	page := string(a.get("/signin", "").expect(http.StatusOK).Body)
	for _, want := range []string{"entry-card", "entry-visual", "seg-tabs", "country-btn", `name="phone"`, "Keep me signed in"} {
		if !strings.Contains(page, want) {
			t.Errorf("the sign-in screen is missing %s", want)
		}
	}
	create := string(a.get("/signup", "").expect(http.StatusOK).Body)
	if !strings.Contains(create, "Create account") || !strings.Contains(create, "Already have an account?") {
		t.Error("the create-account step should offer the way back to signing in")
	}
	// "Your details": the steps bar, the name, the password and its five checks.
	for _, want := range []string{
		"entry-steps", "Your details", "Verify", `name="display_name"`,
		`name="password"`, "pw-checks", "Password strength", "8 chars", "@#$",
		`name="accept_terms"`,
	} {
		if !strings.Contains(create, want) {
			t.Errorf("the create-account step is missing %s", want)
		}
	}
	if strings.Contains(create, "Keep me signed in") {
		t.Error("the design does not ask to stay signed in while an account is being made")
	}
	guest := string(a.get("/guest?ns=popup", "").expect(http.StatusOK).Body)
	if !strings.Contains(guest, "Welcome, guest") || !strings.Contains(guest, "Start playing") {
		t.Error("the guest popup is not the one the design draws")
	}
}

// sleepPastResend waits out the server's thirty seconds by ageing the row instead,
// so the suite stays fast.
func (a *app) sleepPastResend(t *testing.T, destination string) {
	t.Helper()
	if _, err := a.DB.Pool.Exec(t.Context(),
		"UPDATE auth_codes SET sent_at = sent_at - interval '2 minutes' WHERE destination = $1", destination); err != nil {
		t.Fatal(err)
	}
}

// "Create account" asks for the name and the password before the code goes out
// ("1 · Your details → 2 · Verify"), so verifying the six digits finishes the
// account in one step rather than asking for a name afterwards.
func TestCreateAccountWithDetails(t *testing.T) {
	a := newApp(t)
	details := map[string]any{
		"channel": "phone", "country": "TN", "phone": "22 334 455",
		"creating": true, "display_name": "Malek", "accept_terms": true,
	}

	// A password that fails a check is refused before a code is sent.
	weak := clone(details)
	weak["password"] = "abcDEF"
	refused := a.post("/api/v1/auth/code", weak, "").expect(http.StatusUnprocessableEntity)
	if !strings.Contains(string(refused.Body), "password") {
		t.Errorf("a weak password should be named as the problem: %s", refused.Body)
	}
	var sentAnyway int
	_ = a.DB.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM auth_codes WHERE destination = '+21622334455'").Scan(&sentAnyway)
	if sentAnyway != 0 {
		t.Errorf("nothing should be sent before the details are good, got %d codes", sentAnyway)
	}

	// The terms are not optional either.
	noTerms := clone(details)
	noTerms["password"] = "Admin@1234"
	noTerms["accept_terms"] = false
	a.post("/api/v1/auth/code", noTerms, "").expect(http.StatusUnprocessableEntity)

	// All five checks and the terms: the code goes out.
	good := clone(details)
	good["password"] = "Admin@1234"
	sent := a.post("/api/v1/auth/code", good, "").expect(http.StatusOK).data()

	// Proving it makes the account there and then: no "name it" step.
	done := a.post("/api/v1/auth/code/verify", map[string]any{
		"channel": "phone", "country": "TN", "phone": "22334455", "code": sent["dev_code"],
	}, "").expect(http.StatusOK).data()
	if done["new_account"] != false || done["created"] != true {
		t.Fatalf("the account should be created by the code itself: %v", done)
	}
	user := done["user"].(map[string]any)
	if user["display_name"] != "Malek" || user["kind"] != "member" {
		t.Fatalf("the name from the first step should be the account's: %v", user)
	}

	// The password is kept, so the "Use password" tab works for this account once
	// it has an address to sign in by.
	var hashes int
	_ = a.DB.Pool.QueryRow(t.Context(),
		`SELECT count(*) FROM auth_identities WHERE user_id = $1 AND provider = 'password'
		   AND password_hash IS NOT NULL`, user["id"]).Scan(&hashes)
	if hashes != 1 {
		t.Errorf("the password set on the create screen should be stored once, got %d", hashes)
	}
}

// Asking for the code again on the create flow must not ask for the details twice.
func TestCreateAccountResendKeepsDetails(t *testing.T) {
	a := newApp(t)
	a.post("/api/v1/auth/code", map[string]any{
		"channel": "email", "email": "again@example.com", "creating": true,
		"display_name": "Sara", "password": "Admin@1234", "accept_terms": true,
	}, "").expect(http.StatusOK)
	a.sleepPastResend(t, "again@example.com")

	sent := a.post("/api/v1/auth/code", map[string]any{
		"channel": "email", "email": "again@example.com", "creating": true, "resend": true,
	}, "").expect(http.StatusOK).data()
	done := a.post("/api/v1/auth/code/verify", map[string]any{
		"channel": "email", "email": "again@example.com", "code": sent["dev_code"],
	}, "").expect(http.StatusOK).data()
	if done["created"] != true {
		t.Fatalf("a resent code should still finish the account: %v", done)
	}
	if user := done["user"].(map[string]any); user["display_name"] != "Sara" {
		t.Errorf("the name given before the first code should survive a resend: %v", user)
	}
}

func clone(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// "Have a room code? Join a room" is on the entry screen, where nobody is signed in
// yet: a visitor who types a real code becomes a guest in the act of joining
// (screens 28–30).
func TestVisitorJoinsARoomFromTheEntryScreen(t *testing.T) {
	a := newApp(t)
	host := a.member("roomhost")
	code := a.post("/api/v1/rooms", map[string]any{
		"domain_id": a.Content.DomainID, "question_count": 5,
	}, host).expect(http.StatusCreated).data()["code"].(string)

	// The popup opens for a visitor and offers them a name.
	page := a.get("/rooms/join?ns=popup", "")
	page.expect(http.StatusOK)
	if !strings.Contains(string(page.Body), "Join a room") {
		t.Fatalf("the popup should be the one the design draws: %s", firstLines(page.Body))
	}
	csrf, token := csrfFrom(t, page)

	// A code nobody uses is refused, and costs no account.
	bad := a.entryForm(t, "/rooms/join", "from=entry&name=Nour&code=ABC123&csrf_token="+token, csrf)
	bad.expect(http.StatusUnprocessableEntity)
	if !strings.Contains(string(bad.Body), "No open room has that code") {
		t.Errorf("a wrong code should say so in the popup: %s", firstLines(bad.Body))
	}

	// The real one lets them in, as the guest they have just become.
	ok := a.entryForm(t, "/rooms/join", "from=entry&name=Nour&code="+code+"&csrf_token="+token, csrf)
	if ok.Code != http.StatusSeeOther && ok.Code != http.StatusFound && ok.Code != http.StatusOK {
		t.Fatalf("joining should send the player to the lobby, got %d: %s", ok.Code, firstLines(ok.Body))
	}
	if where := ok.Hdr.Get("Location") + ok.Hdr.Get("HX-Redirect"); !strings.Contains(where, code) {
		t.Errorf("the player should land in the room, not at %q", where)
	}
	var guests int
	_ = a.DB.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM users WHERE kind = 'guest' AND display_name = 'Nour'").Scan(&guests)
	if guests != 1 {
		t.Errorf("the visitor should have become exactly one guest, got %d", guests)
	}
}

// csrfFrom reads the pair a browser would carry: the cookie, and the token the form
// prints inside itself.
func csrfFrom(t *testing.T, r reply) (cookie, token string) {
	t.Helper()
	for _, c := range r.Hdr.Values("Set-Cookie") {
		if strings.HasPrefix(c, "sj_csrf=") {
			cookie = strings.SplitN(strings.TrimPrefix(c, "sj_csrf="), ";", 2)[0]
		}
	}
	if m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindSubmatch(r.Body); m != nil {
		token = string(m[1])
	}
	if cookie == "" || token == "" {
		t.Fatalf("no CSRF pair on the page (cookie %q, token %q)", cookie, token)
	}
	return cookie, token
}

// entryForm posts a form the way the signed-out entry screen does: no session, but
// the CSRF cookie the page handed out.
func (a *app) entryForm(t *testing.T, path, body, csrf string) reply {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "sj_csrf", Value: csrf})
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return reply{t: t, Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

func firstLines(b []byte) string {
	s := string(b)
	if len(s) > 240 {
		s = s[:240]
	}
	return s
}

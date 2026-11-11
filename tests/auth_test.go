package tests

import (
	"net/http"
	"strings"
	"testing"
)

// T-08: one test per auth endpoint (1–3, 5–10, 12, 15, 16).
func TestRegisterAndLogin(t *testing.T) {
	a := newApp(t)

	r := a.post("/api/v1/auth/register", map[string]any{
		"display_name": "Aisha T.", "username": "aisha", "email": "aisha@example.com",
		"password": "averysafe1", "accept_terms": true,
	}, "").expect(http.StatusCreated)
	user := r.data()["user"].(map[string]any)
	if user["kind"] != "member" || user["username"] != "aisha" {
		t.Fatalf("unexpected user: %v", user)
	}

	// A new account starts with at least one domain (DM-03, decision D-04).
	token := r.data()["token"].(string)
	domains := a.get("/api/v1/me/domains", token).expect(http.StatusOK).list()
	if len(domains) == 0 {
		t.Error("a new account must have at least one domain")
	}

	// #2 sign in with the email, then with the username.
	for _, login := range []string{"aisha@example.com", "aisha"} {
		res := a.post("/api/v1/auth/login", map[string]any{"login": login, "password": "averysafe1"}, "").expect(http.StatusOK)
		if res.data()["token"] == "" {
			t.Errorf("no token when signing in with %q", login)
		}
	}
	// Wrong password never says which half was wrong.
	bad := a.post("/api/v1/auth/login", map[string]any{"login": "aisha", "password": "nope"}, "").expect(http.StatusUnauthorized)
	if bad.errorCode() != "bad_credentials" {
		t.Errorf("want bad_credentials, got %s", bad.errorCode())
	}
}

func TestRegisterValidation(t *testing.T) {
	a := newApp(t)
	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{"short password", map[string]any{"display_name": "A", "username": "abcd", "email": "a@b.co", "password": "short", "accept_terms": true}, "invalid"},
		{"bad username", map[string]any{"display_name": "A", "username": "a b", "email": "a@b.co", "password": "averysafe1", "accept_terms": true}, "invalid"},
		{"no terms", map[string]any{"display_name": "A", "username": "abcd", "email": "a@b.co", "password": "averysafe1"}, "invalid"},
		{"bad email", map[string]any{"display_name": "A", "username": "abcd", "email": "nope", "password": "averysafe1", "accept_terms": true}, "invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := a.post("/api/v1/auth/register", c.body, "").expect(http.StatusUnprocessableEntity)
			if got := r.errorCode(); got != c.code {
				t.Errorf("want %s, got %s", c.code, got)
			}
		})
	}
	a.member("taken")
	r := a.post("/api/v1/auth/register", map[string]any{
		"display_name": "Other", "username": "taken", "email": "other@example.com",
		"password": "averysafe1", "accept_terms": true,
	}, "").expect(http.StatusConflict)
	if r.errorCode() != "username_taken" {
		t.Errorf("want username_taken, got %s", r.errorCode())
	}
	r = a.post("/api/v1/auth/register", map[string]any{
		"display_name": "Other", "username": "other", "email": "taken@example.com",
		"password": "averysafe1", "accept_terms": true,
	}, "").expect(http.StatusConflict)
	if r.errorCode() != "email_taken" {
		t.Errorf("want email_taken, got %s", r.errorCode())
	}
}

// #4, #5, RL-04: upgrading a guest keeps the same id, so points carry over.
func TestGuestUpgradeKeepsTheSameID(t *testing.T) {
	a := newApp(t)
	token := a.guest("Guest 4821")
	me := a.get("/api/v1/me", token).expect(http.StatusOK).data()
	if me["kind"] != "guest" {
		t.Fatalf("want a guest, got %v", me["kind"])
	}
	guestID := me["id"]
	if me["expires_at"] == nil {
		t.Error("a guest must expire (RL-02)")
	}

	up := a.post("/api/v1/auth/guest/upgrade", map[string]any{
		"display_name": "Aisha T.", "username": "aisha2", "email": "aisha2@example.com",
		"password": "averysafe1", "accept_terms": true,
	}, token).expect(http.StatusCreated)
	upgraded := up.data()["user"].(map[string]any)
	if upgraded["id"] != guestID {
		t.Errorf("the id must not change: %v → %v", guestID, upgraded["id"])
	}
	if upgraded["kind"] != "member" || upgraded["expires_at"] != nil {
		t.Errorf("upgraded user should be a member that does not expire: %v", upgraded)
	}
	// A member cannot upgrade again.
	newToken := up.data()["token"].(string)
	a.post("/api/v1/auth/guest/upgrade", map[string]any{
		"display_name": "x", "username": "aisha3", "email": "a3@example.com",
		"password": "averysafe1", "accept_terms": true,
	}, newToken).expect(http.StatusConflict)
}

// #10 username availability, used while the player types.
func TestUsernameAvailable(t *testing.T) {
	a := newApp(t)
	a.member("omar")
	if a.get("/api/v1/auth/username-available?username=omar", "").expect(http.StatusOK).data()["available"] != false {
		t.Error("omar should be taken")
	}
	if a.get("/api/v1/auth/username-available?username=nour", "").expect(http.StatusOK).data()["available"] != true {
		t.Error("nour should be free")
	}
	a.get("/api/v1/auth/username-available?username=a", "").expect(http.StatusUnprocessableEntity)
}

// #8, #9 password reset: the link is one-time and signs the other sessions out.
func TestForgotAndResetPassword(t *testing.T) {
	a := newApp(t)
	token := a.member("khadija")

	a.post("/api/v1/auth/password/forgot", map[string]any{"email": "khadija@example.com"}, "").expect(http.StatusOK)
	if len(a.Mail.sent) != 1 {
		t.Fatalf("want one email, got %d", len(a.Mail.sent))
	}
	link := a.Mail.sent[0].Body
	at := strings.Index(link, "token=")
	if at < 0 {
		t.Fatalf("no token in the email: %s", link)
	}
	resetToken := strings.TrimSpace(link[at+len("token="):])
	if nl := strings.IndexAny(resetToken, "\n "); nl > 0 {
		resetToken = resetToken[:nl]
	}

	a.post("/api/v1/auth/password/reset", map[string]any{"token": resetToken, "password": "brandnew12"}, "").expect(http.StatusOK)
	// The old session is gone and the old password no longer works.
	a.get("/api/v1/me", token).expect(http.StatusUnauthorized)
	a.post("/api/v1/auth/login", map[string]any{"login": "khadija", "password": "averysafe1"}, "").expect(http.StatusUnauthorized)
	a.post("/api/v1/auth/login", map[string]any{"login": "khadija", "password": "brandnew12"}, "").expect(http.StatusOK)
	// The link cannot be used twice.
	a.post("/api/v1/auth/password/reset", map[string]any{"token": resetToken, "password": "another123"}, "").expect(http.StatusBadRequest)

	// An unknown email answers the same way, so the screen leaks nothing.
	a.Mail.sent = nil
	a.post("/api/v1/auth/password/forgot", map[string]any{"email": "nobody@example.com"}, "").expect(http.StatusOK)
	if len(a.Mail.sent) != 0 {
		t.Error("no email should be sent for an unknown address")
	}
}

// #6 refresh, #7 logout.
func TestRefreshAndLogout(t *testing.T) {
	a := newApp(t)
	token := a.member("youssef")
	a.post("/api/v1/auth/refresh", nil, token).expect(http.StatusOK)
	a.post("/api/v1/auth/logout", nil, token).expect(http.StatusNoContent)
	a.get("/api/v1/me", token).expect(http.StatusUnauthorized)
	a.post("/api/v1/auth/refresh", nil, token).expect(http.StatusUnauthorized)
}

// #3 Google sign-in is off until a client id is configured.
func TestGoogleNeedsConfiguration(t *testing.T) {
	a := newApp(t)
	r := a.post("/api/v1/auth/google", map[string]any{"id_token": "xxx"}, "").expect(http.StatusServiceUnavailable)
	if r.errorCode() != "google_not_configured" {
		t.Errorf("want google_not_configured, got %s", r.errorCode())
	}
}

// #12 profile, #13 settings, #15 devices, #16 delete.
func TestProfileSettingsDevicesDelete(t *testing.T) {
	a := newApp(t)
	token := a.member("nour")

	name := "Nour E."
	r := a.do(http.MethodPatch, "/api/v1/me", map[string]any{"display_name": name, "lang": "fr"}, token).expect(http.StatusOK)
	if r.data()["display_name"] != name || r.data()["lang"] != "fr" {
		t.Errorf("profile not updated: %v", r.data())
	}
	a.do(http.MethodPatch, "/api/v1/me", map[string]any{"lang": "zz"}, token).expect(http.StatusUnprocessableEntity)

	s := a.do(http.MethodPatch, "/api/v1/me/settings", map[string]any{
		"theme": "dark", "daily_goal": 10, "islamic_only": true, "reminder_time": "19:30",
	}, token).expect(http.StatusOK).data()
	if s["theme"] != "dark" || s["islamic_only"] != true || s["reminder_time"] != "19:30" {
		t.Errorf("settings not saved: %v", s)
	}
	a.do(http.MethodPatch, "/api/v1/me/settings", map[string]any{"daily_goal": 0}, token).expect(http.StatusUnprocessableEntity)

	a.post("/api/v1/me/devices", map[string]any{"platform": "android", "push_token": "abc123"}, token).expect(http.StatusNoContent)
	a.post("/api/v1/me/devices", map[string]any{"platform": "nokia", "push_token": "abc123"}, token).expect(http.StatusUnprocessableEntity)

	a.do(http.MethodDelete, "/api/v1/me", map[string]any{"confirm": "nope"}, token).expect(http.StatusUnprocessableEntity)
	a.do(http.MethodDelete, "/api/v1/me", map[string]any{"confirm": "DELETE"}, token).expect(http.StatusNoContent)
	a.get("/api/v1/me", token).expect(http.StatusUnauthorized)
}

// Guests are real users: they can play, and the routes that need a player accept them (RL-02).
func TestGuestIsARealUser(t *testing.T) {
	a := newApp(t)
	token := a.guest("")
	me := a.get("/api/v1/me", token).expect(http.StatusOK).data()
	if me["display_name"] != "Guest" {
		t.Errorf("a guest with no name becomes %q", me["display_name"])
	}
	a.get("/api/v1/me/domains", token).expect(http.StatusOK)
	a.get("/api/v1/me", "").expect(http.StatusUnauthorized)
}

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsernamePattern(t *testing.T) {
	valid := []string{"abc", "malek", "malek_z", "a.b.c", "User123", "____", "a23456789012345678901234"}
	for _, u := range valid {
		if !usernamePattern.MatchString(u) {
			t.Errorf("%q should be a valid username", u)
		}
	}

	invalid := []string{
		"",
		"ab",                        // too short
		"a234567890123456789012345", // 25 chars, too long
		"has space",
		"has-dash",
		"emoji😀",
		"عربي",
		"semi;colon",
		"a\nb",
	}
	for _, u := range invalid {
		if usernamePattern.MatchString(u) {
			t.Errorf("%q should be rejected", u)
		}
	}
}

func TestAttemptLimiter(t *testing.T) {
	l := &attemptLimiter{
		entries: map[string]*attemptEntry{},
		max:     3,
		window:  time.Minute,
	}

	for i := 1; i <= 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed (max 3)", i)
		}
	}
	if l.allow("1.2.3.4") {
		t.Error("the fourth attempt should be blocked")
	}

	// A different client is tracked independently.
	if !l.allow("5.6.7.8") {
		t.Error("a separate IP must not inherit another's budget")
	}

	// A successful login clears the counter.
	l.reset("1.2.3.4")
	if !l.allow("1.2.3.4") {
		t.Error("reset should clear the counter")
	}
}

func TestAttemptLimiterAllowsEmptyKey(t *testing.T) {
	l := &attemptLimiter{entries: map[string]*attemptEntry{}, max: 1, window: time.Minute}
	for i := 0; i < 5; i++ {
		if !l.allow("") {
			t.Fatal("an unknown client key must not be rate limited into a lockout")
		}
	}
}

func TestAttemptLimiterWindowExpiry(t *testing.T) {
	l := &attemptLimiter{entries: map[string]*attemptEntry{}, max: 1, window: 10 * time.Millisecond}

	if !l.allow("ip") {
		t.Fatal("first attempt should pass")
	}
	if l.allow("ip") {
		t.Fatal("second attempt inside the window should fail")
	}

	time.Sleep(15 * time.Millisecond)
	if !l.allow("ip") {
		t.Error("the budget should refill once the window has passed")
	}
}

func TestCSRFRoundTrip(t *testing.T) {
	a := NewAuth(nil, time.Hour, false)

	// First call mints a token and sets the cookie.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	token := a.IssueCSRF(rec, req)
	if len(token) < 32 {
		t.Fatalf("token too short: %q", token)
	}

	cookie := rec.Result().Cookies()[0]
	if cookie.Name != CSRFCookie {
		t.Fatalf("expected the CSRF cookie, got %q", cookie.Name)
	}

	// A matching form field passes.
	post := httptest.NewRequest(http.MethodPost, "/login", nil)
	post.AddCookie(cookie)
	post.Form = map[string][]string{CSRFField: {token}}
	post.PostForm = post.Form
	if !a.VerifyCSRF(post) {
		t.Error("a matching form token should verify")
	}

	// The header variant passes too.
	hdr := httptest.NewRequest(http.MethodPost, "/login", nil)
	hdr.AddCookie(cookie)
	hdr.Header.Set(CSRFHeader, token)
	if !a.VerifyCSRF(hdr) {
		t.Error("a matching header token should verify")
	}
}

func TestCSRFRejects(t *testing.T) {
	a := NewAuth(nil, time.Hour, false)

	rec := httptest.NewRecorder()
	a.IssueCSRF(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	cookie := rec.Result().Cookies()[0]

	t.Run("wrong token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.AddCookie(cookie)
		req.Header.Set(CSRFHeader, "not-the-token")
		if a.VerifyCSRF(req) {
			t.Error("a mismatched token must be rejected")
		}
	})

	t.Run("no token submitted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.AddCookie(cookie)
		if a.VerifyCSRF(req) {
			t.Error("a missing token must be rejected")
		}
	})

	t.Run("no cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set(CSRFHeader, "anything")
		if a.VerifyCSRF(req) {
			t.Error("a missing cookie must be rejected")
		}
	})
}

func TestIssueCSRFReusesExistingCookie(t *testing.T) {
	a := NewAuth(nil, time.Hour, false)

	rec := httptest.NewRecorder()
	first := a.IssueCSRF(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])

	second := a.IssueCSRF(httptest.NewRecorder(), req)
	if first != second {
		t.Errorf("token rotated within a session: %q then %q", first, second)
	}
}

func TestClientIP(t *testing.T) {
	t.Run("from RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9:51234"
		if got := clientIP(req); got != "203.0.113.9" {
			t.Errorf("got %q, want 203.0.113.9", got)
		}
	})

	// The forwarding header is resolved once, in the router, and only when the
	// operator declared a proxy. Re-reading it here would hand the rate limiter
	// an identity the caller chose for themselves.
	t.Run("X-Forwarded-For is ignored", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1"
		req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
		if got := clientIP(req); got != "10.0.0.1" {
			t.Errorf("got %q, want the peer address 10.0.0.1", got)
		}
	})

	t.Run("bare address without a port", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9"
		if got := clientIP(req); got != "203.0.113.9" {
			t.Errorf("got %q, want 203.0.113.9", got)
		}
	})

	t.Run("IPv6 with a port", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "[2001:db8::1]:51234"
		if got := clientIP(req); got != "2001:db8::1" {
			t.Errorf("got %q, want 2001:db8::1", got)
		}
	})
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Errorf("got %q, want the input unchanged", got)
	}
	if got := truncate("abcdefghij", 4); got != "abcd" {
		t.Errorf("got %q, want abcd", got)
	}
}

func TestAvatarSeedsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		s := newAvatarSeed()
		if s == "" {
			t.Fatal("empty avatar seed")
		}
		if seen[s] {
			t.Fatalf("duplicate avatar seed on iteration %d: %q", i, s)
		}
		seen[s] = true
	}
}

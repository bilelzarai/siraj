package handlers

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bilelzarai/siraj/internal/config"
)

// safeNext is what stands between a crafted ?next= and an open redirect after
// a successful login, so it gets the most attention here.
func TestSafeNextRejectsOffsiteTargets(t *testing.T) {
	hostile := []string{
		"https://evil.example.com",
		"//evil.example.com",
		"//evil.example.com/path",
		"http://evil.example.com/app",
		"javascript:alert(1)",
		"",
		"   ",
		"app", // relative, no leading slash
		"\\\\evil.example.com",
	}
	for _, in := range hostile {
		if got := safeNext(in); got != "/app" {
			t.Errorf("safeNext(%q) = %q, want /app", in, got)
		}
	}
}

func TestSafeNextKeepsLocalPaths(t *testing.T) {
	allowed := []string{"/app", "/messages/123", "/history?mode=solo", "/u/malek"}
	for _, in := range allowed {
		if got := safeNext(in); got != in {
			t.Errorf("safeNext(%q) = %q, want it unchanged", in, got)
		}
	}
}

// backTo and the CSRF rejection both send the browser to the Referer, which is
// set by whoever made the request. Anything that is not a path on this host has
// to come back as the fallback.
func TestBackToPrefersSameSiteReferer(t *testing.T) {
	cases := []struct {
		name     string
		referer  string
		fallback string
		want     string
	}{
		{"no referer", "", "/friends", "/friends"},
		{"relative referer", "/friends?tab=search", "/friends", "/friends?tab=search"},
		{"same-host absolute referer keeps path and query", "http://example.com/friends?tab=requests", "/friends", "/friends?tab=requests"},
		{"protocol-relative is refused", "//evil.example.com/x", "/friends", "/friends"},
		{"another host is refused", "https://evil.example.com/friends", "/friends", "/friends"},
		{"another host on a matching path is refused", "http://evil.example.com/friends?tab=requests", "/friends", "/friends"},
		{"non-http scheme is refused", "javascript:alert(1)", "/friends", "/friends"},
		{"host-only referer is refused", "http://example.com", "/friends", "/friends"},
		{"garbage is refused", "http://[::1]:namedport/x", "/friends", "/friends"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// httptest.NewRequest serves requests as example.com.
			r := httptest.NewRequest(http.MethodPost, "/friends/accept", nil)
			if tc.referer != "" {
				r.Header.Set("Referer", tc.referer)
			}
			if got := backTo(r, tc.fallback); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The forwarding chain is read from the right, because everything to the left
// of our own proxies was written by someone we do not control.
func TestForwardedForCountsFromTheRight(t *testing.T) {
	cases := []struct {
		name   string
		header string
		hops   int
		want   string
	}{
		{"no header", "", 1, ""},
		{"untrusted", "198.51.100.7", 0, ""},
		{"single proxy takes the only entry", "198.51.100.7", 1, "198.51.100.7"},
		{"single proxy ignores a spoofed prefix", "1.2.3.4, 198.51.100.7", 1, "198.51.100.7"},
		{"two proxies step back two", "1.2.3.4, 198.51.100.7, 10.0.0.9", 2, "198.51.100.7"},
		{"chain shorter than configured is refused", "198.51.100.7", 2, ""},
		{"non-IP entry is refused", "not-an-ip", 1, ""},
		{"whitespace is tolerated", "  198.51.100.7  ", 1, "198.51.100.7"},
		{"IPv6 is accepted", "2001:db8::1", 1, "2001:db8::1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := forwardedFor(tc.header, tc.hops); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNilIfZero(t *testing.T) {
	if nilIfZero(0) != nil {
		t.Error("the all-categories sentinel should become a NULL filter")
	}
	if nilIfZero(-3) != nil {
		t.Error("a negative id should also become NULL")
	}
	got := nilIfZero(7)
	if got == nil || *got != 7 {
		t.Errorf("nilIfZero(7) = %v, want a pointer to 7", got)
	}
}

func TestQueryIntFallsBack(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/history?page=3&bad=xyz", nil)

	if got := queryInt(r, "page", 1); got != 3 {
		t.Errorf("page = %d, want 3", got)
	}
	if got := queryInt(r, "bad", 1); got != 1 {
		t.Errorf("unparsable value should fall back, got %d", got)
	}
	if got := queryInt(r, "absent", 9); got != 9 {
		t.Errorf("missing value should fall back, got %d", got)
	}
}

func TestParseUUID(t *testing.T) {
	if _, ok := parseUUID("not-a-uuid"); ok {
		t.Error("garbage should not parse")
	}
	id, ok := parseUUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	if !ok || id.String() != "6ba7b810-9dad-11d1-80b4-00c04fd430c8" {
		t.Errorf("valid uuid failed to round trip: %v %v", id, ok)
	}
}

func TestClipCountsRunesNotBytes(t *testing.T) {
	// Arabic is multi-byte; clipping by bytes would split a character.
	in := "السلام عليكم ورحمة الله"
	got := clip(in, 6)
	if len([]rune(got)) != 6 {
		t.Errorf("clip produced %d runes, want 6", len([]rune(got)))
	}
	if clip("short", 99) != "short" {
		t.Error("a short string should pass through unchanged")
	}
}

func TestIsAPIRequest(t *testing.T) {
	t.Run("json Accept header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("Accept", "application/json")
		if !isAPIRequest(r) {
			t.Error("should be treated as an API call")
		}
	})
	t.Run("fetch marker", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("X-Requested-With", "fetch")
		if !isAPIRequest(r) {
			t.Error("should be treated as an API call")
		}
	})
	t.Run("ordinary form post", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("Accept", "text/html")
		if isAPIRequest(r) {
			t.Error("a browser form post should get HTML, not JSON")
		}
	})
}

// flashHandlers is the minimum wiring readFlash needs: the signing key.
func flashHandlers() *Handlers {
	return &Handlers{cfg: &config.Config{
		SessionSecret: "test-secret-that-is-long-enough-32",
	}}
}

func TestFlashRoundTrip(t *testing.T) {
	h := flashHandlers()

	rec := httptest.NewRecorder()
	h.flash(rec, "success", "Saved")

	cookie := rec.Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)

	out := httptest.NewRecorder()
	flash := h.readFlash(out, req)
	if flash == nil {
		t.Fatal("flash was not read back")
	}
	if flash.Kind != "success" || flash.Text != "Saved" {
		t.Errorf("got %+v, want success/Saved", flash)
	}

	// Reading must clear it, so the message shows exactly once.
	cleared := out.Result().Cookies()[0]
	if cleared.MaxAge >= 0 {
		t.Errorf("flash cookie was not cleared: MaxAge=%d", cleared.MaxAge)
	}
}

func TestReadFlashWithoutCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := flashHandlers().readFlash(httptest.NewRecorder(), req); got != nil {
		t.Errorf("expected no flash, got %+v", got)
	}
}

// A flash is rendered as the application's own words, so a cookie the client
// wrote itself must not be believed.
func TestReadFlashRejectsForgery(t *testing.T) {
	h := flashHandlers()

	forgeries := []struct {
		name  string
		value string
	}{
		{"unsigned payload", base64.RawURLEncoding.EncodeToString([]byte("error|Your account is closed"))},
		{"bogus signature", base64.RawURLEncoding.EncodeToString([]byte("error|Pay here")) + ".not-a-mac"},
		{"legacy plain format", "error|Pay here"},
		{"empty", ""},
	}

	for _, f := range forgeries {
		t.Run(f.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: flashCookie, Value: f.value})
			if got := h.readFlash(httptest.NewRecorder(), req); got != nil {
				t.Errorf("forged flash was accepted: %+v", got)
			}
		})
	}
}

// A flash signed with one key must not verify under another, or rotating
// SESSION_SECRET would not actually invalidate anything.
func TestFlashSignatureIsKeyed(t *testing.T) {
	issuer := flashHandlers()
	rec := httptest.NewRecorder()
	issuer.flash(rec, "info", "Hello")

	other := &Handlers{cfg: &config.Config{SessionSecret: "a-completely-different-secret-key"}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])

	if got := other.readFlash(httptest.NewRecorder(), req); got != nil {
		t.Errorf("flash verified under a different key: %+v", got)
	}
}

// An unknown kind would be interpolated into a CSS class name.
func TestFlashKindIsConstrained(t *testing.T) {
	h := flashHandlers()
	rec := httptest.NewRecorder()
	h.flash(rec, "alert--evil\" onmouseover=x", "Hi")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])

	flash := h.readFlash(httptest.NewRecorder(), req)
	if flash == nil {
		t.Fatal("flash was dropped entirely; it should fall back to a known kind")
	}
	if flash.Kind != "info" {
		t.Errorf("kind = %q, want it clamped to info", flash.Kind)
	}
}

func TestStatusRecorderCapturesFirstWrite(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}

	rec.WriteHeader(http.StatusNotFound)
	rec.WriteHeader(http.StatusInternalServerError) // must be ignored

	if rec.status != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (the first write wins)", rec.status)
	}
	if !headerWritten(rec) {
		t.Error("headerWritten should report true after WriteHeader")
	}
}

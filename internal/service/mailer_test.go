package service

import (
	"context"
	"encoding/base64"
	"mime"
	"os"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/config"
)

func testMailer() *Mailer {
	return NewMailer(config.SMTPConfig{
		Host: "smtp.example.com", Port: 587,
		FromEmail: "noreply@siraj.example", FromName: "Sirāj",
	})
}

// The app's first language is Arabic, so a subject line that is not MIME
// encoded would arrive as mojibake in most clients.
func TestBuildEncodesArabicSubject(t *testing.T) {
	raw, err := testMailer().build(Mail{
		To:       "someone@example.com",
		Subject:  "دعوة للانضمام إلى سِراج",
		TextBody: "مرحبا",
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := string(raw)

	subject := headerValue(msg, "Subject")
	if subject == "" {
		t.Fatal("no Subject header")
	}
	if strings.Contains(subject, "دعوة") {
		t.Error("subject was emitted as raw UTF-8 instead of being MIME encoded")
	}
	if !strings.Contains(subject, "=?UTF-8?") {
		t.Errorf("subject is not an encoded-word: %q", subject)
	}

	decoded, err := new(mime.WordDecoder).DecodeHeader(subject)
	if err != nil {
		t.Fatalf("subject does not decode: %v", err)
	}
	if decoded != "دعوة للانضمام إلى سِراج" {
		t.Errorf("round trip lost the subject: %q", decoded)
	}
}

func TestBuildEncodesDisplayNames(t *testing.T) {
	raw, _ := testMailer().build(Mail{
		To:       "malek@example.com",
		ToName:   "مالك",
		Subject:  "hi",
		TextBody: "x",
	})
	msg := string(raw)

	to := headerValue(msg, "To")
	if !strings.Contains(to, "<malek@example.com>") {
		t.Errorf("To header lost the address: %q", to)
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(to)
	if err != nil || !strings.Contains(decoded, "مالك") {
		t.Errorf("To display name did not round trip: %q (%v)", decoded, err)
	}
}

func TestBuildIsMultipartWithBothBodies(t *testing.T) {
	raw, _ := testMailer().build(Mail{
		To:       "a@b.c",
		Subject:  "s",
		TextBody: "plain version",
		HTMLBody: "<p>html version</p>",
	})
	msg := string(raw)

	ctype := headerValue(msg, "Content-Type")
	if !strings.HasPrefix(ctype, "multipart/alternative") {
		t.Fatalf("Content-Type = %q, want multipart/alternative", ctype)
	}

	boundary := boundaryOf(ctype)
	if boundary == "" {
		t.Fatal("no boundary in Content-Type")
	}
	// Opening delimiter for each part, plus the closing delimiter.
	if n := strings.Count(msg, "--"+boundary); n != 3 {
		t.Errorf("found %d boundary markers, want 3 (two parts + terminator)", n)
	}
	if !strings.Contains(msg, `text/plain; charset="UTF-8"`) {
		t.Error("missing the text/plain part")
	}
	if !strings.Contains(msg, `text/html; charset="UTF-8"`) {
		t.Error("missing the text/html part")
	}

	if got := decodeParts(msg, boundary); !strings.Contains(got, "plain version") ||
		!strings.Contains(got, "html version") {
		t.Errorf("decoded bodies missing content: %q", got)
	}
}

func TestBuildUsesCRLFLineEndings(t *testing.T) {
	raw, _ := testMailer().build(Mail{To: "a@b.c", Subject: "s", TextBody: "x"})
	msg := string(raw)

	// SMTP requires CRLF; a bare LF terminates DATA incorrectly on strict MTAs.
	if strings.Contains(strings.ReplaceAll(msg, "\r\n", ""), "\n") {
		t.Error("message contains a bare LF outside a CRLF pair")
	}
}

func TestBuildDerivesTextFromHTMLWhenMissing(t *testing.T) {
	raw, _ := testMailer().build(Mail{
		To:       "a@b.c",
		Subject:  "s",
		HTMLBody: "<div><p>Hello <b>there</b></p></div>",
	})
	boundary := boundaryOf(headerValue(string(raw), "Content-Type"))
	plain := decodePart(string(raw), boundary, "text/plain")

	if !strings.Contains(plain, "Hello there") {
		t.Errorf("HTML fallback did not produce readable text: %q", plain)
	}
	if strings.Contains(plain, "<p>") {
		t.Errorf("tags leaked into the plain-text part: %q", plain)
	}
}

func TestWrapBase64FoldsLongLines(t *testing.T) {
	wrapped := wrapBase64(strings.Repeat("a", 1000))
	for _, line := range strings.Split(wrapped, "\r\n") {
		if len(line) > 76 {
			t.Fatalf("line of %d chars exceeds the 76-char limit", len(line))
		}
	}
	// It must still decode back to the original.
	if got, err := base64.StdEncoding.DecodeString(
		strings.ReplaceAll(wrapped, "\r\n", "")); err != nil || len(got) != 1000 {
		t.Errorf("folded base64 does not decode cleanly: %v", err)
	}
}

func TestDisabledMailerIsANoOp(t *testing.T) {
	m := NewMailer(config.SMTPConfig{}) // no host
	if m.Enabled() {
		t.Fatal("a mailer with no host should report disabled")
	}
	// Must not attempt a connection, and must not error.
	if err := m.Send(context.Background(), Mail{To: "a@b.c", Subject: "s"}); err != nil {
		t.Errorf("disabled mailer returned an error: %v", err)
	}
}

func TestFormatAddress(t *testing.T) {
	if got := formatAddress("", "a@b.c"); got != "a@b.c" {
		t.Errorf("bare address = %q", got)
	}
	if got := formatAddress("Malek", "a@b.c"); got != "Malek <a@b.c>" {
		t.Errorf("ascii name = %q", got)
	}
}

// TestLiveSMTP exercises a real server. It is skipped unless SMTP_TEST_HOST is
// set, e.g. SMTP_TEST_HOST=localhost SMTP_TEST_PORT=1025 (MailHog).
func TestLiveSMTP(t *testing.T) {
	host := os.Getenv("SMTP_TEST_HOST")
	if host == "" {
		t.Skip("set SMTP_TEST_HOST to run the live SMTP test")
	}
	port := 1025
	if p := os.Getenv("SMTP_TEST_PORT"); p != "" {
		if _, err := fmtSscan(p, &port); err != nil {
			t.Fatalf("bad SMTP_TEST_PORT: %v", err)
		}
	}

	m := NewMailer(config.SMTPConfig{
		Host: host, Port: port,
		FromEmail: "noreply@siraj.local", FromName: "سِراج",
		StartTLS: false,
	})
	err := m.Send(context.Background(), Mail{
		To: "test@example.com", ToName: "مالك",
		Subject:  "اختبار البريد — mail test",
		Locale:   "ar",
		TextBody: "نص عادي",
		HTMLBody: `<p dir="rtl">نص HTML</p>`,
	})
	if err != nil {
		t.Fatalf("live send failed: %v", err)
	}
}

// -------------------------------------------------------------- helpers --

func headerValue(msg, name string) string {
	for _, line := range strings.Split(msg, "\r\n") {
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(line, name+": ") {
			return strings.TrimPrefix(line, name+": ")
		}
	}
	return ""
}

func boundaryOf(contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return params["boundary"]
}

// decodeParts returns every base64 part of the message, decoded and joined.
func decodeParts(msg, boundary string) string {
	var out strings.Builder
	for _, part := range splitParts(msg, boundary) {
		out.WriteString(part.body)
		out.WriteString("\n")
	}
	return out.String()
}

// decodePart returns the decoded body of the first part whose headers declare
// the given media type. Tests that assert on the plain-text alternative must
// not also see the HTML one, which legitimately contains tags.
func decodePart(msg, boundary, mediaType string) string {
	for _, part := range splitParts(msg, boundary) {
		if strings.Contains(part.headers, mediaType) {
			return part.body
		}
	}
	return ""
}

type mimePart struct {
	headers string
	body    string
}

func splitParts(msg, boundary string) []mimePart {
	var out []mimePart
	chunks := strings.Split(msg, "--"+boundary)
	// chunks[0] is the top-level header block, not a part.
	for _, chunk := range chunks[1:] {
		headers, body, found := strings.Cut(chunk, "\r\n\r\n")
		if !found {
			continue
		}
		body = strings.TrimSuffix(strings.TrimSpace(body), "--")
		decoded, err := base64.StdEncoding.DecodeString(
			strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
		if err != nil {
			continue
		}
		out = append(out, mimePart{headers: headers, body: string(decoded)})
	}
	return out
}

func fmtSscan(s string, v *int) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errBadPort
		}
		n = n*10 + int(r-'0')
	}
	*v = n
	return 1, nil
}

const errBadPort sentinelErr = "not a number"

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

// Every mail this application sends goes through one layout, so a second and a
// third do not each bring their own copy of the table markup mail clients need.
func TestMailHTMLEscapesAndStaysSelfContained(t *testing.T) {
	out := MailHTML(MailBody{
		Greeting: `Hello "Ali" <ali>`,
		Body:     "A reply with <script>alert(1)</script> in it",
		Button:   "Open",
		Link:     "http://localhost:8080/support/1?a=1&b=2",
		Footer:   "Footer",
	})

	if strings.Contains(out, "<script>") {
		t.Error("the body was not escaped; a support reply is text somebody typed")
	}
	for _, want := range []string{"&quot;Ali&quot;", "&lt;script&gt;", "a=1&amp;b=2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in the rendered mail", want)
		}
	}
	// No remote anything: mail clients block it and some flag the message.
	for _, bad := range []string{"<style", "http://cdn", "<img"} {
		if strings.Contains(out, bad) {
			t.Errorf("the mail carries %q, which mail clients strip or distrust", bad)
		}
	}
	// The link is also printed as text, for clients that do not render buttons.
	if strings.Count(out, "localhost:8080/support/1") < 2 {
		t.Error("the link appears only in the button; a client that strips it leaves no way through")
	}
}

// A message with nothing to press is still a valid message — not every mail has
// a destination.
func TestMailHTMLWithoutAButton(t *testing.T) {
	out := MailHTML(MailBody{Greeting: "Hello", Body: "Something happened.", Footer: "Bye"})
	if strings.Contains(out, "<a href") {
		t.Error("a mail with no link rendered an anchor")
	}
	if !strings.Contains(out, "Something happened.") {
		t.Error("the body is missing")
	}
}

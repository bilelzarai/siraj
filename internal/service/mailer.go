package service

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/bilelzarai/siraj/internal/config"
)

// Mail is one outbound message. Both bodies are sent as a multipart/alternative
// so plain-text clients still get something readable.
type Mail struct {
	To       string
	ToName   string
	Subject  string
	HTMLBody string
	TextBody string
	Locale   string
}

// MailBody is the one shape every message this application sends: a greeting, a
// sentence, a button, and a line for the person who was not expecting it.
//
// One shape on purpose. The layout used to live beside the password reset, the
// only mail there was, so the second and third would each have brought their
// own copy of the table markup that mail clients need.
type MailBody struct {
	Greeting string
	Body     string
	Button   string
	Link     string
	Footer   string
}

// MailHTML renders a self-contained message. Mail clients strip <style>, so the
// styling is inline and the layout is a table, which is what survives them.
func MailHTML(b MailBody) string {
	esc := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
	}
	button := ""
	if b.Button != "" && b.Link != "" {
		button = `<tr><td align="center" style="padding-bottom:20px">` +
			`<a href="` + esc(b.Link) + `" style="display:inline-block;background:#0f7a5a;color:#ffffff;text-decoration:none;padding:12px 26px;border-radius:999px;font-size:14px;font-weight:600">` +
			esc(b.Button) + `</a></td></tr>`
	}
	tail := ""
	if b.Link != "" {
		tail = `<tr><td style="font-size:11px;color:#9aa8a4;padding-top:14px;word-break:break-all">` + esc(b.Link) + `</td></tr>`
	}
	return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f6f5;padding:28px 0">
<tr><td align="center">
<table role="presentation" width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:14px;padding:28px;font-family:system-ui,-apple-system,Segoe UI,sans-serif;color:#1a2220">
<tr><td style="font-size:17px;font-weight:600;padding-bottom:10px">` + esc(b.Greeting) + `</td></tr>
<tr><td style="font-size:14px;line-height:1.7;color:#4a5a56;padding-bottom:20px">` + esc(b.Body) + `</td></tr>
` + button + `<tr><td style="font-size:12px;line-height:1.6;color:#7b8b86">` + esc(b.Footer) + `</td></tr>
` + tail + `</table></td></tr></table>`
}

// Mailer sends mail over SMTP. With no host configured it logs the message
// instead, so every flow works in development without any mail server.
type Mailer struct {
	cfg config.SMTPConfig
}

func NewMailer(cfg config.SMTPConfig) *Mailer { return &Mailer{cfg: cfg} }

func (m *Mailer) Enabled() bool { return m.cfg.Enabled() }

// Send delivers one message. It is synchronous and bounded by a short timeout,
// because callers hold an HTTP request open.
func (m *Mailer) Send(ctx context.Context, msg Mail) error {
	if !m.cfg.Enabled() {
		slog.InfoContext(ctx, "mail not sent (SMTP disabled)",
			"to", msg.To, "subject", msg.Subject)
		return nil
	}

	raw, err := m.build(msg)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial smtp %s: %w", addr, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer client.Close()

	// STARTTLS when the server offers it. MailHog does not, which is why this
	// is conditional rather than mandatory.
	if m.cfg.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}

	if m.cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
	}

	if err := client.Mail(m.cfg.FromEmail); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		w.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}

	_ = client.Quit()
	slog.InfoContext(ctx, "mail sent", "to", msg.To, "subject", msg.Subject)
	return nil
}

// SendAsync delivers in the background. Invite flows use it so a slow or
// unreachable mail server never blocks the user's response.
func (m *Mailer) SendAsync(msg Mail) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := m.Send(ctx, msg); err != nil {
			slog.Error("async mail failed", "to", msg.To, "error", err)
		}
	}()
}

// build assembles RFC 5322 bytes. Everything that can carry non-ASCII — the
// display names, the subject, both bodies — is encoded explicitly, because the
// app's primary language is Arabic.
func (m *Mailer) build(msg Mail) ([]byte, error) {
	boundary, err := randomBoundary()
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	write := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\r\n", args...)
	}

	write("From: %s", formatAddress(m.cfg.FromName, m.cfg.FromEmail))
	write("To: %s", formatAddress(msg.ToName, msg.To))
	write("Subject: %s", mime.QEncoding.Encode("UTF-8", msg.Subject))
	write("Date: %s", time.Now().Format(time.RFC1123Z))
	write("MIME-Version: 1.0")
	if msg.Locale != "" {
		write("Content-Language: %s", msg.Locale)
	}
	write("Auto-Submitted: auto-generated")
	write(`Content-Type: multipart/alternative; boundary="%s"`, boundary)
	write("")

	text := msg.TextBody
	if text == "" {
		text = stripTags(msg.HTMLBody)
	}

	write("--%s", boundary)
	write(`Content-Type: text/plain; charset="UTF-8"`)
	write("Content-Transfer-Encoding: base64")
	write("")
	write("%s", wrapBase64(text))

	if msg.HTMLBody != "" {
		write("--%s", boundary)
		write(`Content-Type: text/html; charset="UTF-8"`)
		write("Content-Transfer-Encoding: base64")
		write("")
		write("%s", wrapBase64(msg.HTMLBody))
	}

	write("--%s--", boundary)
	return []byte(b.String()), nil
}

// formatAddress produces `"Display Name" <addr>`, MIME-encoding the name when
// it is not plain ASCII.
func formatAddress(name, addr string) string {
	if name == "" {
		return addr
	}
	return fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("UTF-8", name), addr)
}

func randomBoundary() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "siraj_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

// wrapBase64 encodes and folds to 76 characters, which some strict MTAs require.
func wrapBase64(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))

	var out strings.Builder
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		out.WriteString(enc[i:end])
		if end < len(enc) {
			out.WriteString("\r\n")
		}
	}
	return out.String()
}

// stripTags produces a crude plain-text fallback when a caller supplies only
// HTML. It is deliberately simple: templates always pass a real text body.
func stripTags(html string) string {
	var out strings.Builder
	depth := 0
	for _, r := range html {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(collapseSpace(out.String()))
}

func collapseSpace(s string) string {
	var out strings.Builder
	var lastSpace bool
	for _, r := range s {
		isSpace := r == ' ' || r == '\t' || r == '\n' || r == '\r'
		if isSpace {
			if !lastSpace {
				out.WriteByte(' ')
			}
			lastSpace = true
			continue
		}
		lastSpace = false
		out.WriteRune(r)
	}
	return out.String()
}

// Package mailer sends the few emails the app needs: password reset links and,
// later, support replies. In development it writes them to the log; with SMTP_ADDR set
// it sends them for real (Mailpit, Mailhog or a provider).
package mailer

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

type Mail struct {
	To      string
	Subject string
	Body    string
}

type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// Log prints the mail instead of sending it, so development needs no mail server.
type Log struct{}

func (Log) Send(ctx context.Context, m Mail) error {
	slog.InfoContext(ctx, "email (not sent: no SMTP_ADDR)", "to", m.To, "subject", m.Subject, "body", m.Body)
	return nil
}

// SMTP sends through a plain SMTP server.
type SMTP struct {
	Addr string // host:port
	From string
}

func (s SMTP) Send(ctx context.Context, m Mail) error {
	msg := strings.Join([]string{
		"From: " + s.From,
		"To: " + m.To,
		"Subject: " + m.Subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		m.Body,
	}, "\r\n")
	if err := smtp.SendMail(s.Addr, nil, s.From, []string{m.To}, []byte(msg)); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	return nil
}

// New picks the driver from the configuration.
func New(addr, from string) Mailer {
	if addr == "" {
		return Log{}
	}
	return SMTP{Addr: addr, From: from}
}

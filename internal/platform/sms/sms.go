// Package sms sends the one-time codes of the V5 sign-in flow.
//
// There is no provider wired yet, and pretending otherwise would be worse than
// saying so: the default driver writes the message to the log, exactly as the mailer
// does for e-mail in development. Point SMS_PROVIDER at a real gateway and implement
// Sender once; nothing above this package changes.
package sms

import (
	"context"
	"log/slog"
)

// Message is one text to one number.
type Message struct {
	To   string // E.164
	Body string
	// Call asks for the code to be read out loud instead of texted ("Call me
	// instead" in the design). A gateway that cannot do it should send the text.
	Call bool
	Lang string
}

// Sender is what the auth service needs.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Logger is the development driver: it writes the message where the developer can
// read it, so the flow can be walked end to end without a gateway account.
type Logger struct{}

func (Logger) Send(_ context.Context, m Message) error {
	how := "sms"
	if m.Call {
		how = "call"
	}
	slog.Info("sms (no provider configured: not actually sent)", "to", m.To, "how", how, "body", m.Body)
	return nil
}

// New returns the driver named by the configuration. Only the log driver exists so
// far; an unknown name falls back to it rather than failing to boot.
func New(provider string) Sender {
	switch provider {
	default:
		return Logger{}
	}
}

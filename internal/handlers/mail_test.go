package handlers_test

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bilelzarai/siraj/internal/models"
)

// fakeSMTP is a real SMTP server, small enough to run inside a test. The mail
// path is worth exercising over a socket rather than mocked away: what broke
// before was not the sending, it was that nothing ever asked for a send.
type fakeSMTP struct {
	addr     string
	mu       sync.Mutex
	received []string
	ln       net.Listener
}

func startSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{addr: ln.Addr().String(), ln: ln}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	say := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}

	say("220 fake ESMTP")
	var body strings.Builder
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")

		if inData {
			if trimmed == "." {
				inData = false
				s.mu.Lock()
				s.received = append(s.received, body.String())
				s.mu.Unlock()
				body.Reset()
				say("250 OK")
				continue
			}
			body.WriteString(trimmed + "\n")
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
			say("250-fake")
			say("250 SIZE 10240000")
		case strings.HasPrefix(trimmed, "MAIL FROM"), strings.HasPrefix(trimmed, "RCPT TO"):
			say("250 OK")
		case trimmed == "DATA":
			inData = true
			say("354 go ahead")
		case trimmed == "QUIT":
			say("221 bye")
			return
		default:
			say("250 OK")
		}
	}
}

// readable is the message as a mail client sees it: headers as sent, and every
// base64 part decoded. Matching the raw bytes would only ever prove that the
// encoder ran.
func readable(raw string) string {
	var out strings.Builder
	out.WriteString(raw)
	for _, part := range strings.Split(raw, "Content-Transfer-Encoding: base64") {
		body := strings.TrimSpace(part)
		if i := strings.Index(body, "\n\n"); i >= 0 {
			body = body[i+2:]
		}
		// Up to the next boundary.
		if i := strings.Index(body, "--"); i > 0 {
			body = body[:i]
		}
		cleaned := strings.Join(strings.Fields(body), "")
		if decoded, err := base64.StdEncoding.DecodeString(cleaned); err == nil {
			out.WriteString("\n")
			out.Write(decoded)
		}
	}
	return out.String()
}

// waitFor polls until one message matching want has arrived, because the
// application sends asynchronously — it will not hold a request open on a mail
// server.
func (s *fakeSMTP) waitFor(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, m := range s.received {
			if decoded := readable(m); strings.Contains(decoded, want) {
				s.mu.Unlock()
				return decoded
			}
		}
		s.mu.Unlock()
		time.Sleep(25 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("no mail containing %q arrived; %d message(s) did", want, len(s.received))
	return ""
}

// Opening a ticket has to reach the people who can answer it, and a staff reply
// has to reach the person who asked. Both were in-app only: a badge and a
// stream event, which reach nobody who is not already looking at the screen.
func TestSupportSendsMailBothWays(t *testing.T) {
	smtp := startSMTP(t)
	a := newAppWithSMTP(t, smtp.addr)

	a.register("mailstaff")
	a.promote("mailstaff", models.RoleAdmin)

	player := newAppSharing(t, a)
	player.register("mailplayer")

	// The player opens a ticket — staff should hear about it by mail.
	status, loc := player.post("/support/new", url.Values{
		"kind": {models.TicketQuestion}, "subject": {"Why is my streak wrong?"},
		"body": {"It reset after a correct answer."},
	})
	if status != 303 {
		t.Fatalf("opening a ticket → %d", status)
	}
	ticketID := strings.TrimPrefix(loc, "/support/")

	got := smtp.waitFor(t, "Why is my streak wrong?")
	if !strings.Contains(got, "mailstaff@example.com") {
		t.Errorf("the new-ticket mail did not go to staff:\n%s", firstLines(got, 12))
	}
	if !strings.Contains(got, "/admin/support/"+ticketID) {
		t.Error("the new-ticket mail carries no link to the ticket")
	}

	// Staff reply — the player should hear about it by mail.
	if status, _ := a.post("/admin/support/"+ticketID+"/reply", url.Values{
		"body": {"Fixed it, thank you for the report."},
	}); status != 303 {
		t.Fatalf("staff reply → %d", status)
	}

	reply := smtp.waitFor(t, "Fixed it, thank you for the report")
	if !strings.Contains(reply, "mailplayer@example.com") {
		t.Errorf("the reply mail did not go to the person who asked:\n%s", firstLines(reply, 12))
	}
	if !strings.Contains(reply, "/support/"+ticketID) {
		t.Error("the reply mail carries no link back to the conversation")
	}
}

// An internal note is for staff. It must never be mailed to the player.
func TestInternalNoteIsNotMailedToThePlayer(t *testing.T) {
	smtp := startSMTP(t)
	a := newAppWithSMTP(t, smtp.addr)
	a.register("noteadmin")
	a.promote("noteadmin", models.RoleAdmin)

	player := newAppSharing(t, a)
	player.register("noteplayer")
	_, loc := player.post("/support/new", url.Values{
		"kind":    {models.TicketQuestion},
		"subject": {"A question about notes"},
		"body":    {"Asking something that needs an internal note."},
	})
	ticketID := strings.TrimPrefix(loc, "/support/")
	smtp.waitFor(t, "A question about notes") // the staff notification

	if status, _ := a.post("/admin/support/"+ticketID+"/reply", url.Values{
		"body": {"SECRET internal note about this person"}, "internal": {"1"},
	}); status != 303 {
		t.Fatalf("internal note → %d", status)
	}

	time.Sleep(300 * time.Millisecond)
	smtp.mu.Lock()
	defer smtp.mu.Unlock()
	for _, m := range smtp.received {
		if strings.Contains(readable(m), "SECRET internal note") {
			t.Fatal("an internal note was emailed out")
		}
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return fmt.Sprint(strings.Join(lines, "\n"))
}

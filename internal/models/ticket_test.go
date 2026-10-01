package models

import "testing"

// What a ticket is doing, from the point of view of the person who opened it.
// The status enum answers a different question — it is for triage, and
// "waiting_user" tells staff something while telling its owner nothing.
func TestTicketStageReadsForItsOwner(t *testing.T) {
	cases := []struct {
		name   string
		ticket Ticket
		want   string
	}{
		{"just opened", Ticket{Status: TicketOpen, LastSender: "user"}, TicketStageNew},
		{"somebody has it", Ticket{Status: TicketInProgress, LastSender: "user"}, TicketStageWorking},
		{"staff answered", Ticket{Status: TicketOpen, LastSender: "staff"}, TicketStageYourTurn},
		{"explicitly waiting", Ticket{Status: TicketWaitingUser, LastSender: "user"}, TicketStageYourTurn},
		{"resolved", Ticket{Status: TicketResolved, LastSender: "staff"}, TicketStageSettled},
		{"closed", Ticket{Status: TicketClosed, LastSender: "user"}, TicketStageSettled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ticket.Stage(); got != tc.want {
				t.Errorf("stage = %q, want %q", got, tc.want)
			}
		})
	}
}

// The one thing a list of tickets has to make obvious is whose move it is.
func TestNeedsYou(t *testing.T) {
	answered := Ticket{Status: TicketOpen, LastSender: "staff"}
	if !answered.NeedsYou() {
		t.Error("a ticket with a staff reply is not marked as needing an answer")
	}
	waiting := Ticket{Status: TicketOpen, LastSender: "user"}
	if waiting.NeedsYou() {
		t.Error("a ticket waiting on staff was marked as needing the player")
	}
	// A settled ticket needs nothing from anybody.
	done := Ticket{Status: TicketClosed, LastSender: "staff"}
	if done.NeedsYou() {
		t.Error("a closed ticket was marked as needing an answer")
	}
}

// Every stage has something to say about what happens next — a screen that
// names a state and leaves the reader to guess the consequence is half a
// sentence.
func TestEveryStageHasBothKeys(t *testing.T) {
	for _, stage := range []string{TicketStageNew, TicketStageWorking, TicketStageYourTurn, TicketStageSettled} {
		ticket := Ticket{}
		switch stage {
		case TicketStageNew:
			ticket = Ticket{Status: TicketOpen, LastSender: "user"}
		case TicketStageWorking:
			ticket = Ticket{Status: TicketInProgress, LastSender: "user"}
		case TicketStageYourTurn:
			ticket = Ticket{Status: TicketWaitingUser}
		case TicketStageSettled:
			ticket = Ticket{Status: TicketClosed}
		}
		if got := ticket.StageKey(); got != "support.stage."+stage {
			t.Errorf("%s: stage key = %q", stage, got)
		}
		if got := ticket.StageNextKey(); got != "support.next."+stage {
			t.Errorf("%s: next key = %q", stage, got)
		}
	}
}

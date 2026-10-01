package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A duel outcome has to reach the other player's open screen, not only their
// notification row. This was the one kind of news in the application that was
// written to the database and never published — so the opponent's badge moved
// and their scoreboard did not.
func TestDuelOutcomeReachesTheStream(t *testing.T) {
	hub := NewHub()
	other := uuid.New()
	events, unsubscribe := hub.Subscribe(other)
	defer unsubscribe()

	hub.Publish(other, Event{Type: "challenge.completed", ChallengeID: "abc"})

	select {
	case ev := <-events:
		if ev.Type != "challenge.completed" {
			t.Errorf("type = %q", ev.Type)
		}
		if ev.ChallengeID != "abc" {
			t.Errorf("the event does not say which duel it is about: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing arrived on the stream")
	}
}

// Two tabs are two subscribers, and both have to hear it.
func TestHubReachesEveryOpenTab(t *testing.T) {
	hub := NewHub()
	user := uuid.New()
	first, closeFirst := hub.Subscribe(user)
	second, closeSecond := hub.Subscribe(user)
	defer closeFirst()
	defer closeSecond()

	hub.Publish(user, Event{Type: "message.new"})

	for i, ch := range []chan Event{first, second} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("tab %d heard nothing", i+1)
		}
	}
}

// And nobody else's.
func TestHubDoesNotLeakToOtherPeople(t *testing.T) {
	hub := NewHub()
	mine, done := hub.Subscribe(uuid.New())
	defer done()

	hub.Publish(uuid.New(), Event{Type: "message.new"})

	select {
	case ev := <-mine:
		t.Fatalf("received somebody else's event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

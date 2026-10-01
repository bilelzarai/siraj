package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/repository"
)

func thread(t *testing.T, a, b uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := repo(t).EnsureConversation(t.Context(), a, b)
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	return id
}

// Taking a message back leaves a tombstone. Removing the row outright would
// take a sentence out of the other person's thread after they had read it and
// replied, leaving an answer to nothing.
func TestWithdrawMessageLeavesATombstone(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	me := newUser(t, "msg_me", "msg.me@example.com")
	them := newUser(t, "msg_them", "msg.them@example.com")
	conv := thread(t, me.ID, them.ID)

	sent, err := r.SendMessage(ctx, conv, me.ID, repository.NewMessage{Body: "Something I regret"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// Only its sender.
	if err := r.WithdrawMessage(ctx, sent.ID, them.ID); err == nil {
		t.Error("somebody withdrew a message they did not send")
	}
	if err := r.WithdrawMessage(ctx, sent.ID, me.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	messages, err := r.Messages(ctx, conv, me.ID, 0, 50)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want the tombstone kept", len(messages))
	}
	if !messages[0].Withdrawn() {
		t.Error("the message is not marked as withdrawn")
	}
	if messages[0].Body != "" {
		t.Errorf("the text survived withdrawal: %q", messages[0].Body)
	}
	// And twice is not an error worth showing.
	if err := r.WithdrawMessage(ctx, sent.ID, me.ID); err == nil {
		t.Error("withdrawing twice reported success")
	}
}

// A withdrawn message is not unread news, and is not the preview either.
func TestWithdrawnMessagesLeaveTheCounts(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	me := newUser(t, "count_me", "count.me@example.com")
	them := newUser(t, "count_them", "count.them@example.com")
	conv := thread(t, me.ID, them.ID)

	first, err := r.SendMessage(ctx, conv, them.ID, repository.NewMessage{Body: "Read this"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := r.SendMessage(ctx, conv, them.ID, repository.NewMessage{Body: "And this"}); err != nil {
		t.Fatalf("send: %v", err)
	}

	if n, _ := r.TotalUnread(ctx, me.ID); n != 2 {
		t.Fatalf("unread = %d, want 2", n)
	}
	if err := r.WithdrawMessage(ctx, first.ID, them.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if n, _ := r.TotalUnread(ctx, me.ID); n != 1 {
		t.Errorf("unread = %d after one was withdrawn, want 1", n)
	}
}

// The divider goes above the first message the reader had not seen.
func TestFirstUnreadIsWhereTheyLeftOff(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	me := newUser(t, "unread_me", "unread.me@example.com")
	them := newUser(t, "unread_them", "unread.them@example.com")
	conv := thread(t, me.ID, them.ID)

	if _, err := r.SendMessage(ctx, conv, them.ID, repository.NewMessage{Body: "Older"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := r.MarkRead(ctx, conv, me.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	newer, err := r.SendMessage(ctx, conv, them.ID, repository.NewMessage{Body: "Newer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	got, err := r.FirstUnreadID(ctx, conv, me.ID)
	if err != nil {
		t.Fatalf("first unread: %v", err)
	}
	if got != newer.ID {
		t.Errorf("divider at %d, want the first unseen message %d", got, newer.ID)
	}

	// Nothing unread is not an error, it is zero.
	if _, err := r.MarkRead(ctx, conv, me.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if got, err := r.FirstUnreadID(ctx, conv, me.ID); err != nil || got != 0 {
		t.Errorf("first unread = %d, %v; want 0 and no error", got, err)
	}
}

// Searching a thread finds text and never surfaces a withdrawn message.
func TestSearchMessages(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	me := newUser(t, "find_me", "find.me@example.com")
	them := newUser(t, "find_them", "find.them@example.com")
	conv := thread(t, me.ID, them.ID)

	for _, body := range []string{"the battle of Badr", "what time is iftar", "nothing to see"} {
		if _, err := r.SendMessage(ctx, conv, me.ID, repository.NewMessage{Body: body}); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	gone, err := r.SendMessage(ctx, conv, me.ID, repository.NewMessage{Body: "Badr again, withdrawn"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := r.WithdrawMessage(ctx, gone.ID, me.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	found, err := r.SearchMessages(ctx, conv, me.ID, "badr", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d, want only the one that is still there", len(found))
	}
	if found[0].Body != "the battle of Badr" {
		t.Errorf("found %q", found[0].Body)
	}
	if empty, err := r.SearchMessages(ctx, conv, me.ID, "   ", 20); err != nil || len(empty) != 0 {
		t.Errorf("a blank search returned %d rows", len(empty))
	}
}

// A receipt is news for the sender: their tick moves from one to two when the
// other person opens the thread. Nothing published that, so it moved on their
// next reload — which is to say, once it no longer mattered.
func TestReadingAThreadIsVisibleToTheSender(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	sender := newUser(t, "receipt_from", "receipt.from@example.com")
	reader := newUser(t, "receipt_to", "receipt.to@example.com")
	conv := thread(t, sender.ID, reader.ID)

	sent, err := r.SendMessage(ctx, conv, sender.ID, repository.NewMessage{Body: "Did you see this?"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.Read() {
		t.Fatal("a message was read before anybody opened it")
	}

	// What the sender sees before and after the other person opens the thread.
	before, err := r.Messages(ctx, conv, sender.ID, 0, 10)
	if err != nil || len(before) != 1 || before[0].Read() {
		t.Fatalf("before reading: %+v, %v", before, err)
	}

	if _, err := r.MarkRead(ctx, conv, reader.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	after, err := r.Messages(ctx, conv, sender.ID, 0, 10)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if !after[0].Read() {
		t.Error("the message is still unread after the other person opened the thread")
	}

	// And reading your own does nothing: the tick is about them, not you.
	own, err := r.SendMessage(ctx, conv, reader.ID, repository.NewMessage{Body: "Yes"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := r.MarkRead(ctx, conv, reader.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	fresh, err := r.Messages(ctx, conv, reader.ID, own.ID-1, 10)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(fresh) > 0 && fresh[0].Read() {
		t.Error("opening a thread marked your own message as read by you")
	}
}

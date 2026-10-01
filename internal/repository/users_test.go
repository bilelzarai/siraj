package repository_test

import (
	"context"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// newUser inserts an account the way registration does — the address already
// lowercased — and returns it.
func newUser(t *testing.T, username, email string) *models.User {
	t.Helper()
	r := repo(t)
	u := &models.User{
		Username:     username,
		Email:        email,
		PasswordHash: "x",
		DisplayName:  username,
		AvatarSeed:   username,
		Locale:       "en",
	}
	if err := r.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

// The login form takes one field for either a username or an email, and phones
// capitalise the first letter of an address by default. Matching the address
// exactly means those people are told their credentials are wrong — while the
// password-reset form directly below, which lowercases, accepts the same
// string and mails them a link.
func TestUserByIdentifierMatchesEmailWhateverTheCase(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	want := newUser(t, "case_probe", "case.probe@example.com")

	for _, typed := range []string{
		"case.probe@example.com",
		"Case.Probe@Example.com",
		"CASE.PROBE@EXAMPLE.COM",
	} {
		got, err := r.UserByIdentifier(ctx, typed)
		if err != nil {
			t.Fatalf("%q: %v", typed, err)
		}
		if got.ID != want.ID {
			t.Errorf("%q resolved to %s, want %s", typed, got.Username, want.Username)
		}
	}
}

// The username half of the same field is matched exactly, which is the
// established behaviour: usernames are shown as chosen and are part of a
// profile URL.
func TestUserByIdentifierStillMatchesUsername(t *testing.T) {
	r := repo(t)
	want := newUser(t, "name_probe", "name.probe@example.com")

	got, err := r.UserByIdentifier(context.Background(), "name_probe")
	if err != nil {
		t.Fatalf("by username: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("resolved to %s, want %s", got.Username, want.Username)
	}
}

// Every badge the navigation shows, in one round trip. Five separate questions
// before each page render is five network hops the moment the database stops
// being a unix socket.
func TestBadgeCountsAnswersEverythingAtOnce(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	me := newUser(t, "badge_me", "badge.me@example.com")
	them := newUser(t, "badge_them", "badge.them@example.com")

	// One friend request waiting on me.
	if err := r.RequestFriendship(ctx, them.ID, me.ID); err != nil {
		t.Fatalf("request friendship: %v", err)
	}
	// One unread notification.
	if err := r.Notify(ctx, me.ID, "friend.request", map[string]any{"user_id": them.ID.String()}); err != nil {
		t.Fatalf("notify: %v", err)
	}

	counts, err := r.BadgeCounts(ctx, me.ID)
	if err != nil {
		t.Fatalf("badge counts: %v", err)
	}
	if counts.FriendRequests != 1 {
		t.Errorf("friend requests = %d, want 1", counts.FriendRequests)
	}
	if counts.Notifications != 1 {
		t.Errorf("notifications = %d, want 1", counts.Notifications)
	}
	if counts.UnreadMessages != 0 || counts.PendingChallenges != 0 || counts.SupportUnread != 0 {
		t.Errorf("counts = %+v, want zeros for what has not happened", counts)
	}

	// And it agrees with the single-purpose queries it replaced.
	if n, err := r.IncomingRequestCount(ctx, me.ID); err != nil || n != counts.FriendRequests {
		t.Errorf("IncomingRequestCount = %d, %v; batched said %d", n, err, counts.FriendRequests)
	}
	if n, err := r.UnreadNotificationCount(ctx, me.ID); err != nil || n != counts.Notifications {
		t.Errorf("UnreadNotificationCount = %d, %v; batched said %d", n, err, counts.Notifications)
	}
}

// Blocking existed everywhere except in a write: the enum value, the constants,
// the search branch, the relation switch and the red tag were all unreachable.
func TestBlockingIsReachableAndStopsEverything(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	me := newUser(t, "block_me", "block.me@example.com")
	pest := newUser(t, "block_pest", "block.pest@example.com")

	// They are friends first, which is the case that matters: a block has to
	// end an existing relationship, not merely refuse a new one.
	if err := r.RequestFriendship(ctx, pest.ID, me.ID); err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := r.RespondToFriendship(ctx, pest.ID, me.ID, true); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if friends, err := r.AreFriends(ctx, me.ID, pest.ID); err != nil || !friends {
		t.Fatalf("AreFriends = %v, %v; want them friends to start with", friends, err)
	}

	if err := r.BlockUser(ctx, me.ID, pest.ID); err != nil {
		t.Fatalf("block: %v", err)
	}

	blocked, err := r.IsBlockedBetween(ctx, pest.ID, me.ID)
	if err != nil || !blocked {
		t.Errorf("IsBlockedBetween = %v, %v; want true in either direction", blocked, err)
	}
	if friends, err := r.AreFriends(ctx, me.ID, pest.ID); err != nil || friends {
		t.Errorf("AreFriends = %v after a block, want false", friends)
	}
	// And they cannot ask to be friends again.
	if err := r.RequestFriendship(ctx, pest.ID, me.ID); err == nil {
		t.Error("a blocked person sent a friend request")
	}

	rel, err := r.Relation(ctx, me.ID, pest.ID)
	if err != nil || rel != models.RelationBlocked {
		t.Errorf("Relation = %q, %v; want blocked", rel, err)
	}

	if err := r.UnblockUser(ctx, me.ID, pest.ID); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if blocked, err := r.IsBlockedBetween(ctx, me.ID, pest.ID); err != nil || blocked {
		t.Errorf("still blocked after unblocking: %v, %v", blocked, err)
	}
	// Unblocking leaves strangers, not friends: what they had was ended.
	if friends, err := r.AreFriends(ctx, me.ID, pest.ID); err != nil || friends {
		t.Errorf("AreFriends = %v after unblocking, want false", friends)
	}
}

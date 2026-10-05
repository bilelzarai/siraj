package repository_test

import (
	"context"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// Every list of people is scanned by collectUserCards, which reads eight
// columns. A query that selects seven answers with
// "number of field descriptions must equal number of destinations", and the
// screens that show these lists discard the error and render an empty panel —
// so the mistake is invisible on the page and silent in the log. Two queries
// had drifted that way: the people search and the support assignment menu.
//
// The guard is the row, not the call: pgx only compares the two counts when
// there is something to scan, so every query below is given a person to find.
func TestEveryUserCardQueryScansWhatItSelects(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	viewer := newUser(t, "card_viewer", "card.viewer@example.com")
	friend := newUser(t, "card_friend", "card.friend@example.com")
	asking := newUser(t, "card_asking", "card.asking@example.com")
	asked := newUser(t, "card_asked", "card.asked@example.com")
	staff := newUser(t, "card_staff", "card.staff@example.com")

	// Accepted both ways, so the friend lists have a row.
	if err := r.RequestFriendship(ctx, viewer.ID, friend.ID); err != nil {
		t.Fatalf("request friendship: %v", err)
	}
	if err := r.RespondToFriendship(ctx, viewer.ID, friend.ID, true); err != nil {
		t.Fatalf("accept friendship: %v", err)
	}
	// One request waiting on the viewer, one waiting on somebody else.
	if err := r.RequestFriendship(ctx, asking.ID, viewer.ID); err != nil {
		t.Fatalf("incoming request: %v", err)
	}
	if err := r.RequestFriendship(ctx, viewer.ID, asked.ID); err != nil {
		t.Fatalf("outgoing request: %v", err)
	}
	if err := r.SetUserRole(ctx, staff.ID, models.RoleModerator); err != nil {
		t.Fatalf("set role: %v", err)
	}

	// A room with both of them standing in it. The owner of a room is not
	// seated by CreateThread, so the viewer joins like everybody else.
	room, err := r.CreateThread(ctx, models.ConversationRoom, "Card room", "", viewer.ID, nil)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if _, err := r.JoinThread(ctx, room.ID, viewer.ID); err != nil {
		t.Fatalf("viewer joins room: %v", err)
	}
	if _, err := r.JoinThread(ctx, room.ID, friend.ID); err != nil {
		t.Fatalf("friend joins room: %v", err)
	}

	cards := []struct {
		name string
		run  func() ([]*models.UserCard, error)
	}{
		{"SearchUsers", func() ([]*models.UserCard, error) {
			return r.SearchUsers(ctx, viewer.ID, "card_friend", 25)
		}},
		{"Friends", func() ([]*models.UserCard, error) {
			return r.Friends(ctx, viewer.ID)
		}},
		{"SearchFriends", func() ([]*models.UserCard, error) {
			return r.SearchFriends(ctx, viewer.ID, "", 25, 0)
		}},
		{"IncomingRequests", func() ([]*models.UserCard, error) {
			return r.IncomingRequests(ctx, viewer.ID)
		}},
		{"OutgoingRequests", func() ([]*models.UserCard, error) {
			return r.OutgoingRequests(ctx, viewer.ID)
		}},
		{"StaffMembers", func() ([]*models.UserCard, error) {
			return r.StaffMembers(ctx)
		}},
		{"Members", func() ([]*models.UserCard, error) {
			return r.Members(ctx, room.ID)
		}},
		{"RoomPeers", func() ([]*models.UserCard, error) {
			return r.RoomPeers(ctx, viewer.ID, "", 0, 0)
		}},
	}
	for _, c := range cards {
		got, err := c.run()
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s returned nothing, so nothing was scanned and the "+
				"column count was never checked — fix the fixture", c.name)
		}
	}

	// The two single-row lookups, scanned by hand rather than by the collector.
	if _, err := r.UserCardByUsername(ctx, friend.Username); err != nil {
		t.Errorf("UserCardByUsername: %v", err)
	}
	if _, err := r.UserCardInMyRoom(ctx, viewer.ID, friend.Username); err != nil {
		t.Errorf("UserCardInMyRoom: %v", err)
	}

	// attachMembers fills the faces on a room in the conversation list, and
	// selects the same eight columns with the conversation id in front.
	convs, err := r.Conversations(ctx, viewer.ID)
	if err != nil {
		t.Fatalf("conversations: %v", err)
	}
	var seated bool
	for _, c := range convs {
		if c.ID == room.ID && len(c.Members) > 0 {
			seated = true
		}
	}
	if !seated {
		t.Error("attachMembers put nobody in the room, so its scan never ran")
	}
}

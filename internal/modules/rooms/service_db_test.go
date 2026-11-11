package rooms

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/dbtest"
	"siraj/internal/db/sqlc"
	"siraj/internal/modules/catalog"
	"siraj/internal/modules/games"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

// newRooms is the rooms service over the test database, with the live hub it needs so
// announcing a change does not reach into nothing.
func newRooms(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	d := dbtest.Open(t)
	svc := NewService(d, games.NewService(d, catalog.NewService(d)))
	svc.SetLive(live.NewHub())
	return svc, d
}

// bankDomain writes a domain with enough published questions for a room to deal a
// real game. Nothing is seeded by the migrations, so the test writes its own content.
func bankDomain(t *testing.T, d *db.DB) int64 {
	t.Helper()
	return dbtest.Seed(t, d, 10).DomainID
}

func guest(t *testing.T, d *db.DB, name string) *authctx.Principal {
	t.Helper()
	row, err := d.Q.CreateGuest(context.Background(), sqlc.CreateGuestParams{
		DisplayName: name, Lang: "en",
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return &authctx.Principal{ID: row.ID, Kind: string(row.Kind), Lang: "en"}
}

// A room of four takes sides, the host deals them again, and starting the game writes
// the two sides onto the player rows (RM-05, ★ game_players.team).
func TestTeamsSurviveFromTheLobbyIntoTheGame(t *testing.T) {
	svc, d := newRooms(t)
	ctx := context.Background()

	host := guest(t, d, "Aisha T.")
	scope := games.Scope{DomainID: bankDomain(t, d), Kind: "custom"}
	room, err := svc.Host(ctx, HostInput{Who: host, Scope: scope, Count: 5, Seconds: 20, MaxPlayers: 8, Mode: ModeTeams})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Omar K.", "Khadija M.", "Bilal R."} {
		if err := svc.Join(ctx, guest(t, d, name), room); err != nil {
			t.Fatal(err)
		}
	}
	room, err = svc.ByCode(ctx, room.Code)
	if err != nil {
		t.Fatal(err)
	}

	// Four players, so the room really plays in teams.
	members, _ := svc.Members(ctx, room)
	if got := svc.Mode(svc.Settings(room), len(members)); got != ModeTeams {
		t.Fatalf("a room of four set to teams plays %s", got)
	}

	// The host deals the sides again; both come back filled and even.
	if err := svc.Shuffle(ctx, host, room); err != nil {
		t.Fatal(err)
	}
	room, _ = svc.ByCode(ctx, room.Code)
	sides, err := svc.Teams(ctx, room)
	if err != nil {
		t.Fatal(err)
	}
	if len(sides[0]) != 2 || len(sides[1]) != 2 {
		t.Fatalf("shuffle gave %d v %d", len(sides[0]), len(sides[1]))
	}

	// Only the host may deal them.
	other := guest(t, d, "Someone else")
	if err := svc.Shuffle(ctx, other, room); err == nil {
		t.Error("a player who is not the host shuffled the teams")
	}

	game, err := svc.Start(ctx, host, room)
	if err != nil {
		t.Fatalf("starting the room: %v", err)
	}
	rows, err := svc.Standings(ctx, host, game.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("want four players on the game, got %d", len(rows))
	}
	if !HasTeams(rows) {
		t.Fatal("the game did not keep the room's two sides")
	}
	_, _, counts := TeamTotals(rows)
	if counts != [2]int{2, 2} {
		t.Fatalf("the sides reached the game as %v", counts)
	}
	for _, r := range rows {
		if got := sides.Of(r.UserID); got != int(r.Team) {
			t.Errorf("%s was on side %d in the lobby and %d in the game", r.Name, got, r.Team)
		}
	}
}

// A room that drops below four players starts for itself instead of two against one,
// and nobody carries a side into the game (RM-05).
func TestAShortRoomStartsForItself(t *testing.T) {
	svc, d := newRooms(t)
	ctx := context.Background()

	host := guest(t, d, "Host")
	scope := games.Scope{DomainID: bankDomain(t, d), Kind: "custom"}
	room, err := svc.Host(ctx, HostInput{Who: host, Scope: scope, Count: 5, Seconds: 20, MaxPlayers: 8, Mode: ModeTeams})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Join(ctx, guest(t, d, "Only friend"), room); err != nil {
		t.Fatal(err)
	}
	room, _ = svc.ByCode(ctx, room.Code)

	game, err := svc.Start(ctx, host, room)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.Standings(ctx, host, game.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if HasTeams(rows) {
		t.Fatal("a room of two started in teams")
	}
	for _, r := range rows {
		if r.Team != -1 {
			t.Errorf("%s carries side %d into a game played for itself", r.Name, r.Team)
		}
	}
}

package perm_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"siraj/internal/db"
	"siraj/internal/db/dbtest"
	"siraj/internal/modules/perm"
	"siraj/internal/platform/apperr"
)

// §7 — ownership, visibility and sharing, and §25 #9 — share and revoke.

func setup(t *testing.T) (*perm.Service, *db.DB, context.Context) {
	t.Helper()
	d := dbtest.Open(t)
	t.Cleanup(d.Close)
	return perm.NewService(d), d, context.Background()
}

func user(t *testing.T, d *db.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := d.Pool.Exec(context.Background(), `
		INSERT INTO users (id, kind, display_name, email, lang)
		VALUES ($1, 'member', $2, $3, 'en')`,
		id, "T"+id.String()[:6], id.String()[:8]+"@t.local")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// node writes one directly: these tests are about who may touch it, not about how it
// is built.
func node(t *testing.T, d *db.DB, owner uuid.UUID, visibility, status string) int64 {
	t.Helper()
	var id int64
	var o any
	if owner != uuid.Nil {
		o = owner
	}
	err := d.Pool.QueryRow(context.Background(), `
		INSERT INTO nodes (owner_id, slug, node_type, visibility, status)
		VALUES ($1, $2, 'node', $3::node_visibility, $4::node_status) RETURNING id`,
		o, "n-"+uuid.NewString()[:8], visibility, status).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func res(id int64, owner uuid.UUID, visibility, status string) perm.Resource {
	return perm.Resource{Kind: "node", ID: id, OwnerID: owner, Visibility: visibility, Status: status}
}

func can(t *testing.T, s *perm.Service, ctx context.Context, who perm.Subject, a perm.Action, r perm.Resource) bool {
	t.Helper()
	ok, err := s.Can(ctx, who, a, r)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// Official content: read and play what is published, never change it, whoever asks.
func TestOfficialContentIsReadOnlyToPlayers(t *testing.T) {
	s, d, ctx := setup(t)
	id := node(t, d, uuid.Nil, "public", "published")
	r := res(id, uuid.Nil, "public", "published")
	player := perm.Subject{ID: user(t, d)}
	guest := perm.Subject{}

	for _, c := range []struct {
		who  perm.Subject
		a    perm.Action
		want bool
		why  string
	}{
		{guest, perm.View, true, "a visitor may read published official content"},
		{guest, perm.Play, true, "a visitor may play it"},
		{guest, perm.Create, false, "a visitor may not build under it"},
		{player, perm.View, true, "a player may read it"},
		{player, perm.Create, true, "a player may extend it"},
		{player, perm.Edit, false, "a player may not edit official content"},
		{player, perm.Delete, false, "a player may not delete it"},
		{player, perm.Share, false, "a player may not share official content"},
		{perm.Subject{Staff: true}, perm.Edit, true, "staff manage official content"},
	} {
		if got := can(t, s, ctx, c.who, c.a, r); got != c.want {
			t.Errorf("%s: %s = %v, want %v", c.why, c.a, got, c.want)
		}
	}
}

// An unpublished official node is invisible, so even reading it is refused.
func TestUnpublishedOfficialContentIsInvisible(t *testing.T) {
	s, d, ctx := setup(t)
	id := node(t, d, uuid.Nil, "public", "draft")
	r := res(id, uuid.Nil, "public", "draft")
	player := perm.Subject{ID: user(t, d)}

	if can(t, s, ctx, player, perm.View, r) {
		t.Error("a player can see an unpublished official node")
	}
	if can(t, s, ctx, player, perm.Create, r) {
		t.Error("a player can extend an unpublished official node")
	}
	// And the error says not-found, so the node does not announce itself.
	err := s.Ensure(ctx, player, perm.View, r)
	if e, ok := apperr.As(err); !ok || e.Status != 404 {
		t.Errorf("want 404, got %v", err)
	}
}

// §25 #9 — a share opens a branch at a level, and revoking closes it.
func TestShareAndRevoke(t *testing.T) {
	s, d, ctx := setup(t)
	owner, friend := user(t, d), user(t, d)
	id := node(t, d, owner, "private", "draft")
	r := res(id, owner, "private", "draft")
	them := perm.Subject{ID: friend}

	if can(t, s, ctx, them, perm.View, r) {
		t.Fatal("a private branch was readable before it was shared")
	}
	if err := s.Grant(ctx, perm.Subject{ID: owner}, r, friend, "play"); err != nil {
		t.Fatal(err)
	}
	if !can(t, s, ctx, them, perm.View, r) {
		t.Error("after a play share they still cannot view")
	}
	if !can(t, s, ctx, them, perm.Play, r) {
		t.Error("after a play share they cannot play")
	}
	if can(t, s, ctx, them, perm.Edit, r) {
		t.Error("a play share let them edit")
	}

	// Raising the level reaches further; it never changes who owns the node.
	if err := s.Grant(ctx, perm.Subject{ID: owner}, r, friend, "edit"); err != nil {
		t.Fatal(err)
	}
	if !can(t, s, ctx, them, perm.Edit, r) {
		t.Error("an edit share does not allow editing")
	}
	if can(t, s, ctx, them, perm.Share, r) {
		t.Error("an edit share let them share it on")
	}

	if err := s.Revoke(ctx, perm.Subject{ID: owner}, r, friend); err != nil {
		t.Fatal(err)
	}
	if can(t, s, ctx, them, perm.View, r) {
		t.Error("access survived being revoked")
	}
}

// A share reaches the whole branch: sharing a subject must not mean sharing each
// lesson under it one at a time.
func TestShareReachesTheBranch(t *testing.T) {
	s, d, ctx := setup(t)
	owner, friend := user(t, d), user(t, d)
	ctxB := context.Background()

	top := node(t, d, owner, "private", "draft")
	var child, grandchild int64
	if err := d.Pool.QueryRow(ctxB, `
		INSERT INTO nodes (parent_id, owner_id, slug, node_type) VALUES ($1, $2, 'c', 'node')
		RETURNING id`, top, owner).Scan(&child); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctxB, `
		INSERT INTO nodes (parent_id, owner_id, slug, node_type) VALUES ($1, $2, 'g', 'node')
		RETURNING id`, child, owner).Scan(&grandchild); err != nil {
		t.Fatal(err)
	}

	if err := s.Grant(ctx, perm.Subject{ID: owner}, res(top, owner, "private", "draft"), friend, "view"); err != nil {
		t.Fatal(err)
	}
	them := perm.Subject{ID: friend}
	for _, id := range []int64{child, grandchild} {
		if !can(t, s, ctx, them, perm.View, res(id, owner, "private", "draft")) {
			t.Errorf("node %d under a shared branch is not readable", id)
		}
	}
	// A sibling branch that was never shared stays closed.
	other := node(t, d, owner, "private", "draft")
	if can(t, s, ctx, them, perm.View, res(other, owner, "private", "draft")) {
		t.Error("an unshared branch of the same owner was readable")
	}
}

// Only somebody who may manage access can hand it out.
func TestOnlyManagersCanShare(t *testing.T) {
	s, d, ctx := setup(t)
	owner, friend, stranger := user(t, d), user(t, d), user(t, d)
	id := node(t, d, owner, "private", "draft")
	r := res(id, owner, "private", "draft")

	if err := s.Grant(ctx, perm.Subject{ID: stranger}, r, friend, "view"); err == nil {
		t.Error("a stranger shared somebody else's branch")
	}
	// Even an edit share does not carry the right to share it on.
	if err := s.Grant(ctx, perm.Subject{ID: owner}, r, friend, "edit"); err != nil {
		t.Fatal(err)
	}
	if err := s.Grant(ctx, perm.Subject{ID: friend}, r, stranger, "view"); err == nil {
		t.Error("an edit share let somebody pass access on")
	}
}

// Another player's public branch is readable and playable, and nothing more.
func TestPublicBranchesAreReadOnly(t *testing.T) {
	s, d, ctx := setup(t)
	owner := user(t, d)
	id := node(t, d, owner, "public", "published")
	r := res(id, owner, "public", "published")
	them := perm.Subject{ID: user(t, d)}

	if !can(t, s, ctx, them, perm.View, r) {
		t.Error("a public branch is not readable")
	}
	if !can(t, s, ctx, them, perm.Play, r) {
		t.Error("a public branch is not playable")
	}
	for _, a := range []perm.Action{perm.Edit, perm.Delete, perm.Share, perm.Manage} {
		if can(t, s, ctx, them, a, r) {
			t.Errorf("a public branch allowed %s", a)
		}
	}
}

// Moderation is a platform role: no share can grant it.
func TestModerationIsNotShareable(t *testing.T) {
	s, d, ctx := setup(t)
	owner, friend := user(t, d), user(t, d)
	id := node(t, d, owner, "private", "draft")
	r := res(id, owner, "private", "draft")

	if err := s.Grant(ctx, perm.Subject{ID: owner}, r, friend, "manage"); err != nil {
		t.Fatal(err)
	}
	if can(t, s, ctx, perm.Subject{ID: friend}, perm.Moderate, r) {
		t.Error("a manage share granted moderation")
	}
	if !can(t, s, ctx, perm.Subject{Staff: true}, perm.Moderate, r) {
		t.Error("staff cannot moderate")
	}
}

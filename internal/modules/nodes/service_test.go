package nodes_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"siraj/internal/db"
	"siraj/internal/db/dbtest"
	"siraj/internal/modules/nodes"
	"siraj/internal/modules/perm"
	"siraj/internal/platform/apperr"
)

// These cover the scenarios the master prompt calls critical (§25 #3–#7): deep
// official structure, independent player structure, extending an official node, and
// the two things a player must never be able to do.

// A viewer is a real account: nodes record who created them, so an invented id is
// refused by the foreign key — as it should be.
type harness struct {
	svc *nodes.Service
	db  *db.DB
	ctx context.Context
}

func newHarness(t *testing.T) harness {
	t.Helper()
	d := dbtest.Open(t)
	t.Cleanup(d.Close)
	return harness{svc: nodes.NewService(d, perm.NewService(d)), db: d, ctx: context.Background()}
}

// account writes a member and returns a viewer for them.
func account(t *testing.T, d *db.DB, staff bool) nodes.Viewer {
	t.Helper()
	id := uuid.New()
	_, err := d.Pool.Exec(context.Background(), `
		INSERT INTO users (id, kind, display_name, email, lang)
		VALUES ($1, 'member', $2, $3, 'en')`,
		id, "Test "+id.String()[:8], id.String()[:8]+"@test.local")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return nodes.Viewer{ID: id, Staff: staff, Lang: "en"}
}

func mk(t *testing.T, s *nodes.Service, ctx context.Context, v nodes.Viewer, parent int64, slug string) nodes.Node {
	t.Helper()
	in := nodes.Input{ParentID: parent, Slug: slug, Names: map[string]string{"en": slug}}
	// Staff content is published here on purpose: a draft official node is invisible
	// to players, so a test that extended one would be testing the draft rule rather
	// than the extension rule.
	if v.Staff {
		in.Status = "published"
	}
	n, err := s.Create(ctx, v, in)
	if err != nil {
		t.Fatalf("create %q: %v", slug, err)
	}
	return n
}

// draft is an official node nobody has published, for the rules that turn on that.
func draft(t *testing.T, s *nodes.Service, ctx context.Context, v nodes.Viewer, slug string) nodes.Node {
	t.Helper()
	n, err := s.Create(ctx, v, nodes.Input{Slug: slug, Names: map[string]string{"en": slug}})
	if err != nil {
		t.Fatalf("create draft %q: %v", slug, err)
	}
	return n
}

// §25 #3 — an admin builds a domain and ten levels under it, with no schema change.
func TestStaffBuildsADeepOfficialTree(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	admin := account(t, h.db, true)

	root := mk(t, s, ctx, admin, 0, "sport")
	if !root.Official() {
		t.Fatal("a node staff created is not owned by the platform")
	}
	parent := root.ID
	for i := 1; i <= 10; i++ {
		parent = mk(t, s, ctx, admin, parent, "level-"+string(rune('a'+i-1))).ID
	}
	deepest, err := s.Get(ctx, admin, parent)
	if err != nil {
		t.Fatal(err)
	}
	if deepest.Depth != 10 {
		t.Errorf("deepest node is at depth %d, want 10", deepest.Depth)
	}
	stats, err := s.Stats(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Descendants != 10 {
		t.Errorf("root reports %d descendants, want 10", stats.Descendants)
	}
	trail, err := s.Trail(ctx, admin, parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(trail) != 11 {
		t.Errorf("breadcrumb has %d steps, want 11", len(trail))
	}
}

// §25 #4 — a player's own domain is theirs, independent of the official tree.
func TestPlayerBuildsTheirOwnDomain(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	p := account(t, h.db, false)

	root := mk(t, s, ctx, p, 0, "my-domain")
	if root.Official() {
		t.Fatal("a player's domain was recorded as the platform's")
	}
	if root.OwnerID != p.ID {
		t.Errorf("owner = %v, want %v", root.OwnerID, p.ID)
	}
	if root.Visible != "private" {
		t.Errorf("a player's new node is %q, want private", root.Visible)
	}
	child := mk(t, s, ctx, p, root.ID, "deeper")
	if child.ExtendsID != 0 {
		t.Error("a node under the player's own root was marked an extension")
	}
}

// §25 #5 — a player extends an official node. The official row is untouched and the
// extension points at it.
func TestPlayerExtendsAnOfficialNode(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	admin, p := account(t, h.db, true), account(t, h.db, false)

	official := mk(t, s, ctx, admin, 0, "football")
	league := mk(t, s, ctx, admin, official.ID, "premier-league")

	ext := mk(t, s, ctx, p, league.ID, "my-analysis")
	if ext.ExtendsID != league.ID {
		t.Errorf("extends_id = %d, want the official parent %d", ext.ExtendsID, league.ID)
	}
	if ext.Official() {
		t.Fatal("the extension is owned by the platform")
	}
	// The official node is exactly as it was.
	after, err := s.Get(ctx, admin, league.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Official() || after.ParentID != official.ID {
		t.Fatal("extending an official node changed it")
	}
	// And the player can keep building under their own extension.
	deep := mk(t, s, ctx, p, ext.ID, "tactics")
	if deep.Depth != 3 {
		t.Errorf("the extension's child is at depth %d, want 3", deep.Depth)
	}
}

// §25 #6 — a player cannot modify official data, by any of the write paths.
func TestPlayerCannotChangeOfficialContent(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	admin, p := account(t, h.db, true), account(t, h.db, false)
	official := mk(t, s, ctx, admin, 0, "official")

	if _, err := s.Update(ctx, p, official.ID, nodes.Input{Slug: "stolen"}); err == nil {
		t.Error("a player edited an official node")
	} else if e, ok := apperr.As(err); !ok || e.Status != 403 {
		t.Errorf("want 403, got %v", err)
	}
	if err := s.Archive(ctx, p, official.ID); err == nil {
		t.Error("a player archived an official node")
	}
	if err := s.Move(ctx, p, official.ID, 0); err == nil {
		t.Error("a player moved an official node")
	}
}

// §25 #7 — one player cannot see or touch another's private branch. Reading it must
// answer not-found: "forbidden" would confirm that it exists.
func TestPrivateBranchesStayPrivate(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	a, b := account(t, h.db, false), account(t, h.db, false)
	theirs := mk(t, s, ctx, a, 0, "a-private")

	if _, err := s.Get(ctx, b, theirs.ID); err == nil {
		t.Fatal("a player read another player's private node")
	} else if e, ok := apperr.As(err); !ok || e.Status != 404 {
		t.Errorf("want 404 so the node does not announce itself, got %v", err)
	}
	if _, err := s.Update(ctx, b, theirs.ID, nodes.Input{Slug: "x"}); err == nil {
		t.Fatal("a player edited another player's node")
	}
	// It is not in their listing either.
	roots, _, err := s.Children(ctx, b, 0, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range roots {
		if n.ID == theirs.ID {
			t.Fatal("another player's private root appeared in the listing")
		}
	}
	// The owner still sees it.
	if _, err := s.Get(ctx, a, theirs.ID); err != nil {
		t.Fatalf("the owner cannot read their own node: %v", err)
	}
}

// A guest sees published official branches and nothing else.
func TestGuestSeesOnlyPublishedOfficialNodes(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	admin, p := account(t, h.db, true), account(t, h.db, false)
	guest := nodes.Viewer{Lang: "en"}

	unpublished := draft(t, s, ctx, admin, "unpublished")
	mine := mk(t, s, ctx, p, 0, "players-own")
	live, err := s.Create(ctx, admin, nodes.Input{
		Slug: "live", Status: "published", Names: map[string]string{"en": "Live"},
	})
	if err != nil {
		t.Fatal(err)
	}

	roots, _, err := s.Children(ctx, guest, 0, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, n := range roots {
		seen[n.ID] = true
	}
	if !seen[live.ID] {
		t.Error("a guest cannot see a published official domain")
	}
	if seen[unpublished.ID] {
		t.Error("a guest can see an unpublished official domain")
	}
	if seen[mine.ID] {
		t.Error("a guest can see a player's private domain")
	}
}

// Moving a node into its own subtree is refused, and the message says why.
func TestMoveRefusesALoop(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	admin := account(t, h.db, true)
	root := mk(t, s, ctx, admin, 0, "root")
	mid := mk(t, s, ctx, admin, root.ID, "mid")
	leaf := mk(t, s, ctx, admin, mid.ID, "leaf")

	if err := s.Move(ctx, admin, mid.ID, leaf.ID); err == nil {
		t.Fatal("a node was moved inside itself")
	}
	// A legitimate move still works, and carries the subtree.
	other := mk(t, s, ctx, admin, 0, "other")
	if err := s.Move(ctx, admin, mid.ID, other.ID); err != nil {
		t.Fatalf("a valid move failed: %v", err)
	}
	moved, err := s.Get(ctx, admin, leaf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.RootID != other.ID {
		t.Errorf("the leaf's root is %d, want %d: the subtree did not follow", moved.RootID, other.ID)
	}
}

// Archiving a branch takes its descendants with it.
func TestArchiveTakesTheBranch(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	admin := account(t, h.db, true)
	root := mk(t, s, ctx, admin, 0, "root")
	mid := mk(t, s, ctx, admin, root.ID, "mid")
	leaf := mk(t, s, ctx, admin, mid.ID, "leaf")

	if err := s.Archive(ctx, admin, mid.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, admin, leaf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "archived" {
		t.Errorf("the leaf under an archived branch is %q, want archived", got.Status)
	}
}

// PERM-2 end to end: a branch shared with somebody appears in their listing and reads
// back for them, and stays invisible to everybody else. The permission service covers
// the decision; this covers the four visibility clauses in nodes.sql honouring it,
// which is where a share would silently fail to take effect.
func TestSharedBranchesAreVisibleToTheirGuests(t *testing.T) {
	h := newHarness(t)
	s, ctx := h.svc, h.ctx
	owner, friend, stranger := account(t, h.db, false), account(t, h.db, false), account(t, h.db, false)

	branch := mk(t, s, ctx, owner, 0, "private-branch")
	child := mk(t, s, ctx, owner, branch.ID, "a-lesson")

	// Before the share, nobody else can see it.
	for _, who := range []nodes.Viewer{friend, stranger} {
		if _, err := s.Get(ctx, who, branch.ID); err == nil {
			t.Fatal("a private branch was readable before it was shared")
		}
	}

	res, err := s.Resource(ctx, branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := perm.NewService(h.db)
	if err := p.Grant(ctx, perm.Subject{ID: owner.ID}, res, friend.ID, "play"); err != nil {
		t.Fatal(err)
	}

	// The friend can now read the branch and what is under it.
	if _, err := s.Get(ctx, friend, branch.ID); err != nil {
		t.Errorf("the person it was shared with cannot read it: %v", err)
	}
	if _, err := s.Get(ctx, friend, child.ID); err != nil {
		t.Errorf("a share must reach the branch, not only its top node: %v", err)
	}
	// The stranger still cannot.
	if _, err := s.Get(ctx, stranger, branch.ID); err == nil {
		t.Error("an unshared player can read a shared branch")
	}
	// And a share does not let them change it: play is not edit.
	if _, err := s.Update(ctx, friend, branch.ID, nodes.Input{Slug: "theirs-now"}); err == nil {
		t.Error("a play share allowed an edit")
	}

	// Revoking closes it again.
	if err := p.Revoke(ctx, perm.Subject{ID: owner.ID}, res, friend.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, friend, branch.ID); err == nil {
		t.Error("access survived being revoked")
	}
}

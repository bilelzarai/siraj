package content_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"siraj/internal/db"
	"siraj/internal/db/dbtest"
	"siraj/internal/modules/content"
	"siraj/internal/modules/perm"
	"siraj/internal/platform/apperr"
)

// §8 — content of every kind, attached at any depth, obeying the same access rules as
// the branch it hangs on. §14 — a new kind must cost no code change.

func setup(t *testing.T) (*content.Service, *perm.Service, *db.DB, context.Context) {
	t.Helper()
	d := dbtest.Open(t)
	t.Cleanup(d.Close)
	p := perm.NewService(d)
	return content.NewService(d, p), p, d, context.Background()
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

// node writes one at a given depth under an optional parent.
func node(t *testing.T, d *db.DB, owner uuid.UUID, parent int64, visibility, status string) (int64, perm.Resource) {
	t.Helper()
	var id int64
	var o, p any
	if owner != uuid.Nil {
		o = owner
	}
	if parent > 0 {
		p = parent
	}
	err := d.Pool.QueryRow(context.Background(), `
		INSERT INTO nodes (parent_id, owner_id, slug, node_type, visibility, status)
		VALUES ($1, $2, $3, 'node', $4::node_visibility, $5::node_status) RETURNING id`,
		p, o, "n-"+uuid.NewString()[:8], visibility, status).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id, perm.Resource{Kind: "node", ID: id, OwnerID: owner, Visibility: visibility, Status: status}
}

func flashcard(front, back string) map[string]map[string]any {
	return map[string]map[string]any{"en": {"front": front, "back": back}}
}

// A flashcard hangs straight on a node — no category had to be invented for it (§8).
func TestContentAttachesToAnyNode(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner := user(t, d)
	who := perm.Subject{ID: owner}

	// Ten levels down, to show depth is not a special case.
	parent := int64(0)
	var res perm.Resource
	for i := 0; i < 10; i++ {
		parent, res = node(t, d, owner, parent, "private", "draft")
	}

	item, err := s.Create(ctx, who, res, content.Input{
		NodeID: parent, Type: "flashcard", Fields: flashcard("ما هو التجويد؟", "علم تلاوة القرآن"),
	})
	if err != nil {
		t.Fatalf("create on a tenth-level node: %v", err)
	}
	if item.NodeID != parent {
		t.Errorf("hung on node %d, want %d", item.NodeID, parent)
	}
	if item.Official() {
		t.Error("a player's flashcard was recorded as the platform's")
	}
	if item.Visibility != "private" {
		t.Errorf("a player's new content is %q, want private", item.Visibility)
	}

	got, total, err := s.OnNode(ctx, who, parent, "", false, 1, 25, "en")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("the node holds %d items, want 1", total)
	}
	if got[0].Fields["front"] != "ما هو التجويد؟" {
		t.Errorf("the field came back as %v", got[0].Fields["front"])
	}
}

// Content listed over a branch finds what is below it, which is what a challenge
// scoped to "the whole branch" needs (§10.2).
func TestContentListsOverTheBranch(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner := user(t, d)
	who := perm.Subject{ID: owner}

	top, topRes := node(t, d, owner, 0, "private", "draft")
	childID, childRes := node(t, d, owner, top, "private", "draft")

	if _, err := s.Create(ctx, who, topRes, content.Input{
		NodeID: top, Type: "flashcard", Fields: flashcard("top", "x"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, who, childRes, content.Input{
		NodeID: childID, Type: "flashcard", Fields: flashcard("child", "y"),
	}); err != nil {
		t.Fatal(err)
	}

	_, direct, err := s.OnNode(ctx, who, top, "", false, 1, 25, "en")
	if err != nil {
		t.Fatal(err)
	}
	if direct != 1 {
		t.Errorf("direct on the top node found %d, want 1", direct)
	}
	_, whole, err := s.OnNode(ctx, who, top, "", true, 1, 25, "en")
	if err != nil {
		t.Fatal(err)
	}
	if whole != 2 {
		t.Errorf("the branch holds %d, want 2", whole)
	}
}

// Content obeys the same rules as the branch it hangs on: another player cannot read
// it, and cannot write to somebody else's node.
func TestContentObeysThePermissionService(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner, stranger := user(t, d), user(t, d)
	nodeID, res := node(t, d, owner, 0, "private", "draft")

	item, err := s.Create(ctx, perm.Subject{ID: owner}, res, content.Input{
		NodeID: nodeID, Type: "flashcard", Fields: flashcard("secret", "x"),
	})
	if err != nil {
		t.Fatal(err)
	}

	them := perm.Subject{ID: stranger}
	if _, err := s.Get(ctx, them, item.ID, "en"); err == nil {
		t.Error("a stranger read private content")
	} else if e, ok := apperr.As(err); !ok || e.Status != 404 {
		t.Errorf("want 404 so the item does not announce itself, got %v", err)
	}
	if _, err := s.Create(ctx, them, res, content.Input{
		NodeID: nodeID, Type: "flashcard", Fields: flashcard("intruder", "x"),
	}); err == nil {
		t.Error("a stranger wrote content onto somebody else's node")
	}
	if err := s.Delete(ctx, them, item.ID); err == nil {
		t.Error("a stranger deleted somebody else's content")
	}
	// The owner still can.
	if err := s.Delete(ctx, perm.Subject{ID: owner}, item.ID); err != nil {
		t.Fatalf("the owner cannot delete their own content: %v", err)
	}
}

// A player may attach their own content to an official node — that is what extending
// means — without the official node becoming theirs.
func TestPlayerAttachesContentToOfficialNode(t *testing.T) {
	s, _, d, ctx := setup(t)
	player := user(t, d)
	officialID, officialRes := node(t, d, uuid.Nil, 0, "public", "published")

	item, err := s.Create(ctx, perm.Subject{ID: player}, officialRes, content.Input{
		NodeID: officialID, Type: "flashcard", Fields: flashcard("my note", "x"),
	})
	if err != nil {
		t.Fatalf("a player cannot attach content to an official node: %v", err)
	}
	if item.Official() {
		t.Error("the player's content is owned by the platform")
	}
	if item.OwnerID != player {
		t.Errorf("owner = %v, want %v", item.OwnerID, player)
	}
	// Another player does not see it.
	if _, total, err := s.OnNode(ctx, perm.Subject{ID: user(t, d)}, officialID, "", false, 1, 25, "en"); err != nil {
		t.Fatal(err)
	} else if total != 0 {
		t.Errorf("a stranger sees %d items on the official node, want 0", total)
	}
}

// §14 — a kind nobody wrote code for works the moment it is in the registry.
func TestANewKindNeedsNoCode(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner := user(t, d)
	who := perm.Subject{ID: owner}
	nodeID, res := node(t, d, owner, 0, "private", "draft")

	// A kind this codebase has never heard of. content_types is configuration and
	// survives the cleaner between tests, so this has to be repeatable.
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO content_types (code, icon, fields, playable, position)
		VALUES ('recipe', 'book', ARRAY['title','steps'], false, 99)
		ON CONFLICT (code) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Pool.Exec(context.Background(), `DELETE FROM content_types WHERE code = 'recipe'`)
	})

	types, err := s.Types(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ty := range types {
		if ty.Code == "recipe" {
			found = true
		}
	}
	if !found {
		t.Fatal("a kind added to the registry is not offered")
	}

	// It validates against the fields the registry declares — no branch for it here.
	if _, err := s.Create(ctx, who, res, content.Input{
		NodeID: nodeID, Type: "recipe",
		Fields: map[string]map[string]any{"en": {"title": "Harira"}},
	}); err == nil {
		t.Error("a recipe with no steps was accepted")
	}
	item, err := s.Create(ctx, who, res, content.Input{
		NodeID: nodeID, Type: "recipe",
		Fields: map[string]map[string]any{"en": {"title": "Harira", "steps": "Soak the lentils."}},
	})
	if err != nil {
		t.Fatalf("a complete recipe was refused: %v", err)
	}
	if item.Type != "recipe" {
		t.Errorf("stored as %q", item.Type)
	}
	got, _, err := s.OnNode(ctx, who, nodeID, "recipe", false, 1, 25, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Fields["steps"] != "Soak the lentils." {
		t.Errorf("the recipe did not come back whole: %v", got)
	}
}

// An unknown kind is refused rather than stored as something nobody can render.
func TestUnknownKindIsRefused(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner := user(t, d)
	nodeID, res := node(t, d, owner, 0, "private", "draft")

	if _, err := s.Create(ctx, perm.Subject{ID: owner}, res, content.Input{
		NodeID: nodeID, Type: "nonsense", Fields: flashcard("a", "b"),
	}); err == nil {
		t.Fatal("content of an unknown kind was stored")
	}
}

// An answer written with an item is checked against it, through the kind's own rule.
// This is the round trip the game engine will make: store, then check.
func TestAnswerRoundTrip(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner := user(t, d)
	who := perm.Subject{ID: owner}
	nodeID, res := node(t, d, owner, 0, "private", "draft")

	for _, c := range []struct {
		kind   string
		fields map[string]any
		answer map[string]any
		right  content.Answer
		wrong  content.Answer
	}{
		{
			"question",
			map[string]any{"body": "Which surah?", "answers": "a|b|c|d"},
			map[string]any{"index": 2},
			content.Answer{Choice: 2}, content.Answer{Choice: 0},
		},
		{
			"true_false",
			map[string]any{"body": "At-Tawbah opens without the Basmala."},
			map[string]any{"yes": true},
			content.Answer{Yes: true}, content.Answer{Yes: false},
		},
		{
			"text_answer",
			map[string]any{"body": "Name the third pillar.", "answer": "zakat"},
			map[string]any{"text": []any{"zakat", "zakah"}},
			content.Answer{Text: "Zakah"}, content.Answer{Text: "hajj"},
		},
		{
			"number",
			map[string]any{"body": "How many pillars?", "answer": "5"},
			map[string]any{"number": 5},
			content.Answer{Text: "٥"}, content.Answer{Text: "4"},
		},
	} {
		item, err := s.Create(ctx, who, res, content.Input{
			NodeID: nodeID, Type: c.kind,
			Fields: map[string]map[string]any{"en": c.fields},
			Answer: c.answer,
		})
		if err != nil {
			t.Fatalf("%s: create: %v", c.kind, err)
		}
		ok, points, err := s.Check(ctx, item.ID, c.right)
		if err != nil {
			t.Fatalf("%s: check: %v", c.kind, err)
		}
		if !ok {
			t.Errorf("%s: the right answer was marked wrong", c.kind)
		}
		if points != 10 {
			t.Errorf("%s: worth %d points, want the default 10", c.kind, points)
		}
		if ok, _, err := s.Check(ctx, item.ID, c.wrong); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Errorf("%s: a wrong answer was marked right", c.kind)
		}
	}
}

// The engine's view of the universal model: playable content from a branch, scoped
// the way §10.2 asks, with the answer never travelling with the question (§10.1).
func TestPickRespectsScopeAndPlayability(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner := user(t, d)
	who := perm.Subject{ID: owner}

	top, topRes := node(t, d, owner, 0, "public", "published")
	childID, childRes := node(t, d, owner, top, "public", "published")

	// A playable question on each level, and a flashcard that is not playable.
	for _, c := range []struct {
		res    perm.Resource
		nodeID int64
		kind   string
	}{
		{topRes, top, "question"},
		{childRes, childID, "question"},
		{childRes, childID, "flashcard"},
	} {
		fields := map[string]any{"body": "q", "answers": "a|b|c|d"}
		if c.kind == "flashcard" {
			fields = map[string]any{"front": "f", "back": "b"}
		}
		if _, err := s.Create(ctx, who, c.res, content.Input{
			NodeID: c.nodeID, Type: c.kind, Status: "published",
			Fields: map[string]map[string]any{"en": fields},
			Answer: map[string]any{"index": 0},
		}); err != nil {
			t.Fatalf("%s on %d: %v", c.kind, c.nodeID, err)
		}
	}

	for _, c := range []struct {
		scope string
		want  int
		why   string
	}{
		{"direct", 1, "only the question on the top node"},
		{"subtree", 1, "only the question below it — the flashcard is not playable"},
		{"both", 2, "both questions, still not the flashcard"},
	} {
		got, err := s.Available(ctx, who, top, c.scope, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: %s — got %d, want %d", c.scope, c.why, got, c.want)
		}
	}

	picked, err := s.Pick(ctx, who, top, "both", "", 0, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(picked) != 2 {
		t.Fatalf("picked %d items, want 2", len(picked))
	}
	for _, p := range picked {
		if p.Type != "question" {
			t.Errorf("a %q was dealt: only playable kinds should be", p.Type)
		}
	}

	// Excluding what has already been dealt is how a game avoids repeats.
	again, err := s.Pick(ctx, who, top, "both", "", 0, 10, []int64{picked[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].ID == picked[0].ID {
		t.Errorf("an excluded item came back: %v", again)
	}
}

// Another player's private branch is never dealt from, however the scope is asked for.
func TestPickNeverDealsSomebodyElsesPrivateContent(t *testing.T) {
	s, _, d, ctx := setup(t)
	owner, stranger := user(t, d), user(t, d)
	nodeID, res := node(t, d, owner, 0, "private", "draft")

	if _, err := s.Create(ctx, perm.Subject{ID: owner}, res, content.Input{
		NodeID: nodeID, Type: "question", Status: "published",
		Fields: map[string]map[string]any{"en": {"body": "private", "answers": "a|b"}},
		Answer: map[string]any{"index": 0},
	}); err != nil {
		t.Fatal(err)
	}

	n, err := s.Available(ctx, perm.Subject{ID: stranger}, nodeID, "both", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("a stranger can play %d items from a private branch, want 0", n)
	}
	picked, err := s.Pick(ctx, perm.Subject{ID: stranger}, nodeID, "both", "", 0, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(picked) != 0 {
		t.Errorf("a stranger was dealt %d items from a private branch", len(picked))
	}
	// The owner can.
	if n, err := s.Available(ctx, perm.Subject{ID: owner}, nodeID, "both", "", 0); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Errorf("the owner can play %d of their own items, want 1", n)
	}
}

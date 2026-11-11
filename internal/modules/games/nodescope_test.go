package games_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"siraj/internal/db"
	"siraj/internal/db/dbtest"
)

// CUT-1: a game can be played from any node of the universal tree, and the scope
// decides how much of the branch counts (§10.2). These assert the selection itself
// against a real tree, because an off-by-one in `path <@` is invisible until a player
// gets a question from the wrong branch.
//
// The queries are exercised directly: the service wraps them with Your-domains and
// Islamic-only rules that are v1 concerns and would hide what is being measured here.

type tree struct {
	root, mid, leaf int64
}

// plant builds root → mid → leaf and hangs one published question on each.
func plant(t *testing.T, d *db.DB) tree {
	t.Helper()
	ctx := context.Background()
	var out tree
	mk := func(parent int64, slug string) int64 {
		var id int64
		var p any
		if parent > 0 {
			p = parent
		}
		err := d.Pool.QueryRow(ctx, `
			INSERT INTO nodes (parent_id, slug, node_type, status, visibility)
			VALUES ($1, $2, 'node', 'published', 'public') RETURNING id`, p, slug).Scan(&id)
		if err != nil {
			t.Fatalf("node %s: %v", slug, err)
		}
		return id
	}
	out.root = mk(0, "root-"+uuid.NewString()[:8])
	out.mid = mk(out.root, "mid-"+uuid.NewString()[:8])
	out.leaf = mk(out.mid, "leaf-"+uuid.NewString()[:8])

	// A question needs a category for the v1 columns; the trigger from migration 21
	// would overwrite node_id from it, so the category points at the node we want.
	for _, n := range []int64{out.root, out.mid, out.leaf} {
		var categoryID int64
		err := d.Pool.QueryRow(ctx, `
			INSERT INTO categories (domain_id, slug, icon, color, node_id)
			VALUES ((SELECT id FROM domains ORDER BY id LIMIT 1), $1, 'book', '#000', $2)
			RETURNING id`, "cat-"+uuid.NewString()[:8], n).Scan(&categoryID)
		if err != nil {
			t.Fatalf("category for node %d: %v", n, err)
		}
		_, err = d.Pool.Exec(ctx, `
			INSERT INTO questions (category_id, difficulty, points, correct_index, status, main_lang)
			VALUES ($1, 1, 10, 0, 'published', 'en')`, categoryID)
		if err != nil {
			t.Fatalf("question on node %d: %v", n, err)
		}
	}
	return out
}

// count runs the scope clause the engine uses and says how many questions it selects.
func count(t *testing.T, d *db.DB, node int64, scope string) int {
	t.Helper()
	var n int
	err := d.Pool.QueryRow(context.Background(), `
		SELECT count(*)::int FROM questions q
		WHERE q.status = 'published' AND q.deleted_at IS NULL
		  AND CASE $2::text
		        WHEN 'direct'  THEN q.node_id = $1
		        WHEN 'subtree' THEN q.node_id IN (
		               SELECT n.id FROM nodes n
		                WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = $1)
		                  AND n.id <> $1 AND n.deleted_at IS NULL)
		        ELSE q.node_id IN (
		               SELECT n.id FROM nodes n
		                WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = $1)
		                  AND n.deleted_at IS NULL)
		      END`, node, scope).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNodeScopeSelectsTheRightQuestions(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	// A domain to hang the v1 categories off; the tree is what is being tested.
	if _, err := d.Pool.Exec(context.Background(), `
		INSERT INTO domains (slug, family, icon, color, motif, is_published)
		VALUES ('scope-probe', 'general', 'book', '#000', 'book', true)
		ON CONFLICT (slug) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	tr := plant(t, d)

	for _, c := range []struct {
		name  string
		node  int64
		scope string
		want  int
	}{
		{"direct on the root takes only the root's own", tr.root, "direct", 1},
		{"subtree on the root takes the two below it", tr.root, "subtree", 2},
		{"both on the root takes the whole branch", tr.root, "both", 3},
		{"direct on the middle takes one", tr.mid, "direct", 1},
		{"subtree on the middle takes the leaf", tr.mid, "subtree", 1},
		{"both on the middle takes two", tr.mid, "both", 2},
		{"a leaf has no descendants", tr.leaf, "subtree", 0},
		{"both on a leaf is just the leaf", tr.leaf, "both", 1},
	} {
		if got := count(t, d, c.node, c.scope); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// Migration 21 keeps node_id in step whichever path writes the question. Without it
// everything written after migration 20 would be invisible to the engine.
func TestQuestionTakesItsCategoryNode(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO domains (slug, family, icon, color, motif, is_published)
		VALUES ('follow-probe', 'general', 'book', '#000', 'book', true)
		ON CONFLICT (slug) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var node, other int64
	for _, dst := range []*int64{&node, &other} {
		if err := d.Pool.QueryRow(ctx, `
			INSERT INTO nodes (slug, node_type, status) VALUES ($1, 'node', 'published')
			RETURNING id`, "follow-"+uuid.NewString()[:8]).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	var categoryID int64
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO categories (domain_id, slug, icon, color, node_id)
		VALUES ((SELECT id FROM domains WHERE slug='follow-probe'), $1, 'book', '#000', $2)
		RETURNING id`, "cat-"+uuid.NewString()[:8], node).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}

	// Written the v1 way — only category_id — the question still lands on the node.
	var questionID, got int64
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO questions (category_id, difficulty, points, correct_index, status, main_lang)
		VALUES ($1, 1, 10, 0, 'published', 'en') RETURNING id, node_id`,
		categoryID).Scan(&questionID, &got); err != nil {
		t.Fatal(err)
	}
	if got != node {
		t.Fatalf("a question written with only a category has node_id %d, want %d", got, node)
	}

	// And a category that is re-pointed carries its questions along.
	if _, err := d.Pool.Exec(ctx, `UPDATE categories SET node_id = $1 WHERE id = $2`, other, categoryID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `SELECT node_id FROM questions WHERE id = $1`, questionID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != other {
		t.Errorf("after the category moved, the question is on node %d, want %d", got, other)
	}
}

// CUT-4b: a game scoped to a node deals from the universal model, so content that
// exists only in content_items — a true/false, a typed answer, anything born after
// the v1 bank — can be played at all. Before this the engine read `questions`, and
// such an item could never be dealt however it was scoped.
func TestNodeScopedGameDealsContentItems(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var nodeID int64
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO nodes (slug, node_type, status, visibility)
		VALUES ($1, 'node', 'published', 'public') RETURNING id`,
		"deal-"+uuid.NewString()[:8]).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	// Five items with no v1 question behind them.
	for i := 0; i < 5; i++ {
		var itemID int64
		if err := d.Pool.QueryRow(ctx, `
			INSERT INTO content_items (node_id, type_code, visibility, status, difficulty, points, answer)
			VALUES ($1, 'question', 'public', 'published', 1, 10, '{"index": 1}'::jsonb)
			RETURNING id`, nodeID).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Pool.Exec(ctx, `
			INSERT INTO content_item_translations (item_id, lang, fields)
			VALUES ($1, 'en', jsonb_build_object('body', 'New model', 'answers', 'a|b|c|d', 'explanation', ''))`,
			itemID); err != nil {
			t.Fatal(err)
		}
	}

	// None of them has a question row, so the v1 selection would find nothing.
	var viaBank int
	if err := d.Pool.QueryRow(ctx, `
		SELECT count(*)::int FROM questions q WHERE q.node_id = $1 AND q.deleted_at IS NULL`,
		nodeID).Scan(&viaBank); err != nil {
		t.Fatal(err)
	}
	if viaBank != 0 {
		t.Fatalf("the probe wrote %d v1 questions: the test would prove nothing", viaBank)
	}

	// The universal selection finds all five.
	var viaContent int
	if err := d.Pool.QueryRow(ctx, `
		SELECT count(*)::int FROM content_items i
		JOIN content_types ct ON ct.code = i.type_code AND ct.playable
		JOIN nodes n ON n.id = i.node_id
		WHERE n.path <@ (SELECT a.path FROM nodes a WHERE a.id = $1)
		  AND i.status = 'published' AND i.deleted_at IS NULL`, nodeID).Scan(&viaContent); err != nil {
		t.Fatal(err)
	}
	if viaContent != 5 {
		t.Errorf("the universal selection found %d items, want 5", viaContent)
	}

	// And the display branch renders one without a v1 question: body and answers
	// come from content_item_translations, the theming from the node.
	var body string
	var answers []string
	if err := d.Pool.QueryRow(ctx, `
		SELECT COALESCE(it.fields->>'body', ''),
		       string_to_array(COALESCE(it.fields->>'answers', ''), '|')::text[]
		FROM content_items i
		JOIN content_item_translations it ON it.item_id = i.id AND it.lang = 'en'
		WHERE i.node_id = $1 AND i.question_id IS NULL LIMIT 1`, nodeID).Scan(&body, &answers); err != nil {
		t.Fatal(err)
	}
	if body != "New model" || len(answers) != 4 {
		t.Errorf("the item renders as %q with %d answers", body, len(answers))
	}
}

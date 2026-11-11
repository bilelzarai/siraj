package db_test

import (
	"context"
	"testing"

	"siraj/internal/db/dbtest"
)

// The universal hierarchy has to hold four promises, and each is cheap to break with
// an innocent-looking change to a trigger. These lock them down.

// Unlimited depth: the product's own example runs eight levels, so ten is the floor.
// A tree this deep must cost no schema change at all.
func TestNodesNestWithoutLimit(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var root int64
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (slug, node_type) VALUES ('sport', 'domain') RETURNING id`).Scan(&root); err != nil {
		t.Fatal(err)
	}
	parent := root
	for i := 1; i <= 10; i++ {
		var id int64
		err := d.Pool.QueryRow(ctx,
			`INSERT INTO nodes (parent_id, slug, node_type) VALUES ($1, $2, 'node') RETURNING id`,
			parent, "level-"+itoa(i)).Scan(&id)
		if err != nil {
			t.Fatalf("level %d: %v", i, err)
		}
		parent = id
	}

	var depth, labels int
	var rootID int64
	if err := d.Pool.QueryRow(ctx,
		`SELECT depth, nlevel(path), root_id FROM nodes WHERE id = $1`, parent).
		Scan(&depth, &labels, &rootID); err != nil {
		t.Fatal(err)
	}
	if depth != 10 {
		t.Errorf("depth = %d, want 10", depth)
	}
	if labels != 11 {
		t.Errorf("path carries %d labels, want 11 (root + ten levels)", labels)
	}
	if rootID != root {
		t.Errorf("root_id = %d, want %d: a descendant must know its domain", rootID, root)
	}
}

// Moving a branch carries everything under it: path, depth and root_id of every
// descendant follow the node that moved.
func TestNodesMoveCarriesTheSubtree(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var sport, business int64
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (slug, node_type) VALUES ('sport', 'domain') RETURNING id`).Scan(&sport); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (slug, node_type) VALUES ('business', 'domain') RETURNING id`).Scan(&business); err != nil {
		t.Fatal(err)
	}
	var branch, leaf int64
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (parent_id, slug, node_type) VALUES ($1, 'branch', 'node') RETURNING id`,
		sport).Scan(&branch); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (parent_id, slug, node_type) VALUES ($1, 'leaf', 'node') RETURNING id`,
		branch).Scan(&leaf); err != nil {
		t.Fatal(err)
	}

	if _, err := d.Pool.Exec(ctx, `UPDATE nodes SET parent_id = $1 WHERE id = $2`, business, branch); err != nil {
		t.Fatal(err)
	}

	var rootID int64
	var depth int
	var inside bool
	err := d.Pool.QueryRow(ctx, `
		SELECT n.root_id, n.depth, n.path <@ (SELECT path FROM nodes WHERE id = $2)
		FROM nodes n WHERE n.id = $1`, leaf, branch).Scan(&rootID, &depth, &inside)
	if err != nil {
		t.Fatal(err)
	}
	if rootID != business {
		t.Errorf("leaf root_id = %d, want %d: the subtree did not follow its branch", rootID, business)
	}
	if depth != 2 {
		t.Errorf("leaf depth = %d, want 2", depth)
	}
	if !inside {
		t.Error("leaf is no longer inside its branch's path")
	}
}

// A node may never become its own ancestor. The database refuses it, so no caller —
// handler, service, import or a hand-written statement — can write a loop.
func TestNodesRefuseACycle(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var top, middle, bottom int64
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (slug, node_type) VALUES ('top', 'domain') RETURNING id`).Scan(&top); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (parent_id, slug, node_type) VALUES ($1, 'middle', 'node') RETURNING id`,
		top).Scan(&middle); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (parent_id, slug, node_type) VALUES ($1, 'bottom', 'node') RETURNING id`,
		middle).Scan(&bottom); err != nil {
		t.Fatal(err)
	}

	if _, err := d.Pool.Exec(ctx, `UPDATE nodes SET parent_id = $1 WHERE id = $2`, bottom, middle); err == nil {
		t.Fatal("a node was moved under its own descendant: the tree can now loop")
	}
	if _, err := d.Pool.Exec(ctx, `UPDATE nodes SET parent_id = id WHERE id = $1`, middle); err == nil {
		t.Fatal("a node was made its own parent")
	}
}

// A slug is unique among siblings, not across the whole tree: two different domains
// may each have a "basics".
func TestNodesSlugIsUniquePerParent(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var a, b int64
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (slug, node_type) VALUES ('alpha', 'domain') RETURNING id`).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (slug, node_type) VALUES ('beta', 'domain') RETURNING id`).Scan(&b); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []int64{a, b} {
		if _, err := d.Pool.Exec(ctx,
			`INSERT INTO nodes (parent_id, slug, node_type) VALUES ($1, 'basics', 'node')`, parent); err != nil {
			t.Fatalf("the same slug under a different parent must be allowed: %v", err)
		}
	}
	if _, err := d.Pool.Exec(ctx,
		`INSERT INTO nodes (parent_id, slug, node_type) VALUES ($1, 'basics', 'node')`, a); err == nil {
		t.Fatal("two siblings share a slug")
	}
}

// An extension belongs to a player. The platform does not extend itself, and the
// constraint says so rather than leaving it to callers.
func TestNodesExtensionNeedsAnOwner(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var official int64
	if err := d.Pool.QueryRow(ctx,
		`INSERT INTO nodes (slug, node_type) VALUES ('official', 'domain') RETURNING id`).Scan(&official); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Pool.Exec(ctx,
		`INSERT INTO nodes (parent_id, slug, node_type, extends_id) VALUES ($1, 'orphan-extension', 'node', $1)`,
		official); err == nil {
		t.Fatal("an extension was written with no owner")
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// CUT-2 and CUT-3: mastery and a set's questions follow their category onto the tree
// whichever code path writes them. Without this a row written after migration 25
// would be invisible to anything reading node_id.
func TestMasteryAndSetQuestionsFollowTheirCategory(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var domainID, categoryID, nodeID, userID any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO domains (slug, family, icon, color, motif, is_published)
		VALUES ('cut-probe', 'general', 'book', '#000', 'book', true) RETURNING id`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO nodes (slug, node_type, status) VALUES ('cut-node', 'node', 'published')
		RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO categories (domain_id, slug, icon, color, node_id)
		VALUES ($1, 'cut-cat', 'book', '#000', $2) RETURNING id`, domainID, nodeID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO users (kind, display_name, email, lang)
		VALUES ('member', 'Cut', 'cut@probe.local', 'en') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	// Mastery written the v1 way still lands on the node.
	var got any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO user_category_stats (user_id, category_id) VALUES ($1, $2)
		RETURNING node_id`, userID, categoryID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != nodeID {
		t.Errorf("mastery landed on node %v, want %v", got, nodeID)
	}

	// And a question inside a set.
	var setID any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO question_sets (owner_id, name) VALUES ($1, 'probe') RETURNING id`,
		userID).Scan(&setID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO set_questions (set_id, position, category_id, correct_index)
		VALUES ($1, 1, $2, 0) RETURNING node_id`, setID, categoryID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != nodeID {
		t.Errorf("the set question landed on node %v, want %v", got, nodeID)
	}
}

// Migration 26: the v1 bank is mirrored into content_items, and stays mirrored.
// A question written or edited afterwards must appear in the new model too, or the
// engine reading content_items would never see it.
func TestQuestionsMirrorIntoContent(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var domainID, nodeID, categoryID any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO domains (slug, family, icon, color, motif, is_published)
		VALUES ('mirror-probe', 'general', 'book', '#000', 'book', true) RETURNING id`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO nodes (slug, node_type, status) VALUES ('mirror-node', 'node', 'published')
		RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO categories (domain_id, slug, icon, color, node_id)
		VALUES ($1, 'mirror-cat', 'book', '#000', $2) RETURNING id`, domainID, nodeID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}

	// A question written the v1 way.
	var questionID int64
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO questions (category_id, difficulty, points, correct_index, status, main_lang)
		VALUES ($1, 2, 20, 3, 'published', 'en') RETURNING id`, categoryID).Scan(&questionID); err != nil {
		t.Fatal(err)
	}

	var itemID int64
	var status, kind string
	var points int
	var answer []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT id, status::text, type_code, points, answer FROM content_items WHERE question_id = $1`,
		questionID).Scan(&itemID, &status, &kind, &points, &answer)
	if err != nil {
		t.Fatalf("the question was not mirrored: %v", err)
	}
	if kind != "question" || status != "published" || points != 20 {
		t.Errorf("mirrored as kind=%q status=%q points=%d", kind, status, points)
	}
	if string(answer) != `{"index": 3}` {
		t.Errorf("the right answer came across as %s, want {\"index\": 3}", answer)
	}

	// Its wording follows too.
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO question_translations (question_id, lang, body, answers, explanation)
		VALUES ($1, 'en', 'Which one?', ARRAY['a','b','c','d'], 'Because.')`, questionID); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := d.Pool.QueryRow(ctx, `
		SELECT fields->>'body' FROM content_item_translations WHERE item_id = $1 AND lang = 'en'`,
		itemID).Scan(&body); err != nil {
		t.Fatalf("the wording was not mirrored: %v", err)
	}
	if body != "Which one?" {
		t.Errorf("mirrored body %q", body)
	}

	// And an edit carries over rather than leaving the two disagreeing.
	if _, err := d.Pool.Exec(ctx, `UPDATE questions SET status = 'archived' WHERE id = $1`, questionID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `SELECT status::text FROM content_items WHERE id = $1`, itemID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "archived" {
		t.Errorf("after archiving the question the item is %q", status)
	}
}

// CUT-4 step one: a round records both the v1 question and the content item it
// stands for, whichever the caller writes. Without this the engine could not be
// moved onto the universal model one query at a time — a half-switched deal would
// leave rows that only one half of the code could read.
func TestGameRoundsPairBothIDs(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var domainID, nodeID, categoryID, userID, gameID any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO domains (slug, family, icon, color, motif, is_published)
		VALUES ('pair-probe', 'general', 'book', '#000', 'book', true) RETURNING id`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	// Migration 27 gives the category its node, so nothing has to be made by hand.
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO categories (domain_id, slug, icon, color)
		VALUES ($1, 'pair-cat', 'book', '#000') RETURNING id, node_id`, domainID).Scan(&categoryID, &nodeID); err != nil {
		t.Fatal(err)
	}
	if nodeID == nil {
		t.Fatal("a new category was created without a node: the tree is no longer the whole picture")
	}
	var questionID, itemID int64
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO questions (category_id, difficulty, points, correct_index, status, main_lang)
		VALUES ($1, 1, 10, 0, 'published', 'en') RETURNING id`, categoryID).Scan(&questionID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx,
		`SELECT id FROM content_items WHERE question_id = $1`, questionID).Scan(&itemID); err != nil {
		t.Fatalf("the question did not mirror: %v", err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO users (kind, display_name, email, lang)
		VALUES ('member', 'Pair', 'pair@probe.local', 'en') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO games (owner_id, mode, question_count, seconds_per_question)
		VALUES ($1, 'solo', 1, 20) RETURNING id`, userID).Scan(&gameID); err != nil {
		t.Fatal(err)
	}

	// Written the v1 way: the item follows.
	var gotItem, gotQuestion any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO game_questions (game_id, position, question_id) VALUES ($1, 0, $2)
		RETURNING item_id`, gameID, questionID).Scan(&gotItem); err != nil {
		t.Fatal(err)
	}
	if gotItem != itemID {
		t.Errorf("a round written with a question has item %v, want %d", gotItem, itemID)
	}

	// And the other way round: the question follows the item.
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO game_questions (game_id, position, item_id) VALUES ($1, 1, $2)
		RETURNING question_id`, gameID, itemID).Scan(&gotQuestion); err != nil {
		t.Fatal(err)
	}
	if gotQuestion != questionID {
		t.Errorf("a round written with an item has question %v, want %d", gotQuestion, questionID)
	}
}

// MP-2 (§10.3): a match must not change under the people playing it. An edit to the
// source bumps the version and leaves the frozen one alone, so a round reads back as
// it was dealt however far the source has moved on.
func TestDealtRoundsAreFrozenAgainstEdits(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var nodeID, itemID, userID, gameID any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO nodes (slug, node_type, status, visibility)
		VALUES ('freeze-node', 'node', 'published', 'public') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO content_items (node_id, type_code, visibility, status, difficulty, points, answer)
		VALUES ($1, 'question', 'public', 'published', 1, 10, '{"index": 1}'::jsonb)
		RETURNING id`, nodeID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO content_item_translations (item_id, lang, fields)
		VALUES ($1, 'en', jsonb_build_object('body', 'Original wording', 'answers', 'a|b|c|d'))`,
		itemID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO users (kind, display_name, email, lang)
		VALUES ('member', 'Freeze', 'freeze@probe.local', 'en') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO games (owner_id, mode, question_count, seconds_per_question)
		VALUES ($1, 'solo', 1, 20) RETURNING id`, userID).Scan(&gameID); err != nil {
		t.Fatal(err)
	}

	// Dealing freezes it.
	var dealtVersion int
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO game_questions (game_id, position, item_id) VALUES ($1, 0, $2)
		RETURNING item_version`, gameID, itemID).Scan(&dealtVersion); err != nil {
		t.Fatal(err)
	}
	if dealtVersion < 1 {
		t.Fatalf("the round recorded version %d", dealtVersion)
	}

	// Now the source is edited, twice over: its wording and its right answer.
	if _, err := d.Pool.Exec(ctx, `
		UPDATE content_item_translations
		SET fields = jsonb_build_object('body', 'Rewritten entirely', 'answers', 'w|x|y|z')
		WHERE item_id = $1 AND lang = 'en'`, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Pool.Exec(ctx,
		`UPDATE content_items SET answer = '{"index": 3}'::jsonb, points = 99 WHERE id = $1`, itemID); err != nil {
		t.Fatal(err)
	}

	var liveVersion int
	if err := d.Pool.QueryRow(ctx, `SELECT version FROM content_items WHERE id = $1`, itemID).Scan(&liveVersion); err != nil {
		t.Fatal(err)
	}
	if liveVersion <= dealtVersion {
		t.Errorf("after two edits the live version is %d, dealt was %d: edits are not counted", liveVersion, dealtVersion)
	}

	// The frozen copy is untouched: the match reads as it was played.
	var body, answers string
	var frozenPoints int
	var frozenAnswer []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT v.texts->'en'->>'body', v.texts->'en'->>'answers', v.points, v.answer
		FROM content_item_versions v
		JOIN game_questions gq ON gq.item_id = v.item_id AND gq.item_version = v.version
		WHERE gq.game_id = $1 AND gq.position = 0`, gameID).Scan(&body, &answers, &frozenPoints, &frozenAnswer)
	if err != nil {
		t.Fatal(err)
	}
	if body != "Original wording" {
		t.Errorf("the frozen round now reads %q: the match changed under the player", body)
	}
	if answers != "a|b|c|d" {
		t.Errorf("the frozen answers are %q", answers)
	}
	if frozenPoints != 10 {
		t.Errorf("the round is now worth %d, was dealt at 10", frozenPoints)
	}
	if string(frozenAnswer) != `{"index": 1}` {
		t.Errorf("what counted as right became %s", frozenAnswer)
	}

	// Deleting the source does not take the history with it.
	if _, err := d.Pool.Exec(ctx, `UPDATE content_items SET deleted_at = now() WHERE id = $1`, itemID); err != nil {
		t.Fatal(err)
	}
	var stillThere int
	if err := d.Pool.QueryRow(ctx, `
		SELECT count(*)::int FROM content_item_versions v
		JOIN game_questions gq ON gq.item_id = v.item_id AND gq.item_version = v.version
		WHERE gq.game_id = $1`, gameID).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere != 1 {
		t.Error("deleting the source content erased the round it was played in")
	}
}

// Freezing the same version twice stores it once: an item played a thousand times
// costs one row per version, not one per deal.
func TestFreezingIsIdempotent(t *testing.T) {
	d := dbtest.Open(t)
	defer d.Close()
	ctx := context.Background()

	var nodeID, itemID any
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO nodes (slug, node_type, status) VALUES ('once-node', 'node', 'published')
		RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, `
		INSERT INTO content_items (node_id, type_code, status, difficulty, points)
		VALUES ($1, 'question', 'published', 1, 10) RETURNING id`, nodeID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := d.Pool.Exec(ctx, `SELECT freeze_content_item($1)`, itemID); err != nil {
			t.Fatal(err)
		}
	}
	var rows int
	if err := d.Pool.QueryRow(ctx,
		`SELECT count(*)::int FROM content_item_versions WHERE item_id = $1`, itemID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("freezing one version five times stored %d rows, want 1", rows)
	}
}

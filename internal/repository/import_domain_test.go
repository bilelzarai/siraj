package repository_test

import (
	"context"
	"testing"
)

// The sweep that tells an operator why the bank is quiet. A live question in a
// live category inside a retired subject area is reachable by nobody, and
// before this there was nothing on any screen that said so.
func TestTheIntegritySweepNamesARetiredSubjectArea(t *testing.T) {
	r := repo(t)
	ctx := context.Background()

	var domainID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO domains (slug, icon, color, sort_order, is_active)
		VALUES ('swept','🧹','#334155',95,true) RETURNING id`).Scan(&domainID); err != nil {
		t.Fatalf("creating a subject area: %v", err)
	}
	var categoryID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO categories (slug, icon, color, sort_order, domain_id, is_active)
		VALUES ('swept-cat','🧽','#334155',96,$1,true) RETURNING id`, domainID).Scan(&categoryID); err != nil {
		t.Fatalf("creating a category: %v", err)
	}
	var questionID int
	if err := testPool.QueryRow(ctx, `
		INSERT INTO questions (category_id, difficulty, points, correct_index, is_active)
		VALUES ($1, 1, 10, 0, true) RETURNING id`, categoryID).Scan(&questionID); err != nil {
		t.Fatalf("creating a question: %v", err)
	}

	reported := func(t *testing.T, kind string) bool {
		t.Helper()
		issues, err := r.IntegrityIssues(ctx)
		if err != nil {
			t.Fatalf("sweep: %v", err)
		}
		for _, issue := range issues {
			if issue.QuestionID == questionID && issue.Kind == kind {
				return true
			}
		}
		return false
	}

	if reported(t, "orphan_domain") {
		t.Fatal("a live subject area is reported as retired")
	}
	if _, err := testPool.Exec(ctx, `UPDATE domains SET is_active = false WHERE id = $1`, domainID); err != nil {
		t.Fatalf("retiring: %v", err)
	}
	if !reported(t, "orphan_domain") {
		t.Error("a question nobody can reach is not reported: the sweep calls the bank healthy")
	}
	// And it is not confused with the check one level down.
	if reported(t, "orphan_category") {
		t.Error("the retired subject area was reported as a retired category")
	}
}

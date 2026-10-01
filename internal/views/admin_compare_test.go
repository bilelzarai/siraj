package views

import (
	"context"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
)

func testCtx(t *testing.T) Ctx {
	t.Helper()
	bundle, err := i18n.New("en")
	if err != nil {
		t.Fatalf("load catalogs: %v", err)
	}
	return Ctx{Tr: bundle.Printer("en"), Locale: "en", Dir: "ltr"}
}

func renderCompare(t *testing.T, cmp *models.QuestionCompare, chrome CompareChrome) string {
	t.Helper()
	var out strings.Builder
	if err := comparePanel(testCtx(t), cmp, chrome).Render(context.Background(), &out); err != nil {
		t.Fatalf("render: %v", err)
	}
	return out.String()
}

// importChrome is how the import preview dresses the panel.
func importChrome(line int) CompareChrome {
	return CompareChrome{
		Anchor:    ImportAnchor(line),
		Title:     "Row",
		CloseHref: "#import-report",
	}
}

// The panel's whole job is to put the differing word in front of the reader, so
// the spacing around a highlight has to survive being written as HTML — without
// it the sentence renders as "the Battle ofBadrtook place".
func TestImportCompareKeepsSpacingAroundHighlights(t *testing.T) {
	cmp := &models.QuestionCompare{
		Line: 758, Percent: 86, RightID: 1050100069, Differences: 1,
		Locales: []models.CompareLocale{{
			Code: "en", Name: "English", Dir: "ltr", InLeft: true, InRight: true,
			Prompt: models.ComparePair{
				Left:  "The Battle of Badr took place",
				Right: "The Battle of Uhud took place",
				LeftDiff: []models.DiffSpan{
					{Text: "The Battle of ", Same: true},
					{Text: "Badr ", Same: false},
					{Text: "took place", Same: true},
				},
				RightDiff: []models.DiffSpan{
					{Text: "The Battle of ", Same: true},
					{Text: "Uhud ", Same: false},
					{Text: "took place", Same: true},
				},
			},
		}},
	}

	html := renderCompare(t, cmp, importChrome(cmp.Line))
	for _, want := range []string{
		`The Battle of <mark class="cmp__diff cmp__diff--left">Badr </mark>took place`,
		`The Battle of <mark class="cmp__diff cmp__diff--right">Uhud </mark>took place`,
		`id="cmp-758"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered panel is missing %q", want)
		}
	}
}

// Two rows that are the same question field for field say so, rather than
// leaving an admin to read four languages before reaching that conclusion.
func TestImportCompareAnnouncesAnExactMatch(t *testing.T) {
	html := renderCompare(t, &models.QuestionCompare{
		Line: 12, Percent: 100, RightID: 900, Identical: true,
	}, importChrome(12))
	if !strings.Contains(html, "The same question, field for field") {
		t.Error("an identical pair was rendered without saying so")
	}
}

// The content-health sweep dresses the same panel differently: two questions
// that both exist, so both column heads are links, and the panel is addressed
// by the pair rather than by a row of a file.
func TestComparePanelLinksBothSidesForABankPair(t *testing.T) {
	html := renderCompare(t, &models.QuestionCompare{
		LeftID: 1010100023, RightID: 1010100024, Percent: 88, Differences: 3,
	}, CompareChrome{
		Anchor:    DuplicateAnchor(1010100023, 1010100024),
		Title:     "Question 1010100023 beside question 1010100024",
		CloseHref: "#duplicates",
	})

	for _, want := range []string{
		`id="dup-1010100023-1010100024"`,
		`href="/admin/questions/1010100023/edit"`,
		`href="/admin/questions/1010100024/edit"`,
		`href="#duplicates"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered panel is missing %q", want)
		}
	}
}

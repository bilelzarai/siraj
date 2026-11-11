package catalog

import (
	"testing"

	"siraj/internal/view"
)

// The "Your domains" grid is two rows of three and the flagship fills two of those
// slots, so a page holds six slots — not six cards (HM-05).
func TestPackDomainsNeverSpillsPastTwoRows(t *testing.T) {
	mine := make([]view.Domain, 7)
	for i := range mine {
		mine[i] = view.Domain{ID: int64(i + 1)}
	}
	mine[3].IsFeatured = true
	featured := &mine[3]

	grid := packDomains(mine, featured, true)
	for n, page := range grid {
		used := 0
		for _, d := range page.Domains {
			if d.ID == featured.ID {
				used += 2
			} else {
				used++
			}
		}
		if page.Add {
			used++
		}
		if used > slotsPerPage {
			t.Fatalf("page %d fills %d slots, which is a third row", n+1, used)
		}
	}
	if got := grid[0].Domains[0].ID; got != featured.ID {
		t.Fatalf("the flagship opens the grid: first card is %d, want %d", got, featured.ID)
	}
	if grid[0].Add {
		t.Fatal("the Add card belongs on the last page, not the first")
	}
	if !grid[len(grid)-1].Add {
		t.Fatal("the last page is missing the Add card")
	}

	// Every domain is shown exactly once, across the pages.
	seen := map[int64]int{}
	for _, page := range grid {
		for _, d := range page.Domains {
			seen[d.ID]++
		}
	}
	if len(seen) != len(mine) {
		t.Fatalf("showed %d domains, have %d", len(seen), len(mine))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("domain %d appears %d times", id, n)
		}
	}
}

// Seven domains and a flagship come to 2 + 6 + 1 (Add) = 9 slots, which is two pages.
func TestPackDomainsPageCount(t *testing.T) {
	for _, tc := range []struct {
		name     string
		count    int
		flagship bool
		rest     bool
		want     int
	}{
		{"one domain, nothing left to add", 1, true, false, 1},
		{"five singles fill one page", 5, false, true, 1},
		{"flagship plus four fills one page", 5, true, false, 1},
		{"seven with a flagship and more to add", 7, true, true, 2},
		{"no domains at all", 0, false, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mine := make([]view.Domain, tc.count)
			for i := range mine {
				mine[i] = view.Domain{ID: int64(i + 1)}
			}
			var featured *view.Domain
			if tc.flagship && tc.count > 0 {
				mine[0].IsFeatured = true
				featured = &mine[0]
			}
			if got := len(packDomains(mine, featured, tc.rest)); got != tc.want {
				t.Fatalf("got %d pages, want %d", got, tc.want)
			}
		})
	}
}

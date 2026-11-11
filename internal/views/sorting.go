package views

import (
	"net/url"

	"github.com/bilelzarai/siraj/internal/repository"
)

// SortState is everything a sortable header needs: what the list is sorted by,
// and how to build the address that changes it.
//
// The query is carried whole so a sort keeps the filter and the page size it
// was asked from. The page number is not: changing the sort reorders the whole
// list, so page nine of the old order is not a place in the new one, and
// landing there would look like rows had vanished.
type SortState struct {
	Sort  repository.Sort
	Path  string
	Query url.Values
}

// SortHref is where this header points.
func (s SortState) SortHref(column string, descFirst bool) string {
	q := url.Values{}
	for k, v := range s.Query {
		if k == "sort" || k == "dir" || k == "page" {
			continue
		}
		q[k] = v
	}
	q.Set("sort", column)
	if s.Sort.Next(column, descFirst) {
		q.Set("dir", "desc")
	}
	if len(q) == 0 {
		return s.Path
	}
	return s.Path + "?" + q.Encode()
}

// Sorted reports whether the list is in anything other than its natural order,
// which is what decides whether a way back to that order is offered.
func (s SortState) Sorted() bool { return s.Sort.Active() }

// ResetHref is the way back to the list's own order.
func (s SortState) ResetHref() string {
	q := url.Values{}
	for k, v := range s.Query {
		if k == "sort" || k == "dir" || k == "page" {
			continue
		}
		q[k] = v
	}
	if len(q) == 0 {
		return s.Path
	}
	return s.Path + "?" + q.Encode()
}

// SortCol describes one sortable column.
//
// A struct rather than six positional arguments: a header carries a column
// key, a label, which way it should sort first, whether it is numeric and
// which responsive class its cell has, and five of those are easy to transpose
// when they are all written as bare values in a row.
type SortCol struct {
	Column string
	Label  string
	// DescFirst is what the first press asks for. A name reads A to Z; a
	// count, a date and a score read largest and newest first, because that is
	// the end of them anybody is looking for.
	DescFirst bool
	// Numeric right-aligns the column, the way the kit's tables do.
	Numeric bool
	// Class is the responsive class the cell carries — the columns that go
	// away at narrow widths have to keep it on the <th> as well as the <td>,
	// or the header stays when its column leaves.
	Class string
}


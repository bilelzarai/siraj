package handlers

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/bilelzarai/siraj/internal/views"
)

// PageSizes is what a list offers. Small enough at one end to check a change
// quickly, large enough at the other to scan a whole category — and a closed
// set, because the size goes into a LIMIT.
var PageSizes = []int{5, 10, 20, 50}

// DefaultPageSize is what a list shows when nobody has chosen.
const DefaultPageSize = 20

// pageSizeCookie remembers the choice, so it survives the next visit and every
// other list. Somebody who wants ten rows wants ten rows everywhere.
const pageSizeCookie = "siraj_rows"

// Paging is one list's window onto its rows: which page, how many per page, and
// how many pages that works out to.
type Paging struct {
	Page  int
	Size  int
	Total int
	Pages int
	// Sizes is the offer, carried so the view does not have to import it.
	Sizes []int
	// Path and Query are what the pager links rebuild from, so paging never
	// drops the search and filters the list was showing.
	Path  string
	Query url.Values
}

// paging reads the page and the size from the request, remembering the size.
//
// Four screens had four hardcoded constants and no way to change any of them
// from the page. The size is validated against PageSizes rather than clamped,
// because a number that came from a query string has no business reaching a
// LIMIT unchecked.
func (h *Handlers) paging(w http.ResponseWriter, r *http.Request) Paging {
	size := 0
	if v := r.URL.Query().Get("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && allowedSize(n) {
			size = n
			http.SetCookie(w, &http.Cookie{
				Name: pageSizeCookie, Value: strconv.Itoa(n), Path: "/",
				HttpOnly: true, Secure: h.cfg.SecureCookies,
				SameSite: http.SameSiteLaxMode, MaxAge: 365 * 24 * 60 * 60,
			})
		}
	}
	if size == 0 {
		if c, err := r.Cookie(pageSizeCookie); err == nil {
			if n, err := strconv.Atoi(c.Value); err == nil && allowedSize(n) {
				size = n
			}
		}
	}
	if size == 0 {
		size = DefaultPageSize
	}

	page := queryInt(r, "page", 1)
	if page < 1 {
		page = 1
	}

	query := r.URL.Query()
	query.Del("page")
	query.Del("size")

	return Paging{Page: page, Size: size, Sizes: PageSizes, Path: r.URL.Path, Query: query}
}

// withTotal fills in the row count and the page count, and pulls an
// out-of-range page back to the last one that has rows — which is where a
// deletion or a narrower filter can leave someone.
func (p Paging) withTotal(total int) Paging {
	p.Total = total
	p.Pages = pageCount(total, p.Size)
	if p.Pages > 0 && p.Page > p.Pages {
		p.Page = p.Pages
	}
	return p
}

// Offset is the first row of this page.
func (p Paging) Offset() int { return (p.Page - 1) * p.Size }

// URL rebuilds this list's address with a different page and size, keeping
// every filter that was on it.
func (p Paging) URL(page, size int) string {
	q := url.Values{}
	for k, v := range p.Query {
		q[k] = v
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if size > 0 && size != DefaultPageSize {
		q.Set("size", strconv.Itoa(size))
	}
	if len(q) == 0 {
		return p.Path
	}
	return p.Path + "?" + q.Encode()
}

// PageURL is the same page of rows at a different offset.
func (p Paging) PageURL(page int) string { return p.URL(page, p.Size) }

// SizeURL is the same list at a different number of rows, from the top:
// changing the size while on page nine of ten would otherwise land on a page
// that no longer exists.
func (p Paging) SizeURL(size int) string { return p.URL(1, size) }

// Range is the human "showing 21–40 of 96", which is the sentence that makes a
// hard cap visible instead of leaving a list to simply stop.
func (p Paging) Range() (int, int) {
	if p.Total == 0 {
		return 0, 0
	}
	first := p.Offset() + 1
	last := p.Offset() + p.Size
	if last > p.Total {
		last = p.Total
	}
	return first, last
}

// pagerFor is the view's half of the same window.
func pagerFor(p Paging) views.Pager {
	first, last := p.Range()
	return views.Pager{
		Page: p.Page, Pages: p.Pages, Total: p.Total, Size: p.Size,
		Sizes: p.Sizes, First: first, Last: last,
		PageHref: p.PageURL, SizeHref: p.SizeURL,
	}
}

func allowedSize(n int) bool {
	for _, s := range PageSizes {
		if s == n {
			return true
		}
	}
	return false
}

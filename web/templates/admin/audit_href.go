package admin

import (
	"net/url"
	"strconv"
)

// auditHref rebuilds A11's own address with one parameter replaced, so every control
// in the toolbar keeps what the others have chosen. Changing a filter sends the
// reader back to the first page: page 3 of the old filter is not page 3 of the new one.
func auditHref(path string, d AuditData, set ...string) string {
	q := url.Values{}
	if d.Q != "" {
		q.Set("q", d.Q)
	}
	if d.Actor != "" {
		q.Set("actor", d.Actor)
	}
	if d.Area != "" {
		q.Set("area", d.Area)
	}
	if d.Days > 0 {
		q.Set("days", strconv.Itoa(d.Days))
	}
	if d.Page > 1 {
		q.Set("page", strconv.Itoa(d.Page))
	}
	for i := 0; i+1 < len(set); i += 2 {
		key, value := set[i], set[i+1]
		if value == "" {
			q.Del(key)
		} else {
			q.Set(key, value)
		}
		if key != "page" {
			q.Del("page")
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

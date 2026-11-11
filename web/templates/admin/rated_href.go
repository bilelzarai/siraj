package admin

import (
	"net/url"
	"strconv"

	"siraj/internal/view"
)

// ratedHref rebuilds A09's address with one parameter replaced, so the range and the
// reason keep each other's choice. Anything but the page sends the reader to page one.
func ratedHref(d RatedData, set ...string) string {
	q := url.Values{}
	if d.Days > 0 && d.Days != 30 {
		q.Set("days", strconv.Itoa(d.Days))
	}
	if d.Reason != "" {
		q.Set("reason", d.Reason)
	}
	if d.Page > 1 {
		q.Set("page", strconv.Itoa(d.Page))
	}
	for i := 0; i+1 < len(set); i += 2 {
		key, value := set[i], set[i+1]
		if value == "" || (key == "days" && value == "30") {
			q.Del(key)
		} else {
			q.Set(key, value)
		}
		if key != "page" {
			q.Del("page")
		}
	}
	if len(q) == 0 {
		return "/admin/rated"
	}
	return "/admin/rated?" + q.Encode()
}

// reasonLabel names a reason in the reader's language, falling back to the value the
// player's screen recorded. Reasons come from the data, so a new one shows what it is
// rather than a raw key (§14).
func reasonLabel(v *view.Ctx, reason string) string {
	if key := "rate.reason." + reason; v.Has(key) {
		return v.Tr(key)
	}
	return reason
}

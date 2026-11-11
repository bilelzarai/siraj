package admin

import (
	"net/url"
	"strconv"

	"siraj/internal/view"
)

// commentHref rebuilds A08's address with one parameter replaced, so the tabs, the
// search and the language picker keep each other's choices. Changing anything but the
// page sends the reader back to page one: page three of one tab is not page three of
// another.
func commentHref(d CommentsData, set ...string) string {
	q := url.Values{}
	if d.Tab != "" && d.Tab != "needs" {
		q.Set("tab", d.Tab)
	}
	if d.Query != "" {
		q.Set("q", d.Query)
	}
	if d.Lang != "" {
		q.Set("lang", d.Lang)
	}
	if d.Page > 1 {
		q.Set("page", strconv.Itoa(d.Page))
	}
	for i := 0; i+1 < len(set); i += 2 {
		key, value := set[i], set[i+1]
		if value == "" || (key == "tab" && value == "needs") {
			q.Del(key)
		} else {
			q.Set(key, value)
		}
		if key != "page" {
			q.Del("page")
		}
	}
	if len(q) == 0 {
		return "/admin/comments"
	}
	return "/admin/comments?" + q.Encode()
}

// flaggedLabel reads one report as one report, not as "1 reports". The same shape the
// player screens use (pages.countOfQuestions): this stylesheet has no plural rules,
// so the singular is its own string.
func flaggedLabel(v *view.Ctx, n int) string {
	if n == 1 {
		return v.Tr("admin.flagged_one")
	}
	return v.Tr("admin.flagged_n", "n", n)
}

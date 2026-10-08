package handlers_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/bilelzarai/siraj/internal/models"
)

// The drawer the directory's eye opens. A fragment, fetched when it is opened,
// so nothing about it is exercised by loading the list — and a fragment that
// arrives wrapped in a whole document is a drawer with a page inside it.
func TestTheUserPanelIsAFragmentWithTheAccountInIt(t *testing.T) {
	a := newApp(t)
	a.register("panelreader")
	a.promote("panelreader", models.RoleAdmin)

	other := newAppSharing(t, a)
	other.register("panelsubject")
	id := userIDByName(t, a, "panelsubject")

	// Something in the account's history, so the panel has a trail to show.
	if status, _ := a.post("/admin/users/"+id+"/role",
		url.Values{"role": {models.RoleModerator}}); status != 303 {
		t.Fatal("could not change a role")
	}

	status, body := a.get("/admin/users/" + id + "/panel")
	if status != 200 {
		t.Fatalf("the panel → %d", status)
	}
	for _, shell := range []string{"<!doctype", "<html", `<aside class="sidebar"`} {
		if strings.Contains(strings.ToLower(body), shell) {
			t.Errorf("the panel carries page chrome (%q); it is rendered into a drawer", shell)
		}
	}
	for _, want := range []string{
		"drawer-head", "drawer-body", "drawer-foot",
		"panelsubject",                       // who it is
		`name="role"`,                        // the role, changed from here
		`action="/admin/users/` + id + `/suspend"`, // and the destructive half
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the panel is missing %q", want)
		}
	}
	// The history is read from the trail, so the role change just made is in it.
	if !strings.Contains(body, "timeline") {
		t.Error("the panel shows no moderation history")
	}

	// A panel for an account that is not there is a 404, not an empty drawer.
	if status, _ := a.get("/admin/users/00000000-0000-0000-0000-000000000000/panel"); status != 404 {
		t.Errorf("a panel for no account → %d, want 404", status)
	}
}

// The eye is a real link as well as a panel trigger: with no script it has to
// go somewhere useful rather than do nothing.
func TestTheEyeWorksWithoutScript(t *testing.T) {
	a := newApp(t)
	a.register("noscript")
	a.promote("noscript", models.RoleAdmin)

	_, body := a.get("/admin/users")
	if !strings.Contains(body, `data-panel="/admin/users/`) {
		t.Error("the eye does not name a panel to open")
	}
	if !strings.Contains(body, `href="/u/noscript"`) {
		t.Error("the eye is not a link without script")
	}
	// And exactly one drawer for the whole list, not one per row.
	if got := strings.Count(body, `data-panel-host`); got != 1 {
		t.Errorf("%d drawer hosts on the page, want 1", got)
	}
}

// Everyone / Players / Staff is the split the directory is used to make, and
// the counts beside the tabs have to be of the whole directory rather than of
// the page.
func TestTheDirectorySplitsByKind(t *testing.T) {
	a := newApp(t)
	a.register("kindadmin")
	a.promote("kindadmin", models.RoleAdmin)
	player := newAppSharing(t, a)
	player.register("kindplayer")

	_, staffOnly := a.get("/admin/users?kind=staff")
	if !strings.Contains(staffOnly, "kindadmin") {
		t.Error("staff does not include an admin")
	}
	if strings.Contains(staffOnly, "@kindplayer") {
		t.Error("staff includes a player")
	}

	_, playersOnly := a.get("/admin/users?kind=player")
	if !strings.Contains(playersOnly, "@kindplayer") {
		t.Error("players does not include a player")
	}
	if strings.Contains(playersOnly, "@kindadmin") {
		t.Error("players includes an admin")
	}

	// An unknown kind shows everyone rather than nothing.
	_, all := a.get("/admin/users?kind=nonsense")
	if !strings.Contains(all, "@kindplayer") || !strings.Contains(all, "@kindadmin") {
		t.Error("an unrecognised kind narrowed the directory")
	}
}

package settings

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/pages"
)

// registerSettings mounts P29 (OT-02). Every section posts on its own so one failure
// never loses the rest of the page.
func (w *Web) registerSettings(public, private *gin.RouterGroup) {
	private.GET("/settings", w.page)
	private.POST("/settings/profile", w.saveProfile)
	private.POST("/settings/content", w.saveContent)
	private.POST("/settings/goal", w.saveGoal)
	private.POST("/settings/notifications", w.saveNotifications)
	private.POST("/settings/privacy", w.savePrivacy)
	private.GET("/settings/delete", w.deletePage)
	private.POST("/settings/delete", w.deleteAccount)
}

// notifyDefaults and privacyDefaults are what a new player gets; the jsonb bags only
// ever hold these keys, so a missing key means "the default".
var notifyDefaults = map[string]bool{"qotd": true, "challenges": true, "messages": true, "rooms": true, "weekly": false}

var privacyDefaults = map[string]string{"challenge_from": "friends", "message_from": "friends"}

func (w *Web) data(c *gin.Context, v *view.Ctx) (pages.SettingsData, error) {
	d := pages.SettingsData{
		Name: v.DisplayName, Username: v.Username, IslamicOnly: v.IslamicOnly,
		Theme: v.Theme, DailyGoal: 5, ShowOnBoards: true,
		Notify: map[string]bool{}, Privacy: map[string]string{},
	}
	for k, val := range notifyDefaults {
		d.Notify[k] = val
	}
	for k, val := range privacyDefaults {
		d.Privacy[k] = val
	}
	if v.IsGuest() {
		return d, nil
	}
	s, err := w.svc.Get(c.Request.Context(), v.User)
	if err != nil {
		return d, err
	}
	d.DailyGoal = s.DailyGoal
	d.Theme = s.Theme
	d.IslamicOnly = s.IslamicOnly
	var notify map[string]bool
	if len(s.Notify) > 0 && json.Unmarshal(s.Notify, &notify) == nil {
		for k, val := range notify {
			d.Notify[k] = val
		}
	}
	var privacy map[string]any
	if len(s.Privacy) > 0 && json.Unmarshal(s.Privacy, &privacy) == nil {
		if on, ok := privacy["leaderboard"].(bool); ok {
			d.ShowOnBoards = on
		}
		for _, k := range []string{"challenge_from", "message_from"} {
			if val, ok := privacy[k].(string); ok {
				d.Privacy[k] = val
			}
		}
	}
	if user, err := w.svc.auth.Me(c.Request.Context(), v.User.ID); err == nil && user.Email.Valid {
		d.Email = user.Email.String
	}
	providers, err := w.svc.Providers(c.Request.Context(), v.User.ID)
	if err != nil {
		return d, err
	}
	d.HasGoogle, d.HasPassword = providers["google"], providers["password"]
	return d, nil
}

func (w *Web) page(c *gin.Context) {
	v := view.From(c)
	d, err := w.data(c, v)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Page(c, pages.Settings(v, d))
}

func (w *Web) saveProfile(c *gin.Context) {
	v := view.From(c)
	in := ProfileInput{DisplayName: c.PostForm("display_name"), Username: c.PostForm("username")}
	if err := w.svc.UpdateProfile(c.Request.Context(), v.User, in); err != nil {
		w.back(c, v, err)
		return
	}
	render.Toast(c, v.Tr("settings.saved"))
	render.Redirect(c, "/settings")
}

// saveContent owns the Islamic-only switch; turning it on repaints the whole shell,
// so the answer is a refresh (DM-09).
func (w *Web) saveContent(c *gin.Context) {
	v := view.From(c)
	on := c.PostForm("islamic_only") != ""
	if err := w.svc.SetIslamicOnly(c.Request.Context(), v.User, on); err != nil {
		w.back(c, v, err)
		return
	}
	render.Refresh(c)
}

func (w *Web) saveGoal(c *gin.Context) {
	v := view.From(c)
	goal := int32(atoiDefault(c.PostForm("daily_goal"), 5))
	if err := w.svc.Update(c.Request.Context(), v.User, Input{DailyGoal: &goal}); err != nil {
		w.back(c, v, err)
		return
	}
	render.Toast(c, v.Tr("settings.saved"))
	render.Redirect(c, "/settings")
}

func (w *Web) saveNotifications(c *gin.Context) {
	v := view.From(c)
	notify := map[string]bool{
		"qotd":       c.PostForm("notify_qotd") != "",
		"challenges": c.PostForm("notify_challenges") != "",
		"messages":   c.PostForm("notify_messages") != "",
		"rooms":      c.PostForm("notify_rooms") != "",
		"weekly":     c.PostForm("notify_weekly") != "",
	}
	raw, _ := json.Marshal(notify)
	if err := w.svc.Update(c.Request.Context(), v.User, Input{Notify: raw}); err != nil {
		w.back(c, v, err)
		return
	}
	render.Toast(c, v.Tr("settings.saved"))
	render.Redirect(c, "/settings")
}

func (w *Web) savePrivacy(c *gin.Context) {
	v := view.From(c)
	privacy := map[string]any{
		"leaderboard":    c.PostForm("show_on_boards") != "",
		"challenge_from": pickOne(c.PostForm("challenge_from"), "friends", "anyone", "none"),
		"message_from":   pickOne(c.PostForm("message_from"), "friends", "none"),
	}
	raw, _ := json.Marshal(privacy)
	if err := w.svc.Update(c.Request.Context(), v.User, Input{Privacy: raw}); err != nil {
		w.back(c, v, err)
		return
	}
	render.Toast(c, v.Tr("settings.saved"))
	render.Redirect(c, "/settings")
}

func (w *Web) deletePage(c *gin.Context) {
	v := view.From(c)
	render.Page(c, pages.DeleteAccount(v, ""))
}

func (w *Web) deleteAccount(c *gin.Context) {
	v := view.From(c)
	if err := w.svc.DeleteAccount(c.Request.Context(), v.User, c.PostForm("confirm")); err != nil {
		message := v.Tr("settings.delete_confirm_hint")
		if e, ok := apperr.As(err); ok && e.Status >= 500 {
			message = v.Tr("error.body")
			_ = c.Error(err)
		}
		render.Status(c, http.StatusUnprocessableEntity, pages.DeleteAccount(v, message))
		return
	}
	w.clearSession(c)
	render.Redirect(c, "/welcome")
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

// back re-renders Settings with the service's message on top, so a bad username does
// not throw away everything else the player typed.
func (w *Web) back(c *gin.Context, v *view.Ctx, err error) {
	message := v.Tr("error.body")
	status := http.StatusInternalServerError
	if e, ok := apperr.As(err); ok {
		status, message = e.Status, e.Message
	} else {
		_ = c.Error(err)
	}
	cp := v.Clone()
	cp.Flash = message
	d, dataErr := w.data(c, cp)
	if dataErr != nil {
		w.fail(c, v, dataErr)
		return
	}
	render.Status(c, status, pages.Settings(cp, d))
}

func atoiDefault(v string, def int) int {
	n := 0
	if v == "" {
		return def
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

func pickOne(value string, allowed ...string) string {
	for _, a := range allowed {
		if value == a {
			return value
		}
	}
	return allowed[0]
}

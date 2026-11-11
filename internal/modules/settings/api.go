package settings

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/apiutil"
	"siraj/internal/platform/authctx"
)

type API struct{ svc *Service }

func NewAPI(s *Service) *API { return &API{svc: s} }

func (a *API) Register(public, private *gin.RouterGroup) {
	private.GET("/me/settings", a.get)           // part of #11 / #13
	private.PATCH("/me/settings", a.update)      // #13
	private.POST("/me/onboarding", a.onboarding) // #14
}

// SettingsDTO is what clients read and write.
type SettingsDTO struct {
	Theme        string          `json:"theme"`
	DailyGoal    int32           `json:"daily_goal"`
	ReminderTime string          `json:"reminder_time,omitempty"`
	IslamicOnly  bool            `json:"islamic_only"`
	Notify       json.RawMessage `json:"notify"`
	Privacy      json.RawMessage `json:"privacy"`
}

func dto(s sqlc.UserSetting) SettingsDTO {
	out := SettingsDTO{
		Theme: s.Theme, DailyGoal: s.DailyGoal, IslamicOnly: s.IslamicOnly,
		Notify: json.RawMessage(s.Notify), Privacy: json.RawMessage(s.Privacy),
	}
	if s.ReminderTime.Valid {
		mins := s.ReminderTime.Microseconds / 60_000_000
		out.ReminderTime = pad2(int(mins/60)) + ":" + pad2(int(mins%60))
	}
	if len(out.Notify) == 0 {
		out.Notify = json.RawMessage("{}")
	}
	if len(out.Privacy) == 0 {
		out.Privacy = json.RawMessage("{}")
	}
	return out
}

func pad2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func (a *API) get(c *gin.Context) {
	s, err := a.svc.Get(c.Request.Context(), authctx.Get(c))
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dto(s))
}

type updateBody struct {
	Theme        *string          `json:"theme"`
	DailyGoal    *int32           `json:"daily_goal"`
	ReminderTime *string          `json:"reminder_time"`
	IslamicOnly  *bool            `json:"islamic_only"`
	Notify       *json.RawMessage `json:"notify"`
	Privacy      *json.RawMessage `json:"privacy"`
}

func (a *API) update(c *gin.Context) {
	var body updateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	in := Input{Theme: body.Theme, DailyGoal: body.DailyGoal, ReminderTime: body.ReminderTime, IslamicOnly: body.IslamicOnly}
	if body.Notify != nil {
		in.Notify = *body.Notify
	}
	if body.Privacy != nil {
		in.Privacy = *body.Privacy
	}
	who := authctx.Get(c)
	if err := a.svc.Update(c.Request.Context(), who, in); err != nil {
		apiutil.Fail(c, err)
		return
	}
	s, err := a.svc.Get(c.Request.Context(), who)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dto(s))
}

// onboarding is endpoint 14: the mobile app saves the same three steps in one call.
type onboardingBody struct {
	Lang         string  `json:"lang"`
	DomainIDs    []int64 `json:"domain_ids"`
	IslamicOnly  bool    `json:"islamic_only"`
	DailyGoal    int32   `json:"daily_goal"`
	ReminderTime string  `json:"reminder_time"`
}

func (a *API) onboarding(c *gin.Context) {
	var body onboardingBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apiutil.Fail(c, apiutil.BadJSON(err))
		return
	}
	who := authctx.Get(c)
	err := a.svc.SaveOnboarding(c.Request.Context(), who, OnboardingInput{
		Lang: body.Lang, DomainIDs: body.DomainIDs, IslamicOnly: body.IslamicOnly,
		DailyGoal: body.DailyGoal, ReminderTime: body.ReminderTime,
	})
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	s, err := a.svc.Get(c.Request.Context(), who)
	if err != nil {
		apiutil.Fail(c, err)
		return
	}
	apiutil.OK(c, dto(s))
}

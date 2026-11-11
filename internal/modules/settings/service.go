// Package settings owns what a player can change about their own account:
// theme, language, Islamic content only, daily goal, notifications and privacy
// (endpoints 12, 13 and the website's Settings screen).
package settings

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/modules/auth"
	"siraj/internal/modules/catalog"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

type Service struct {
	db      *db.DB
	auth    *auth.Service
	catalog *catalog.Service
}

func NewService(d *db.DB, a *auth.Service, cat *catalog.Service) *Service {
	return &Service{db: d, auth: a, catalog: cat}
}

var themes = map[string]bool{"system": true, "light": true, "dark": true}

// SetTheme stores the theme of a signed-in player (BR-04). Visitors keep it in a cookie.
func (s *Service) SetTheme(ctx context.Context, who *authctx.Principal, theme string) error {
	if !themes[theme] {
		return apperr.Invalid(map[string]string{"theme": "system, light or dark"})
	}
	return s.db.Q.SetTheme(ctx, sqlc.SetThemeParams{UserID: who.ID, Theme: theme})
}

// SetLang changes the language of the interface and of every question (LG-01, LG-03).
func (s *Service) SetLang(ctx context.Context, who *authctx.Principal, lang string) error {
	if !i18n.Known(lang) {
		return apperr.Invalid(map[string]string{"lang": "ar, en or fr"})
	}
	return s.db.Q.SetUserLang(ctx, sqlc.SetUserLangParams{ID: who.ID, Lang: lang})
}

// SetIslamicOnly switches the Islamic-content-only mode (DM-09). The player's general
// domains stay in user_domains and are only hidden (decision D-05).
func (s *Service) SetIslamicOnly(ctx context.Context, who *authctx.Principal, on bool) error {
	if err := s.db.Q.SetIslamicOnly(ctx, sqlc.SetIslamicOnlyParams{UserID: who.ID, IslamicOnly: on}); err != nil {
		return err
	}
	who.IslamicOnly = on
	return nil
}

// Input is a partial update of the settings screen; nil fields are left alone.
type Input struct {
	Theme         *string
	DailyGoal     *int32
	ReminderTime  *string // "HH:MM" or "" to clear it
	IslamicOnly   *bool
	Notify        []byte // jsonb bag
	Privacy       []byte // jsonb bag
	ClearReminder bool
}

func (s *Service) Update(ctx context.Context, who *authctx.Principal, in Input) error {
	fields := map[string]string{}
	p := sqlc.UpdateSettingsParams{UserID: who.ID, ClearReminder: in.ClearReminder}
	if in.Theme != nil {
		if !themes[*in.Theme] {
			fields["theme"] = "system, light or dark"
		}
		p.Theme = pgtype.Text{String: *in.Theme, Valid: true}
	}
	if in.DailyGoal != nil {
		if *in.DailyGoal < 1 || *in.DailyGoal > 100 {
			fields["daily_goal"] = "between 1 and 100 questions"
		}
		p.DailyGoal = pgtype.Int4{Int32: *in.DailyGoal, Valid: true}
	}
	if in.ReminderTime != nil {
		t := strings.TrimSpace(*in.ReminderTime)
		if t == "" {
			p.ClearReminder = true
		} else {
			hh, mm, ok := parseHHMM(t)
			if !ok {
				fields["reminder_time"] = "use HH:MM"
			} else {
				p.ReminderTime = pgtype.Time{Microseconds: int64(hh)*3600_000_000 + int64(mm)*60_000_000, Valid: true}
			}
		}
	}
	if in.IslamicOnly != nil {
		p.IslamicOnly = pgtype.Bool{Bool: *in.IslamicOnly, Valid: true}
	}
	if len(in.Notify) > 0 {
		p.Notify = in.Notify
	}
	if len(in.Privacy) > 0 {
		p.Privacy = in.Privacy
	}
	if len(fields) > 0 {
		return apperr.Invalid(fields)
	}
	if err := s.db.Q.UpdateSettings(ctx, p); err != nil {
		return err
	}
	if in.IslamicOnly != nil {
		who.IslamicOnly = *in.IslamicOnly
	}
	return nil
}

func (s *Service) Get(ctx context.Context, who *authctx.Principal) (sqlc.UserSetting, error) {
	return s.auth.Settings(ctx, who.ID)
}

// ProfileInput is the Settings › Profile form (endpoint 12 on the website).
type ProfileInput struct {
	DisplayName string
	Username    string
}

func (s *Service) UpdateProfile(ctx context.Context, who *authctx.Principal, in ProfileInput) error {
	p := auth.ProfileInput{}
	if name := strings.TrimSpace(in.DisplayName); name != "" {
		p.DisplayName = &name
	}
	if username := strings.TrimSpace(in.Username); username != "" {
		p.Username = &username
	}
	_, err := s.auth.UpdateProfile(ctx, who, p)
	return err
}

// Providers is which sign-in methods the account has (Settings › Account).
func (s *Service) Providers(ctx context.Context, id uuid.UUID) (map[string]bool, error) {
	return s.auth.Providers(ctx, id)
}

// DeleteAccount is endpoint 16; "DELETE" must be typed to confirm.
func (s *Service) DeleteAccount(ctx context.Context, who *authctx.Principal, confirm string) error {
	return s.auth.DeleteAccount(ctx, who, confirm)
}

func parseHHMM(v string) (int, int, bool) {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, false
	}
	hh, mm := 0, 0
	for i, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				return 0, 0, false
			}
			n = n*10 + int(r-'0')
		}
		if i == 0 {
			hh = n
		} else {
			mm = n
		}
	}
	if hh > 23 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// OnboardingInput is the three-step onboarding form (AU-04, endpoint 14).
// The chosen categories are not stored: no table holds category preferences, so the
// screen passes them to the first game setup instead (see docs/06-DECISIONS.md D-15).
type OnboardingInput struct {
	Lang         string
	DomainIDs    []int64
	IslamicOnly  bool
	DailyGoal    int32
	ReminderTime string
}

// SaveOnboarding stores the language, the player's domains and the daily goal in one go.
func (s *Service) SaveOnboarding(ctx context.Context, who *authctx.Principal, in OnboardingInput) error {
	if in.Lang != "" {
		if err := s.SetLang(ctx, who, in.Lang); err != nil {
			return err
		}
		who.Lang = in.Lang
	}
	// Islamic-only first: replacing the domains is checked against it (DM-09).
	if err := s.SetIslamicOnly(ctx, who, in.IslamicOnly); err != nil {
		return err
	}
	if len(in.DomainIDs) > 0 {
		if err := s.catalog.ReplaceMyDomains(ctx, who, in.DomainIDs, sqlc.UserDomainSourceOnboarding); err != nil {
			return err
		}
	}
	goal := in.DailyGoal
	if goal == 0 {
		goal = 5
	}
	update := Input{DailyGoal: &goal, ClearReminder: in.ReminderTime == ""}
	if in.ReminderTime != "" {
		update.ReminderTime = &in.ReminderTime
	}
	return s.Update(ctx, who, update)
}

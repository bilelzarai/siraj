package app

import (
	"context"

	"siraj/internal/http/middleware"
	"siraj/internal/modules/auth"
	"siraj/internal/modules/catalog"
	"siraj/internal/modules/challenges"
	"siraj/internal/modules/messages"
	"siraj/internal/platform/authctx"
)

// shell loads the part of every web page that lives around the screen itself:
// the viewer's name and level, their domains (DM-03) and the navigation badges.
type shell struct {
	auth       *auth.Service
	catalog    *catalog.Service
	challenges *challenges.Service
	messages   *messages.Service
}

func (s shell) Load(ctx context.Context, p *authctx.Principal, lang string) (middleware.ShellData, error) {
	out := middleware.ShellData{}
	user, err := s.auth.Me(ctx, p.ID)
	if err != nil {
		return out, err
	}
	out.DisplayName = user.DisplayName
	if user.Username.Valid {
		out.Username = user.Username.String
	}
	out.Level = user.Level
	out.XP = user.Xp
	out.XPNext = nextLevelXP(user.Level)
	out.IslamicOnly = p.IslamicOnly
	if settings, err := s.auth.Settings(ctx, p.ID); err == nil {
		out.Theme = settings.Theme
		out.IslamicOnly = settings.IslamicOnly
	}

	domains, err := s.catalog.MyDomains(ctx, p, lang)
	if err != nil {
		return out, err
	}
	out.MyDomains = catalog.ViewDomains(domains)

	// The two badges of the navigation: challenges waiting for me and unread
	// threads (CH-11, MS-06). A guest has neither, so the badge stays off.
	if p.Kind == "member" {
		if s.challenges != nil {
			if received, _, _, err := s.challenges.Counts(ctx, p); err == nil {
				out.Counts.Challenges = received
			}
		}
		if s.messages != nil {
			if n, err := s.messages.Unread(ctx, p.ID); err == nil {
				out.Counts.Messages = n
			}
		}
	}
	return out, nil
}

// nextLevelXP is the XP needed for the next level. Levels get wider as they go:
// level n needs 100 × n XP on top of the level before (progress module owns the real
// curve; the shell only needs the ring around the avatar).
func nextLevelXP(level int32) int32 {
	if level < 1 {
		level = 1
	}
	return 100 * level * (level + 1) / 2
}

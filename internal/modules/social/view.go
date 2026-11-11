package social

import (
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/web/templates/pages"

	"siraj/internal/platform/avatar"
)

// person maps a service Person onto the page's own type (pages never import a module).
func person(p Person) pages.Person {
	return pages.Person{
		ID: p.ID.String(), Username: p.Username, Name: p.Name,
		Initials: avatar.Initials(p.Name), Color: avatar.Color(p.ID.String()),
		Level: p.Level, Online: p.Online, IsFriend: p.IsFriend, Requested: p.Requested,
		Guest: p.IsGuest, Wins: p.Wins, Losses: p.Losses,
	}
}

func toText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

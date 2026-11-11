package app

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/modules/challenges"
	"siraj/internal/modules/messages"
	"siraj/internal/modules/rooms"
	"siraj/internal/modules/social"
	"siraj/internal/platform/authctx"
	"siraj/internal/view"
)

// roomsForMessages is the Rooms tab of P22, read from the live rooms module. It lives
// here, in the wiring, so neither module has to import the other (NF-01).
type roomsForMessages struct{ rooms *rooms.Service }

func (a roomsForMessages) RoomInbox(ctx context.Context, userID uuid.UUID) ([]messages.RoomRow, error) {
	list, err := a.rooms.PlayerRooms(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]messages.RoomRow, 0, len(list))
	for _, r := range list {
		out = append(out, messages.RoomRow{
			Code: r.Code, Title: r.Title, People: r.People,
			Preview: r.Preview, At: r.At, Members: r.Members, Open: r.Open,
		})
	}
	return out, nil
}

func (a roomsForMessages) RoomLines(ctx context.Context, who *authctx.Principal, code string) (messages.RoomRow, []messages.RoomLine, error) {
	row, lines, err := a.rooms.ChatByCode(ctx, who, code)
	if err != nil {
		return messages.RoomRow{}, nil, err
	}
	out := make([]messages.RoomLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, messages.RoomLine{
			Author: l.Author, Body: l.Body, Kind: l.Kind, System: l.System,
			Mine: l.Mine, At: l.When, FileName: l.FileName, FileSize: l.FileSize,
			FileURL: l.FileURL, Duration: l.Duration,
		})
	}
	return messages.RoomRow{
		Code: row.Code, Title: row.Title, People: row.People,
		Preview: row.Preview, At: row.At, Members: row.Members, Open: row.Open,
	}, out, nil
}

func (a roomsForMessages) RoomSay(ctx context.Context, who *authctx.Principal, code, body string, upload uuid.UUID, kind string) error {
	return a.rooms.SayByCode(ctx, who, code, body, upload, kind)
}

// peopleForMessages is who the "New message" dialog offers: a member picks a friend,
// a guest picks one of the guests they played with (RL-03, §4 of 03-BACKEND-CHANGES).
type peopleForMessages struct{ social *social.Service }

func (a peopleForMessages) MessageCandidates(ctx context.Context, who *authctx.Principal) ([]messages.Candidate, error) {
	list, err := a.social.MessagePeople(ctx, who)
	if err != nil {
		return nil, err
	}
	out := make([]messages.Candidate, 0, len(list))
	for _, p := range list {
		out = append(out, messages.Candidate{
			ID: p.ID, Name: p.Name, Note: p.Note, Online: p.Online, Guest: p.Guest,
		})
	}
	return out, nil
}

// blockForMessages is the red button of the block dialog: the social module owns the
// rule, the messages screen only asks for it (FR-07).
type blockForMessages struct{ social *social.Service }

func (a blockForMessages) BlockUser(c *gin.Context, other uuid.UUID) error {
	return a.social.Block(c.Request.Context(), view.From(c).User, other)
}

// challengesForMessages lets the composer post a challenge card into a thread
// without the messages module importing the challenges one (NF-01).
type challengesForMessages struct{ challenges *challenges.Service }

func (a challengesForMessages) Shareable(ctx context.Context, who *authctx.Principal, lang string) ([]messages.ChallengeCard, error) {
	rows, err := a.challenges.Shareable(ctx, who, lang)
	if err != nil {
		return nil, err
	}
	out := make([]messages.ChallengeCard, len(rows))
	for i, r := range rows {
		out[i] = messages.ChallengeCard{ID: r.ID, Title: r.Title, Questions: r.Questions, ScoreToBeat: r.ScoreToBeat}
	}
	return out, nil
}

func (a challengesForMessages) Card(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (messages.ChallengeCard, error) {
	r, err := a.challenges.Card(ctx, who, id, lang)
	if err != nil {
		return messages.ChallengeCard{}, err
	}
	return messages.ChallengeCard{ID: r.ID, Title: r.Title, Questions: r.Questions, ScoreToBeat: r.ScoreToBeat}, nil
}

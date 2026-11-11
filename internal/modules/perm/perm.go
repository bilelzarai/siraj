// Package perm answers one question for the whole platform: may this person do this
// to this thing (§7)?
//
// It exists because the answer was spread across modules — a check in the nodes
// service, another in the admin middleware, a third implied by a query's WHERE — and
// a rule that lives in three places is a rule that will disagree with itself. Every
// handler, query and event asks here instead.
//
// The model is deliberately small:
//
//	subject   who is asking — a guest, a player, or staff
//	action    what they want to do, from the eight §7 names
//	resource  what they want to do it to, with its owner and visibility
//
// Ownership and sharing are the two things that grant access; a platform role only
// ever widens it for staff. Nothing here hides a button: a screen asks the same
// question the handler does, so what is shown and what is allowed cannot drift.
package perm

import (
	"context"

	"github.com/google/uuid"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// Action is one of the eight §7 verbs.
type Action string

const (
	View     Action = "view"
	Play     Action = "play"
	Create   Action = "create"
	Edit     Action = "edit"
	Delete   Action = "delete"
	Share    Action = "share"
	Manage   Action = "manage"
	Moderate Action = "moderate"
)

// Level is how far a share reaches, in the order the database stores them. The order
// is the whole point: anyone who may edit may also play and view, so a check is one
// comparison.
type Level int

const (
	LevelNone Level = iota
	LevelView
	LevelPlay
	LevelEdit
	LevelManage
)

func levelOf(v string) Level {
	switch v {
	case "view":
		return LevelView
	case "play":
		return LevelPlay
	case "edit":
		return LevelEdit
	case "manage":
		return LevelManage
	default:
		return LevelNone
	}
}

// needs is the level an action demands of whoever is not the owner.
func needs(a Action) Level {
	switch a {
	case View:
		return LevelView
	case Play:
		return LevelPlay
	case Create, Edit, Delete:
		return LevelEdit
	case Share, Manage:
		return LevelManage
	case Moderate:
		// Moderation is a platform role, never something a share can grant.
		return LevelManage + 1
	default:
		return LevelManage + 1
	}
}

// Subject is who is asking. A nil principal is a visitor with no session.
type Subject struct {
	ID    uuid.UUID
	Staff bool
	Guest bool
}

// SubjectOf reads a subject from the request's principal.
func SubjectOf(who *authctx.Principal) Subject {
	if who == nil {
		return Subject{}
	}
	return Subject{ID: who.ID, Staff: who.IsStaff(), Guest: who.Kind == "guest"}
}

func (s Subject) signedIn() bool { return s.ID != uuid.Nil }

// Resource is what is being acted on. Kind names the table it came from so a future
// resource — a set, a challenge, a content item — answers through the same call.
type Resource struct {
	Kind       string
	ID         int64
	OwnerID    uuid.UUID
	Visibility string
	Status     string
}

// Official is content the platform owns.
func (r Resource) Official() bool { return r.OwnerID == uuid.Nil }

type Service struct{ db *db.DB }

func NewService(d *db.DB) *Service { return &Service{db: d} }

// Can answers whether the subject may take the action. It never returns a bare false
// for an error: a database that cannot be read is not a denial, and treating it as
// one would quietly lock people out.
func (s *Service) Can(ctx context.Context, who Subject, action Action, r Resource) (bool, error) {
	// Staff may do anything the console offers. This is the one place a platform role
	// short-circuits a resource rule, and it is deliberate: official content is
	// managed by the platform (§5).
	if who.Staff {
		return true, nil
	}

	// Official content: everyone may read and play what is published. Nobody but
	// staff may change it, however it is shared — §6.2 is explicit that a player
	// extends official content rather than editing it.
	if r.Official() {
		switch action {
		case View, Play:
			return r.Status == "published", nil
		case Create:
			// Creating *under* an official node is extending it, which any signed-in
			// player may do. The new node will be theirs, not the platform's.
			return who.signedIn() && r.Status == "published", nil
		default:
			return false, nil
		}
	}

	// A player's own content.
	if who.signedIn() && r.OwnerID == who.ID {
		return true, nil
	}

	// Public content of another player is readable and playable, nothing more.
	if r.Visibility == "public" && r.Status == "published" {
		if action == View || action == Play {
			return true, nil
		}
	}

	// Finally, a share. Only nodes carry shares today; other kinds fall through to a
	// denial rather than silently claiming a level nobody granted.
	if !who.signedIn() || r.Kind != "node" || r.ID == 0 {
		return false, nil
	}
	level, err := s.sharedLevel(ctx, r.ID, who.ID)
	if err != nil {
		return false, err
	}
	return level >= needs(action), nil
}

// Ensure is Can for a handler: it returns the error the caller should answer with.
//
// A denial on something the subject cannot even see is reported as not-found, because
// "forbidden" tells them the thing exists. Where they can already see it, forbidden is
// the honest answer and the more useful one.
func (s *Service) Ensure(ctx context.Context, who Subject, action Action, r Resource) error {
	ok, err := s.Can(ctx, who, action, r)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if visible, err := s.Can(ctx, who, View, r); err == nil && visible {
		return apperr.Forbidden("you cannot " + string(action) + " this")
	}
	return apperr.NotFound(r.Kind)
}

func (s *Service) sharedLevel(ctx context.Context, node int64, user uuid.UUID) (Level, error) {
	raw, err := s.db.Q.NodeSharedLevel(ctx, sqlc.NodeSharedLevelParams{Target: node, Who: user})
	if err != nil {
		return LevelNone, err
	}
	return levelOf(string(raw)), nil
}

// ---------------------------------------------------------------- sharing

// Grant opens a node to somebody at a level. Only whoever may manage access can do
// it, and sharing never changes who owns the node (§7).
func (s *Service) Grant(ctx context.Context, who Subject, r Resource, to uuid.UUID, level string) error {
	if err := s.Ensure(ctx, who, Share, r); err != nil {
		return err
	}
	if to == who.ID {
		return apperr.Invalid(map[string]string{"user": "that is you"})
	}
	if levelOf(level) == LevelNone {
		return apperr.Invalid(map[string]string{"level": "view, play, edit or manage"})
	}
	return s.db.Q.ShareNode(ctx, sqlc.ShareNodeParams{
		NodeID: r.ID, UserID: to, Level: sqlc.ShareLevel(level),
		InvitedBy: uuid.NullUUID{UUID: who.ID, Valid: who.signedIn()},
	})
}

// Revoke closes it again.
func (s *Service) Revoke(ctx context.Context, who Subject, r Resource, from uuid.UUID) error {
	if err := s.Ensure(ctx, who, Share, r); err != nil {
		return err
	}
	return s.db.Q.UnshareNode(ctx, sqlc.UnshareNodeParams{NodeID: r.ID, UserID: from})
}

// Shared is who a node is open to, for the screen that manages it.
type Shared struct {
	UserID uuid.UUID
	Name   string
	Level  string
}

func (s *Service) SharedWith(ctx context.Context, who Subject, r Resource) ([]Shared, error) {
	if err := s.Ensure(ctx, who, Share, r); err != nil {
		return nil, err
	}
	rows, err := s.db.Q.NodeShares(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Shared, 0, len(rows))
	for _, row := range rows {
		out = append(out, Shared{UserID: row.UserID, Name: row.DisplayName, Level: string(row.Level)})
	}
	return out, nil
}

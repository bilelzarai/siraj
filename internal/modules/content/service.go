// Package content is everything a player can be given to learn from (§8), hanging on
// a node of the universal hierarchy at any depth.
//
// One shape serves every kind. What all content has in common — where it hangs, who
// owns it, what it is worth, whether it is published — is a column; what only one
// kind has is a field in jsonb, described by a row in content_types. Adding a kind is
// that row, not a migration and not a branch in this file (§14).
//
// Access is never decided here: every read and write asks the permission service, so
// a flashcard obeys the same rules as the branch it hangs on.
package content

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/modules/perm"
	"siraj/internal/platform/apperr"
)

type Service struct {
	db   *db.DB
	perm *perm.Service
}

func NewService(d *db.DB, p *perm.Service) *Service { return &Service{db: d, perm: p} }

// Type is one kind of content, as the registry describes it.
type Type struct {
	Code     string
	Icon     string
	Fields   []string
	Playable bool
}

// Item is one piece of content. Fields holds whatever its kind carries, in the
// reader's language: {"front": …, "back": …} for a flashcard.
type Item struct {
	ID         int64
	NodeID     int64
	Type       string
	Icon       string
	Playable   bool
	OwnerID    uuid.UUID
	Status     string
	Visibility string
	Difficulty int
	Points     int
	TimeLimit  int
	Tags       []string
	Fields     map[string]any
}

// Official is content the platform owns.
func (i Item) Official() bool { return i.OwnerID == uuid.Nil }

// Title is the one line a list shows. Each kind names its headline differently, so
// the first field the kind declares is the one shown — no per-kind branch here.
func (i Item) Title(order []string) string {
	for _, f := range order {
		if s, ok := i.Fields[f].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	for _, f := range []string{"front", "body", "instructions", "question"} {
		if s, ok := i.Fields[f].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Types lists the kinds a writer may choose from.
func (s *Service) Types(ctx context.Context) ([]Type, error) {
	rows, err := s.db.Q.ContentTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Type, 0, len(rows))
	for _, r := range rows {
		out = append(out, Type{Code: r.Code, Icon: r.Icon, Fields: r.Fields, Playable: r.Playable})
	}
	return out, nil
}

// OnNode lists what hangs on a node, or on its whole branch. The caller passes the
// node it has already been allowed to read; this does not re-open that question, but
// it does hide drafts that are not the viewer's.
func (s *Service) OnNode(ctx context.Context, who perm.Subject, node int64, typeCode string, subtree bool, page, perPage int, lang string) ([]Item, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 25
	}
	viewer := uuid.NullUUID{UUID: who.ID, Valid: who.ID != uuid.Nil}
	rows, err := s.db.Q.ContentOnNode(ctx, sqlc.ContentOnNodeParams{
		Lang: lang, NodeID: node, Subtree: subtree, TypeCode: typeCode,
		Staff: who.Staff, Viewer: viewer,
		Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountContentOnNode(ctx, sqlc.CountContentOnNodeParams{
		NodeID: node, Subtree: subtree, TypeCode: typeCode, Staff: who.Staff, Viewer: viewer,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		i := Item{
			ID: r.ID, NodeID: r.NodeID, Type: r.TypeCode, Icon: r.Icon, Playable: r.Playable,
			Status: string(r.Status), Visibility: string(r.Visibility),
			Difficulty: int(r.Difficulty), Points: int(r.Points), TimeLimit: int(r.TimeLimit),
			Tags: r.Tags,
		}
		if r.OwnerID.Valid {
			i.OwnerID = r.OwnerID.UUID
		}
		if len(r.Fields) > 0 {
			_ = json.Unmarshal(r.Fields, &i.Fields)
		}
		out = append(out, i)
	}
	return out, int(total), nil
}

// Input is a new piece of content.
type Input struct {
	NodeID     int64
	Type       string
	Difficulty int
	Points     int
	TimeLimit  int
	Tags       []string
	Status     string
	Visibility string
	// Fields per language: Fields["en"]["front"]. The reader's own language must be
	// filled; the rest may follow (§16).
	Fields map[string]map[string]any
	// Answer is what counts as right, in the shape the kind expects:
	// {"index": 2} · {"indexes": [0,3]} · {"yes": true} · {"text": ["zakat"]} ·
	// {"number": 5, "tolerance": 0.5}. It is language-independent on purpose — the
	// answer to "how many pillars" is the same number in every language.
	Answer map[string]any
}

// Create hangs a new item on a node. Whoever may create under that node may do it,
// which is the same question extending a branch asks — so content and structure obey
// one rule rather than two.
func (s *Service) Create(ctx context.Context, who perm.Subject, nodeRes perm.Resource, in Input) (Item, error) {
	if err := s.perm.Ensure(ctx, who, perm.Create, nodeRes); err != nil {
		return Item{}, err
	}
	kind, err := s.db.Q.ContentType(ctx, in.Type)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, apperr.Invalid(map[string]string{"type": "no such kind of content"})
	}
	if err != nil {
		return Item{}, err
	}
	if err := validate(kind, in); err != nil {
		return Item{}, err
	}

	owner := uuid.NullUUID{}
	if !who.Staff {
		owner = uuid.NullUUID{UUID: who.ID, Valid: true}
	}
	visibility := in.Visibility
	if visibility == "" {
		// The platform publishes; a player's work starts private and is theirs to
		// open up, the same default the hierarchy uses (§7).
		visibility = "private"
		if who.Staff {
			visibility = "public"
		}
	}

	var out Item
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateContentItem(ctx, sqlc.CreateContentItemParams{
			NodeID: in.NodeID, TypeCode: in.Type, Owner: owner,
			Visibility: sqlc.NodeVisibility(visibility),
			Status:     sqlc.NodeStatus(first(in.Status, "draft")),
			Difficulty: int16(clamp(in.Difficulty, 1, 3)),
			Points:     int32(or(in.Points, 10)),
			TimeLimit:  int32(in.TimeLimit),
			// The column is NOT NULL: a nil slice reaches Postgres as NULL, not as
			// an empty array.
			Tags:  tags(in.Tags),
			Actor: uuid.NullUUID{UUID: who.ID, Valid: who.ID != uuid.Nil},
		})
		if err != nil {
			return err
		}
		out = item(row, []byte("{}"), kind.Icon, kind.Playable)
		// What counts as right, when the kind is one that is answered at all.
		if len(in.Answer) > 0 {
			raw, err := json.Marshal(in.Answer)
			if err != nil {
				return err
			}
			if err := q.SetContentAnswer(ctx, sqlc.SetContentAnswerParams{ID: row.ID, Answer: raw}); err != nil {
				return err
			}
		}
		for _, lang := range i18n.Langs {
			fields := in.Fields[lang]
			if len(fields) == 0 {
				continue
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				return err
			}
			if err := q.SetContentText(ctx, sqlc.SetContentTextParams{
				ItemID: row.ID, Lang: lang, Fields: raw,
			}); err != nil {
				return err
			}
			if lang == i18n.Fallback || out.Fields == nil {
				out.Fields = fields
			}
		}
		return nil
	})
	return out, err
}

// Check compares a submitted answer against what the item records as right.
//
// The item is read here, when the answer arrives — never when the question is dealt,
// so the correct answer does not travel to the client ahead of the submission
// (§10.1). It returns whether the answer was right and what it was worth.
func (s *Service) Check(ctx context.Context, itemID int64, got Answer) (bool, int, error) {
	key, err := s.db.Q.ContentAnswerKey(ctx, itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, apperr.NotFound("content")
	}
	if err != nil {
		return false, 0, err
	}
	var want map[string]any
	if len(key.Answer) > 0 {
		_ = json.Unmarshal(key.Answer, &want)
	}
	return Correct(key.AnswerKind, want, got), int(key.Points), nil
}

// Get reads one item, refusing what the viewer may not see.
func (s *Service) Get(ctx context.Context, who perm.Subject, id int64, lang string) (Item, error) {
	row, err := s.db.Q.ContentItem(ctx, sqlc.ContentItemParams{ID: id, Lang: lang})
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, apperr.NotFound("content")
	}
	if err != nil {
		return Item{}, err
	}
	got := Item{
		ID: row.ID, NodeID: row.NodeID, Type: row.TypeCode,
		Status: string(row.Status), Visibility: string(row.Visibility),
		Difficulty: int(row.Difficulty), Points: int(row.Points), TimeLimit: int(row.TimeLimit),
		Tags: row.Tags,
	}
	if row.OwnerID.Valid {
		got.OwnerID = row.OwnerID.UUID
	}
	if len(row.Fields) > 0 {
		_ = json.Unmarshal(row.Fields, &got.Fields)
	}
	if err := s.perm.Ensure(ctx, who, perm.View, s.resource(got)); err != nil {
		return Item{}, err
	}
	return got, nil
}

// Delete takes an item out of play. It is soft: a match that used it has to keep
// making sense (§10.3).
func (s *Service) Delete(ctx context.Context, who perm.Subject, id int64) error {
	row, err := s.db.Q.ContentOwner(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("content")
	}
	if err != nil {
		return err
	}
	r := perm.Resource{
		Kind: "content", ID: row.ID,
		Visibility: string(row.Visibility), Status: string(row.Status),
	}
	if row.OwnerID.Valid {
		r.OwnerID = row.OwnerID.UUID
	}
	if err := s.perm.Ensure(ctx, who, perm.Delete, r); err != nil {
		return err
	}
	return s.db.Q.DeleteContentItem(ctx, id)
}

func (s *Service) resource(i Item) perm.Resource {
	return perm.Resource{
		Kind: "content", ID: i.ID, OwnerID: i.OwnerID,
		Visibility: i.Visibility, Status: i.Status,
	}
}

// validate checks the kind's own fields are present in at least one language. What a
// kind needs is read from the registry, so a new kind validates without new code.
func validate(kind sqlc.ContentType, in Input) error {
	if in.NodeID <= 0 {
		return apperr.Invalid(map[string]string{"node": "content has to hang somewhere"})
	}
	for _, lang := range i18n.Langs {
		if hasAll(in.Fields[lang], kind.Fields) {
			return nil
		}
	}
	missing := map[string]string{}
	for _, f := range kind.Fields {
		// "explanation" is the one field every kind treats as optional; the rest of
		// what a kind declares is what it is.
		if f == "explanation" {
			continue
		}
		missing[f] = "required"
	}
	if len(missing) == 0 {
		return nil
	}
	return apperr.Invalid(missing)
}

func hasAll(fields map[string]any, want []string) bool {
	if len(fields) == 0 {
		return false
	}
	for _, f := range want {
		if f == "explanation" {
			continue
		}
		switch v := fields[f].(type) {
		case string:
			if strings.TrimSpace(v) == "" {
				return false
			}
		case nil:
			return false
		}
	}
	return true
}

func item(r sqlc.ContentItem, fields []byte, icon string, playable bool) Item {
	out := Item{
		ID: r.ID, NodeID: r.NodeID, Type: r.TypeCode, Icon: icon, Playable: playable,
		Status: string(r.Status), Visibility: string(r.Visibility),
		Difficulty: int(r.Difficulty), Points: int(r.Points), TimeLimit: int(r.TimeLimit),
		Tags: r.Tags,
	}
	if r.OwnerID.Valid {
		out.OwnerID = r.OwnerID.UUID
	}
	if len(fields) > 0 {
		_ = json.Unmarshal(fields, &out.Fields)
	}
	return out
}

// tags keeps a nil slice from becoming a NULL.
func tags(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func first(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func or(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}

// ---------------------------------------------------------------- playing

// Playable is one item the engine has dealt. It deliberately carries no answer: the
// correct one is read when a submission arrives, never when a question goes out
// (§10.1).
type Playable struct {
	ID         int64
	NodeID     int64
	Type       string
	Difficulty int
	Points     int
	TimeLimit  int
}

// Pick is the engine's selection over the universal model: playable content from a
// branch, scoped the way §10.2 asks — direct, subtree, or both.
//
// It replaces nothing yet. The v1 engine still reads `questions`, and migration 26
// keeps the two in step, so this can be adopted one caller at a time (CUT-4).
func (s *Service) Pick(ctx context.Context, who perm.Subject, node int64, scope, typeCode string, difficulty, limit int, exclude []int64) ([]Playable, error) {
	if exclude == nil {
		exclude = []int64{}
	}
	rows, err := s.db.Q.PickContent(ctx, sqlc.PickContentParams{
		NodeID: node, NodeScope: nodeScope(scope), TypeCode: typeCode,
		Difficulty: int16(difficulty), Exclude: exclude,
		Viewer: uuid.NullUUID{UUID: who.ID, Valid: who.ID != uuid.Nil},
		Lim:    int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Playable, 0, len(rows))
	for _, r := range rows {
		out = append(out, Playable{
			ID: r.ID, NodeID: r.NodeID, Type: r.TypeCode,
			Difficulty: int(r.Difficulty), Points: int(r.Points), TimeLimit: int(r.TimeLimit),
		})
	}
	return out, nil
}

// Available says how much of a branch can actually be played, which is what a setup
// screen shows before anyone commits to a game.
func (s *Service) Available(ctx context.Context, who perm.Subject, node int64, scope, typeCode string, difficulty int) (int, error) {
	n, err := s.db.Q.CountPlayableContent(ctx, sqlc.CountPlayableContentParams{
		NodeID: node, NodeScope: nodeScope(scope), TypeCode: typeCode,
		Difficulty: int16(difficulty),
		Viewer:     uuid.NullUUID{UUID: who.ID, Valid: who.ID != uuid.Nil},
	})
	return int(n), err
}

// nodeScope keeps an unknown value from quietly widening a selection: anything the
// query does not know reads as the whole branch, never as "no scope at all".
func nodeScope(v string) string {
	switch v {
	case "direct", "subtree", "both":
		return v
	default:
		return "both"
	}
}

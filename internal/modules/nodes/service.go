// Package nodes is the universal hierarchy (§4): one tree of unlimited depth that
// holds the platform's official branches and every player's own, side by side.
//
// What used to be a domain is a node with no parent; what used to be a category is a
// node one level down. Nothing here knows about either word — a node is a node at any
// depth, which is what lets a tenth level exist without a migration.
//
// The service owns two rules the database cannot state on its own:
//   - who may see a node (Viewer), and
//   - who may change it (writable), which is where "a player never edits official
//     content" lives.
//
// Structure itself — paths, depth, cycles — is the database's job (migration 20), so
// no caller can write a broken tree whatever layer it comes from.
package nodes

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/modules/perm"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

type Service struct {
	db *db.DB
	// perm is the one authority on who may do what (§7). The hierarchy asks it
	// rather than deciding for itself.
	perm *perm.Service
}

func NewService(d *db.DB, p *perm.Service) *Service { return &Service{db: d, perm: p} }

// Viewer is who is asking. Staff see every branch; a player sees the official
// published tree plus their own; a guest sees only the official published tree.
type Viewer struct {
	ID    uuid.UUID
	Staff bool
	Lang  string
}

// ViewerOf reads the viewer off a principal. A nil principal is a visitor.
func ViewerOf(who *authctx.Principal, lang string) Viewer {
	v := Viewer{Lang: lang}
	if who != nil {
		v.ID, v.Staff = who.ID, who.IsStaff()
	}
	return v
}

func (v Viewer) id() uuid.NullUUID {
	return uuid.NullUUID{UUID: v.ID, Valid: v.ID != uuid.Nil}
}

// Node is one node as the screens need it.
type Node struct {
	ID        int64
	ParentID  int64
	RootID    int64
	Depth     int
	Name      string
	Slug      string
	Type      string
	Icon      string
	Color     string
	Status    string
	Visible   string
	OwnerID   uuid.UUID
	ExtendsID int64
	Position  int
	Children  int
	Questions int
	// Under is the official branch an extension hangs from, for the one line the
	// library prints under such a row.
	Under string
}

// Official is true for a node the platform owns. It is the one distinction the
// screens draw between branches (§6.2).
func (n Node) Official() bool { return n.OwnerID == uuid.Nil }

// Crumb is one step of the trail from the root down.
type Crumb struct {
	ID       int64
	Name     string
	Depth    int
	Official bool
}

// Stats is what a branch holds: used by the tree screen and by a challenge choosing
// between "direct content only" and "the whole branch" (§10.2).
type Stats struct {
	Descendants      int
	DirectQuestions  int
	SubtreeQuestions int
}

// Children is one page of a node's children. parent 0 means the roots — the list the
// product calls domains.
func (s *Service) Children(ctx context.Context, v Viewer, parent int64, page, perPage int) ([]Node, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	rows, err := s.db.Q.NodeChildren(ctx, sqlc.NodeChildrenParams{
		Lang: v.Lang, Staff: v.Staff, Viewer: v.id(), Parent: nullID(parent),
		Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountNodeChildren(ctx, sqlc.CountNodeChildrenParams{
		Staff: v.Staff, Viewer: v.id(), Parent: nullID(parent),
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]Node, 0, len(rows))
	for _, r := range rows {
		n := Node{
			ID: r.ID, Depth: int(r.Depth), Slug: r.Slug, Name: r.Name,
			Type: r.NodeType, Icon: r.Icon, Color: r.Color,
			Status: string(r.Status), Visible: string(r.Visibility),
			Position: int(r.Position),
			ParentID: r.ParentID.Int64, RootID: r.RootID.Int64,
			ExtendsID: r.ExtendsID.Int64,
			Children:  int(r.Children), Questions: int(r.Questions),
		}
		if r.OwnerID.Valid {
			n.OwnerID = r.OwnerID.UUID
		}
		out = append(out, n)
	}
	return out, int(total), nil
}

// Library is what "my library" opens on: everything the player owns that is an entry
// point — their own roots, and the branches they have hung under official nodes. An
// extension sits deep in the official tree, so listing only roots would leave the
// player no way back to their own work.
func (s *Service) Library(ctx context.Context, v Viewer, page, perPage int) ([]Node, int, error) {
	if v.ID == uuid.Nil {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 25
	}
	owner := uuid.NullUUID{UUID: v.ID, Valid: true}
	rows, err := s.db.Q.LibraryRoots(ctx, sqlc.LibraryRootsParams{
		Lang: v.Lang, Owner: owner, Lim: int32(perPage), Off: int32((page - 1) * perPage),
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.Q.CountLibraryRoots(ctx, owner)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Node, 0, len(rows))
	for _, r := range rows {
		n := Node{
			ID: r.ID, Name: r.Name, Slug: r.Slug, Icon: r.Icon, Color: r.Color,
			Status: string(r.Status), OwnerID: v.ID,
			Children: int(r.Children), Questions: int(r.Questions),
			ExtendsID: r.ExtendsID.Int64,
			// Under names the official branch an extension hangs from, so the row can
			// say where it lives without a second query.
			Under: r.Under,
		}
		out = append(out, n)
	}
	return out, int(total), nil
}

// Get reads one node, or answers not-found when the viewer may not see it. A node
// somebody may not read must not announce itself by answering "forbidden".
func (s *Service) Get(ctx context.Context, v Viewer, id int64) (Node, error) {
	row, err := s.db.Q.NodeByID(ctx, sqlc.NodeByIDParams{
		ID: id, Lang: v.Lang, Staff: v.Staff, Viewer: v.id(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, apperr.NotFound("node")
	}
	if err != nil {
		return Node{}, err
	}
	n := Node{
		ID: row.ID, Depth: int(row.Depth), Slug: row.Slug, Name: row.Name,
		Type: row.NodeType, Icon: row.Icon, Color: row.Color,
		Status: string(row.Status), Visible: string(row.Visibility),
		Position: int(row.Position),
		ParentID: row.ParentID.Int64, RootID: row.RootID.Int64,
		ExtendsID: row.ExtendsID.Int64,
	}
	if row.OwnerID.Valid {
		n.OwnerID = row.OwnerID.UUID
	}
	return n, nil
}

// Trail is the breadcrumb from the root down to and including the node.
func (s *Service) Trail(ctx context.Context, v Viewer, id int64) ([]Crumb, error) {
	rows, err := s.db.Q.NodeBreadcrumb(ctx, sqlc.NodeBreadcrumbParams{ID: id, Lang: v.Lang})
	if err != nil {
		return nil, err
	}
	out := make([]Crumb, 0, len(rows))
	for _, r := range rows {
		out = append(out, Crumb{
			ID: r.ID, Name: r.Name, Depth: int(r.Depth), Official: !r.OwnerID.Valid,
		})
	}
	return out, nil
}

func (s *Service) Stats(ctx context.Context, id int64) (Stats, error) {
	row, err := s.db.Q.NodeSubtreeStats(ctx, id)
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		Descendants:      int(row.Descendants),
		DirectQuestions:  int(row.DirectQuestions),
		SubtreeQuestions: int(row.SubtreeQuestions),
	}, nil
}

// Search finds nodes by name, optionally inside one branch, and prints the trail so a
// result ten levels down still says where it lives.
type Result struct {
	ID    int64
	Name  string
	Trail string
	Depth int
	Icon  string
	Color string
}

func (s *Service) Search(ctx context.Context, v Viewer, q string, within int64, limit int) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit < 1 {
		limit = 20
	}
	rows, err := s.db.Q.SearchNodes(ctx, sqlc.SearchNodesParams{
		Lang: v.Lang, Q: q, Within: nullID(within), Staff: v.Staff, Viewer: v.id(), Lim: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(rows))
	for _, r := range rows {
		out = append(out, Result{
			ID: r.ID, Name: r.Name, Trail: r.Trail,
			Depth: int(r.Depth), Icon: r.Icon, Color: r.Color,
		})
	}
	return out, nil
}

// Input is a new node or a change to one.
type Input struct {
	ParentID   int64
	ExtendsID  int64
	Type       string
	Slug       string
	Icon       string
	Color      string
	Position   int
	Status     string
	Visibility string
	// Names and Descriptions are keyed by language. The main language must be
	// present; the rest may follow later (§16).
	Names        map[string]string
	Descriptions map[string]string
}

// Create adds a node. Staff create official nodes; a player creates their own, and a
// player's node under an official parent is an extension — the official row is never
// touched (§6.2).
func (s *Service) Create(ctx context.Context, v Viewer, in Input) (Node, error) {
	if err := validate(in); err != nil {
		return Node{}, err
	}
	if v.ID == uuid.Nil && !v.Staff {
		return Node{}, apperr.Forbidden("sign in to create")
	}
	// A parent has to be one the viewer may build under.
	if in.ParentID > 0 {
		parent, err := s.db.Q.NodeOwner(ctx, in.ParentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return Node{}, apperr.Invalid(map[string]string{"parent": "no such node"})
		}
		if err != nil {
			return Node{}, err
		}
		// May this viewer build under that parent? Creating under an official node is
		// extending it, which the permission service allows; creating under somebody
		// else's private branch is not.
		parentRes := perm.Resource{
			Kind: "node", ID: in.ParentID,
			Visibility: string(parent.Visibility), Status: string(parent.Status),
		}
		if parent.OwnerID.Valid {
			parentRes.OwnerID = parent.OwnerID.UUID
		}
		if err := s.perm.Ensure(ctx, perm.Subject{ID: v.ID, Staff: v.Staff}, perm.Create, parentRes); err != nil {
			return Node{}, err
		}
		// A player building under an official node is extending it.
		if !v.Staff && !parent.OwnerID.Valid {
			in.ExtendsID = in.ParentID
		}
	}

	var out Node
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		owner := uuid.NullUUID{}
		if !v.Staff {
			owner = uuid.NullUUID{UUID: v.ID, Valid: true}
		}
		params := sqlc.CreateNodeParams{
			Parent: nullID(in.ParentID), Owner: owner, Extends: nullID(in.ExtendsID),
			NodeType:   first(in.Type, "node"),
			Visibility: sqlc.NodeVisibility(first(in.Visibility, defaultVisibility(v))),
			Status:     sqlc.NodeStatus(first(in.Status, "draft")),
			Slug:       in.Slug, Icon: in.Icon, Color: in.Color,
			Position: int32(in.Position),
			Actor:    v.id(),
		}
		// A slug only has to be unique among its siblings, and a player never types
		// one — it is derived from the name they gave. Two names that derive to the
		// same slug (two Arabic titles, say, which carry no Latin letters at all) must
		// not make the second one fail, so the collision is resolved here rather than
		// handed back as "slug already taken" for something nobody entered.
		row, err := q.CreateNode(ctx, params)
		for attempt := 2; err != nil && isDuplicateSlug(err) && attempt <= 50; attempt++ {
			params.Slug = in.Slug + "-" + strconv.Itoa(attempt)
			row, err = q.CreateNode(ctx, params)
		}
		if err != nil {
			return err
		}
		out = node(row, in.Names[i18n.Fallback])
		return s.writeText(ctx, q, row.ID, in)
	})
	return out, err
}

// Update changes a node the viewer owns.
func (s *Service) Update(ctx context.Context, v Viewer, id int64, in Input) (Node, error) {
	if err := s.writable(ctx, v, id); err != nil {
		return Node{}, err
	}
	var out Node
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateNode(ctx, sqlc.UpdateNodeParams{
			ID: id, Actor: v.id(),
			NodeType: text(in.Type), Slug: text(in.Slug), Icon: text(in.Icon), Color: text(in.Color),
			Visibility: visibility(in.Visibility), Status: status(in.Status),
			Position: position(in.Position),
		})
		if err != nil {
			return err
		}
		out = node(row, in.Names[i18n.Fallback])
		return s.writeText(ctx, q, id, in)
	})
	return out, err
}

// Move re-parents a node. The database refuses a loop, and the whole subtree follows.
func (s *Service) Move(ctx context.Context, v Viewer, id, parent int64) error {
	if err := s.writable(ctx, v, id); err != nil {
		return err
	}
	if parent > 0 {
		if err := s.writable(ctx, v, parent); err != nil {
			// Moving a branch under a node the viewer cannot build in is not allowed,
			// but staff may move official nodes anywhere.
			if !v.Staff {
				return err
			}
		}
	}
	_, err := s.db.Q.MoveNode(ctx, sqlc.MoveNodeParams{ID: id, Parent: nullID(parent), Actor: v.id()})
	if err != nil && strings.Contains(err.Error(), "inside its own subtree") {
		return apperr.Invalid(map[string]string{"parent": "a node cannot be moved inside itself"})
	}
	return err
}

// Archive takes a branch out of play, with everything under it.
func (s *Service) Archive(ctx context.Context, v Viewer, id int64) error {
	if err := s.writable(ctx, v, id); err != nil {
		return err
	}
	return s.db.Q.ArchiveNode(ctx, sqlc.ArchiveNodeParams{ID: id, Actor: v.id()})
}

func (s *Service) Restore(ctx context.Context, v Viewer, id int64) error {
	if err := s.writable(ctx, v, id); err != nil {
		return err
	}
	return s.db.Q.RestoreNode(ctx, sqlc.RestoreNodeParams{ID: id, Actor: v.id()})
}

// writable asks the permission service whether this viewer may change this node. The
// rule itself lives there, once, so the console, the library and a future API cannot
// answer it differently (PERM-1).
func (s *Service) writable(ctx context.Context, v Viewer, id int64) error {
	r, err := s.resource(ctx, id)
	if err != nil {
		return err
	}
	return s.perm.Ensure(ctx, perm.Subject{ID: v.ID, Staff: v.Staff}, perm.Edit, r)
}

// resource reads just enough of a node for a permission question.
func (s *Service) resource(ctx context.Context, id int64) (perm.Resource, error) {
	row, err := s.db.Q.NodeOwner(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return perm.Resource{}, apperr.NotFound("node")
	}
	if err != nil {
		return perm.Resource{}, err
	}
	r := perm.Resource{
		Kind: "node", ID: row.ID,
		Visibility: string(row.Visibility), Status: string(row.Status),
	}
	if row.OwnerID.Valid {
		r.OwnerID = row.OwnerID.UUID
	}
	return r, nil
}

// Resource exposes a node as something the permission service can answer about, for
// callers outside this package (the sharing screens).
func (s *Service) Resource(ctx context.Context, id int64) (perm.Resource, error) {
	return s.resource(ctx, id)
}

func (s *Service) writeText(ctx context.Context, q *sqlc.Queries, id int64, in Input) error {
	for _, lang := range i18n.Langs {
		name := strings.TrimSpace(in.Names[lang])
		if name == "" {
			continue
		}
		if err := q.SetNodeText(ctx, sqlc.SetNodeTextParams{
			NodeID: id, Lang: lang, Name: name, Description: in.Descriptions[lang],
		}); err != nil {
			return err
		}
	}
	return nil
}

func validate(in Input) error {
	fields := map[string]string{}
	if strings.TrimSpace(in.Slug) == "" {
		fields["slug"] = "a slug is required"
	}
	if strings.TrimSpace(in.Names[i18n.Fallback]) == "" {
		fields["name_"+i18n.Fallback] = "the name is required in the default language"
	}
	if len(fields) > 0 {
		return apperr.Invalid(fields)
	}
	return nil
}

// defaultVisibility: what the platform publishes is public; what a player makes
// starts private and is theirs to open up (§7).
func defaultVisibility(v Viewer) string {
	if v.Staff {
		return "public"
	}
	return "private"
}

func node(r sqlc.Node, name string) Node {
	n := Node{
		ID: r.ID, Depth: int(r.Depth), Slug: r.Slug, Name: name,
		Type: r.NodeType, Icon: r.Icon, Color: r.Color,
		Status: string(r.Status), Visible: string(r.Visibility),
		Position: int(r.Position),
	}
	if r.ParentID.Valid {
		n.ParentID = r.ParentID.Int64
	}
	if r.RootID.Valid {
		n.RootID = r.RootID.Int64
	}
	if r.OwnerID.Valid {
		n.OwnerID = r.OwnerID.UUID
	}
	if r.ExtendsID.Valid {
		n.ExtendsID = r.ExtendsID.Int64
	}
	if n.Name == "" {
		n.Name = r.Slug
	}
	return n
}

func nullID(v int64) pgtype.Int8 { return pgtype.Int8{Int64: v, Valid: v > 0} }

func text(v string) pgtype.Text { return pgtype.Text{String: v, Valid: v != ""} }

func position(v int) pgtype.Int4 { return pgtype.Int4{Int32: int32(v), Valid: v > 0} }

func visibility(v string) sqlc.NullNodeVisibility {
	return sqlc.NullNodeVisibility{NodeVisibility: sqlc.NodeVisibility(v), Valid: v != ""}
}

func status(v string) sqlc.NullNodeStatus {
	return sqlc.NullNodeStatus{NodeStatus: sqlc.NodeStatus(v), Valid: v != ""}
}

// isDuplicateSlug recognises the sibling-slug index refusing a second row.
func isDuplicateSlug(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "nodes_sibling_slug"
}

func first(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

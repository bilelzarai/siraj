package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
	// ErrInvalid is a request that cannot be carried out as asked — a group
	// with no name, a room nobody is in.
	ErrInvalid = errors.New("invalid")
	// ErrBusy is somebody trying to be in two places the rules allow only one
	// of: a second live match, a second room. It is separate from ErrConflict
	// because the screen has something specific and useful to say about it.
	ErrBusy = errors.New("already engaged")
	// ErrAlreadyStarted is a match somebody tried to begin twice. Nobody made
	// a mistake — it is the second press of a button that worked.
	ErrAlreadyStarted = errors.New("already started")
	// ErrNotEnoughPlayers is a match begun before enough people are in it.
	ErrNotEnoughPlayers = errors.New("not enough players have joined")
	// ErrLastAdmin guards against an admin locking everyone out of the
	// admin area by demoting or deleting the only remaining one.
	ErrLastAdmin = errors.New("cannot remove the last admin")
)

// Repo is the single entry point to persistent storage. Methods are grouped
// across files by aggregate (users, content, games, challenges, social).
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) Pool() *pgxpool.Pool { return r.pool }

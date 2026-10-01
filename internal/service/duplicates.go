package service

import (
	"context"
	"sync"
	"time"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

// DuplicateSweepTTL is how stale the content-health list may be.
//
// The sweep is a trigram self-join over every translation: about a second in
// Arabic and three and a half in English on two thousand questions, and it ran
// on every visit to the screen. It is a report, not a live query — a question
// added in the last few minutes not yet appearing in a list of possible
// duplicates costs nobody anything, and the moderator working through the list
// gets a page that opens immediately.
const DuplicateSweepTTL = 10 * time.Minute

// Duplicates serves the content-health sweep from a short-lived cache.
type Duplicates struct {
	repo *repository.Repo

	mu     sync.Mutex
	cached map[string]*sweepResult
}

type sweepResult struct {
	pairs []*models.DuplicatePair
	at    time.Time
	// ready is closed when the first sweep for this key finishes. A second
	// visitor arriving mid-sweep waits for it rather than starting another:
	// two concurrent trigram self-joins are how one slow screen becomes two.
	ready chan struct{}
	err   error
}

func NewDuplicates(repo *repository.Repo) *Duplicates {
	return &Duplicates{repo: repo, cached: map[string]*sweepResult{}}
}

// Pairs returns the near-duplicates for one language, sweeping only when what
// is held is older than the TTL.
func (d *Duplicates) Pairs(ctx context.Context, locale string, threshold float64,
	limit int) ([]*models.DuplicatePair, time.Time, error) {

	d.mu.Lock()
	entry, ok := d.cached[locale]
	if ok && time.Since(entry.at) < DuplicateSweepTTL {
		d.mu.Unlock()
		<-entry.ready
		return entry.pairs, entry.at, entry.err
	}
	if ok && isPending(entry) {
		// Somebody else is already running this one.
		d.mu.Unlock()
		<-entry.ready
		return entry.pairs, entry.at, entry.err
	}
	fresh := &sweepResult{ready: make(chan struct{})}
	d.cached[locale] = fresh
	d.mu.Unlock()

	pairs, err := d.repo.NearDuplicates(ctx, threshold, limit, locale)

	d.mu.Lock()
	fresh.pairs, fresh.err, fresh.at = pairs, err, time.Now()
	if err != nil {
		// A failed sweep is not worth caching for ten minutes.
		delete(d.cached, locale)
	}
	d.mu.Unlock()
	close(fresh.ready)

	return pairs, fresh.at, err
}

// Invalidate drops what is held, so a change an admin just made shows up
// without waiting out the TTL.
func (d *Duplicates) Invalidate() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cached = map[string]*sweepResult{}
}

func isPending(e *sweepResult) bool {
	select {
	case <-e.ready:
		return false
	default:
		return true
	}
}

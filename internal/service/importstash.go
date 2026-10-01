package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ImportStash holds a previewed upload until the admin applies it.
//
// Applying used to mean attaching the same file a second time, which asked the
// admin to find it again in a file dialog and got them nothing: the preview
// they were approving described a file the server had already read and then
// thrown away. Worse, the two halves were only tied together by a hash of the
// bytes, so picking the wrong file was a mistake the interface invited and
// then had to catch.
//
// Holding the bytes makes the question go away. What is applied is the same
// upload that was previewed, because it is the same bytes — not because they
// matched a checksum.
//
// In memory rather than in the database: this is a file somebody is part-way
// through applying, not content. A restart loses it, and the screen falls back
// to asking for the file again, which is exactly what it used to do every
// time.
type ImportStash struct {
	mu      sync.Mutex
	entries map[string]*stashEntry

	// ttl is how long an unapplied preview stays usable, and perOwner caps how
	// many an admin can leave lying around — a preview is a few hundred
	// kilobytes and nothing obliges anyone to finish one.
	ttl      time.Duration
	perOwner int
}

type stashEntry struct {
	owner    uuid.UUID
	filename string
	format   string
	data     []byte
	created  time.Time
}

func NewImportStash() *ImportStash {
	return &ImportStash{
		entries:  map[string]*stashEntry{},
		ttl:      30 * time.Minute,
		perOwner: 4,
	}
}

// Put files an upload away and returns the token that fetches it back.
func (s *ImportStash) Put(owner uuid.UUID, filename, format string, data []byte) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(owner)
	s.entries[token] = &stashEntry{
		owner: owner, filename: filename, format: format,
		data: data, created: time.Now(),
	}
	return token
}

// Get returns a stashed upload, and false when the token is unknown, expired,
// or belongs to somebody else.
//
// The owner check is the point of storing it rather than trusting the client:
// a token is a short random string, and without this one admin could apply
// another's pending import by guessing at it.
func (s *ImportStash) Get(token string, owner uuid.UUID) (filename, format string, data []byte, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, found := s.entries[token]
	if !found || entry.owner != owner || time.Since(entry.created) > s.ttl {
		return "", "", nil, false
	}
	return entry.filename, entry.format, entry.data, true
}

// Drop forgets one upload, called once it has been applied.
func (s *ImportStash) Drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, token)
}

// sweepLocked clears anything expired, and trims this owner back to the cap by
// dropping their oldest. Called on Put, so the map cannot grow without someone
// actively uploading.
func (s *ImportStash) sweepLocked(owner uuid.UUID) {
	var mine []string
	for token, entry := range s.entries {
		if time.Since(entry.created) > s.ttl {
			delete(s.entries, token)
			continue
		}
		if entry.owner == owner {
			mine = append(mine, token)
		}
	}

	for len(mine) >= s.perOwner {
		oldest := mine[0]
		for _, token := range mine {
			if s.entries[token].created.Before(s.entries[oldest].created) {
				oldest = token
			}
		}
		delete(s.entries, oldest)
		for i, token := range mine {
			if token == oldest {
				mine = append(mine[:i], mine[i+1:]...)
				break
			}
		}
	}
}

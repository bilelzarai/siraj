// Package live is the one place the server pushes from: a player's open screens hold
// a single event stream, and anything that changes for them — a notification, a
// message, a friend request, a room filling up — is announced on it.
//
// The stream carries signals, never content: a screen that hears "messages" asks the
// server for the part of itself that changed. That keeps every rule and every DTO on
// the server side (NF-03, NF-05) and means one connection replaces every poll.
package live

import (
	"sync"

	"github.com/google/uuid"
)

// Topic names what changed. Screens listen for the ones they show.
type Topic string

const (
	Notifications Topic = "notifications"
	Messages      Topic = "messages"
	Friends       Topic = "friends"
	Challenges    Topic = "challenges"
	Rooms         Topic = "rooms"
	Duels         Topic = "duels"
	Catalog       Topic = "catalog"
)

// buffer is how many signals a slow screen may fall behind before the oldest are
// dropped. A signal is one word, so a short buffer is plenty; dropping one is safe
// because the next one makes the screen refetch everything anyway.
const buffer = 8

// Hub holds every open stream, keyed by the player they belong to.
type Hub struct {
	mu      sync.RWMutex
	streams map[uuid.UUID]map[*Stream]struct{}
}

// Stream is one open connection. Read from C; call Close when the request ends.
type Stream struct {
	C    chan Topic
	hub  *Hub
	user uuid.UUID
	once sync.Once
}

func NewHub() *Hub {
	return &Hub{streams: map[uuid.UUID]map[*Stream]struct{}{}}
}

// Open starts a stream for one player.
func (h *Hub) Open(user uuid.UUID) *Stream {
	s := &Stream{C: make(chan Topic, buffer), hub: h, user: user}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.streams[user] == nil {
		h.streams[user] = map[*Stream]struct{}{}
	}
	h.streams[user][s] = struct{}{}
	return s
}

// Close ends a stream. It is safe to call twice.
func (s *Stream) Close() {
	s.once.Do(func() {
		s.hub.mu.Lock()
		defer s.hub.mu.Unlock()
		if set, ok := s.hub.streams[s.user]; ok {
			delete(set, s)
			if len(set) == 0 {
				delete(s.hub.streams, s.user)
			}
		}
		close(s.C)
	})
}

// Send tells one player's screens that something of theirs changed. It never blocks:
// a stream that is not keeping up loses the signal, not the request that sent it.
func (h *Hub) Send(user uuid.UUID, topics ...Topic) {
	if h == nil || user == uuid.Nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.streams[user] {
		for _, t := range topics {
			select {
			case s.C <- t:
			default:
			}
		}
	}
}

// SendAll tells several players at once (a room, the people in a challenge).
func (h *Hub) SendAll(users []uuid.UUID, topics ...Topic) {
	for _, u := range users {
		h.Send(u, topics...)
	}
}

// Broadcast tells every open stream: the catalog changed under everybody's feet.
func (h *Hub) Broadcast(topics ...Topic) {
	if h == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, set := range h.streams {
		for s := range set {
			for _, t := range topics {
				select {
				case s.C <- t:
				default:
				}
			}
		}
	}
}

// Watching reports how many streams a player has open (used by the health page).
func (h *Hub) Watching() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, set := range h.streams {
		n += len(set)
	}
	return n
}

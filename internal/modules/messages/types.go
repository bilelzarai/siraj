package messages

import (
	"time"

	"github.com/google/uuid"
)

// Tab is one of the three lists the inbox is split into (MS-01).
type Tab string

const (
	TabPeople Tab = "people"
	TabGroups Tab = "groups"
	TabRooms  Tab = "rooms"
)

// kinds maps a tab onto the conversation kinds it holds. The Sirāj team thread sits
// with people, which is where the design puts it.
func (t Tab) kinds() []string {
	switch t {
	case TabGroups:
		return []string{"group"}
	case TabRooms:
		return []string{"room"}
	}
	return []string{"direct", "system"}
}

// Valid keeps a hand-typed tab out of the query.
func ParseTab(s string) Tab {
	switch Tab(s) {
	case TabGroups:
		return TabGroups
	case TabRooms:
		return TabRooms
	}
	return TabPeople
}

// Row is one line of the inbox: who the thread is with, what was said last, and the
// marks the player put on it (MS-01).
type Row struct {
	ID       string
	UserID   uuid.UUID
	Name     string
	Preview  string
	At       time.Time
	Unread   int
	Online   bool
	Pinned   bool
	Muted    bool
	Guest    bool
	Group    bool
	Room     bool
	System   bool
	Members  int
	RoomCode string
}

// RoomRow is a live room seen from the inbox. The rooms module fills it, so messages
// never reaches into a room itself (NF-01).
type RoomRow struct {
	Code    string
	Title   string
	People  string
	Preview string
	At      time.Time
	Members int
	Open    bool
}

// RoomLine is one line of a room's chat, in the shape the thread draws.
type RoomLine struct {
	Author   string
	Body     string
	Kind     string
	FileName string
	FileSize int64
	FileURL  string
	Duration int
	System   bool
	Mine     bool
	At       time.Time
}

// Bubble is one message in a thread (MS-02). One struct covers every shape the design
// draws: a line of text, a quote above it, a file, a photo, a voice note, a challenge
// card, a line the room itself said, and the tombstone of a deleted message.
type Bubble struct {
	ID       string
	Kind     string
	Body     string
	At       time.Time
	Mine     bool
	From     string
	Deleted  bool
	Read     bool
	System   bool
	Quote    string
	QuoteWho string
	// A file, a photo or a voice note.
	FileName string
	FileSize int64
	FileURL  string
	Duration int
	// A challenge card: what it is and the game it opens.
	Title       string
	Line        string
	ChallengeID string
}

// Head is everything over the thread: who it is with, what they are doing, and
// whether this thread takes a reply at all (MS-02, MS-03).
type Head struct {
	ID       string
	Name     string
	UserID   uuid.UUID
	Presence string
	Online   bool
	Group    bool
	Room     bool
	System   bool
	Guest    bool
	People   string
	RoomCode string
	CanReply bool
}

// Candidate is one person the "New message" dialog offers: a friend for a member, and
// for a guest one of the guests they actually played with (§4 of 03-BACKEND-CHANGES).
type Candidate struct {
	ID     uuid.UUID
	Name   string
	Note   string
	Online bool
	Guest  bool
}

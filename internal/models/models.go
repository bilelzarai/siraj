package models

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------- identity --

type User struct {
	ID              uuid.UUID
	Username        string
	Email           string
	PasswordHash    string
	DisplayName     string
	Bio             string
	Country         string
	AvatarSeed      string
	Locale          string
	Theme           string
	XP              int
	Coins           int
	GamesPlayed     int
	GamesWon        int
	BestStreak      int
	Role            string
	Status          string
	SuspendedReason string
	LastSeenAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// IsTemporary marks a player who is not an account: somebody playing
	// without signing up, or somebody handed the phone for one round. They
	// answer questions and take a place in a match like anybody else, and
	// everything they do is swept when ExpiresAt passes.
	IsTemporary bool
	// HostUserID is the account responsible for a guest sitting at the same
	// device. Nil for the anonymous visitor themselves, who belongs to a
	// browser session rather than to a person.
	HostUserID *uuid.UUID
	// GuestKey ties an anonymous visitor to the browser session that made
	// them. It is the anonymous equivalent of a host.
	GuestKey string
	// ExpiresAt is when this temporary player stops existing. Nil for an
	// account, which does not.
	ExpiresAt *time.Time
}

// Roles, in increasing order of privilege.
const (
	RolePlayer    = "player"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"

	StatusUserActive    = "active"
	StatusUserSuspended = "suspended"
)

// IsAdmin reports full administrative rights.
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// CanModerate covers content work: questions, categories, translation review,
// and the integrity report. Admins can do everything a moderator can.
func (u *User) CanModerate() bool { return u.Role == RoleAdmin || u.Role == RoleModerator }

// IsSuspended blocks sign-in and gameplay without destroying the account.
func (u *User) IsSuspended() bool { return u.Status == StatusUserSuspended }

// IsGuest reports a player with no account behind them. Everything an account
// is for — keeping progress, friends, messages, a challenge history — is
// refused for these, and refused server-side rather than by hiding a button.
func (u *User) IsGuest() bool { return u.IsTemporary }

// IsLocalPlayer reports a guest created by somebody else to play on their
// device, as opposed to the anonymous visitor at the keyboard.
func (u *User) IsLocalPlayer() bool { return u.IsTemporary && u.HostUserID != nil }

// OwnerID is who a temporary player belongs to: their host if they have one,
// otherwise themselves. It is the identity a seat is checked against.
func (u *User) OwnerID() uuid.UUID {
	if u.HostUserID != nil {
		return *u.HostUserID
	}
	return u.ID
}

// RoleLabelKey maps a role to its translation key.
func RoleLabelKey(role string) string { return "admin.role." + role }

// Level is derived from XP on a widening curve: every level costs 100 XP more
// than the one before it, so level N is reached at 50*N*(N-1) XP.
func (u *User) Level() int {
	lvl := 1
	for xpForLevel(lvl+1) <= u.XP {
		lvl++
	}
	return lvl
}

// LevelProgress returns how far the user is into their current level, as
// (earned, needed) within the level.
func (u *User) LevelProgress() (int, int) {
	lvl := u.Level()
	base := xpForLevel(lvl)
	next := xpForLevel(lvl + 1)
	return u.XP - base, next - base
}

func (u *User) LevelPercent() int {
	earned, needed := u.LevelProgress()
	if needed <= 0 {
		return 100
	}
	p := earned * 100 / needed
	if p > 100 {
		return 100
	}
	return p
}

func xpForLevel(level int) int {
	if level <= 1 {
		return 0
	}
	return 50 * level * (level - 1)
}

// GamesWon counts duel victories. There is deliberately no win-rate helper
// here: dividing duel wins by rounds played, which is what the old one did,
// produced a percentage of two different things.

// Initials backs the generated avatar.
func (u *User) Initials() string {
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return "?"
	}
	return string(runes[0])
}

type Session struct {
	ID        string
	UserID    uuid.UUID
	UserAgent string
	IP        string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// ------------------------------------------------------------ game content --

type Category struct {
	ID          int
	Slug        string
	Icon        string
	Color       string
	SortOrder   int
	Name        string // resolved for the active locale
	Description string
}

type Question struct {
	ID           int
	CategoryID   int
	CategorySlug string
	CategoryName string
	CategoryIcon string
	Difficulty   int
	Points       int
	CorrectIndex int
	Source       string
	Prompt       string
	Choices      []string
	Explanation  string

	// Locale is the language the text above actually came back in, which is
	// not always the one that was asked for: a question with no approved
	// translation falls back. The play screen needs it for lang/dir, since
	// tagging Arabic text as French renders it left-to-right.
	Locale string
}

// MinSetQuestions is how many a collection needs before a match can be played
// on it. It is the round minimum: a set that cannot fill a round is a set you
// would be offered and then refused, which is worse than not being offered.
//
// service.MinQuestions is the same number and they are checked against each
// other by a test, because two constants that must agree and are declared apart
// will not stay agreed.
const MinSetQuestions = 5

// QuestionSet is a player's own collection of questions — theirs to keep, to
// name, and to play with the friends they invite. Private: the invitation to a
// match is what shares it, so there is no second access-control system to keep
// in agreement with the friendship rules.
type QuestionSet struct {
	ID        uuid.UUID
	OwnerID   uuid.UUID
	Name      string
	Locale    string
	Count     int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Playable reports whether a match can be played on this collection.
func (s *QuestionSet) Playable() bool { return s.Count >= MinSetQuestions }

// Needed is how many more questions it takes to be playable.
func (s *QuestionSet) Needed() int {
	if s.Playable() {
		return 0
	}
	return MinSetQuestions - s.Count
}

// ---------------------------------------------------------------- gameplay --

const (
	ModeSolo      = "solo"
	ModeChallenge = "challenge"
	ModeDaily     = "daily"

	StatusActive    = "active"
	StatusFinished  = "finished"
	StatusAbandoned = "abandoned"
)

type GameSession struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Mode           string
	Status         string
	CategoryID     *int
	CategoryName   string
	CategoryIcon   string
	Difficulty     int
	Locale         string
	QuestionIDs    []int
	Cursor         int
	TotalQuestions int
	CorrectCount   int
	BestStreak     int
	Score          int
	XPEarned       int
	DurationMS     int
	ChallengeID    *uuid.UUID
	StartedAt      time.Time
	FinishedAt     *time.Time
	// ServedAt is when the question at the current cursor was first shown. It
	// is the server's own clock for the speed bonus, so a client cannot claim
	// to have answered instantly.
	//
	// Nil once the page has been closed and the clock banked into ElapsedMS:
	// the question is waiting to be resumed, not being read right now.
	ServedAt *time.Time

	// ElapsedMS is time already banked against the current question by earlier
	// visits to it. Time spent since ServedAt adds to this, rather than
	// replacing it.
	ElapsedMS int

	// DeadlineAt is when the question at the current cursor stops being
	// answerable. A challenge round sets it and never moves it back: leaving
	// the page does not pause anything, and coming back to a question whose
	// deadline has gone finds it lost rather than waiting.
	//
	// Nil in a solo round, which has no deadline at all — that is exactly the
	// difference between a round you can come back to and one you cannot.
	DeadlineAt *time.Time
}

func (g *GameSession) Accuracy() int {
	if g.TotalQuestions == 0 {
		return 0
	}
	return g.CorrectCount * 100 / g.TotalQuestions
}

func (g *GameSession) IsComplete() bool { return g.Cursor >= g.TotalQuestions }

// Resumable reports whether this round can be picked up where it was left.
// Only a solo round can: a challenge is played against other people who are
// not waiting, and the daily is one attempt.
func (g *GameSession) Resumable() bool { return g.Mode == ModeSolo }

// Timed reports a round whose questions expire on their own.
func (g *GameSession) Timed() bool { return g.Mode == ModeChallenge }

// Expired reports that the question on screen has run out of time as of now.
func (g *GameSession) Expired(now time.Time) bool {
	return g.DeadlineAt != nil && !now.Before(*g.DeadlineAt)
}

// RemainingMS is how long is left on the current question, floored at zero.
func (g *GameSession) RemainingMS(now time.Time) int {
	if g.DeadlineAt == nil {
		return 0
	}
	left := g.DeadlineAt.Sub(now).Milliseconds()
	if left < 0 {
		return 0
	}
	return int(left)
}

type GameAnswer struct {
	ID            int64
	SessionID     uuid.UUID
	QuestionID    int
	Position      int
	SelectedIndex int
	IsCorrect     bool
	TimeMS        int
	PointsAwarded int
	AnsweredAt    time.Time

	// Hydrated for the history review screen.
	Prompt       string
	Choices      []string
	Explanation  string
	CorrectIndex int
}

// NoAnswer is what SelectedIndex holds for a question nobody answered — one
// that ran out of time, or that was still on screen when a challenge round was
// walked away from. The four choices are 0 to 3, so it cannot be mistaken for
// one of them.
const NoAnswer = -1

// Answered reports whether somebody actually chose something. The review screen
// reads Choices[SelectedIndex], which is why this is not optional.
func (a *GameAnswer) Answered() bool { return a.SelectedIndex >= 0 }

const (
	ChallengePending   = "pending"
	ChallengeAccepted  = "accepted"
	ChallengeDeclined  = "declined"
	ChallengeCompleted = "completed"
	ChallengeExpired   = "expired"
	// ChallengeCancelled is a match that is over before it began: the host
	// called it off, or everybody invited said no. It is final — nobody joins
	// it, nobody accepts it, nobody resumes it — and it holds nobody.
	ChallengeCancelled = "cancelled"
)

// Live reports a match that can still be joined, accepted or played.
func (c *Challenge) Live() bool {
	return c.Status == ChallengePending || c.Status == ChallengeAccepted
}

// Over reports a match nothing more will happen to.
func (c *Challenge) Over() bool { return !c.Live() }

// ChallengePlayer is one person in a match: their invitation, their round and
// their score. A duel used to keep both people in the challenge row, which made
// "two players" a fact of the schema rather than a choice.
type ChallengePlayer struct {
	UserID   uuid.UUID
	IsHost   bool
	State    string
	Score    *int
	PlayedAt *time.Time
	Card     *UserCard
	// Team is which side they are on, 1-based. Zero is nobody's side, which is
	// where every player in a free-for-all stands.
	Team int
	// Local marks a guest playing on somebody else's device, so the screen can
	// say whose turn it is to be handed the phone.
	Local bool
	// Origin is how this player was reachable when they were invited:
	// OriginHost, or one of the three sources.
	Origin string
}

// The states a player moves through in a match.
const (
	PlayerInvited  = "invited"
	PlayerJoined   = "joined"
	PlayerPlayed   = "played"
	PlayerDeclined = "declined"
	// PlayerEliminated is somebody who never started while everyone else
	// finished. Kept apart from declined: declining is something you did,
	// being eliminated is something that happened because you did not.
	PlayerEliminated = "eliminated"
)

// Out reports whether this player is no longer part of the result — they said
// no, or the match went ahead without them.
func (p ChallengePlayer) Out() bool {
	return p.State == PlayerDeclined || p.State == PlayerEliminated
}

// HasPlayed reports whether this player's round is in.
func (p ChallengePlayer) HasPlayed() bool { return p.State == PlayerPlayed }

// MinChallengePlayers is how many people it takes to have a match at all. One
// person answering questions alone is a solo round, and the results screen has
// nothing to compare against.
const MinChallengePlayers = 2

// MaxChallengePlayers bounds an invitation list. Past this the scoreboard stops
// being readable and the match stops being a thing anybody finishes.
const MaxChallengePlayers = 10

// The two shapes a match can take.
//
// A duel is everybody for themselves, whether that is two people or six. A team
// match splits them into sides and adds the scores up — which is the only
// reason the distinction exists: with one player per side the two are the same
// game and the scoreboard should not pretend otherwise.
const (
	FormatDuel = "duel"
	FormatTeam = "team"
)

// MaxTeams is how many sides a match can have. Two is the common case; more
// than this and a team stops being a team.
const MaxTeams = 4

// MinPerSide is how many people a side needs before it is a side.
//
// Two. One person on their own is not a team, whatever the scoreboard adds
// their score to, and a match of two sides where one of them is a single
// player is a duel with an audience. This is the rule; MinTeamPlayers below
// is what it works out to.
const MinPerSide = 2

// MinTeamPlayers is the smallest team match there is: two sides, both of them
// real, which is four people.
const MinTeamPlayers = MinPerSide * 2

// Where the people in a match came from.
//
// A match is with one group, never a mixture. The three are not interchangeable:
// a friend answers on their own phone in their own time; a guest at this device
// is waiting for the phone to be handed to them; somebody in your room is
// reachable only while you are both standing in it. Putting all three in one
// match is three different games at once, and a scoreboard that compares them
// is comparing nothing.
const (
	// SourceFriends is people you have a standing relationship with.
	SourceFriends = "friends"
	// SourceDevice is the guests at this keyboard, taking turns on one phone.
	SourceDevice = "device"
	// SourceRoom is the people in the room you are in, for as long as you are
	// both in it.
	SourceRoom = "room"

	// OriginHost marks the person who opened the match. They are exempt from
	// the rule above: they are the one doing the inviting.
	OriginHost = "host"
)

// ValidSource reports whether a string names one of the three.
func ValidSource(source string) bool {
	switch source {
	case SourceFriends, SourceDevice, SourceRoom:
		return true
	}
	return false
}

// SourceLabelKey maps a source to its translation key.
func SourceLabelKey(source string) string { return "challenge.from." + source }

// Team is one side of a team match and the sum of what its players scored.
type Team struct {
	Number  int
	Players []*ChallengePlayer
	Score   int
	// Done reports that everyone on this side has played, so the total is
	// final rather than a running count.
	Done bool
}

type Challenge struct {
	ID     uuid.UUID
	HostID uuid.UUID
	// Format is FormatDuel or FormatTeam.
	Format string
	// PlayerSource is which of the three groups this match is with. Every
	// player in it but the host came from that one group.
	PlayerSource string
	// CancelledAt and CancelledBy record a match that was called off, so the
	// screen can say what happened to it rather than showing a game that
	// simply stopped existing.
	CancelledAt *time.Time
	CancelledBy *uuid.UUID
	// StartedAt is the moment the host set it going. Until then the match is a
	// lobby: people accept, and nobody plays. Nil means nobody has begun, and
	// is what keeps a remote match from being two people answering the same
	// questions on different days.
	StartedAt *time.Time
	// HostTeam is the side the host put themselves on when opening a team
	// match. Read only at creation; afterwards the host is a player row like
	// everybody else.
	HostTeam    int
	WinnerTeam  *int
	RematchOf   *uuid.UUID
	Players     []*ChallengePlayer
	CategoryID  *int
	Difficulty  int
	QuestionIDs []int
	Status      string
	WinnerID    *uuid.UUID
	Message     string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	CompletedAt *time.Time

	CategoryName string
	CategoryIcon string
}

// Player finds one participant, or nil when they are not in this match.
func (c *Challenge) Player(userID uuid.UUID) *ChallengePlayer {
	for _, p := range c.Players {
		if p.UserID == userID {
			return p
		}
	}
	return nil
}

// Standing is the players who have finished, best score first — the scoreboard.
func (c *Challenge) Standing() []*ChallengePlayer {
	out := make([]*ChallengePlayer, 0, len(c.Players))
	for _, p := range c.Players {
		if p.HasPlayed() {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return deref(out[i].Score) > deref(out[j].Score)
	})
	return out
}

// Ranked is everybody in the match in the order a scoreboard shows them:
// whoever has played, best score first, then the people still to play, then
// anybody who declined.
func (c *Challenge) Ranked() []*ChallengePlayer {
	out := make([]*ChallengePlayer, len(c.Players))
	copy(out, c.Players)
	weight := func(p *ChallengePlayer) int {
		switch p.State {
		case PlayerPlayed:
			return 0
		case PlayerDeclined, PlayerEliminated:
			return 2
		default:
			return 1
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if wi, wj := weight(out[i]), weight(out[j]); wi != wj {
			return wi < wj
		}
		return deref(out[i].Score) > deref(out[j].Score)
	})
	return out
}

// Waiting is everyone the match is still expecting, so the screen can name them
// rather than saying "waiting" and leaving you to guess who for.
func (c *Challenge) Waiting() []*ChallengePlayer {
	var out []*ChallengePlayer
	for _, p := range c.Players {
		if p.State == PlayerInvited || p.State == PlayerJoined {
			out = append(out, p)
		}
	}
	return out
}

// Joined counts everyone who is in — the number that has to reach
// MinChallengePlayers before the match can start.
func (c *Challenge) Joined() int {
	n := 0
	for _, p := range c.Players {
		if p.State == PlayerJoined || p.State == PlayerPlayed {
			n++
		}
	}
	return n
}

// ReadyToStart reports whether enough people are in for the match to mean
// anything.
func (c *Challenge) ReadyToStart() bool { return c.Joined() >= MinChallengePlayers }

// Winner is the highest score once everyone who is playing has played, and nil
// while any of them still might. A tie has no winner, which is a draw.
func (c *Challenge) Winner() *ChallengePlayer {
	if len(c.Waiting()) > 0 {
		return nil
	}
	standing := c.Standing()
	if len(standing) < MinChallengePlayers {
		return nil
	}
	if len(standing) > 1 && deref(standing[0].Score) == deref(standing[1].Score) {
		return nil
	}
	return standing[0]
}

func deref(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

// IsTeamMatch reports a match played in sides.
func (c *Challenge) IsTeamMatch() bool { return c.Format == FormatTeam }

// Teams is the scoreboard of a team match: each side, who is on it, and what
// they have between them. Sides with nobody on them are left out, so a match
// set up for three and played by two does not show an empty column.
func (c *Challenge) Teams() []*Team {
	byNumber := map[int]*Team{}
	var order []int
	for _, p := range c.Players {
		if p.Team <= 0 || p.Out() {
			continue
		}
		t := byNumber[p.Team]
		if t == nil {
			t = &Team{Number: p.Team, Done: true}
			byNumber[p.Team] = t
			order = append(order, p.Team)
		}
		t.Players = append(t.Players, p)
		if p.HasPlayed() {
			t.Score += deref(p.Score)
		} else {
			t.Done = false
		}
	}
	sort.Ints(order)
	out := make([]*Team, 0, len(order))
	for _, n := range order {
		out = append(out, byNumber[n])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// WinningTeam is the side with the highest total once every side has finished,
// and nil while any of them still might change. A tie has no winner.
func (c *Challenge) WinningTeam() *Team {
	if !c.IsTeamMatch() {
		return nil
	}
	teams := c.Teams()
	if len(teams) < 2 {
		return nil
	}
	for _, t := range teams {
		if !t.Done {
			return nil
		}
	}
	if teams[0].Score == teams[1].Score {
		return nil
	}
	return teams[0]
}

// Others is everybody in this match but the viewer, still in it.
//
// It is what a card about a match has to name. Reading challenger_id instead —
// which is the host — showed somebody their own name as the person who had
// challenged them, on a match they had opened themselves.
func (c *Challenge) Others(viewerID uuid.UUID) []*ChallengePlayer {
	out := make([]*ChallengePlayer, 0, len(c.Players))
	for _, p := range c.Players {
		if p.UserID == viewerID || p.Out() {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Waitingish is how many people a cancellation would affect: everybody still
// in the match but the host. It is what the confirmation quotes, because
// "cancel this match?" and "cancel this match for three other people?" are
// different questions.
func (c *Challenge) Waitingish() int {
	n := 0
	for _, p := range c.Players {
		if p.IsHost || p.Out() {
			continue
		}
		n++
	}
	return n
}

// HasPlayer reports whether somebody holds a place in this match.
func (c *Challenge) HasPlayer(userID uuid.UUID) bool { return c.Player(userID) != nil }

// HasActivePlayer reports somebody still in it — not declined, not left behind.
// It is the difference between being named on a match and having something to
// do about it.
func (c *Challenge) HasActivePlayer(userID uuid.UUID) bool {
	p := c.Player(userID)
	return p != nil && !p.Out()
}

// WhoActs is which of the people at one device this match is about.
//
// A phone can hold several of a match's players — an account and the guests
// sitting with them — so "me" is a choice. It has been made in four different
// places in this codebase and got a different answer in each, which is the
// whole reason it lives here now: the screen that draws a card and the handler
// that answers its buttons have to mean the same person, or you get a Cancel
// button that answers "only the host can cancel a match".
//
// The order is "who here still has something to do":
//
//	the seat, while whoever holds it is still in the match;
//	otherwise the account holder, if the match is waiting on them;
//	otherwise the seat, for their result once nobody here has a move left;
//	otherwise the account holder.
func (c *Challenge) WhoActs(seat, account uuid.UUID) uuid.UUID {
	if seat != uuid.Nil && c.HasActivePlayer(seat) {
		return seat
	}
	if account != uuid.Nil && c.HasActivePlayer(account) {
		return account
	}
	if seat != uuid.Nil && c.HasPlayer(seat) {
		return seat
	}
	return account
}

// StillToAct is everybody the match is waiting on: invited and not answered,
// or in and not played.
func (c *Challenge) StillToAct(except uuid.UUID) []*ChallengePlayer {
	out := make([]*ChallengePlayer, 0, len(c.Players))
	for _, p := range c.Players {
		if p.UserID == except || p.Out() || p.HasPlayed() {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Started reports that the host has set the match going.
func (c *Challenge) Started() bool { return c.StartedAt != nil }

// Lobby reports a match still gathering people: live, and not yet begun.
func (c *Challenge) Lobby() bool { return c.Live() && !c.Started() }

// Accepted is how many people have said they are here, the host included.
func (c *Challenge) Accepted() int { return c.Joined() }

// Hosted reports whether this match is the viewer's own.
func (c *Challenge) Hosted(viewerID uuid.UUID) bool { return c.HostID == viewerID }

// LocalPlayers is everyone in this match sharing the host's device, in the
// order they take their turn.
func (c *Challenge) LocalPlayers() []*ChallengePlayer {
	var out []*ChallengePlayer
	for _, p := range c.Players {
		if p.Local && !p.Out() {
			out = append(out, p)
		}
	}
	return out
}

// Outcome reports the result from viewerID's point of view: "win", "loss",
// "draw" or "" when the duel is not finished.
func (c *Challenge) Outcome(viewerID uuid.UUID) string {
	if c.Status != ChallengeCompleted {
		return ""
	}
	if c.WinnerID == nil {
		return "draw"
	}
	if *c.WinnerID == viewerID {
		return "win"
	}
	return "loss"
}

// ------------------------------------------------------------------ social --

const (
	FriendPending  = "pending"
	FriendAccepted = "accepted"
	FriendBlocked  = "blocked"
)

// UserCard is the lightweight projection used everywhere a person is listed.
type UserCard struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
	AvatarSeed  string
	Country     string
	XP          int
	LastSeenAt  time.Time

	// Relation is how the person looking at this card stands with it. Search
	// results used to offer "Add friend" to everyone, including people who had
	// already accepted, so the button lied about what pressing it would do.
	// Declining leaves no row, so it reads back as RelationNone and the pair
	// can ask again rather than being stuck on "sent" for good.
	Relation RelationState
	// IsTemporary marks a player with no account: a guest at somebody's
	// device, or somebody playing anonymously. They have no public profile
	// and no private thread, so a list must not offer either.
	IsTemporary bool
}

// HasProfile reports somebody there is a page to send a reader to. A
// temporary player is a seat for an evening, not a person you can visit.
func (u *UserCard) HasProfile() bool { return !u.IsTemporary }

func (u *UserCard) Initials() string {
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return "?"
	}
	return string(runes[0])
}

func (u *UserCard) Level() int {
	usr := User{XP: u.XP}
	return usr.Level()
}

// IsOnline treats anyone seen in the last three minutes as present.
func (u *UserCard) IsOnline() bool {
	return time.Since(u.LastSeenAt) < 3*time.Minute
}

// RelationState describes how the viewer relates to another profile.
type RelationState string

const (
	RelationNone     RelationState = "none"
	RelationSelf     RelationState = "self"
	RelationFriends  RelationState = "friends"
	RelationOutgoing RelationState = "outgoing"
	RelationIncoming RelationState = "incoming"
	RelationBlocked  RelationState = "blocked"
)

// Conversation kinds. A direct thread is two people; a group is people someone
// chose; a room is a subject anyone may join.
const (
	ConversationDirect = "direct"
	ConversationGroup  = "group"
	ConversationRoom   = "room"
)

type Conversation struct {
	ID   uuid.UUID
	Kind string
	// Title names a group or a room. A direct thread has none: it is called
	// after the person on the other end of it.
	Title string
	// Topic is what a room is for, shown under its name.
	Topic   string
	OwnerID *uuid.UUID
	// Other is the person on the other end of a direct thread, and nil for
	// anything with more than two people in it.
	Other *UserCard
	// Members is everyone in a group or a room, for the mosaic the panel draws
	// and the names above what they say.
	Members []*UserCard
	// MemberCount is how many there are in total, which is more than Members
	// holds once a room is larger than a row of faces.
	MemberCount   int
	LastMessage   string
	LastSenderID  *uuid.UUID
	LastMessageAt time.Time
	UnreadCount   int
	// Muted is this viewer's own choice not to be told about this thread. It
	// silences the notification, never the messages.
	Muted bool
	// Matched marks a row that came back from a search rather than from the
	// inbox, so the preview can be shown as the line that matched.
	Matched bool
	// Joined reports whether the viewer is in this room. A room can be listed
	// to somebody who has not joined it — that is what makes it open.
	Joined bool
	// IsTemporary marks a room opened by a player with no account. It holds
	// temporary players and nothing else, and it is swept once the last of
	// them is gone. Filled from the owner by a trigger, so it cannot disagree
	// with who opened it.
	IsTemporary bool
}

// IsDirect reports a thread between exactly two people.
func (c *Conversation) IsDirect() bool { return c.Kind == "" || c.Kind == ConversationDirect }

// IsRoom reports an open space anyone may join.
func (c *Conversation) IsRoom() bool { return c.Kind == ConversationRoom }

// Name is what to call this thread on screen: the other person for a direct
// thread, the title for anything else.
func (c *Conversation) Name() string {
	if c.IsDirect() && c.Other != nil {
		return c.Other.DisplayName
	}
	return c.Title
}

// MessageQuote is the line a reply is answering, as much of it as a quote
// needs: who said it and what it said.
type MessageQuote struct {
	ID        int64
	Body      string
	SenderID  uuid.UUID
	Withdrawn bool
}

// Attachment describes bytes somebody sent. The bytes themselves are on disk,
// named by this id.
type Attachment struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	Kind    string
	Mime    string
	Name    string
	Bytes   int64
	// DurationMS is how long a recording runs, measured by the browser that
	// made it. Zero means unknown, which is every attachment that is not a
	// voice note and any recording whose length nobody measured — a WebM
	// stream from MediaRecorder does not carry its own duration.
	DurationMS int
	CreatedAt  time.Time
}

const (
	AttachmentImage = "image"
	AttachmentAudio = "audio"
	AttachmentFile  = "file"
)

// Length is the human reading of DurationMS — 0:07, 1:23. Empty when
// nobody measured it, so the template can leave the space alone rather than
// claiming a recording lasts no time at all.
func (a *Attachment) Length() string {
	if a.DurationMS <= 0 {
		return ""
	}
	total := (a.DurationMS + 500) / 1000
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// Size is the human reading of Bytes.
func (a *Attachment) Size() string {
	switch {
	case a.Bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(a.Bytes)/(1<<20))
	case a.Bytes >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(a.Bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", a.Bytes)
	}
}

type Message struct {
	ID             int64
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	Body           string
	CreatedAt      time.Time
	ReadAt         *time.Time

	// DeletedAt marks a message its sender took back. The row stays: removing
	// it would take a sentence out of the other person's thread after they had
	// read it, leaving a reply answering nothing.
	DeletedAt *time.Time

	// ReplyTo is the message this one answers, quoted above it. Nil for most
	// messages, and for a reply whose original has since been deleted
	// outright — the reply survives, the quote does not.
	ReplyTo *MessageQuote

	// Attachment is what this message carries besides words: a photo, a voice
	// note, a file. A message may be an attachment and nothing else.
	Attachment *Attachment
}

// Withdrawn reports whether the sender took this message back.
func (m *Message) Withdrawn() bool { return m.DeletedAt != nil }

// Read reports whether the other person has seen it.
func (m *Message) Read() bool { return m.ReadAt != nil }

type Notification struct {
	ID        int64
	UserID    uuid.UUID
	Kind      string
	Payload   map[string]any
	ReadAt    *time.Time
	CreatedAt time.Time
}

// ----------------------------------------------------------------- rewards --

type Badge struct {
	ID          int
	Slug        string
	Icon        string
	Threshold   int
	Kind        string
	Name        string
	Description string
	EarnedAt    *time.Time
}

func (b *Badge) Earned() bool { return b.EarnedAt != nil }

// ------------------------------------------------------------- leaderboard --

type LeaderboardEntry struct {
	Rank        int
	User        *UserCard
	XP          int
	GamesPlayed int
	GamesWon    int
	Accuracy    int
}

// ------------------------------------------------------------------- stats --

type UserStats struct {
	GamesPlayed    int
	TotalScore     int
	TotalCorrect   int
	TotalQuestions int
	BestStreak     int
	PerfectRounds  int
	AvgAccuracy    int
	ChallengesWon  int
	FriendCount    int
	ByCategory     []CategoryStat
	Last7Days      []DayStat
}

type CategoryStat struct {
	CategoryID   int
	CategoryName string
	CategoryIcon string
	Color        string
	Played       int
	Correct      int
	Total        int
	Accuracy     int
}

type DayStat struct {
	Day     time.Time
	Games   int
	Score   int
	Correct int
}

// ------------------------------------------------------------ admin area --

// AuditEntry is one recorded privileged action.
type AuditEntry struct {
	ID            int64
	ActorID       *uuid.UUID
	ActorUsername string
	Action        string
	TargetKind    string
	TargetID      string
	Detail        map[string]any
	IP            string
	CreatedAt     time.Time
}

// AdminUser is the projection the admin directory lists.
type AdminUser struct {
	ID              uuid.UUID
	Username        string
	DisplayName     string
	AvatarSeed      string
	Email           string
	Role            string
	Status          string
	SuspendedReason string
	Country         string
	Locale          string
	XP              int
	GamesPlayed     int
	CreatedAt       time.Time
	LastSeenAt      time.Time
}

func (u *AdminUser) IsSuspended() bool { return u.Status == StatusUserSuspended }

func (u *AdminUser) Initials() string {
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return "?"
	}
	return string(runes[0])
}

// AdminQuestion is one row of the admin question browser.
type AdminQuestion struct {
	ID           int
	CategoryID   int
	CategoryName string
	CategoryIcon string
	Difficulty   int
	Points       int
	CorrectIndex int
	IsActive     bool
	Prompt       string
	// PresentLocales is every locale this question has been written in, and
	// LocaleCount its length. Naming them, rather than only counting them, is
	// what lets the review screen ask for the ones that are missing.
	PresentLocales []string
	LocaleCount    int
	PendingReview  int
	// PendingLocales names the locales actually awaiting review, so the queue
	// offers approve and reject for those and not for every shipped language.
	PendingLocales []string
	CreatedAt      time.Time
}

// ShippedLocales is how many languages a question needs to be complete. It
// mirrors len(i18n.Supported); models stays free of that import so it can
// remain the one package that depends on nothing.
const ShippedLocales = 3

// TranslationDraft is one locale of a question as the admin form edits it.
type TranslationDraft struct {
	Prompt      string
	Choices     []string
	Explanation string
	Source      string // human | machine | import
	NeedsReview bool
}

// QuestionDraft is a question plus whichever locales are being written. The
// admin form can submit one locale or all of them.
type QuestionDraft struct {
	ID           int
	CategoryID   int
	Difficulty   int
	Points       int
	CorrectIndex int
	Source       string
	IsActive     bool
	CreatedBy    *uuid.UUID
	Translations map[string]TranslationDraft
}

// PlatformStats is the admin dashboard.
type PlatformStats struct {
	Users, Admins, Moderators, Suspended int
	ActiveToday, NewThisWeek             int
	Questions, ActiveQuestions           int
	Categories, Translations             int
	PendingReview                        int
	RoundsPlayed, AnswersRecorded        int
	Duels, Messages, Friendships         int
	Coverage                             []CoverageRow
}

// CoverageRow is how many questions exist for one category and difficulty —
// the number that shows where the bank is still thin.
type CoverageRow struct {
	CategoryID   int
	CategoryName string
	CategoryIcon string
	Difficulty   int
	Count        int
}

// DuplicatePair is two question prompts that look like the same question.
type DuplicatePair struct {
	LeftID      int
	RightID     int
	Locale      string
	LeftPrompt  string
	RightPrompt string
	Score       float64

	// SameFileLine is set when the match is another row of the same upload
	// rather than a question in the bank. There is no id to link to then — the
	// row it repeats has not been written either.
	SameFileLine int
}

func (d *DuplicatePair) Percent() int { return int(d.Score * 100) }

// IntegrityIssue is one content problem found by the integrity report.
type IntegrityIssue struct {
	Kind       string
	QuestionID int
	Detail     string
}

// ImportRun records one bulk question upload.
type ImportRun struct {
	ID            uuid.UUID
	ActorID       *uuid.UUID
	ActorUsername string
	Filename      string
	Format        string
	Committed     bool
	Total         int
	Added         int
	Updated       int
	Skipped       int
	Rejected      int
	Report        []ImportRow
	CreatedAt     time.Time
}

// ImportRow is the per-record outcome shown in the import preview.
type ImportRow struct {
	Line    int    `json:"line"`
	Ref     string `json:"ref"`
	Outcome string `json:"outcome"` // add | update | skip | reject
	Reason  string `json:"reason,omitempty"`

	// Prompt is the row's own question text. The report used to print an id
	// and nothing else, which told an admin which line was in trouble but not
	// which question — so it is carried here and shown on hover.
	Prompt string `json:"prompt,omitempty"`

	// RefExists says whether Ref names a question that is actually in the bank.
	// A row being added does not exist yet, so its reference is text rather
	// than a link — following one went to a page that was not there.
	RefExists bool `json:"refExists,omitempty"`

	// SimilarID, SimilarPrompt and Percent describe the question this row was
	// found to resemble. Set only on a near-duplicate, and what the decision
	// buttons act on.
	SimilarID     int    `json:"similarId,omitempty"`
	SimilarPrompt string `json:"similarPrompt,omitempty"`
	Percent       int    `json:"percent,omitempty"`
}

// RefID is the row's reference read back as a question id, and false when the
// reference is a prompt excerpt instead — a file may not carry ids at all.
func (r ImportRow) RefID() (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(r.Ref))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ----------------------------------------------------------- question diff --

// MaxComparePanels bounds how many side-by-side comparisons one screen builds.
//
// A file carrying no ids and uploaded a second time flags every row it has, and
// the duplicate sweep can return sixty pairs; each panel costs a lookup and a
// screenful of markup. Fifty is more than anybody reads in one sitting, and
// both screens still list every flagged row whether or not it has a panel
// behind it.
const MaxComparePanels = 50

// QuestionCompare is two questions set against each other, field by field.
//
// Both screens that raise a duplicate used to say only how alike two prompts
// are — "86% like question #1030200079" — which is the one thing an admin
// cannot act on: a percentage does not say whether the two differ by a proper
// noun, and so are different questions, or by a comma. So both sides are
// carried in full and the call is made by reading them.
//
// The two sides are left and right rather than anything more specific, because
// they are not always the same kinds of thing: on the import preview the left
// is a row in a file that may never be written, and on the content-health sweep
// both sides are questions the bank already has.
type QuestionCompare struct {
	// LeftID and RightID name the questions, and LeftID is zero when the left
	// side is a file row with no question behind it yet.
	LeftID, RightID int
	// Line is the row in the uploaded file, for a comparison that came from an
	// import; zero otherwise.
	Line    int
	Percent int

	// Identical is true when nothing shown here differs. That is the one case
	// where "duplicate" is not a judgement call, and it is worth stating rather
	// than leaving to be noticed.
	Identical bool
	// Differences counts the fields that do not match, so a panel worth opening
	// can be told from one that is not before opening it.
	Differences int

	Category   ComparePair
	Difficulty ComparePair
	Points     ComparePair

	// LeftCorrect and RightCorrect are the answer index on each side. The correct
	// answer belongs to the question rather than to a language, so it is held
	// here and printed as the letter players see.
	LeftCorrect, RightCorrect int

	Locales []CompareLocale
}

// CorrectSame reports whether both sides mark the same choice as correct.
func (c QuestionCompare) CorrectSame() bool { return c.LeftCorrect == c.RightCorrect }

// CompareLocale is one language's worth of the comparison.
//
// Every language either side has is carried, whichever language the screen was
// filtered to. Two prompts can be word-for-word in French and about different
// battles in Arabic, and a decision taken from the one language that raised the
// flag is a decision taken on a third of the evidence.
type CompareLocale struct {
	Code, Name, Dir, Flag string

	// InLeft and InRight say which side carries this language at all. A file
	// that only has English is not proposing to blank the Arabic, and the panel
	// must not read as though it were.
	InLeft, InRight bool

	Prompt      ComparePair
	Choices     []ComparePair
	Explanation ComparePair
}

// ComparePair is one field as each side has it.
type ComparePair struct {
	Left, Right string
	Same        bool

	// LeftDiff and RightDiff are those same two strings cut into runs the other
	// side also has and runs it does not, so the eye lands on the word that
	// changed instead of re-reading both sentences. Empty when the two match, or
	// when one side is missing and there is nothing to line up against.
	LeftDiff, RightDiff []DiffSpan
}

// Filled reports whether there is anything to show for this field at all.
func (p ComparePair) Filled() bool { return p.Left != "" || p.Right != "" }

// DiffSpan is a run of text that either appears on both sides or on one.
type DiffSpan struct {
	Text string
	Same bool
}

// ---------------------------------------------------------------- ratings --

// MaxStars is the top of the scale players rate a question on.
const MaxStars = 5

// PoorRatingThreshold is the average at or below which a question is put in
// front of an admin, and MinRatingVotes is how many ratings it takes before
// that average is treated as evidence. One player giving one star says
// something about the player as often as about the question.
const (
	PoorRatingThreshold = 2.0
	MinRatingVotes      = 3
)

// RatedQuestion is a question players have marked down, as the admin alert
// lists it.
type RatedQuestion struct {
	ID           int
	Prompt       string
	CategoryName string
	CategoryIcon string
	Votes        int
	Average      float64
}

// Stars rounds the average to whole stars, for the display that shows them.
func (r RatedQuestion) Stars() int { return int(r.Average + 0.5) }

// ---------------------------------------------------------------- support --

// Ticket kinds. These are what let staff triage a queue without reading
// every conversation first.
const (
	TicketSuggestion = "suggestion"
	TicketQuestion   = "question"
	TicketBug        = "bug"
	TicketAccount    = "account"
	TicketAbuse      = "abuse"
	TicketOther      = "other"
)

// Ticket statuses.
const (
	TicketOpen        = "open"
	TicketInProgress  = "in_progress"
	TicketWaitingUser = "waiting_user"
	TicketResolved    = "resolved"
	TicketClosed      = "closed"
)

// Ticket priorities.
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"
)

// TicketKinds, TicketStatuses and TicketPriorities are the canonical orders
// the filter UI renders in.
var (
	TicketKinds = []string{
		TicketSuggestion, TicketQuestion, TicketBug,
		TicketAccount, TicketAbuse, TicketOther,
	}
	TicketStatuses = []string{
		TicketOpen, TicketInProgress, TicketWaitingUser,
		TicketResolved, TicketClosed,
	}
	TicketPriorities = []string{
		PriorityUrgent, PriorityHigh, PriorityNormal, PriorityLow,
	}
)

func TicketKindKey(kind string) string         { return "support.kind." + kind }
func TicketStatusKey(status string) string     { return "support.status." + status }
func TicketPriorityKey(priority string) string { return "support.priority." + priority }

// Ticket is one support conversation.
// What a ticket is doing, said the way a person would say it. The status enum
// is for triage — open, in_progress, waiting_user — and none of those words
// tells the person who opened it whether anybody is coming, or whether the next
// move is theirs.
const (
	TicketStageNew      = "new"      // nobody has answered yet
	TicketStageWorking  = "working"  // somebody has it
	TicketStageYourTurn = "yourturn" // it is waiting on the person who asked
	TicketStageSettled  = "settled"  // resolved or closed
)

// Stage is the ticket from its owner's point of view.
//
// Derived rather than stored, because it is a reading of two facts the ticket
// already has — its status, and who spoke last. A second column would be a
// second thing to keep true.
func (t *Ticket) Stage() string {
	switch t.Status {
	case TicketResolved, TicketClosed:
		return TicketStageSettled
	case TicketWaitingUser:
		return TicketStageYourTurn
	}
	if t.LastSender == "staff" {
		return TicketStageYourTurn
	}
	if t.Status == TicketInProgress || t.AssignedTo != nil {
		return TicketStageWorking
	}
	return TicketStageNew
}

// StageKey and StageNextKey are what the screen says: where this is, and what
// happens next.
func (t *Ticket) StageKey() string     { return "support.stage." + t.Stage() }
func (t *Ticket) StageNextKey() string { return "support.next." + t.Stage() }

// NeedsYou reports whether the next move belongs to the person who asked, which
// is the one thing a list of tickets should make obvious.
func (t *Ticket) NeedsYou() bool { return t.Stage() == TicketStageYourTurn }

type Ticket struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Kind       string
	Status     string
	Priority   string
	Subject    string
	Locale     string
	CategoryID *int
	QuestionID *int
	PagePath   string
	// ReportedUserID is who a report of abuse is about. Nil for every other
	// kind, because every other kind is about the sender's own experience.
	ReportedUserID *uuid.UUID
	AssignedTo     *uuid.UUID

	LastMessageAt time.Time
	LastSender    string
	UserUnread    int
	StaffUnread   int
	MessageCount  int

	CreatedAt  time.Time
	UpdatedAt  time.Time
	ResolvedAt *time.Time

	// Hydrated for listings.
	Username         string
	DisplayName      string
	AvatarSeed       string
	AssigneeUsername string
	CategorySlug     string
	// ReportedUsername is hydrated for the staff screens, so a report names the
	// person rather than an id.
	ReportedUsername    string
	ReportedDisplayName string
}

// NeedsCategory reports whether a question category is a sensible thing to ask
// about this kind of message.
//
// It is the whole of the fix to a form that asked everybody everything: a
// report about a player has no question category, and asking for one is the
// form telling somebody it has not understood what they are reporting.
func TicketNeedsCategory(kind string) bool {
	return kind == TicketQuestion || kind == TicketSuggestion
}

// TicketNeedsPlayer reports whether this kind of message is about somebody.
func TicketNeedsPlayer(kind string) bool { return kind == TicketAbuse }

// AwaitingStaff reports whether the player is the one currently waiting.
func (t *Ticket) AwaitingStaff() bool {
	return t.LastSender == "user" && (t.Status == TicketOpen || t.Status == TicketInProgress)
}

// Urgent covers the two priorities that should jump a queue.
func (t *Ticket) Urgent() bool {
	return t.Priority == PriorityUrgent || t.Priority == PriorityHigh
}

func (t *Ticket) Initials() string {
	name := t.DisplayName
	if name == "" {
		name = t.Username
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return "?"
	}
	return string(runes[0])
}

// TicketMessage is one turn in a support conversation.
type TicketMessage struct {
	ID             int64
	TicketID       uuid.UUID
	SenderID       *uuid.UUID
	SenderRole     string // user | staff | system
	Body           string
	Internal       bool
	CreatedAt      time.Time
	SenderName     string
	SenderUsername string
	SenderAvatar   string
}

func (m *TicketMessage) FromStaff() bool { return m.SenderRole == "staff" }

// TicketCounts drives the badges on both sides.
type TicketCounts struct {
	MyUnread      int // unread staff replies, for the player
	OpenTotal     int
	AwaitingReply int
	HighPriority  int
	AssignedToMe  int
}

// CannedReply is a saved staff response.
type CannedReply struct {
	ID     int
	Locale string
	Title  string
	Body   string
}

// QuestionComment is a player's remark about one question, written just after
// they answered it. It is player-authored text, so it is moderatable and is
// never folded back into the question bank.
type QuestionComment struct {
	ID             int64
	QuestionID     int64
	Locale         string
	Body           string
	CreatedAt      time.Time
	AuthorName     string
	AuthorUsername string
	AuthorSeed     string
	Hidden         bool
}

// Anonymous reports whether the author's account is gone. The row survives so
// the conversation stays readable, but there is nobody to attribute it to.
func (c *QuestionComment) Anonymous() bool { return c.AuthorUsername == "" }

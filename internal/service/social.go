package service

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

const MaxMessageLength = 2000

var (
	ErrBlocked          = errors.New("blocked")
	ErrNotFriends       = errors.New("not friends")
	ErrEmptyMessage     = errors.New("empty message")
	ErrSelfTarget       = errors.New("cannot target yourself")
	ErrDuelInFlight     = errors.New("a duel is already in progress")
	ErrTooManyPlayers   = errors.New("too many players")
	ErrNotEnoughPlayers = errors.New("not enough players")
	// ErrInvalidThread is a group or a room that cannot be opened as asked —
	// no name, or a group with nobody in it.
	ErrInvalidThread    = errors.New("invalid thread")
	ErrTooManyQuestions = errors.New("too many written questions")
	ErrBadQuestion      = errors.New("incomplete question")
	ErrTooFewWritten    = errors.New("not enough written questions")
	ErrSetTooSmall      = errors.New("the set holds too few questions")
	// ErrUnreachable is somebody you may not write to or challenge: not a
	// friend, and not standing in the same room.
	ErrUnreachable = errors.New("not reachable")
	// ErrInMatch is a player already inside a live match. An invitation can
	// wait; being in two at once cannot.
	ErrInMatch = errors.New("already in a match")
	// ErrNoRoom is asked for a random opponent while in no room. Random
	// opponents come from the room and from nowhere else.
	ErrNoRoom = errors.New("not in a room")
	// ErrRoomEmpty is a room with nobody else in it to play against.
	ErrRoomEmpty = errors.New("nobody else in the room")
	// ErrGuest is an account-only action attempted by a temporary player.
	ErrGuest = errors.New("an account is needed")
	// ErrOneSidedTeams is a team match where everybody ended up on the same
	// side, which is a free-for-all wearing a different label.
	ErrOneSidedTeams = errors.New("a team match needs two sides")
	// ErrTeamsNeedMore is a team match with nobody to be on a team with: two
	// players is one against one however the sides are labelled, and a side of
	// one person is not a side.
	ErrTeamsNeedMore = errors.New("a team match needs a side with more than one player on it")
	// ErrInRoom is opening a room while already in one. A person is in one
	// room at a time, and opening one puts them in it.
	ErrInRoom = errors.New("already in a room")
	// ErrMixedSources is a match built from two of the three groups at once —
	// a friend and a guest at the device, or a room member and a friend. A
	// match is with one group.
	ErrMixedSources = errors.New("a match is with one group of players")
	// ErrNotInRoom is inviting somebody from the room who is not in it, which
	// includes somebody who has walked out of it since the list was drawn.
	ErrNotInRoom = errors.New("they are not in your room")
	// ErrNotYourPlayer is a guest id that belongs to somebody else's device.
	ErrNotYourPlayer = errors.New("not a player at this device")
	// ErrMatchOver is an answer to a match that has already ended — a late
	// accept on something that was cancelled while the page was open.
	ErrMatchOver = errors.New("that match is over")
	// ErrAlreadyStarted is a second press of start. Nobody made a mistake.
	ErrAlreadyStarted = errors.New("that match has already started")
	// ErrNotStarted is trying to play before the host has set the match going.
	ErrNotStarted = errors.New("the host has not started it yet")
)

// Social covers friendships, direct messages and duel invitations.
type Social struct {
	repo *repository.Repo
	hub  *Hub
}

func NewSocial(repo *repository.Repo, hub *Hub) *Social {
	return &Social{repo: repo, hub: hub}
}

// ---------------------------------------------------------------- friends --

// Block stops someone reaching this person: no messages, no duels, no requests.
func (s *Social) Block(ctx context.Context, blockerID, targetID uuid.UUID) error {
	if blockerID == targetID {
		return ErrSelfTarget
	}
	return s.repo.BlockUser(ctx, blockerID, targetID)
}

// Unblock leaves the two strangers again, not friends: whatever they had
// before the block was ended by it.
func (s *Social) Unblock(ctx context.Context, blockerID, targetID uuid.UUID) error {
	return s.repo.UnblockUser(ctx, blockerID, targetID)
}

func (s *Social) SendFriendRequest(ctx context.Context, from, to uuid.UUID) error {
	if from == to {
		return ErrSelfTarget
	}
	if err := s.repo.RequestFriendship(ctx, from, to); err != nil {
		return err
	}
	_ = s.repo.Notify(ctx, to, "friend.request", map[string]any{"user_id": from.String()})
	s.hub.Publish(to, Event{Type: EventFriendRequest})
	return nil
}

func (s *Social) RespondFriendRequest(ctx context.Context, requesterID, addresseeID uuid.UUID, accept bool) error {
	if err := s.repo.RespondToFriendship(ctx, requesterID, addresseeID, accept); err != nil {
		return err
	}
	if accept {
		_ = s.repo.Notify(ctx, requesterID, "friend.accepted",
			map[string]any{"user_id": addresseeID.String()})
		s.hub.Publish(requesterID, Event{Type: EventFriendAccepted})
	}
	return nil
}

func (s *Social) RemoveFriend(ctx context.Context, a, b uuid.UUID) error {
	return s.repo.RemoveFriendship(ctx, a, b)
}

// --------------------------------------------------------------- messages --

// OpenConversation returns (or creates) the thread with another user, after
// checking they are friends.
func (s *Social) OpenConversation(ctx context.Context, viewerID, otherID uuid.UUID) (uuid.UUID, error) {
	if viewerID == otherID {
		return uuid.Nil, ErrSelfTarget
	}
	reachable, err := s.Reachable(ctx, viewerID, otherID)
	if err != nil {
		return uuid.Nil, err
	}
	if !reachable {
		return uuid.Nil, ErrUnreachable
	}
	return s.repo.EnsureConversation(ctx, viewerID, otherID)
}

// Reachable is the whole answer to "may I write to this person, or challenge
// them" — and there are exactly two ways to be reachable.
//
// A friend, which is a standing relationship both people agreed to. Or somebody
// in the room you are in right now, which is not a relationship at all: it
// lasts as long as you are both standing there and goes when either of you
// leaves. Everything else — a username from the leaderboard, a name in a search
// box, the whole rest of the application — is nobody you may write to, which is
// what stops this being a place where strangers can reach you.
func (s *Social) Reachable(ctx context.Context, viewerID, otherID uuid.UUID) (bool, error) {
	if viewerID == otherID {
		return false, nil
	}
	friends, err := s.repo.AreFriends(ctx, viewerID, otherID)
	if err != nil {
		return false, err
	}
	if friends {
		return true, nil
	}
	return s.repo.ShareRoom(ctx, viewerID, otherID)
}

// Send posts a message into a thread the sender participates in.
func (s *Social) Send(ctx context.Context, convID, senderID uuid.UUID, body string, replyTo int64, attachment *uuid.UUID) (*models.Message, error) {
	body = strings.TrimSpace(body)
	// A photograph with nothing said about it is still a message. Words with
	// nothing attached are the other half, and neither is nothing.
	if body == "" && attachment == nil {
		return nil, ErrEmptyMessage
	}
	if len([]rune(body)) > MaxMessageLength {
		body = string([]rune(body)[:MaxMessageLength])
	}

	conv, err := s.repo.Conversation(ctx, convID, senderID)
	if err != nil {
		return nil, err
	}

	// Membership is not enough between two people. A thread outlives the
	// friendship that opened it, so without this a block stopped new
	// conversations and left every existing one wide open.
	//
	// It is the whole rule for a group or a room: you are reachable there
	// because you are in it, and you leave when you no longer want to be.
	// Blocking one member cannot silence a room for everyone else.
	if conv.IsDirect() && conv.Other != nil {
		blocked, err := s.repo.IsBlockedBetween(ctx, senderID, conv.Other.ID)
		if err != nil {
			return nil, err
		}
		if blocked {
			return nil, ErrBlocked
		}
	}

	msg, err := s.repo.SendMessage(ctx, convID, senderID, repository.NewMessage{
		Body: body, ReplyTo: replyTo, Attachment: attachment,
	})
	if err != nil {
		return nil, err
	}

	// Everyone in the thread but the person who wrote it. For a pair that is
	// one person; for a room it is everybody in the room, which is the only
	// reason a room is a place you can be spoken to at all.
	members, err := s.repo.MemberIDs(ctx, convID)
	if err != nil {
		return nil, err
	}
	for _, id := range members {
		if id == senderID {
			continue
		}
		// Muting silences the notification, never the message. The live event
		// still goes out — a thread they have open should still fill in.
		muted, err := s.repo.ConversationMuted(ctx, convID, id)
		if err != nil {
			return nil, err
		}
		if !muted {
			_ = s.repo.Notify(ctx, id, "message.new", map[string]any{
				"conversation_id": convID.String(),
				"from":            senderID.String(),
			})
		}
		s.hub.Publish(id, Event{
			Type:           EventMessageNew,
			ConversationID: convID.String(),
		})
	}
	return msg, nil
}

// OpenThread creates a group or a room.
//
// A group is people who were chosen and is closed to everyone else; a room is
// a subject and is open to anybody. The difference is enforced where somebody
// tries to join, not here.
func (s *Social) OpenThread(ctx context.Context, kind, title, topic string, ownerID uuid.UUID, members []uuid.UUID) (*models.Conversation, error) {
	switch kind {
	case models.ConversationGroup, models.ConversationRoom:
	default:
		return nil, ErrInvalidThread
	}
	if strings.TrimSpace(title) == "" {
		return nil, ErrInvalidThread
	}

	// You can only put friends in a group. An invitation into a conversation
	// is a thing that arrives uninvited, and that is only welcome from
	// somebody you already know.
	if kind == models.ConversationGroup {
		friends, err := s.repo.Friends(ctx, ownerID)
		if err != nil {
			return nil, err
		}
		known := make(map[uuid.UUID]bool, len(friends))
		for _, f := range friends {
			known[f.ID] = true
		}
		for _, id := range members {
			if id != ownerID && !known[id] {
				return nil, ErrNotFriends
			}
		}
		if len(members) == 0 {
			return nil, ErrInvalidThread
		}
	}

	conv, err := s.repo.CreateThread(ctx, kind, title, topic, ownerID, members)
	if err != nil {
		if errors.Is(err, repository.ErrBusy) {
			// Opening a room puts you in it, and a person is in one room at a
			// time. Leaving the current one first is the answer, and it is a
			// thing they can do rather than a failure.
			return nil, ErrInRoom
		}
		return nil, err
	}
	// Being put in a group is news; walking into a room is not, because
	// nobody put you there.
	if kind == models.ConversationGroup {
		for _, id := range members {
			if id == ownerID {
				continue
			}
			_ = s.repo.Notify(ctx, id, "message.new", map[string]any{
				"conversation_id": conv.ID.String(),
				"from":            ownerID.String(),
			})
			s.hub.Publish(id, Event{
				Type:           EventMessageNew,
				ConversationID: conv.ID.String(),
			})
		}
	}
	return conv, nil
}

// JoinRoom puts somebody into a room and tells the people already in it.
//
// It returns the room they were in before, if any, because a person is in one
// room at a time and walking into a new one takes them out of the old one. The
// screen says so rather than letting them work it out from a list they are
// suddenly missing from.
func (s *Social) JoinRoom(ctx context.Context, convID, userID uuid.UUID) (*uuid.UUID, error) {
	left, err := s.repo.JoinThread(ctx, convID, userID)
	if err != nil {
		return nil, err
	}
	rooms := []uuid.UUID{convID}
	if left != nil {
		rooms = append(rooms, *left)
	}
	for _, room := range rooms {
		members, err := s.repo.MemberIDs(ctx, room)
		if err != nil {
			return left, err
		}
		for _, id := range members {
			s.hub.Publish(id, Event{Type: EventMessageNew, ConversationID: room.String()})
		}
	}
	return left, nil
}

// ------------------------------------------------------------- challenges --

// CreateChallenge draws a shared question set and invites a friend to it.
// CreateMatch opens a match and invites everyone named.
//
// A match, not a duel: the old shape took one opponent because the schema had
// one column for them. Everyone invited has to be a friend — an invitation is a
// thing that arrives uninvited, and that is only welcome from somebody you
// know — and the list is capped, because a scoreboard nobody reads to the end
// of is not a scoreboard.
// MatchOptions is everything a match is opened with beyond who is in it.
type MatchOptions struct {
	CategoryID *int
	Difficulty int
	Count      int
	Locale     string
	Message    string
	RematchOf  *uuid.UUID
	// Format is models.FormatDuel (everybody for themselves) or
	// models.FormatTeam (sides, and the sides are what is scored).
	Format string
	// Teams says which side each invited player is on, keyed by user id. Read
	// only for a team match; a duel ignores it.
	Teams map[uuid.UUID]int
	// HostTeam is the side the host is on.
	HostTeam int
	// Local is everyone playing on the host's own device — guests handed the
	// phone rather than people invited elsewhere. They are seated already
	// joined, because there is nobody at the other end to accept.
	Local []uuid.UUID
	// PlayerSource is which of the three groups this match is with. Every
	// player in it comes from that one group; mixing is refused rather than
	// quietly producing a match of three different kinds of player.
	PlayerSource string
	// SetID draws the questions from the host's own collection instead of the
	// bank. Fewer than the round needs is topped up from the bank rather than
	// refused: a set of six and a round of ten is a reasonable thing to want.
	SetID *uuid.UUID
	// Authored is written on the spot, for a match that is not worth a set.
	Authored []AuthoredQuestion
}

func (s *Social) CreateMatch(ctx context.Context, host uuid.UUID, invited []uuid.UUID,
	opts MatchOptions) (*models.Challenge, error) {

	categoryID, difficulty, count := opts.CategoryID, opts.Difficulty, opts.Count
	locale, message, rematchOf, authored := opts.Locale, opts.Message, opts.RematchOf, opts.Authored

	source := opts.PlayerSource
	if !models.ValidSource(source) {
		// Nothing said, so read it from what was sent. A caller that sent both
		// kinds is refused below rather than having one of them guessed away.
		source = models.SourceFriends
		if len(opts.Local) > 0 {
			source = models.SourceDevice
		}
	}

	guests, err := s.vetInvitations(ctx, host, invited, opts.Local, source)
	if err != nil {
		return nil, err
	}

	if count < MinQuestions || count > MaxQuestions {
		count = DefaultQuestions
	}
	if len(authored) > repository.MaxAuthoredPerMatch {
		return nil, ErrTooManyQuestions
	}

	// One source per match, never a blend. A round of "six of yours and four
	// from the bank" is two different games in one sitting: your questions are
	// about the people playing, the bank's are about the subject, and mixing
	// them makes a score that compares neither.
	//
	// Written just now wins over a chosen set, and a set over the bank, because
	// each is a more deliberate choice than the one under it.
	var ids []int
	switch {
	case len(authored) > 0:
		// The written ones are the whole round.
		if len(authored) < MinQuestions {
			return nil, ErrTooFewWritten
		}

	case opts.SetID != nil:
		ids, err = s.repo.PickFromSet(ctx, *opts.SetID, host, guests, count)
		if err != nil {
			return nil, err
		}
		if len(ids) < models.MinSetQuestions {
			return nil, ErrSetTooSmall
		}

	default:
		ids, err = s.repo.PickQuestionIDs(ctx, categoryID, difficulty, count, locale)
		if err != nil {
			return nil, err
		}
		if len(ids) < MinQuestions {
			return nil, ErrNotEnoughQuestions
		}
	}

	format := opts.Format
	if format != models.FormatTeam {
		format = models.FormatDuel
	}

	seats := make([]repository.MatchSeat, 0, len(guests))
	local := make(map[uuid.UUID]bool, len(opts.Local))
	for _, id := range opts.Local {
		local[id] = true
	}
	for _, id := range guests {
		seat := repository.MatchSeat{UserID: id, Local: local[id], Origin: source}
		if format == models.FormatTeam {
			seat.Team = clampTeam(opts.Teams[id])
		}
		seats = append(seats, seat)
	}

	// What makes a team match a team match, rather than a word on top of one.
	//
	// Two sides at least — everybody on team one is a free-for-all with extra
	// labels — and at least one of those sides holding more than one person.
	// Without the second test, a host and one guest on separate sides passed as
	// "teams": one against one, with the scoreboard adding up a single number
	// per side and implying there was something to add.
	if format == models.FormatTeam {
		if len(seats)+1 < models.MinTeamPlayers {
			return nil, ErrTeamsNeedMore
		}
		sides := countSides(clampTeam(opts.HostTeam), seats)
		if len(sides) < 2 {
			return nil, ErrOneSidedTeams
		}
		if !anySideSharedBy(sides, 2) {
			return nil, ErrTeamsNeedMore
		}
	}

	ch := &models.Challenge{
		HostID:       host,
		CategoryID:   categoryID,
		Difficulty:   difficulty,
		QuestionIDs:  ids,
		Message:      strings.TrimSpace(message),
		RematchOf:    rematchOf,
		Format:       format,
		PlayerSource: source,
		HostTeam:     clampTeam(opts.HostTeam),
	}
	if err := s.repo.CreateMatch(ctx, ch, seats); err != nil {
		switch {
		case errors.Is(err, repository.ErrConflict):
			// The host is already inside a match. The unique index said so.
			return nil, ErrInMatch
		case errors.Is(err, repository.ErrInvalid):
			// Two groups in one match, caught by the database. Getting here
			// means the check below missed a path, so it is worth reaching.
			return nil, ErrMixedSources
		}
		return nil, err
	}

	// The written ones need the match's id, so they are saved after it exists
	// and prepended to its set.
	if len(authored) > 0 {
		mine, err := s.writeAuthored(ctx, ch.ID, host, locale, difficulty, categoryID, authored)
		if err != nil {
			return nil, err
		}
		ch.QuestionIDs = append(mine, ch.QuestionIDs...)
		if err := s.repo.SetChallengeQuestions(ctx, ch.ID, ch.QuestionIDs); err != nil {
			return nil, err
		}
	}

	for _, guest := range guests {
		// A guest at the host's own device is not sent an invitation. There is
		// nowhere to send it and nobody to open it: they are the person being
		// handed the phone.
		if local[guest] {
			continue
		}
		_ = s.repo.Notify(ctx, guest, "challenge.received", map[string]any{
			"challenge_id": ch.ID.String(),
			"from":         host.String(),
		})
		s.hub.Publish(guest, Event{Type: EventChallengeReceived, ChallengeID: ch.ID.String()})
	}
	return ch, nil
}

// clampTeam keeps a side number inside the range a match can have.
func clampTeam(n int) int {
	if n < 1 {
		return 1
	}
	if n > models.MaxTeams {
		return models.MaxTeams
	}
	return n
}

// hasTwoSides reports whether the seating actually divides anybody.
// countSides is how many people ended up on each side, the host included.
func countSides(hostTeam int, seats []repository.MatchSeat) map[int]int {
	sides := map[int]int{hostTeam: 1}
	for _, seat := range seats {
		sides[clampTeam(seat.Team)]++
	}
	return sides
}

// anySideSharedBy reports whether at least one side has this many people on it.
// A match of four split one-one-one-one is four sides of one, which is a
// free-for-all however the sides are numbered.
func anySideSharedBy(sides map[int]int, n int) bool {
	for _, count := range sides {
		if count >= n {
			return true
		}
	}
	return false
}

// AuthoredQuestion is one question a player wrote for their own match.
type AuthoredQuestion struct {
	Prompt      string
	Choices     []string
	Correct     int
	Explanation string
}

// Valid reports whether this is a question at all: something asked, four things
// to choose between, all different, and one of them marked right.
func (q AuthoredQuestion) Valid() bool {
	if strings.TrimSpace(q.Prompt) == "" || len(q.Choices) != 4 {
		return false
	}
	if q.Correct < 0 || q.Correct > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, choice := range q.Choices {
		choice = strings.TrimSpace(choice)
		if choice == "" || seen[choice] {
			return false
		}
		seen[choice] = true
	}
	return true
}

// writeAuthored saves the host's own questions against the match.
func (s *Social) writeAuthored(ctx context.Context, challengeID, author uuid.UUID,
	locale string, difficulty int, categoryID *int, authored []AuthoredQuestion) ([]int, error) {

	category := 1
	if categoryID != nil && *categoryID > 0 {
		category = *categoryID
	}
	if difficulty <= 0 {
		difficulty = 1
	}

	drafts := make([]models.TranslationDraft, 0, len(authored))
	categories := make([]int, 0, len(authored))
	difficulties := make([]int, 0, len(authored))
	correct := make([]int, 0, len(authored))

	for _, q := range authored {
		if !q.Valid() {
			return nil, ErrBadQuestion
		}
		choices := make([]string, len(q.Choices))
		for i, choice := range q.Choices {
			choices[i] = strings.TrimSpace(choice)
		}
		drafts = append(drafts, models.TranslationDraft{
			Prompt:      strings.TrimSpace(q.Prompt),
			Choices:     choices,
			Explanation: strings.TrimSpace(q.Explanation),
			Source:      "player",
		})
		categories = append(categories, category)
		difficulties = append(difficulties, difficulty)
		correct = append(correct, q.Correct)
	}

	return s.repo.CreateMatchQuestions(ctx, challengeID, author, locale,
		drafts, categories, difficulties, correct)
}

// vetInvitations reduces a submitted list to the people who may actually be
// invited, and refuses the whole match if that leaves nobody.
//
// The source is the rule, and each one is checked on its own terms rather than
// through a general "is this person reachable at all":
//
//	friends  they accepted a friend request, and it still stands
//	room     they are standing in the room the host is standing in, now
//	device   they are a guest this host created
//
// Checking the specific rule matters even where the general one would pass. A
// person can be both a friend and in your room; which of the two a match was
// built on is what it says about itself, and a match labelled "with the room"
// whose players were actually reached as friends is a match that would survive
// everybody leaving the room — which is not what a room match is.
//
// Mixing is refused outright. Not narrowed, not partially accepted: a list with
// a friend and a guest in it is two intentions, and picking one of them for
// somebody is worse than telling them.
func (s *Social) vetInvitations(ctx context.Context, host uuid.UUID, invited, local []uuid.UUID, source string) ([]uuid.UUID, error) {
	switch source {
	case models.SourceDevice:
		if len(invited) > 0 {
			return nil, ErrMixedSources
		}
	case models.SourceFriends, models.SourceRoom:
		if len(local) > 0 {
			return nil, ErrMixedSources
		}
	default:
		return nil, ErrMixedSources
	}

	seen := map[uuid.UUID]bool{host: true}
	var out []uuid.UUID

	for _, id := range invited {
		if seen[id] {
			continue
		}
		seen[id] = true

		ok, err := s.fromSource(ctx, host, id, source)
		if err != nil {
			return nil, err
		}
		if !ok {
			if source == models.SourceRoom {
				return nil, ErrNotInRoom
			}
			return nil, ErrNotFriends
		}
		out = append(out, id)
	}

	// Guests at the device are vetted by the caller, which is the only place
	// that can check ownership — a guest belongs to the host who made them,
	// and nobody else may seat them.
	for _, id := range local {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}

	if len(out) == 0 {
		return nil, ErrSelfTarget
	}
	// The host counts towards the total, so the list of guests is one short of
	// the maximum.
	if len(out) > models.MaxChallengePlayers-1 {
		return nil, ErrTooManyPlayers
	}

	// One match at a time with the same person, as before — but only checked
	// for a one-against-one, where "another duel with them" is unambiguous.
	if len(out) == 1 && len(local) == 0 {
		inFlight, err := s.repo.ActiveChallengeBetween(ctx, host, out[0])
		if err != nil {
			return nil, err
		}
		if inFlight {
			return nil, ErrDuelInFlight
		}
	}
	return out, nil
}

// fromSource asks the one question the declared source asks, and no other.
func (s *Social) fromSource(ctx context.Context, host, other uuid.UUID, source string) (bool, error) {
	switch source {
	case models.SourceRoom:
		return s.repo.ShareRoom(ctx, host, other)
	case models.SourceFriends:
		return s.repo.AreFriends(ctx, host, other)
	}
	return false, nil
}

// RandomOpponents draws people for a match out of the room the host is in, and
// out of nowhere else.
//
// This is the rule that keeps "play someone random" from meaning "play any of
// the thousands of accounts here". A room is a place somebody chose to walk
// into; the people in it have made themselves available in a way the rest of
// the application's users have not.
func (s *Social) RandomOpponents(ctx context.Context, host uuid.UUID, want int) ([]*models.UserCard, error) {
	if _, err := s.repo.CurrentRoom(ctx, host); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNoRoom
		}
		return nil, err
	}

	peers, err := s.repo.RoomPeers(ctx, host, "", 0, 0)
	if err != nil {
		return nil, err
	}
	// Anybody already inside a live match cannot be in this one, so offering
	// them would be offering a refusal.
	free := make([]*models.UserCard, 0, len(peers))
	for _, p := range peers {
		busy, err := s.repo.InActiveMatch(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if !busy {
			free = append(free, p)
		}
	}
	if len(free) == 0 {
		return nil, ErrRoomEmpty
	}

	if want < 1 {
		want = 1
	}
	if want > models.MaxChallengePlayers-1 {
		want = models.MaxChallengePlayers - 1
	}
	shuffle(free)
	if len(free) > want {
		free = free[:want]
	}
	return free, nil
}

// shuffle is a Fisher-Yates over user cards, seeded from the runtime's own
// source. Drawing "someone random" from a list in a fixed order would give the
// same person every time, which is the one thing a random opponent must not be.
func shuffle(cards []*models.UserCard) {
	for i := len(cards) - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		cards[i], cards[j] = cards[j], cards[i]
	}
}

// JoinMatch accepts an invitation.
func (s *Social) JoinMatch(ctx context.Context, challengeID, userID uuid.UUID) error {
	err := s.repo.JoinMatch(ctx, challengeID, userID)
	if err == nil {
		// The host is waiting on exactly this. Nothing told them when an
		// invitation was accepted, so a lobby filling up was something they
		// found out by reloading the page.
		s.announceAccepted(ctx, challengeID, userID)
	}
	if errors.Is(err, repository.ErrConflict) {
		// The invitation is no longer answerable: the match was cancelled or
		// ran out of time while this page was open. A specific error, because
		// the alternative was a five-hundred page over an ordinary race.
		return ErrMatchOver
	}
	return err
}

// DeclineChallenge is one player saying no, and whatever follows from it.
//
// What follows is decided in one transaction against locked rows — see
// repository.DeclineOrCancel — rather than counted in Go from the snapshot the
// page was rendered with. Two people declining at the same moment used to each
// see the other as still present, so neither closed the match and it stayed
// open with nobody in it.
//
// Three outcomes, and the people told differ for each:
//
//	the host said no       everybody is told the match is off
//	the last acceptance went
//	                       everybody is told the match is off
//	somebody else said no  the people still in it are told who dropped out
func (s *Social) DeclineChallenge(ctx context.Context, ch *models.Challenge, viewerID uuid.UUID) error {
	if ch.Player(viewerID) == nil {
		return repository.ErrForbidden
	}

	outcome, err := s.repo.DeclineOrCancel(ctx, ch.ID, viewerID)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return ErrMatchOver
		}
		return err
	}
	s.announce(ctx, ch, viewerID, outcome)
	return nil
}

// CancelMatch is the host calling their own match off.
//
// The same operation as the host declining, on purpose: two ways of saying
// "this is not happening" that closed a match by different routes would be two
// sets of rules to keep in agreement, and they would not stay agreed.
func (s *Social) CancelMatch(ctx context.Context, ch *models.Challenge, hostID uuid.UUID) error {
	outcome, err := s.repo.CancelMatch(ctx, ch.ID, hostID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrForbidden):
			return repository.ErrForbidden
		case errors.Is(err, repository.ErrConflict), errors.Is(err, repository.ErrNotFound):
			return ErrMatchOver
		}
		return err
	}
	s.announce(ctx, ch, hostID, outcome)
	return nil
}

// announceAccepted tells the people already in a match that somebody else is
// in it now — the host above all, who has been waiting for exactly that.
func (s *Social) announceAccepted(ctx context.Context, challengeID, joinerID uuid.UUID) {
	players, err := s.repo.MatchPlayers(ctx, challengeID)
	if err != nil {
		return
	}
	for _, p := range players {
		if p.UserID == joinerID || p.Out() || p.Local {
			continue
		}
		_ = s.repo.Notify(ctx, p.UserID, "challenge.accepted", map[string]any{
			"challenge_id": challengeID.String(),
			"user_id":      joinerID.String(),
		})
		s.hub.Publish(p.UserID, Event{
			Type: EventChallengeAccepted, ChallengeID: challengeID.String(),
		})
	}
}

// announce tells the people a match's end concerns.
//
// Everybody who was still in it when it ended, which is what the transaction
// returned — not the list the page was drawn with, which may be a minute old.
// Temporary players are skipped: a guest at somebody's device has no
// notifications screen to read one on, and writing it is a row nobody will ever
// see.
func (s *Social) announce(ctx context.Context, ch *models.Challenge, actorID uuid.UUID, outcome repository.CancelOutcome) {
	kind := "challenge.declined"
	event := EventChallengeDeclined
	if outcome.Cancelled {
		kind = "challenge.cancelled"
		event = EventChallengeCancelled
	}

	told := map[uuid.UUID]bool{actorID: true}
	tell := func(id uuid.UUID) {
		if told[id] {
			return
		}
		told[id] = true
		if local, err := s.repo.UserByID(ctx, id); err == nil && local.IsTemporary {
			return
		}
		_ = s.repo.Notify(ctx, id, kind, map[string]any{
			"challenge_id": ch.ID.String(),
			"by_host":      outcome.ByHost,
		})
		s.hub.Publish(id, Event{Type: event, ChallengeID: ch.ID.String()})
	}

	// The people the transaction actually released, first — that list is the
	// true one.
	for _, id := range outcome.Freed {
		tell(id)
	}
	// And anybody else who was in the match and is not out of it: somebody who
	// has already played is still owed the news that it was called off.
	for _, p := range ch.Players {
		if p.Out() {
			continue
		}
		tell(p.UserID)
	}
}

// ------------------------------------------------------------------- hub --

// Event is a tiny server-sent notification. The payload deliberately carries
// no content: the client re-fetches, so nothing sensitive rides the stream.
// The events the application publishes. Named here rather than written as
// string literals at each call site: a page declares which of these make it
// stale, and a name that exists in only one of the two places fails silently —
// the screen simply stays wrong, which is the thing this mechanism exists to
// prevent.
const (
	EventMessageNew         = "message.new"
	EventMessageWithdrawn   = "message.withdrawn"
	EventMessageRead        = "message.read"
	EventFriendRequest      = "friend.request"
	EventFriendAccepted     = "friend.accepted"
	EventChallengeReceived  = "challenge.received"
	EventChallengeDeclined  = "challenge.declined"
	EventChallengeCancelled = "challenge.cancelled"
	EventChallengeAccepted  = "challenge.accepted"
	EventChallengeStarted   = "challenge.started"
	EventChallengePlayed    = "challenge.played"
	EventChallengeCompleted = "challenge.completed"
	EventSupportNew         = "support.new"
	EventSupportReply       = "support.reply"
)

type Event struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversationId,omitempty"`
	// ChallengeID lets a screen decide whether the event is about what it is
	// currently showing. A duel result that lands while the opponent is looking
	// at the scoreboard should update that scoreboard, not just a badge.
	ChallengeID string `json:"challengeId,omitempty"`
}

// Hub fans events out to a user's open SSE connections. It is intentionally
// process-local: with more than one instance, swap this for LISTEN/NOTIFY.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[uuid.UUID]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[uuid.UUID]map[chan Event]struct{})}
}

// Subscribe registers a listener and returns it with its unsubscribe func.
func (h *Hub) Subscribe(userID uuid.UUID) (chan Event, func()) {
	ch := make(chan Event, 8)

	h.mu.Lock()
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[chan Event]struct{})
	}
	h.subscribers[userID][ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		if set, ok := h.subscribers[userID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subscribers, userID)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
}

// Publish delivers without blocking: a listener that cannot keep up simply
// misses the nudge and will catch up on its next poll.
func (h *Hub) Publish(userID uuid.UUID, ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers[userID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// presenceWindow is how long a write is suppressed for one user, and therefore
// also how long their entry is worth keeping.
const presenceWindow = time.Minute

// Presence updates last_seen_at at most once a minute per user, so an open
// tab does not generate a write per request.
//
// The map is swept: it held one entry per user seen since boot, which on a
// long-running process is a slow leak in service of a one-minute decision.
type Presence struct {
	repo *repository.Repo
	mu   sync.Mutex
	seen map[uuid.UUID]time.Time
}

func NewPresence(repo *repository.Repo) *Presence {
	p := &Presence{repo: repo, seen: make(map[uuid.UUID]time.Time)}
	go p.sweep()
	return p
}

func (p *Presence) Touch(ctx context.Context, userID uuid.UUID) {
	p.mu.Lock()
	last, ok := p.seen[userID]
	if ok && time.Since(last) < presenceWindow {
		p.mu.Unlock()
		return
	}
	p.seen[userID] = time.Now()
	p.mu.Unlock()

	_ = p.repo.TouchLastSeen(ctx, userID)
}

// sweep drops entries older than the window. Anything that old no longer
// suppresses a write, so keeping it buys nothing.
func (p *Presence) sweep() {
	ticker := time.NewTicker(5 * presenceWindow)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-presenceWindow)
		p.mu.Lock()
		for id, seen := range p.seen {
			if seen.Before(cutoff) {
				delete(p.seen, id)
			}
		}
		p.mu.Unlock()
	}
}

// StartMatch is the host setting a match going for everybody at once.
//
// Accepting an invitation now means "I am here" and nothing more. This is the
// moment a remote match becomes a match: one press, one started_at, a round for
// every player who turned up, and an event telling each of them their round is
// open. Before this they could only play the same questions on different days
// and have the scoreboard pretend that was a contest.
func (s *Social) StartMatch(ctx context.Context, ch *models.Challenge, hostID uuid.UUID) ([]uuid.UUID, error) {
	players, err := s.repo.StartMatch(ctx, ch.ID, hostID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrForbidden):
			return nil, repository.ErrForbidden
		case errors.Is(err, repository.ErrAlreadyStarted):
			return nil, ErrAlreadyStarted
		case errors.Is(err, repository.ErrNotEnoughPlayers):
			return nil, ErrNotEnoughPlayers
		case errors.Is(err, repository.ErrConflict), errors.Is(err, repository.ErrNotFound):
			return nil, ErrMatchOver
		}
		return nil, err
	}

	// Everybody but the person who pressed it. Their page is already going
	// into the round; the others need telling that theirs is open.
	for _, id := range players {
		if id == hostID {
			continue
		}
		if player, err := s.repo.UserByID(ctx, id); err == nil && player.IsTemporary {
			// A guest at the host's device is handed the phone, not notified.
			continue
		}
		_ = s.repo.Notify(ctx, id, "challenge.started", map[string]any{
			"challenge_id": ch.ID.String(),
		})
		s.hub.Publish(id, Event{Type: EventChallengeStarted, ChallengeID: ch.ID.String()})
	}
	return players, nil
}

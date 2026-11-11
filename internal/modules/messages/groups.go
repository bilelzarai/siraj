package messages

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
)

// Managing a group, and putting a challenge into a thread (MS-01, MS-02, CH-10).
//
// A group has an owner — whoever started it. Anybody in it may add a friend and may
// leave; only the owner may remove somebody else or rename it. A direct thread has
// none of this: it is two people and stays two people.

// Member is one person in a group.
type Member struct {
	ID     uuid.UUID
	Name   string
	Online bool
	Guest  bool
	Owner  bool
	Me     bool
}

// Group is what the members panel shows.
type Group struct {
	ID      uuid.UUID
	Title   string
	Members []Member
	// IAmOwner decides which buttons the panel draws.
	IAmOwner bool
	// Candidates are friends not in the group yet, to add from the same panel.
	Candidates []Candidate
}

// MaxGroupSize is the same number StartGroup enforces.
const MaxGroupSize = MaxGroupMembers

// Group reads one group for its panel.
func (s *Service) Group(ctx context.Context, who *authctx.Principal, id uuid.UUID) (Group, error) {
	var out Group
	if err := s.must(ctx, who, id); err != nil {
		return out, err
	}
	conversation, err := s.db.Q.GetConversation(ctx, id)
	if err != nil {
		return out, err
	}
	if conversation.Kind != sqlc.ConversationKindGroup {
		return out, apperr.Conflict("not_a_group", "This thread is between two people")
	}
	out.ID = id
	if conversation.Title.Valid {
		out.Title = conversation.Title.String
	}
	rows, err := s.db.Q.ConversationMembersFull(ctx, id)
	if err != nil {
		return out, err
	}
	inside := map[uuid.UUID]bool{}
	for _, r := range rows {
		inside[r.ID] = true
		member := Member{
			ID: r.ID, Name: r.DisplayName, Online: r.Online,
			Guest: r.Kind == sqlc.UserKindGuest, Owner: r.Role == "owner",
			Me: r.ID == who.ID,
		}
		if member.Me && member.Owner {
			out.IAmOwner = true
		}
		out.Members = append(out.Members, member)
	}
	// Who can still be added: the same people "New message" offers, minus the ones
	// already here.
	if people, err := s.Candidates(ctx, who); err == nil {
		for _, person := range people {
			if !inside[person.ID] {
				out.Candidates = append(out.Candidates, person)
			}
		}
	}
	return out, nil
}

// AddPeople puts friends into a group. Anybody already in it may add; a blocked
// player cannot be added, the same rule that governs writing to them (FR-07).
func (s *Service) AddPeople(ctx context.Context, who *authctx.Principal, id uuid.UUID, people []uuid.UUID) error {
	group, err := s.Group(ctx, who, id)
	if err != nil {
		return err
	}
	if len(people) == 0 {
		return apperr.Invalid(map[string]string{"user_ids": "pick at least one person"})
	}
	if len(group.Members)+len(people) > MaxGroupSize {
		return apperr.Invalid(map[string]string{"user_ids": "too many people for one group"})
	}
	if s.social != nil {
		for _, person := range people {
			blocked, err := s.social.Blocked(ctx, who.ID, person)
			if err != nil {
				return err
			}
			if blocked {
				return apperr.Forbidden("You cannot add one of these players")
			}
		}
	}
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		for _, person := range people {
			if err := q.AddGroupMember(ctx, sqlc.AddGroupMemberParams{
				ConversationID: id, UserID: person,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.announce(ctx, id)
	return nil
}

// RemovePerson takes somebody out. The owner may remove anybody but themselves;
// everybody may remove themselves, which is how leaving works.
func (s *Service) RemovePerson(ctx context.Context, who *authctx.Principal, id, person uuid.UUID) error {
	group, err := s.Group(ctx, who, id)
	if err != nil {
		return err
	}
	if person != who.ID && !group.IAmOwner {
		return apperr.Forbidden("Only the person who started the group can remove somebody")
	}
	if person == who.ID && group.IAmOwner && len(group.Members) > 1 {
		// The group would be left without an owner: the next member takes it on.
		for _, member := range group.Members {
			if member.ID != who.ID {
				if err := s.db.Q.SetConversationOwner(ctx, sqlc.SetConversationOwnerParams{
					ConversationID: id, UserID: member.ID,
				}); err != nil {
					return err
				}
				break
			}
		}
	}
	n, err := s.db.Q.RemoveConversationMember(ctx, sqlc.RemoveConversationMemberParams{
		ConversationID: id, UserID: person,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("member")
	}
	s.announce(ctx, id)
	return nil
}

// Leave is RemovePerson pointed at yourself, which is what the panel's button does.
func (s *Service) Leave(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	if who == nil {
		return apperr.Unauthorized("Sign in first")
	}
	return s.RemovePerson(ctx, who, id, who.ID)
}

// Challenges is what the composer needs in order to post a challenge card: the
// rounds this player can share, and the one they picked. The challenges module
// fills it in, so messages never imports it (NF-01).
type Challenges interface {
	Shareable(ctx context.Context, who *authctx.Principal, lang string) ([]ChallengeCard, error)
	Card(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (ChallengeCard, error)
}

// ChallengeCard is the bubble's content: what the round is and what it asks of the
// person receiving it.
type ChallengeCard struct {
	ID          uuid.UUID
	Title       string
	Questions   int
	ScoreToBeat int
}

// SetChallenges wires the rounds in. Without it the composer simply has nothing to
// attach, and the screen says so.
func (s *Service) SetChallenges(c Challenges) { s.challenges = c }

// Shareable lists the rounds the composer can offer.
func (s *Service) Shareable(ctx context.Context, who *authctx.Principal, lang string) ([]ChallengeCard, error) {
	if s.challenges == nil || who == nil {
		return nil, nil
	}
	return s.challenges.Shareable(ctx, who, lang)
}

// SendChallenge posts a challenge into a thread (MS-02): the card carries the round
// and the button that opens it, and nothing of anybody's score (CH-09).
func (s *Service) SendChallenge(ctx context.Context, who *authctx.Principal, key string, challengeID uuid.UUID, lang string) error {
	id, err := uuid.Parse(key)
	if err != nil {
		return apperr.BadRequest("bad_id", "a challenge can only be posted into a conversation")
	}
	if err := s.must(ctx, who, id); err != nil {
		return err
	}
	if s.challenges == nil {
		return apperr.Conflict("challenges_off", "Challenges are not available here")
	}
	card, err := s.challenges.Card(ctx, who, challengeID, lang)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("challenge")
	}
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"challenge_id":  card.ID.String(),
		"title":         strings.TrimSpace(card.Title),
		"questions":     card.Questions,
		"score_to_beat": card.ScoreToBeat,
	})
	if err != nil {
		return err
	}
	if err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.SendChallengeCard(ctx, sqlc.SendChallengeCardParams{
			ConversationID: id, SenderID: uuid.NullUUID{UUID: who.ID, Valid: true}, Payload: payload,
		}); err != nil {
			return err
		}
		return q.TouchConversation(ctx, id)
	}); err != nil {
		return err
	}
	s.announce(ctx, id)
	return nil
}

// announce tells everyone in the thread that it changed.
func (s *Service) announce(ctx context.Context, id uuid.UUID) {
	if s.live == nil {
		return
	}
	if ids, err := s.db.Q.ConversationMemberIDs(ctx, id); err == nil {
		s.live.SendAll(ids, live.Messages)
	}
}

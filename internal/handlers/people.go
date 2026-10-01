package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/views"
)

// Finding somebody, in the place where you are.
//
// Picking who to message or challenge is a two-second decision, and sending
// somebody to a search screen to make it costs more than the decision does. So
// the picker is a dialog — and what it can find is decided here, not by the
// dialog, because a list the server would refuse to act on is worse than no
// list at all.
//
// There is no global scope. The three below are the whole of it:
//
//	friends  people you have a standing relationship with
//	device   the guests sitting at this keyboard, who are yours
//	room     the people standing in the room you are in, for as long as
//	         you are both in it
//
// The rest of the site's accounts are not searchable from here at any scope.
// The one place that does search everybody is the friends screen, which offers
// nothing but "send a friend request" — an invitation the other person answers.

// PeopleScope names one of the three.
const (
	ScopeFriends = "friends"
	ScopeDevice  = "device"
	ScopeRoom    = "room"
	// ScopeMessage is the two scopes messaging accepts, together: friends and
	// the room. It exists because writing to somebody is allowed from either,
	// and a picker that made the reader choose a tab to find out which would be
	// asking them a question the rule does not ask.
	ScopeMessage = "message"
)

// peoplePage is how many rows one request returns. The dialog asks for more
// when the reader scrolls to the end of them, so this is a page size and not a
// ceiling on what can be found.
const peoplePage = 20

type personResult struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	ID          string `json:"id"`
	// Scope is which of the three this person came from, so a row can say why
	// it is reachable.
	Scope  string `json:"scope"`
	Online bool   `json:"online"`
	// Local marks a guest at this device: they are picked by id rather than by
	// username, because they have no username anybody should see.
	Local bool `json:"local"`
	// Busy marks somebody already inside a match. They can still be invited —
	// an invitation waits — but the dialog says so rather than letting the
	// send fail.
	Busy bool `json:"busy"`
	// Seed is what the avatar colour is derived from, so the dialog draws the
	// same face the rest of the site does.
	Seed string `json:"seed"`
}

type peopleResponse struct {
	People []personResult `json:"people"`
	// More reports that the next page has something in it.
	More bool `json:"more"`
	// Next is the offset to ask for.
	Next int `json:"next"`
	// Room names the room the results came from, for the dialog's empty state.
	Room string `json:"room,omitempty"`
}

// People answers the picker dialog, scoped and paged.
func (h *Handlers) People(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	offset := queryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	scope := r.URL.Query().Get("scope")
	switch scope {
	case ScopeFriends, ScopeDevice, ScopeRoom, ScopeMessage:
	default:
		scope = ScopeFriends
	}

	out := peopleResponse{People: []personResult{}, Next: offset}
	ctx := r.Context()

	switch scope {
	case ScopeDevice:
		// Guests at this keyboard belong to the account holder, never to the
		// seat: a guest cannot conjure more guests by being handed the phone.
		players, err := h.repo.SearchLocalPlayers(ctx, user.ID, query, peoplePage+1, offset)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "server error")
			return
		}
		out.More = len(players) > peoplePage
		if out.More {
			players = players[:peoplePage]
		}
		for _, p := range players {
			out.People = append(out.People, personResult{
				ID: p.ID.String(), Username: p.Username, DisplayName: p.DisplayName,
				Scope: ScopeDevice, Local: true, Seed: p.AvatarSeed, Online: true,
			})
		}

	case ScopeRoom:
		// The room the viewer is standing in, and only that one. A room id may
		// be named — the thread screen does — but it is checked against their
		// membership rather than trusted.
		room, err := h.repo.CurrentRoom(ctx, user.ID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				writeJSON(w, http.StatusOK, out)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "server error")
			return
		}
		if wanted, ok := parseUUID(r.URL.Query().Get("room")); ok && wanted != room.ID {
			// Asking about a room they are not in. Not an error worth a
			// status: they are in one room, and this is not it.
			writeJSON(w, http.StatusOK, out)
			return
		}
		out.Room = room.Name()

		peers, err := h.repo.RoomPeers(ctx, user.ID, query, peoplePage+1, offset)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "server error")
			return
		}
		out.More = len(peers) > peoplePage
		if out.More {
			peers = peers[:peoplePage]
		}
		h.appendCards(ctx, &out, peers, ScopeRoom)

	case ScopeMessage:
		// Both at once, friends first. Deliberately not paged past the first
		// page of each: this is the panel's picker, where the list is short
		// and the search is what finds anybody further down.
		friends, err := h.repo.SearchFriends(ctx, user.ID, query, peoplePage, offset)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "server error")
			return
		}
		h.appendCards(ctx, &out, friends, ScopeFriends)

		if peers, err := h.repo.RoomPeers(ctx, user.ID, query, peoplePage, offset); err == nil {
			seen := make(map[string]bool, len(out.People))
			for _, p := range out.People {
				seen[p.Username] = true
			}
			fresh := peers[:0]
			for _, p := range peers {
				if !seen[p.Username] {
					fresh = append(fresh, p)
				}
			}
			h.appendCards(ctx, &out, fresh, ScopeRoom)
		}

	default:
		friends, err := h.repo.SearchFriends(ctx, user.ID, query, peoplePage+1, offset)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "server error")
			return
		}
		out.More = len(friends) > peoplePage
		if out.More {
			friends = friends[:peoplePage]
		}
		h.appendCards(ctx, &out, friends, ScopeFriends)
	}

	if out.More {
		out.Next = offset + peoplePage
	}
	writeJSON(w, http.StatusOK, out)
}

// appendCards turns user cards into rows, marking the ones already inside a
// match so the dialog can say so before the send does.
func (h *Handlers) appendCards(ctx context.Context, out *peopleResponse, cards []*models.UserCard, scope string) {
	for _, card := range cards {
		busy, err := h.repo.InActiveMatch(ctx, card.ID)
		if err != nil {
			busy = false
		}
		out.People = append(out.People, personResult{
			ID: card.ID.String(), Username: card.Username, DisplayName: card.DisplayName,
			Scope: scope, Online: card.IsOnline(), Busy: busy,
			Seed: views.AvatarSeedFor(card.ID, card.AvatarSeed, card.Username),
		})
	}
}

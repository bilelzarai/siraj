package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// Threads with more than two people in them.
//
// A group is people somebody chose; a room is a subject anybody may join. Both
// are the same row in `conversations` as a direct thread, told apart by kind,
// and both get their membership from `conversation_members` — which is also
// what decides who is allowed to read them.

// memberFaces is how many of a thread's members the panel draws before it
// stops and counts the rest. Four fills the mosaic.
const memberFaces = 4

// attachMembers fills in the faces for every group and room in one pass.
//
// One query for the whole list rather than one per row: a panel of twenty
// threads was twenty round trips, and they were all asking the same question.
func (r *Repo) attachMembers(ctx context.Context, convs []*models.Conversation) error {
	ids := make([]uuid.UUID, 0, len(convs))
	byID := make(map[uuid.UUID]*models.Conversation, len(convs))
	for _, c := range convs {
		if c.IsDirect() {
			continue
		}
		ids = append(ids, c.ID)
		byID[c.ID] = c
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT m.conversation_id,
		       u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp,
		       u.last_seen_at, u.is_temporary
		  FROM conversation_members m
		  JOIN users u ON u.id = m.user_id
		 WHERE m.conversation_id = ANY($1)
		 ORDER BY m.conversation_id, m.joined_at`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var convID uuid.UUID
		var u models.UserCard
		if err := rows.Scan(&convID, &u.ID, &u.Username, &u.DisplayName,
			&u.AvatarSeed, &u.Country, &u.XP, &u.LastSeenAt, &u.IsTemporary); err != nil {
			return err
		}
		c := byID[convID]
		if c == nil || len(c.Members) >= memberFaces {
			continue
		}
		c.Members = append(c.Members, &u)
	}
	return rows.Err()
}

// Members is everyone in a thread, for the screen that says who is here.
func (r *Repo) Members(ctx context.Context, convID uuid.UUID) ([]*models.UserCard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at, u.is_temporary
		  FROM conversation_members m
		  JOIN users u ON u.id = m.user_id
		 WHERE m.conversation_id = $1
		 ORDER BY m.role DESC, m.joined_at`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.UserCard
	for rows.Next() {
		var u models.UserCard
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName,
			&u.AvatarSeed, &u.Country, &u.XP, &u.LastSeenAt, &u.IsTemporary); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

// MemberIDs is everyone a message in this thread has to reach. The live stream
// is published per person, so this is what it is published to.
func (r *Repo) MemberIDs(ctx context.Context, convID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT user_id FROM conversation_members WHERE conversation_id = $1`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CreateThread opens a group or a room and puts its members in it.
//
// The owner is a member like everyone else — the role only decides who may
// rename it and who may remove somebody, never who can read it.
func (r *Repo) CreateThread(ctx context.Context, kind, title, topic string, ownerID uuid.UUID, members []uuid.UUID) (*models.Conversation, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, ErrInvalid
	}

	var c models.Conversation
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO conversations (kind, title, topic, owner_id)
			VALUES ($1, $2, $3, $4)
			RETURNING id, kind, title, topic, owner_id, last_message_at, is_temporary`,
			kind, title, strings.TrimSpace(topic), ownerID,
		).Scan(&c.ID, &c.Kind, &c.Title, &c.Topic, &c.OwnerID, &c.LastMessageAt,
			&c.IsTemporary)
		if err != nil {
			return err
		}

		// A room is made, not entered. Opening one while standing in another
		// is an ordinary thing to do — you are setting a place up, not moving
		// into it — and seating the owner made it impossible: the one-room
		// index refused the insert and the screen said "leave the room you
		// are in first" to somebody who had not asked to go anywhere.
		//
		// A group is the opposite. It is people who were chosen, and the
		// person choosing is one of them.
		if kind != models.ConversationRoom {
			if _, err := tx.Exec(ctx, `
				INSERT INTO conversation_members (conversation_id, user_id, role)
				VALUES ($1, $2, 'owner')`, c.ID, ownerID); err != nil {
				return err
			}
		}
		for _, id := range members {
			if id == ownerID {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO conversation_members (conversation_id, user_id)
				VALUES ($1, $2) ON CONFLICT DO NOTHING`, c.ID, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.Joined = kind != models.ConversationRoom
	return &c, nil
}

// JoinThread puts somebody in a room, and takes them out of whichever room
// they were in before.
//
// A person is in one room at a time. Leaving first is what makes that true
// rather than an error message: walking into a room is a thing you do without
// thinking about the room you were in, and being refused at the door because of
// a room you have forgotten joining is a puzzle, not a rule.
//
// It returns the room they left, if any, so the screen can say so.
//
// Rooms only: a group is people who were chosen, and letting anyone walk into
// one would make the choosing pointless.
func (r *Repo) JoinThread(ctx context.Context, convID, userID uuid.UUID) (*uuid.UUID, error) {
	var left *uuid.UUID

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Is this a room at all, is it a room for them, and are they already
		// in it? Asked first so that a mistyped id never costs somebody the
		// room they are in.
		var isRoom, already, roomTemporary, meTemporary bool
		err := tx.QueryRow(ctx, `
			SELECT c.kind = 'room', c.is_temporary,
			       (SELECT u.is_temporary FROM users u WHERE u.id = $2),
			       EXISTS (SELECT 1 FROM conversation_members m
			                WHERE m.conversation_id = c.id AND m.user_id = $2)
			  FROM conversations c WHERE c.id = $1`, convID, userID).
			Scan(&isRoom, &roomTemporary, &meTemporary, &already)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !isRoom {
			return ErrNotFound
		}
		// A temporary room holds temporary players and nothing else. The
		// trigger refuses this too, and would do so correctly, but an
		// exception raised inside the transaction aborts it; answering here
		// keeps the refusal an answer rather than a fault.
		if roomTemporary != meTemporary {
			return ErrForbidden
		}
		if already {
			return nil
		}

		var previous uuid.UUID
		err = tx.QueryRow(ctx, `
			DELETE FROM conversation_members
			 WHERE user_id = $1 AND is_room
			 RETURNING conversation_id`, userID).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			left = &previous
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, convID, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return left, nil
}

// RoomPreview is an open room seen from outside it.
//
// It exists because of a link that lied. The directory advertises every room
// there is — that is what makes a room open — and each row linked straight to
// the thread. Opening a thread goes through Conversation(), which joins on
// membership and answers ErrNotFound for somebody who is not in it, so every
// room in "rooms you can join" led to a page saying the room does not exist.
//
// The honest answer to "show me that room" from a stranger is the room: its
// name, what it is about, who is in it, and the way in. Not its messages —
// joining is a deliberate act with a consequence, since a person is in one room
// at a time and walking into this one walks them out of another.
//
// Deliberately a separate call rather than a flag on Conversation(): that
// function is the membership check for thirteen other routes, including every
// one that writes, and widening it to let non-members through would have to be
// got right in all of them.
func (r *Repo) RoomPreview(ctx context.Context, convID, viewerID uuid.UUID) (*models.Conversation, error) {
	var c models.Conversation
	err := r.pool.QueryRow(ctx, `
		SELECT c.id, c.kind, c.title, c.topic, c.owner_id, c.last_message_at, c.is_temporary,
		       (SELECT count(*) FROM conversation_members x WHERE x.conversation_id = c.id),
		       EXISTS (SELECT 1 FROM conversation_members x
		                WHERE x.conversation_id = c.id AND x.user_id = $2)
		  FROM conversations c
		 WHERE c.id = $1 AND c.kind = 'room'
		   AND c.is_temporary = (SELECT u.is_temporary FROM users u WHERE u.id = $2)`,
		convID, viewerID).
		Scan(&c.ID, &c.Kind, &c.Title, &c.Topic, &c.OwnerID, &c.LastMessageAt,
			&c.IsTemporary, &c.MemberCount, &c.Joined)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	one := []*models.Conversation{&c}
	if err := r.attachMembers(ctx, one); err != nil {
		return nil, err
	}
	return &c, nil
}

// CurrentRoom is the one room this person is in, or ErrNotFound.
//
// It is what "your room" means everywhere else: who you may message without
// being friends, and who a random opponent can be drawn from.
func (r *Repo) CurrentRoom(ctx context.Context, userID uuid.UUID) (*models.Conversation, error) {
	var c models.Conversation
	err := r.pool.QueryRow(ctx, `
		SELECT c.id, c.kind, c.title, c.topic, c.owner_id, c.last_message_at, c.is_temporary,
		       (SELECT count(*) FROM conversation_members x WHERE x.conversation_id = c.id)
		  FROM conversation_members m
		  JOIN conversations c ON c.id = m.conversation_id
		 WHERE m.user_id = $1 AND m.is_room`, userID).
		Scan(&c.ID, &c.Kind, &c.Title, &c.Topic, &c.OwnerID, &c.LastMessageAt,
			&c.IsTemporary, &c.MemberCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Joined = true
	return &c, nil
}

// ShareRoom reports whether two people are in the same room right now.
//
// It is the second way somebody becomes reachable: messaging and challenges are
// for friends, or for the people standing in the room with you. Being in a room
// together is deliberately temporary — leave it and the reachability goes with
// it, which is what makes an open room safe to have.
func (r *Repo) ShareRoom(ctx context.Context, a, b uuid.UUID) (bool, error) {
	if a == b {
		return false, nil
	}
	var yes bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		      FROM conversation_members x
		      JOIN conversation_members y ON y.conversation_id = x.conversation_id
		     WHERE x.user_id = $1 AND y.user_id = $2 AND x.is_room)`, a, b).Scan(&yes)
	return yes, err
}

// RoomPeersShown is how many of a room's members a picker renders with the
// page. Past this the dialog searches instead, which is what /api/people is
// for — a room of three hundred is a room, not a list.
const RoomPeersShown = 50

// RoomPeers is everybody else in the viewer's room, which is the pool a random
// opponent is drawn from and the list the pickers offer.
//
// query filters by name, for the dialog that searches past what it rendered.
// limit of zero means everybody, which is what the random draw wants: it picks
// from the room, not from the first page of it.
func (r *Repo) RoomPeers(ctx context.Context, viewerID uuid.UUID, query string, limit, offset int) ([]*models.UserCard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at, u.is_temporary
		  FROM conversation_members mine
		  JOIN conversation_members peer ON peer.conversation_id = mine.conversation_id
		  JOIN users u ON u.id = peer.user_id
		 WHERE mine.user_id = $1 AND mine.is_room AND peer.user_id <> $1
		   -- Temporary players are not filtered out here, and deliberately
		   -- not: in a guest's room they are everybody in it, and the room
		   -- itself is what keeps the two kinds apart. The filter used to
		   -- stand in for that separation, and with a real one in place it
		   -- only made a guest's room look empty to the person standing in it.
		   AND ($2 = '' OR u.display_name ILIKE '%' || $2 || '%' OR u.username ILIKE '%' || $2 || '%')
		 ORDER BY u.last_seen_at DESC, u.id
		 LIMIT CASE WHEN $3 > 0 THEN $3 END OFFSET $4`,
		viewerID, strings.TrimSpace(query), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserCards(rows)
}

// LeaveThread takes somebody out of a group or a room. The last one out of a
// room does not close it: a room is a place, and an empty place is still there.
func (r *Repo) LeaveThread(ctx context.Context, convID, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM conversation_members m
		 USING conversations c
		 WHERE c.id = m.conversation_id
		   AND m.conversation_id = $1 AND m.user_id = $2
		   AND c.kind <> 'direct'`, convID, userID)
	return err
}

// OpenRooms is every room, joined or not, newest activity first.
//
// This is the one listing that deliberately shows a thread to somebody who is
// not in it — that is what makes a room open. It carries no messages with it,
// only the name, the subject and how many people are inside.
func (r *Repo) OpenRooms(ctx context.Context, viewerID uuid.UUID, query string, limit int) ([]*models.Conversation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.kind, c.title, c.topic, c.owner_id, c.last_message_at, c.is_temporary,
		       (SELECT count(*) FROM conversation_members x WHERE x.conversation_id = c.id),
		       EXISTS (SELECT 1 FROM conversation_members x
		                WHERE x.conversation_id = c.id AND x.user_id = $1)
		  FROM conversations c
		 WHERE c.kind = 'room'
		   -- Rooms of the viewer's own kind, and only those. A guest's rooms
		   -- are a separate directory from an account's; the subquery rather
		   -- than a parameter so a caller cannot pass the wrong answer.
		   AND c.is_temporary = (SELECT u.is_temporary FROM users u WHERE u.id = $1)
		   AND ($2 = '' OR c.title ILIKE '%' || $2 || '%' OR c.topic ILIKE '%' || $2 || '%')
		 ORDER BY c.last_message_at DESC
		 LIMIT $3`, viewerID, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Conversation
	for rows.Next() {
		var c models.Conversation
		if err := rows.Scan(&c.ID, &c.Kind, &c.Title, &c.Topic, &c.OwnerID,
			&c.LastMessageAt, &c.IsTemporary, &c.MemberCount, &c.Joined); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, r.attachMembers(ctx, out)
}

// PurgeEmptyGuestRooms deletes temporary rooms that nobody is in and nobody
// is left to walk into.
//
// Both halves are needed. Emptiness alone would collect a room in the second
// between being created and anybody joining it — creating a room does not put
// you in it, so every new room is empty. A gone owner alone would collect a
// room with people still talking in it. Together they describe a room with
// nothing left: deleting a guest nulls owner_id and takes their membership
// with it by cascade, so the pair goes true exactly when the last person who
// could have used the room has been swept.
//
// It terminates because every member of a temporary room is a temporary
// player, and every temporary player is swept when their time is up. A room
// of guests therefore always drains, and this is what collects it afterwards.
func (r *Repo) PurgeEmptyGuestRooms(ctx context.Context) (int64, error) {
	ct, err := r.pool.Exec(ctx, `
		DELETE FROM conversations c
		 WHERE c.kind = 'room' AND c.is_temporary
		   AND c.owner_id IS NULL
		   AND NOT EXISTS (SELECT 1 FROM conversation_members m
		                    WHERE m.conversation_id = c.id)`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// MarkThreadSeen moves a member's high-water mark to the newest message.
//
// This is what unread is counted against everywhere except a pair, where
// read_at on the message still answers it — one column is enough for one other
// reader, and it is what the sender's two ticks are drawn from.
func (r *Repo) MarkThreadSeen(ctx context.Context, convID, userID uuid.UUID) (int64, error) {
	ct, err := r.pool.Exec(ctx, `
		UPDATE conversation_members m
		   SET last_read_id = COALESCE(
		       (SELECT max(id) FROM messages WHERE conversation_id = $1), 0)
		 WHERE m.conversation_id = $1 AND m.user_id = $2
		   AND m.last_read_id < COALESCE(
		       (SELECT max(id) FROM messages WHERE conversation_id = $1), 0)`,
		convID, userID)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

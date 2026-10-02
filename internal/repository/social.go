package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bilelzarai/siraj/internal/models"
)

// ---------------------------------------------------------------- friends --

// RequestFriendship creates a pending request. If the other side already sent
// one, both are collapsed into an accepted friendship instead.
func (r *Repo) RequestFriendship(ctx context.Context, requesterID, addresseeID uuid.UUID) error {
	if requesterID == addresseeID {
		return ErrForbidden
	}

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// A block in either direction ends the conversation about friendship.
		var blocked bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM friendships
				 WHERE status = 'blocked'
				   AND ((requester_id = $1 AND addressee_id = $2)
				     OR (requester_id = $2 AND addressee_id = $1)))`,
			requesterID, addresseeID).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return ErrForbidden
		}

		var existingStatus string
		err := tx.QueryRow(ctx, `
			SELECT status::text FROM friendships
			 WHERE requester_id = $1 AND addressee_id = $2`,
			addresseeID, requesterID).Scan(&existingStatus)

		switch {
		case err == nil && existingStatus == models.FriendPending:
			// They asked first — accept rather than create a mirror request.
			_, err = tx.Exec(ctx, `
				UPDATE friendships SET status = 'accepted', responded_at = now()
				 WHERE requester_id = $1 AND addressee_id = $2`,
				addresseeID, requesterID)
			return err
		case err == nil:
			return ErrConflict
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO friendships (requester_id, addressee_id, status)
			VALUES ($1, $2, 'pending')
			ON CONFLICT (requester_id, addressee_id) DO NOTHING`,
			requesterID, addresseeID)
		return err
	})
}

func (r *Repo) RespondToFriendship(ctx context.Context, requesterID, addresseeID uuid.UUID, accept bool) error {
	if accept {
		ct, err := r.pool.Exec(ctx, `
			UPDATE friendships SET status = 'accepted', responded_at = now()
			 WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'`,
			requesterID, addresseeID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		DELETE FROM friendships
		 WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'`,
		requesterID, addresseeID)
	return err
}

// RemoveFriendship deletes the row regardless of which side created it.
func (r *Repo) RemoveFriendship(ctx context.Context, a, b uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM friendships
		 WHERE (requester_id = $1 AND addressee_id = $2)
		    OR (requester_id = $2 AND addressee_id = $1)`, a, b)
	return err
}

// BlockUser stops someone reaching this person at all.
//
// The status existed in the schema, the constants, the search query, the
// relation switch and the red tag in the interface — and nothing ever wrote it,
// so every one of those was unreachable. One row, owned by the blocker, in
// place of whatever relationship there was.
func (r *Repo) BlockUser(ctx context.Context, blockerID, targetID uuid.UUID) error {
	if blockerID == targetID {
		return ErrForbidden
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Whatever was between them — a friendship, a request either way — is
		// replaced. Leaving the old row would keep them friends *and* blocked.
		if _, err := tx.Exec(ctx, `
			DELETE FROM friendships
			 WHERE (requester_id = $1 AND addressee_id = $2)
			    OR (requester_id = $2 AND addressee_id = $1)`, blockerID, targetID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO friendships (requester_id, addressee_id, status, responded_at)
			VALUES ($1, $2, 'blocked', now())`, blockerID, targetID)
		return err
	})
}

// UnblockUser lifts a block, leaving the two strangers again rather than
// restoring a friendship they no longer have.
func (r *Repo) UnblockUser(ctx context.Context, blockerID, targetID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		DELETE FROM friendships
		 WHERE requester_id = $1 AND addressee_id = $2 AND status = 'blocked'`,
		blockerID, targetID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IsBlockedBetween reports a block in either direction. Either direction,
// because being blocked and having blocked both mean the two should not be
// able to reach each other.
func (r *Repo) IsBlockedBetween(ctx context.Context, a, b uuid.UUID) (bool, error) {
	var blocked bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM friendships
			 WHERE status = 'blocked'
			   AND ((requester_id = $1 AND addressee_id = $2)
			     OR (requester_id = $2 AND addressee_id = $1)))`, a, b).Scan(&blocked)
	return blocked, err
}

// Friends lists accepted friends of a user, online people first.
func (r *Repo) Friends(ctx context.Context, userID uuid.UUID) ([]*models.UserCard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at, u.is_temporary
		  FROM friendships f
		  JOIN users u ON u.id = CASE WHEN f.requester_id = $1
		                              THEN f.addressee_id ELSE f.requester_id END
		 WHERE (f.requester_id = $1 OR f.addressee_id = $1)
		   AND f.status = 'accepted'
		 ORDER BY u.last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserCards(rows)
}

// SearchFriends is the friends list, filtered by name and paged.
//
// The pickers ask for people by page rather than all at once: a dialog is not a
// page, and somebody with two hundred friends should get a scrollable list and
// a "load more" rather than two hundred rows rendered before it opens.
func (r *Repo) SearchFriends(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]*models.UserCard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at, u.is_temporary
		  FROM friendships f
		  JOIN users u ON u.id = CASE WHEN f.requester_id = $1
		                              THEN f.addressee_id ELSE f.requester_id END
		 WHERE f.status = 'accepted'
		   AND (f.requester_id = $1 OR f.addressee_id = $1)
		   AND NOT u.is_temporary
		   AND ($2 = '' OR u.display_name ILIKE '%' || $2 || '%' OR u.username ILIKE '%' || $2 || '%')
		 ORDER BY u.last_seen_at DESC, u.id
		 LIMIT $3 OFFSET $4`,
		userID, strings.TrimSpace(query), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserCards(rows)
}

func (r *Repo) FriendCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM friendships
		 WHERE (requester_id = $1 OR addressee_id = $1) AND status = 'accepted'`,
		userID).Scan(&n)
	return n, err
}

// IncomingRequests are people waiting for this user to respond.
func (r *Repo) IncomingRequests(ctx context.Context, userID uuid.UUID) ([]*models.UserCard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at, u.is_temporary
		  FROM friendships f
		  JOIN users u ON u.id = f.requester_id
		 WHERE f.addressee_id = $1 AND f.status = 'pending'
		 ORDER BY f.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserCards(rows)
}

func (r *Repo) OutgoingRequests(ctx context.Context, userID uuid.UUID) ([]*models.UserCard, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at, u.is_temporary
		  FROM friendships f
		  JOIN users u ON u.id = f.addressee_id
		 WHERE f.requester_id = $1 AND f.status = 'pending'
		 ORDER BY f.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectUserCards(rows)
}

func (r *Repo) IncomingRequestCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM friendships WHERE addressee_id = $1 AND status = 'pending'`,
		userID).Scan(&n)
	return n, err
}

// Relation describes how viewer relates to other, for rendering the right
// action button on a profile or search result.
func (r *Repo) Relation(ctx context.Context, viewerID, otherID uuid.UUID) (models.RelationState, error) {
	if viewerID == otherID {
		return models.RelationSelf, nil
	}
	var requesterID uuid.UUID
	var status string
	err := r.pool.QueryRow(ctx, `
		SELECT requester_id, status::text FROM friendships
		 WHERE (requester_id = $1 AND addressee_id = $2)
		    OR (requester_id = $2 AND addressee_id = $1)`, viewerID, otherID,
	).Scan(&requesterID, &status)

	if errors.Is(err, pgx.ErrNoRows) {
		return models.RelationNone, nil
	}
	if err != nil {
		return models.RelationNone, err
	}

	switch status {
	case models.FriendAccepted:
		return models.RelationFriends, nil
	case models.FriendBlocked:
		return models.RelationBlocked, nil
	default:
		if requesterID == viewerID {
			return models.RelationOutgoing, nil
		}
		return models.RelationIncoming, nil
	}
}

// AreFriends is the authorisation check for messaging and duelling.
func (r *Repo) AreFriends(ctx context.Context, a, b uuid.UUID) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM friendships
			 WHERE status = 'accepted'
			   AND ((requester_id = $1 AND addressee_id = $2)
			     OR (requester_id = $2 AND addressee_id = $1)))`, a, b).Scan(&ok)
	return ok, err
}

// ---------------------------------------------------------- conversations --

// normalisePair orders two ids so the (user_a, user_b) unique index works.
func normalisePair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

// EnsureConversation returns the existing thread between two users, creating
// it on first contact.
func (r *Repo) EnsureConversation(ctx context.Context, a, b uuid.UUID) (uuid.UUID, error) {
	ua, ub := normalisePair(a, b)

	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM conversations WHERE user_a = $1 AND user_b = $2`, ua, ub).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}

	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO conversations (user_a, user_b, kind) VALUES ($1, $2, 'direct')
			RETURNING id`, ua, ub).Scan(&id); err != nil {
			return err
		}
		// Membership is what decides who may read a thread, for every kind of
		// thread. A pair whose two people were not recorded as members would
		// be a conversation neither of them could open.
		_, err := tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id)
			VALUES ($1, $2), ($1, $3) ON CONFLICT DO NOTHING`, id, ua, ub)
		return err
	})
	if err != nil {
		// Two people opening the same thread at the same moment: the pair
		// index refused the second, and the first is the answer.
		if qerr := r.pool.QueryRow(ctx,
			`SELECT id FROM conversations WHERE user_a = $1 AND user_b = $2`,
			ua, ub).Scan(&id); qerr == nil {
			return id, nil
		}
		return uuid.Nil, err
	}
	return id, nil
}

// Conversations lists a user's threads with the other party, last message
// preview and unread count, newest activity first.
func (r *Repo) Conversations(ctx context.Context, userID uuid.UUID) ([]*models.Conversation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.kind, c.title, c.topic, c.last_message_at,
		       u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at,
		       COALESCE(lm.body, ''), lm.sender_id,
		       COALESCE(unread.n, 0), st.muted_at IS NOT NULL,
		       (SELECT count(*) FROM conversation_members x WHERE x.conversation_id = c.id)
		  FROM conversations c
		  JOIN conversation_members mem
		    ON mem.conversation_id = c.id AND mem.user_id = $1
		  -- Only a pair has somebody on the other end of it.
		  LEFT JOIN users u
		         ON c.kind = 'direct'
		        AND u.id = CASE WHEN c.user_a = $1 THEN c.user_b ELSE c.user_a END
		  LEFT JOIN conversation_state st
		         ON st.conversation_id = c.id AND st.user_id = $1
		  LEFT JOIN LATERAL (
		        SELECT m.body, m.sender_id FROM messages m
		         WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
		           AND (st.cleared_at IS NULL OR m.created_at > st.cleared_at)
		           AND NOT EXISTS (SELECT 1 FROM message_hidden h
		                            WHERE h.message_id = m.id AND h.user_id = $1)
		         ORDER BY m.id DESC LIMIT 1
		  ) lm ON true
		  LEFT JOIN LATERAL (
		        SELECT count(*) AS n FROM messages m
		         WHERE m.conversation_id = c.id AND m.sender_id <> $1
		           AND m.deleted_at IS NULL
		           -- read_at holds one answer, which is enough for two people
		           -- and nothing at all for five. Everywhere else the member's
		           -- own high-water mark is what unread is counted against.
		           AND CASE WHEN c.kind = 'direct'
		                    THEN m.read_at IS NULL
		                    ELSE m.id > mem.last_read_id END
		           AND (st.cleared_at IS NULL OR m.created_at > st.cleared_at)
		           AND NOT EXISTS (SELECT 1 FROM message_hidden h
		                            WHERE h.message_id = m.id AND h.user_id = $1)
		  ) unread ON true
		   -- A pair is created the moment someone opens it, so without this an
		   -- inbox filled with empty conversations from visits where nobody
		   -- said anything, each pinned to the top by its creation time. A
		   -- group or a room is created on purpose and belongs on the list
		   -- from the moment it exists.
		 WHERE (lm.body IS NOT NULL OR c.kind <> 'direct')
		 ORDER BY c.last_message_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Conversation
	for rows.Next() {
		var c models.Conversation
		var other models.UserCard
		var otherID *uuid.UUID
		var username, display, seed, country *string
		var xp *int
		var seen *time.Time
		if err := rows.Scan(&c.ID, &c.Kind, &c.Title, &c.Topic, &c.LastMessageAt,
			&otherID, &username, &display, &seed, &country, &xp, &seen,
			&c.LastMessage, &c.LastSenderID, &c.UnreadCount, &c.Muted,
			&c.MemberCount); err != nil {
			return nil, err
		}
		if otherID != nil {
			other = models.UserCard{
				ID: *otherID, Username: deref(username), DisplayName: deref(display),
				AvatarSeed: deref(seed), Country: deref(country),
				XP: derefInt(xp), LastSeenAt: derefTime(seen),
			}
			c.Other = &other
		}
		c.Joined = true
		out = append(out, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The faces a group or a room is drawn with. One query for all of them
	// rather than one per row.
	return out, r.attachMembers(ctx, out)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// Conversation loads one thread, verifying the viewer is a participant.
func (r *Repo) Conversation(ctx context.Context, convID, viewerID uuid.UUID) (*models.Conversation, error) {
	var c models.Conversation
	var otherID *uuid.UUID
	var username, display, seed, country *string
	var xp *int
	var seen *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT c.id, c.kind, c.title, c.topic, c.owner_id, c.last_message_at,
		       u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at,
		       s.muted_at IS NOT NULL,
		       (SELECT count(*) FROM conversation_members x WHERE x.conversation_id = c.id)
		  FROM conversations c
		  JOIN conversation_members mem
		    ON mem.conversation_id = c.id AND mem.user_id = $2
		  LEFT JOIN users u
		         ON c.kind = 'direct'
		        AND u.id = CASE WHEN c.user_a = $2 THEN c.user_b ELSE c.user_a END
		  LEFT JOIN conversation_state s ON s.conversation_id = c.id AND s.user_id = $2
		 WHERE c.id = $1`, convID, viewerID,
	).Scan(&c.ID, &c.Kind, &c.Title, &c.Topic, &c.OwnerID, &c.LastMessageAt,
		&otherID, &username, &display, &seed, &country, &xp, &seen,
		&c.Muted, &c.MemberCount)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if otherID != nil {
		c.Other = &models.UserCard{
			ID: *otherID, Username: deref(username), DisplayName: deref(display),
			AvatarSeed: deref(seed), Country: deref(country),
			XP: derefInt(xp), LastSeenAt: derefTime(seen),
		}
	}
	c.Joined = true
	one := []*models.Conversation{&c}
	if err := r.attachMembers(ctx, one); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) TotalUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		  FROM messages m
		  JOIN conversations c ON c.id = m.conversation_id
		  JOIN conversation_members mem
		    ON mem.conversation_id = c.id AND mem.user_id = $1
		  LEFT JOIN conversation_state s ON s.conversation_id = c.id AND s.user_id = $1
		 WHERE m.sender_id <> $1 AND m.deleted_at IS NULL
		   AND CASE WHEN c.kind = 'direct'
		            THEN m.read_at IS NULL
		            ELSE m.id > mem.last_read_id END
		   AND (s.cleared_at IS NULL OR m.created_at > s.cleared_at)
		   AND NOT EXISTS (SELECT 1 FROM message_hidden h
		                    WHERE h.message_id = m.id AND h.user_id = $1)`,
		userID).Scan(&n)
	return n, err
}

// SendMessage appends to a thread and bumps its activity timestamp.
// NewMessage is everything a message can be sent with.
type NewMessage struct {
	Body       string
	ReplyTo    int64
	Attachment *uuid.UUID
}

func (r *Repo) SendMessage(ctx context.Context, convID, senderID uuid.UUID, msg NewMessage) (*models.Message, error) {
	var m models.Message
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Only a message from this same thread may be quoted, so a crafted
		// form cannot pull a line out of somebody else's conversation.
		var parent any
		if msg.ReplyTo > 0 {
			var ok bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM messages WHERE id = $1 AND conversation_id = $2)`,
				msg.ReplyTo, convID).Scan(&ok); err != nil {
				return err
			}
			if ok {
				parent = msg.ReplyTo
			}
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO messages (conversation_id, sender_id, body, reply_to_id, attachment_id)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, conversation_id, sender_id, body, created_at, read_at, deleted_at`,
			convID, senderID, msg.Body, parent, msg.Attachment,
		).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.CreatedAt,
			&m.ReadAt, &m.DeletedAt)
		if err != nil {
			return err
		}
		if parent != nil {
			quote := models.MessageQuote{ID: msg.ReplyTo}
			var gone bool
			if err := tx.QueryRow(ctx,
				`SELECT body, sender_id, deleted_at IS NOT NULL FROM messages WHERE id = $1`,
				msg.ReplyTo).Scan(&quote.Body, &quote.SenderID, &gone); err != nil {
				return err
			}
			quote.Withdrawn = gone
			m.ReplyTo = &quote
		}
		// The attachment, read back onto the message that carries it.
		//
		// Without this the row was written with attachment_id set and returned
		// with nothing on it, so the reply to a send said "no file" and the
		// sender watched their photograph render as an empty bubble until they
		// reloaded the page by hand.
		if msg.Attachment != nil {
			var a models.Attachment
			// duration_ms is null for everything that is not a recording, and
			// for a recording whose length the browser could not measure.
			err := tx.QueryRow(ctx, `
				SELECT id, kind, name, mime, bytes, COALESCE(duration_ms, 0)
				  FROM attachments WHERE id = $1`, *msg.Attachment).
				Scan(&a.ID, &a.Kind, &a.Name, &a.Mime, &a.Bytes, &a.DurationMS)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if err == nil {
				m.Attachment = &a
			}
		}

		_, err = tx.Exec(ctx,
			`UPDATE conversations SET last_message_at = now() WHERE id = $1`, convID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Messages returns a page of a thread in chronological order, as one viewer
// sees it: anything before they cleared the conversation is not theirs to read
// back. afterID > 0 fetches only newer messages, which is what the live poller
// uses; afterID = 0 returns the newest page.
func (r *Repo) Messages(ctx context.Context, convID, viewerID uuid.UUID, afterID int64, limit int) ([]*models.Message, error) {
	if afterID > 0 {
		return r.messagePage(ctx, `
			SELECT m.id AS id, m.conversation_id, m.sender_id, m.body, m.created_at, m.read_at, m.deleted_at,
			       p.id AS reply_id, p.body AS reply_body, p.sender_id AS reply_sender,
			       p.deleted_at IS NOT NULL AS reply_gone,
			       f.id AS file_id, f.kind AS file_kind, f.mime AS file_mime,
			       f.name AS file_name, f.bytes AS file_bytes,
			       f.duration_ms AS file_duration
			  FROM messages m
			  LEFT JOIN messages p ON p.id = m.reply_to_id
			  LEFT JOIN attachments f ON f.id = m.attachment_id
			  LEFT JOIN conversation_state s
			         ON s.conversation_id = m.conversation_id AND s.user_id = $2
			 WHERE m.conversation_id = $1 AND m.id > $3
			   AND (s.cleared_at IS NULL OR m.created_at > s.cleared_at)
			   AND NOT EXISTS (SELECT 1 FROM message_hidden h
			                    WHERE h.message_id = m.id AND h.user_id = $2)
			 ORDER BY m.id ASC LIMIT $4`, convID, viewerID, afterID, limit)
	}
	return r.messagePage(ctx, `
		SELECT * FROM (
			SELECT m.id AS id, m.conversation_id, m.sender_id, m.body, m.created_at, m.read_at, m.deleted_at,
			       p.id AS reply_id, p.body AS reply_body, p.sender_id AS reply_sender,
			       p.deleted_at IS NOT NULL AS reply_gone,
			       f.id AS file_id, f.kind AS file_kind, f.mime AS file_mime,
			       f.name AS file_name, f.bytes AS file_bytes,
			       f.duration_ms AS file_duration
			  FROM messages m
			  LEFT JOIN messages p ON p.id = m.reply_to_id
			  LEFT JOIN attachments f ON f.id = m.attachment_id
			  LEFT JOIN conversation_state s
			         ON s.conversation_id = m.conversation_id AND s.user_id = $2
			 WHERE m.conversation_id = $1
			   AND (s.cleared_at IS NULL OR m.created_at > s.cleared_at)
			   AND NOT EXISTS (SELECT 1 FROM message_hidden h
			                    WHERE h.message_id = m.id AND h.user_id = $2)
			 ORDER BY m.id DESC LIMIT $3
		) page ORDER BY id ASC`, convID, viewerID, limit)
}

// MessagesBefore is the page older than one already on screen. Without it a
// thread is only ever its newest eighty messages and the rest is unreachable.
func (r *Repo) MessagesBefore(ctx context.Context, convID, viewerID uuid.UUID, beforeID int64, limit int) ([]*models.Message, error) {
	return r.messagePage(ctx, `
		SELECT * FROM (
			SELECT m.id AS id, m.conversation_id, m.sender_id, m.body, m.created_at, m.read_at, m.deleted_at,
			       p.id AS reply_id, p.body AS reply_body, p.sender_id AS reply_sender,
			       p.deleted_at IS NOT NULL AS reply_gone,
			       f.id AS file_id, f.kind AS file_kind, f.mime AS file_mime,
			       f.name AS file_name, f.bytes AS file_bytes,
			       f.duration_ms AS file_duration
			  FROM messages m
			  LEFT JOIN messages p ON p.id = m.reply_to_id
			  LEFT JOIN attachments f ON f.id = m.attachment_id
			  LEFT JOIN conversation_state s
			         ON s.conversation_id = m.conversation_id AND s.user_id = $2
			 WHERE m.conversation_id = $1 AND m.id < $3
			   AND (s.cleared_at IS NULL OR m.created_at > s.cleared_at)
			   AND NOT EXISTS (SELECT 1 FROM message_hidden h
			                    WHERE h.message_id = m.id AND h.user_id = $2)
			 ORDER BY m.id DESC LIMIT $4
		) page ORDER BY id ASC`, convID, viewerID, beforeID, limit)
}

func (r *Repo) messagePage(ctx context.Context, query string, args ...any) ([]*models.Message, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Message
	for rows.Next() {
		var m models.Message
		var quoteID *int64
		var quoteBody *string
		var quoteSender *uuid.UUID
		var quoteGone *bool
		var fileID *uuid.UUID
		var fileKind, fileMime, fileName *string
		var fileBytes *int64
		var fileDuration *int
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body,
			&m.CreatedAt, &m.ReadAt, &m.DeletedAt,
			&quoteID, &quoteBody, &quoteSender, &quoteGone,
			&fileID, &fileKind, &fileMime, &fileName, &fileBytes,
			&fileDuration); err != nil {
			return nil, err
		}
		if fileID != nil {
			m.Attachment = &models.Attachment{ID: *fileID}
			if fileKind != nil {
				m.Attachment.Kind = *fileKind
			}
			if fileMime != nil {
				m.Attachment.Mime = *fileMime
			}
			if fileName != nil {
				m.Attachment.Name = *fileName
			}
			if fileBytes != nil {
				m.Attachment.Bytes = *fileBytes
			}
			if fileDuration != nil {
				m.Attachment.DurationMS = *fileDuration
			}
		}
		// A reply whose original was deleted outright keeps the reply and
		// loses the quote, which is what ON DELETE SET NULL leaves behind.
		if quoteID != nil && quoteSender != nil {
			m.ReplyTo = &models.MessageQuote{
				ID: *quoteID, SenderID: *quoteSender,
			}
			if quoteBody != nil {
				m.ReplyTo.Body = *quoteBody
			}
			if quoteGone != nil {
				m.ReplyTo.Withdrawn = *quoteGone
			}
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// WithdrawMessage takes back something its sender wishes they had not sent.
//
// Only your own, and the row stays as a tombstone — deleting it outright would
// remove a sentence the other person has already read and answered, leaving a
// reply to nothing.
func (r *Repo) WithdrawMessage(ctx context.Context, messageID int64, senderID uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE messages SET deleted_at = now(), body = ''
		 WHERE id = $1 AND sender_id = $2 AND deleted_at IS NULL`, messageID, senderID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FirstUnreadID is where the reader left off, so a thread they have been away
// from can show them the line rather than making them scroll and guess.
func (r *Repo) FirstUnreadID(ctx context.Context, convID, viewerID uuid.UUID) (int64, error) {
	var id *int64
	err := r.pool.QueryRow(ctx, `
		SELECT min(id) FROM messages
		 WHERE conversation_id = $1 AND sender_id <> $2
		   AND read_at IS NULL AND deleted_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM message_hidden h
		                    WHERE h.message_id = messages.id AND h.user_id = $2)`,
		convID, viewerID).Scan(&id)
	if err != nil || id == nil {
		return 0, err
	}
	return *id, nil
}

// SearchMessages finds text in one thread, in the order the thread reads.
//
// Newest-first was the old behaviour and it flipped the conversation upside
// down the moment you typed: the same messages, suddenly in reverse. The page
// is picked newest-first — the recent hits are the ones wanted — and then
// turned back the right way round.
func (r *Repo) SearchMessages(ctx context.Context, convID, viewerID uuid.UUID, query string, limit int) ([]*models.Message, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	return r.messagePage(ctx, `
		SELECT * FROM (
			SELECT m.id AS id, m.conversation_id, m.sender_id, m.body, m.created_at, m.read_at, m.deleted_at,
			       p.id AS reply_id, p.body AS reply_body, p.sender_id AS reply_sender,
			       p.deleted_at IS NOT NULL AS reply_gone,
			       f.id AS file_id, f.kind AS file_kind, f.mime AS file_mime,
			       f.name AS file_name, f.bytes AS file_bytes,
			       f.duration_ms AS file_duration
			  FROM messages m
			  LEFT JOIN messages p ON p.id = m.reply_to_id
			  LEFT JOIN attachments f ON f.id = m.attachment_id
			  LEFT JOIN conversation_state s
			         ON s.conversation_id = m.conversation_id AND s.user_id = $2
			 WHERE m.conversation_id = $1 AND m.deleted_at IS NULL
			   AND (s.cleared_at IS NULL OR m.created_at > s.cleared_at)
			   AND NOT EXISTS (SELECT 1 FROM message_hidden h
			                    WHERE h.message_id = m.id AND h.user_id = $2)
			   AND m.body ILIKE '%' || $3 || '%'
			 ORDER BY m.id DESC LIMIT $4
		) page ORDER BY id ASC`, convID, viewerID, query, limit)
}

// MarkRead stamps every inbound message in a thread as read and reports how
// many it stamped.
//
// The count is the point. Without it a caller cannot tell "I marked four
// messages" from "there was nothing left to mark", so the read receipt was
// announced either way — and two open threads answered each other's
// announcements forever.
func (r *Repo) MarkRead(ctx context.Context, convID, viewerID uuid.UUID) (int64, error) {
	// The member's own mark, which is what unread is counted against in a
	// thread with more than one other person in it.
	moved, err := r.MarkThreadSeen(ctx, convID, viewerID)
	if err != nil {
		return 0, err
	}
	// And read_at, which is where the sender's two ticks come from. It only
	// means anything when there is exactly one other reader, so it is only
	// stamped there.
	ct, err := r.pool.Exec(ctx, `
		UPDATE messages m SET read_at = now()
		  FROM conversations c
		 WHERE c.id = m.conversation_id AND c.kind = 'direct'
		   AND m.conversation_id = $1 AND m.sender_id <> $2 AND m.read_at IS NULL`,
		convID, viewerID)
	if err != nil {
		return 0, err
	}
	if n := ct.RowsAffected(); n > 0 {
		return n, nil
	}
	return moved, nil
}

// Receipts is the read state of your own messages in one thread, and nothing
// else. Refreshing a tick used to refetch the whole conversation.
func (r *Repo) Receipts(ctx context.Context, convID, senderID uuid.UUID) (map[int64]bool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, read_at IS NOT NULL
		  FROM messages
		 WHERE conversation_id = $1 AND sender_id = $2 AND deleted_at IS NULL`,
		convID, senderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		var read bool
		if err := rows.Scan(&id, &read); err != nil {
			return nil, err
		}
		out[id] = read
	}
	return out, rows.Err()
}

// ClearConversation hides everything said so far from one participant.
//
// The rows stay. The other person is still in the middle of the conversation,
// and deleting what they are replying to would leave their side answering
// nothing — the same reason a withdrawn message keeps its tombstone.
func (r *Repo) ClearConversation(ctx context.Context, convID, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversation_state (conversation_id, user_id, cleared_at)
		VALUES ($1, $2, now())
		ON CONFLICT (conversation_id, user_id)
		DO UPDATE SET cleared_at = now()`, convID, userID)
	return err
}

// MuteConversation silences the notification, not the thread.
func (r *Repo) MuteConversation(ctx context.Context, convID, userID uuid.UUID, muted bool) error {
	var at any
	if muted {
		at = time.Now()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversation_state (conversation_id, user_id, muted_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (conversation_id, user_id)
		DO UPDATE SET muted_at = $3`, convID, userID, at)
	return err
}

// ConversationMuted reports whether this participant asked not to be told.
func (r *Repo) ConversationMuted(ctx context.Context, convID, userID uuid.UUID) (bool, error) {
	var muted bool
	err := r.pool.QueryRow(ctx, `
		SELECT muted_at IS NOT NULL FROM conversation_state
		 WHERE conversation_id = $1 AND user_id = $2`, convID, userID).Scan(&muted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return muted, err
}

// HideMessage drops one line from one person's copy.
//
// Not the same act as withdrawing: that is the sender removing a sentence from
// the conversation, and this is a reader tidying their own. The row stays, and
// so does the other side's view of it.
func (r *Repo) HideMessage(ctx context.Context, messageID int64, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO message_hidden (message_id, user_id)
		SELECT m.id, $2 FROM messages m
		  JOIN conversations c ON c.id = m.conversation_id
		 WHERE m.id = $1
		   AND EXISTS (SELECT 1 FROM conversation_members mem
		                WHERE mem.conversation_id = c.id AND mem.user_id = $2)
		ON CONFLICT DO NOTHING`, messageID, userID)
	return err
}

// SearchConversations finds text across every thread you are in, and returns
// the conversations carrying it with the matching line as the preview.
//
// In-thread search only helps once you already know which thread. Most of the
// time the thing you half-remember is a sentence, not a person.
func (r *Repo) SearchConversations(ctx context.Context, userID uuid.UUID, query string, limit int) ([]*models.Conversation, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.kind, c.title, c.topic, c.last_message_at,
		       u.id, u.username, u.display_name, u.avatar_seed, u.country, u.xp, u.last_seen_at,
		       hit.body, hit.sender_id, 0, s.muted_at IS NOT NULL
		  FROM conversations c
		  JOIN conversation_members mem
		    ON mem.conversation_id = c.id AND mem.user_id = $1
		  LEFT JOIN users u
		         ON c.kind = 'direct'
		        AND u.id = CASE WHEN c.user_a = $1 THEN c.user_b ELSE c.user_a END
		  LEFT JOIN conversation_state s ON s.conversation_id = c.id AND s.user_id = $1
		  JOIN LATERAL (
		        SELECT m.body, m.sender_id, m.id FROM messages m
		         WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
		           AND (s.cleared_at IS NULL OR m.created_at > s.cleared_at)
		           AND NOT EXISTS (SELECT 1 FROM message_hidden h
		                            WHERE h.message_id = m.id AND h.user_id = $1)
		           AND m.body ILIKE '%' || $2 || '%'
		         ORDER BY m.id DESC LIMIT 1
		  ) hit ON true
		 ORDER BY hit.id DESC LIMIT $3`, userID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Conversation
	for rows.Next() {
		var c models.Conversation
		var otherID *uuid.UUID
		var username, display, seed, country *string
		var xp *int
		var seen *time.Time
		if err := rows.Scan(&c.ID, &c.Kind, &c.Title, &c.Topic, &c.LastMessageAt,
			&otherID, &username, &display, &seed, &country, &xp, &seen,
			&c.LastMessage, &c.LastSenderID, &c.UnreadCount, &c.Muted); err != nil {
			return nil, err
		}
		if otherID != nil {
			c.Other = &models.UserCard{
				ID: *otherID, Username: deref(username), DisplayName: deref(display),
				AvatarSeed: deref(seed), Country: deref(country),
				XP: derefInt(xp), LastSeenAt: derefTime(seen),
			}
		}
		c.Matched = true
		c.Joined = true
		out = append(out, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, r.attachMembers(ctx, out)
}

// --------------------------------------------------------- notifications --

func (r *Repo) Notify(ctx context.Context, userID uuid.UUID, kind string, payload map[string]any) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1, $2, $3)`,
		userID, kind, payload)
	return err
}

// CountNotifications is how many the history holds, so the screen can page
// rather than stop at sixty without saying so.
func (r *Repo) CountNotifications(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

func (r *Repo) Notifications(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*models.Notification, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, kind, payload, read_at, created_at
		  FROM notifications WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Notification
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.Payload,
			&n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

// Notification loads one, scoped to the person it belongs to.
func (r *Repo) Notification(ctx context.Context, id int64, userID uuid.UUID) (*models.Notification, error) {
	var n models.Notification
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, kind, payload, read_at, created_at
		  FROM notifications WHERE id = $1 AND user_id = $2`, id, userID,
	).Scan(&n.ID, &n.UserID, &n.Kind, &n.Payload, &n.ReadAt, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &n, err
}

// MarkNotificationRead clears one, which is what opening it should do. Marking
// the whole list read was the only way, so anything you followed stayed bold.
func (r *Repo) MarkNotificationRead(ctx context.Context, id int64, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now()
		  WHERE id = $1 AND user_id = $2 AND read_at IS NULL`, id, userID)
	return err
}

// MarkConversationNotificationsRead retires the "somebody wrote to you" rows
// for one thread.
//
// Reading a thread stamped its messages and stopped there, so the bell went on
// counting news the reader had already read — seven unread notifications for
// seven messages sitting open on the screen in front of them. A notification
// exists to point at something unseen; once the thing is seen it has nothing
// left to say.
func (r *Repo) MarkConversationNotificationsRead(ctx context.Context, userID, convID uuid.UUID) (int64, error) {
	ct, err := r.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		 WHERE user_id = $1
		   AND kind = 'message.new'
		   AND read_at IS NULL
		   AND payload->>'conversation_id' = $2`,
		userID, convID.String())
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

func (r *Repo) MarkNotificationsRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`,
		userID)
	return err
}

// BadgeCounts is every number the navigation shows, in one round trip.
//
// The view context asked five separate questions before rendering any page —
// unread messages, pending duels, friend requests, support, notifications —
// and the admin chrome asked three more. Each is cheap; nine sequential round
// trips to a database that is not a unix socket is not.
type BadgeCounts struct {
	UnreadMessages    int
	PendingChallenges int
	FriendRequests    int
	SupportUnread     int
	Notifications     int
}

func (r *Repo) BadgeCounts(ctx context.Context, userID uuid.UUID) (BadgeCounts, error) {
	var c BadgeCounts
	err := r.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM messages m
		     JOIN conversations cv ON cv.id = m.conversation_id
		     JOIN conversation_members mem
		       ON mem.conversation_id = cv.id AND mem.user_id = $1
		    WHERE m.sender_id <> $1 AND m.deleted_at IS NULL
		      AND CASE WHEN cv.kind = 'direct'
		               THEN m.read_at IS NULL
		               ELSE m.id > mem.last_read_id END),
		  (SELECT count(*) FROM challenge_players p
		     JOIN challenges c ON c.id = p.challenge_id
		    WHERE p.user_id = $1 AND p.state IN ('invited', 'joined')
		      AND c.status IN ('pending', 'accepted') AND c.expires_at > now()),
		  (SELECT count(*) FROM friendships
		    WHERE addressee_id = $1 AND status = 'pending'),
		  (SELECT COALESCE(sum(user_unread), 0) FROM support_tickets WHERE user_id = $1),
		  (SELECT count(*) FROM notifications
		    WHERE user_id = $1 AND read_at IS NULL)`, userID,
	).Scan(&c.UnreadMessages, &c.PendingChallenges, &c.FriendRequests,
		&c.SupportUnread, &c.Notifications)
	return c, err
}

// PurgeNotificationsOlderThan drops the tail of the notification history.
//
// Seven places write into this table and nothing ever deleted from it. The
// screen shows the most recent sixty; everything behind that was kept forever
// for nobody to read.
func (r *Repo) PurgeNotificationsOlderThan(ctx context.Context, d time.Duration) (int64, error) {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM notifications WHERE created_at < now() - $1::interval`,
		d.String())
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// PurgeOrphanedNotifications drops notifications whose subject is gone.
//
// A notification carries the id of the thing it is about in its payload, which
// is jsonb and so has no foreign key to enforce: delete the ticket, the duel or
// the conversation and the notification stays, pointing at a page that answers
// "not found". Twenty-six support notifications against two surviving tickets
// is what that looks like from the inside.
func (r *Repo) PurgeOrphanedNotifications(ctx context.Context) (int64, error) {
	ct, err := r.pool.Exec(ctx, `
		DELETE FROM notifications n
		 WHERE (n.kind IN ('support.new', 'support.reply')
		        AND n.payload ? 'ticket_id'
		        AND NOT EXISTS (
		            SELECT 1 FROM support_tickets t
		             WHERE t.id = (n.payload->>'ticket_id')::uuid))
		    OR (n.kind = 'message.new'
		        AND n.payload ? 'conversation_id'
		        AND NOT EXISTS (
		            SELECT 1 FROM conversations c
		             WHERE c.id = (n.payload->>'conversation_id')::uuid))
		    OR (n.kind LIKE 'challenge.%'
		        AND n.payload ? 'challenge_id'
		        AND NOT EXISTS (
		            SELECT 1 FROM challenges ch
		             WHERE ch.id = (n.payload->>'challenge_id')::uuid))`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// PurgeSpentPasswordResets drops reset tokens that can no longer be redeemed.
//
// Migration 0005 ships an index for exactly this sweep, with a comment saying
// it is cheap, and nothing swept. A used or expired token is the one kind of
// row there is no reason to keep.
func (r *Repo) PurgeSpentPasswordResets(ctx context.Context) (int64, error) {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM password_resets WHERE used_at IS NOT NULL OR expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

func (r *Repo) UnreadNotificationCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`,
		userID).Scan(&n)
	return n, err
}

// ----------------------------------------------------------- attachments --

// CreateAttachment records bytes that have already been written to disk.
func (r *Repo) CreateAttachment(ctx context.Context, a *models.Attachment) error {
	var duration *int
	if a.DurationMS > 0 {
		duration = &a.DurationMS
	}
	return r.pool.QueryRow(ctx, `
		INSERT INTO attachments (id, owner_id, kind, mime, name, bytes, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`,
		a.ID, a.OwnerID, a.Kind, a.Mime, a.Name, a.Bytes, duration).Scan(&a.CreatedAt)
}

// Attachment loads one, for the route that serves it.
func (r *Repo) Attachment(ctx context.Context, id uuid.UUID) (*models.Attachment, error) {
	var a models.Attachment
	var duration *int
	err := r.pool.QueryRow(ctx, `
		SELECT id, owner_id, kind, mime, name, bytes, duration_ms, created_at
		  FROM attachments WHERE id = $1`, id).
		Scan(&a.ID, &a.OwnerID, &a.Kind, &a.Mime, &a.Name, &a.Bytes, &duration, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if duration != nil {
		a.DurationMS = *duration
	}
	return &a, nil
}

// CanSeeAttachment answers the only question the file route has to ask.
//
// Three ways to be allowed: you sent it, it is somebody's profile photo, or it
// is in a conversation you are part of. Anything else is a file belonging to
// two other people, and a guessed id must not reach it.
func (r *Repo) CanSeeAttachment(ctx context.Context, attID, viewerID uuid.UUID) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM attachments WHERE id = $1 AND owner_id = $2)
		    OR EXISTS (SELECT 1 FROM users WHERE avatar_seed = 'photo:' || $1::text)
		    OR EXISTS (
		         SELECT 1 FROM messages m
		           JOIN conversations c ON c.id = m.conversation_id
		          WHERE m.attachment_id = $1
		            AND EXISTS (SELECT 1 FROM conversation_members mem
		                         WHERE mem.conversation_id = c.id
		                           AND mem.user_id = $2))`,
		attID, viewerID).Scan(&ok)
	return ok, err
}

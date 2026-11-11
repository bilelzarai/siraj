-- Module 6 · Friends, requests, blocks and invite links (FR-01, endpoints 63–74).
-- A pair is always stored with the smaller uuid in user_a.

-- name: ListFriends :many
-- Endpoint 63: friends, online first (seen in the last five minutes), then by name.
SELECT u.id, u.username, u.display_name, u.avatar_url, u.level, u.kind, u.last_seen_at,
  COALESCE(u.last_seen_at > now() - interval '5 minutes', false)::boolean AS online,
  COALESCE((SELECT count(*) FROM challenge_participants cp
            JOIN challenge_participants mine ON mine.challenge_id = cp.challenge_id AND mine.user_id = @user_id
            WHERE cp.user_id = u.id AND cp.result = 'lost' AND mine.result = 'won'), 0)::int AS wins_vs,
  COALESCE((SELECT count(*) FROM challenge_participants cp
            JOIN challenge_participants mine ON mine.challenge_id = cp.challenge_id AND mine.user_id = @user_id
            WHERE cp.user_id = u.id AND cp.result = 'won' AND mine.result = 'lost'), 0)::int AS losses_vs
FROM friendships f
JOIN users u ON u.id = CASE WHEN f.user_a = @user_id THEN f.user_b ELSE f.user_a END
WHERE f.user_a = @user_id OR f.user_b = @user_id
  AND (@q::text = '' OR u.display_name ILIKE '%' || @q || '%' OR u.username ILIKE '%' || @q || '%')
ORDER BY online DESC, u.display_name;

-- name: IsFriend :one
SELECT EXISTS (SELECT 1 FROM friendships WHERE user_a = @a AND user_b = @b);

-- name: AddFriendship :exec
INSERT INTO friendships (user_a, user_b) VALUES (@a, @b) ON CONFLICT DO NOTHING;

-- name: RemoveFriendship :execrows
DELETE FROM friendships WHERE user_a = @a AND user_b = @b;

-- name: CountFriends :one
SELECT count(*) FROM friendships WHERE user_a = @user_id OR user_b = @user_id;

-- name: ListFriendRequests :many
-- Endpoint 65: received (incoming = true) or sent.
SELECT r.id, r.from_id, r.to_id, r.created_at,
       u.username, u.display_name, u.avatar_url, u.level
FROM friend_requests r
JOIN users u ON u.id = CASE WHEN @incoming::boolean THEN r.from_id ELSE r.to_id END
WHERE r.status = 'pending'
  AND ((@incoming::boolean AND r.to_id = @user_id) OR (NOT @incoming::boolean AND r.from_id = @user_id))
ORDER BY r.created_at DESC;

-- name: CreateFriendRequest :one
INSERT INTO friend_requests (from_id, to_id) VALUES (@from_id, @to_id)
ON CONFLICT (from_id, to_id) WHERE status = 'pending' DO UPDATE SET status = 'pending'
RETURNING *;

-- name: GetFriendRequest :one
SELECT * FROM friend_requests WHERE id = @id;

-- name: AnswerFriendRequest :exec
UPDATE friend_requests SET status = @status, responded_at = now()
WHERE id = @id AND status = 'pending';

-- name: PendingRequestBetween :one
SELECT * FROM friend_requests
WHERE status = 'pending'
  AND ((from_id = @a AND to_id = @b) OR (from_id = @b AND to_id = @a))
LIMIT 1;

-- name: CountFriendRequests :one
SELECT count(*) FROM friend_requests WHERE to_id = @user_id AND status = 'pending';

-- name: SearchPeople :many
-- Endpoint 69: find people by name or @username. Blocked people never appear.
SELECT u.id, u.username, u.display_name, u.avatar_url, u.level, u.kind,
  COALESCE(u.last_seen_at > now() - interval '5 minutes', false)::boolean AS online,
  EXISTS (SELECT 1 FROM friendships f
          WHERE (f.user_a = LEAST(@viewer_id::uuid, u.id) AND f.user_b = GREATEST(@viewer_id::uuid, u.id))) AS is_friend,
  EXISTS (SELECT 1 FROM friend_requests r
          WHERE r.status = 'pending' AND r.from_id = @viewer_id AND r.to_id = u.id) AS requested
FROM users u
WHERE u.kind = 'member' AND u.id <> @viewer_id
  AND (u.display_name ILIKE '%' || @q || '%' OR u.username ILIKE '%' || @q || '%')
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = @viewer_id AND b.blocked_id = u.id)
                     OR (b.blocker_id = u.id AND b.blocked_id = @viewer_id))
ORDER BY u.display_name
LIMIT @lim;

-- name: GetUserByUsername :one
-- Endpoint 70: public profile.
SELECT id, username, display_name, avatar_url, level, xp, kind, created_at,
  COALESCE(last_seen_at > now() - interval '5 minutes', false)::boolean AS online
FROM users WHERE username = @username AND kind = 'member';

-- name: PlayedWith :many
-- Endpoint 71: people the viewer met in a game, room or challenge, not friends yet.
-- Whoever asked may already have a request out to them, which is what turns the Add
-- button into "Requested" (FR-03). A guest has no account to befriend, so only the
-- guest-to-guest list ever includes one (RL-03).
SELECT DISTINCT u.id, u.username, u.display_name, u.avatar_url, u.level, u.kind,
  COALESCE(u.last_seen_at > now() - interval '5 minutes', false)::boolean AS online,
  EXISTS (SELECT 1 FROM friend_requests r
          WHERE r.status = 'pending' AND r.from_id = @viewer_id AND r.to_id = u.id) AS requested
FROM game_players mine
JOIN game_players theirs ON theirs.game_id = mine.game_id AND theirs.user_id <> mine.user_id
JOIN users u ON u.id = theirs.user_id
WHERE mine.user_id = @viewer_id
  AND (CASE WHEN @guests_only::boolean THEN u.kind = 'guest' ELSE u.kind = 'member' END)
  AND NOT EXISTS (SELECT 1 FROM friendships f
                  WHERE f.user_a = LEAST(@viewer_id::uuid, u.id) AND f.user_b = GREATEST(@viewer_id::uuid, u.id))
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = @viewer_id AND b.blocked_id = u.id)
                     OR (b.blocker_id = u.id AND b.blocked_id = @viewer_id))
LIMIT @lim;

-- name: SharedHistory :one
-- MS-04 / RL-02: have these two ever met in a game, room or challenge?
SELECT EXISTS (
  SELECT 1 FROM game_players a JOIN game_players b ON b.game_id = a.game_id
   WHERE a.user_id = @a AND b.user_id = @b
  UNION ALL
  SELECT 1 FROM room_members a JOIN room_members b ON b.room_id = a.room_id
   WHERE a.user_id = @a AND b.user_id = @b
  UNION ALL
  SELECT 1 FROM challenge_participants a JOIN challenge_participants b ON b.challenge_id = a.challenge_id
   WHERE a.user_id = @a AND b.user_id = @b
);

-- name: AddBlock :exec
INSERT INTO blocks (blocker_id, blocked_id) VALUES (@blocker_id, @blocked_id) ON CONFLICT DO NOTHING;

-- name: RemoveBlock :execrows
DELETE FROM blocks WHERE blocker_id = @blocker_id AND blocked_id = @blocked_id;

-- name: IsBlocked :one
SELECT EXISTS (
  SELECT 1 FROM blocks WHERE (blocker_id = @a AND blocked_id = @b) OR (blocker_id = @b AND blocked_id = @a)
);

-- name: ListBlocks :many
SELECT u.id, u.username, u.display_name, u.avatar_url
FROM blocks b JOIN users u ON u.id = b.blocked_id
WHERE b.blocker_id = @user_id ORDER BY u.display_name;

-- name: CreateInvite :one
INSERT INTO invites (code, inviter_id, expires_at) VALUES (@code, @inviter_id, @expires_at) RETURNING *;

-- name: GetInvite :one
SELECT * FROM invites WHERE code = @code AND (expires_at IS NULL OR expires_at > now());

-- name: UseInvite :exec
UPDATE invites SET uses = uses + 1 WHERE code = @code;

-- name: MyInvite :one
SELECT * FROM invites WHERE inviter_id = @inviter_id AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC LIMIT 1;

-- name: FriendPresence :many
-- What each friend is doing right now, for the Friends card on Home (HM-07):
-- the domain of a game they are in, otherwise when they were last seen.
SELECT f.id,
       (SELECT COALESCE(dt.name, dtf.name)
          FROM game_players gp
          JOIN games g        ON g.id = gp.game_id AND g.status = 'playing'
          JOIN game_questions gq ON gq.game_id = g.id AND gq.position = 0
          JOIN questions q    ON q.id = gq.question_id
          JOIN categories c   ON c.id = q.category_id
          JOIN domains d      ON d.id = c.domain_id
          LEFT JOIN domain_translations dt  ON dt.domain_id = d.id AND dt.lang = @lang
          LEFT JOIN domain_translations dtf ON dtf.domain_id = d.id AND dtf.lang = 'en'
         WHERE gp.user_id = f.id
         ORDER BY g.started_at DESC LIMIT 1) AS playing_domain,
       f.last_seen_at
FROM users f
WHERE f.id = ANY(@ids::uuid[]);

-- name: GuestsMet :many
-- The guests this guest played beside: in a live room, in a duel, or in a challenge.
-- They are the only people a guest may write to (§4 of 03-BACKEND-CHANGES).
WITH rooms_together AS (
  SELECT DISTINCT other.user_id, 'room'::text AS how
  FROM room_members me
  JOIN room_members other ON other.room_id = me.room_id AND other.user_id <> me.user_id
  WHERE me.user_id = @user_id::uuid
),
games_together AS (
  SELECT DISTINCT other.user_id, g.mode::text AS how
  FROM game_players me
  JOIN games g ON g.id = me.game_id
  JOIN game_players other ON other.game_id = me.game_id AND other.user_id <> me.user_id
  WHERE me.user_id = @user_id::uuid AND other.user_id IS NOT NULL
),
met AS (
  SELECT user_id, min(how) AS how FROM (
    SELECT * FROM rooms_together UNION ALL SELECT * FROM games_together
  ) all_met GROUP BY user_id
)
SELECT u.id, u.display_name, met.how::text AS note,
       COALESCE(u.last_seen_at > now() - interval '5 minutes', false)::boolean AS online
FROM met
JOIN users u ON u.id = met.user_id
WHERE u.kind = 'guest' AND u.id IS NOT NULL
ORDER BY u.display_name
LIMIT 20;

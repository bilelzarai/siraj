-- Live rooms (RM-01…RM-05): a host opens a room with a six-character code, players
-- join with it, everyone gets the same questions at the same time.

-- name: CreateRoom :one
INSERT INTO rooms (code, host_id, is_guest_room, max_players, chat_enabled, settings)
VALUES (@code, @host_id, @is_guest_room, @max_players, @chat_enabled, @settings)
RETURNING *;

-- name: GetRoomByCode :one
SELECT * FROM rooms WHERE code = @code AND status <> 'closed';

-- name: GetRoom :one
SELECT * FROM rooms WHERE id = @id;

-- name: JoinRoom :exec
INSERT INTO room_members (room_id, user_id)
VALUES (@room_id, @user_id)
ON CONFLICT (room_id, user_id) DO UPDATE SET left_at = NULL, kicked = false;

-- name: LeaveRoom :exec
UPDATE room_members SET left_at = now() WHERE room_id = @room_id AND user_id = @user_id;

-- name: SetReady :exec
UPDATE room_members SET is_ready = @is_ready WHERE room_id = @room_id AND user_id = @user_id;

-- name: RoomMembers :many
SELECT m.*, u.display_name, u.username, u.level, u.kind,
       (u.id = r.host_id) AS is_host
FROM room_members m
JOIN rooms r ON r.id = m.room_id
JOIN users u ON u.id = m.user_id
WHERE m.room_id = @room_id AND m.left_at IS NULL AND NOT m.kicked
ORDER BY m.joined_at;

-- name: CountRoomMembers :one
SELECT count(*)::int FROM room_members
WHERE room_id = @room_id AND left_at IS NULL AND NOT kicked;

-- name: SetRoomStatus :exec
UPDATE rooms SET status = @status WHERE id = @id;

-- name: RoomGame :one
-- The game a room is playing, if it started one (RM-03).
SELECT * FROM games WHERE room_id = @room_id ORDER BY created_at DESC LIMIT 1;

-- name: CloseStaleRooms :exec
-- The worker closes rooms nobody came back to (RM-05).
UPDATE rooms SET status = 'closed'
WHERE status <> 'closed' AND updated_at < now() - interval '6 hours';

-- name: PostRoomMessage :one
-- One line of room chat: words, a file, or both (RM-05, MS-02).
INSERT INTO room_messages (room_id, user_id, body, kind, upload_id)
VALUES (@room_id, @user_id, sqlc.narg('body'), @kind, sqlc.narg('upload_id'))
RETURNING *;

-- name: ListRoomMessages :many
-- The last lines of a room's chat, newest first, with who said them and whatever
-- they attached (RM-05).
SELECT m.id, m.user_id, m.body, m.kind AS line_kind, m.created_at,
       u.display_name, u.kind,
       f.file_name, f.bytes AS file_bytes, f.duration_ms AS file_duration,
       f.storage_key AS file_key
FROM room_messages m
JOIN users u ON u.id = m.user_id
LEFT JOIN uploads f ON f.id = m.upload_id
WHERE m.room_id = @room_id
ORDER BY m.created_at DESC, m.id DESC
LIMIT @lim;

-- name: CountAnswersAt :one
-- How many players of a shared game have answered the question at this position: a
-- live room moves on when everyone has (RM-03).
SELECT count(*) FROM game_answers a
JOIN game_players p ON p.id = a.player_id
WHERE a.game_id = @game_id AND a.position = @position;

-- name: CountGamePlayers :one
SELECT count(*) FROM game_players WHERE game_id = @game_id;

-- name: UpdateRoomSettings :exec
-- The host changing the room while it waits: the mode, the two teams, and the rest of
-- the choice the lobby lets them touch (RM-02, RM-05).
UPDATE rooms SET settings = @settings WHERE id = @id;

-- name: OpenRoomsOfFriends :many
-- P13's "Open rooms from friends": the rooms my friends are waiting in, newest first.
-- A guest sees guest rooms only, and a member never sees a guest room (RL-03).
SELECT r.code, r.max_players, r.settings, r.is_guest_room,
       u.id AS host_id, u.display_name AS host_name, u.kind AS host_kind,
       (SELECT count(*)::int FROM room_members m
        WHERE m.room_id = r.id AND m.left_at IS NULL AND NOT m.kicked) AS players
FROM rooms r
JOIN users u ON u.id = r.host_id
WHERE r.status = 'open'
  AND r.is_guest_room = @guest_rooms
  AND (r.host_id = ANY(@friend_ids::uuid[]))
ORDER BY r.updated_at DESC
LIMIT @lim;

-- name: MyRooms :many
-- The Rooms tab of Messages: the live rooms this player is in, with the last thing
-- said in each (MS-01, RM-05).
SELECT r.id, r.code, r.status, r.settings,
       (SELECT count(*)::int FROM room_members m
        WHERE m.room_id = r.id AND m.left_at IS NULL AND NOT m.kicked) AS players,
       COALESCE((SELECT COALESCE(rm.body, '{' || rm.kind || '}') FROM room_messages rm
        WHERE rm.room_id = r.id
        ORDER BY rm.created_at DESC, rm.id DESC LIMIT 1), '')::text AS last_body,
       (SELECT rm.created_at FROM room_messages rm WHERE rm.room_id = r.id
        ORDER BY rm.created_at DESC, rm.id DESC LIMIT 1) AS last_at
FROM room_members me
JOIN rooms r ON r.id = me.room_id
WHERE me.user_id = @user_id AND me.left_at IS NULL AND NOT me.kicked
  AND r.status <> 'closed'
ORDER BY r.updated_at DESC
LIMIT 20;

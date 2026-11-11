-- Direct messages, groups and the Sirāj team thread (MS-01…MS-06). A conversation is
-- a direct thread between two players (direct_key keeps it unique), a group a player
-- made with friends, or the system thread support replies land in. A live room's chat
-- lives with the room itself and is read through rooms.sql.

-- name: FindDirect :one
SELECT * FROM conversations WHERE direct_key = @direct_key;

-- name: CreateDirect :one
INSERT INTO conversations (kind, direct_key) VALUES ('direct', @direct_key)
RETURNING *;

-- name: AddConversationMember :exec
INSERT INTO conversation_members (conversation_id, user_id)
VALUES (@conversation_id, @user_id)
ON CONFLICT (conversation_id, user_id) DO UPDATE SET hidden_at = NULL;

-- name: GetConversation :one
SELECT * FROM conversations WHERE id = @id;

-- name: IsConversationMember :one
SELECT EXISTS (
  SELECT 1 FROM conversation_members
  WHERE conversation_id = @conversation_id AND user_id = @user_id
)::bool;

-- name: MyConversations :many
-- The inbox (MS-01). One row per thread with everything a row of the list draws: who
-- it is with, what was said last and by whom, the time, the unread count, and the pin
-- and mute the player set on it. Pinned threads come first, as the design shows.
SELECT c.id, c.kind, c.title, c.last_message_at, me.pinned, me.muted,
       COALESCE(other.id, '00000000-0000-0000-0000-000000000000'::uuid) AS other_id,
       COALESCE(other.display_name, '') AS other_name,
       COALESCE(other.online, false) AS other_online,
       COALESCE(other.kind, 'member')::text AS other_kind,
       last.body        AS last_body,
       -- A thread with no message yet has no last line: the empty string, not NULL,
       -- so the inbox renders it as "no messages yet" rather than failing to scan.
       COALESCE(last.kind::text, '') AS last_kind,
       last.sender_id   AS last_sender,
       COALESCE(sender.display_name, '') AS last_sender_name,
       (SELECT count(*) FROM messages m
          WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
            AND m.sender_id IS DISTINCT FROM sqlc.arg('user_id')::uuid
            AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id))::int AS unread,
       (SELECT count(*) FROM conversation_members cm WHERE cm.conversation_id = c.id)::int AS members
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
LEFT JOIN conversation_members them
       ON them.conversation_id = c.id AND them.user_id <> @user_id
LEFT JOIN (
  SELECT u.id, u.display_name, u.kind,
         (u.last_seen_at > now() - interval '5 minutes') AS online
  FROM users u
) other ON other.id = them.user_id
LEFT JOIN LATERAL (
  SELECT m.body, m.kind, m.sender_id FROM messages m
  WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
  ORDER BY m.id DESC LIMIT 1
) last ON true
LEFT JOIN users sender ON sender.id = last.sender_id
WHERE me.user_id = @user_id AND me.hidden_at IS NULL
  AND (@kinds::text[] = '{}' OR c.kind::text = ANY(@kinds::text[]))
ORDER BY me.pinned DESC, c.last_message_at DESC NULLS LAST, c.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountInbox :one
-- The inbox's total. It was capped at @lim with no offset and no count, so a player
-- with more conversations than the cap simply could not reach them (§17).
SELECT count(*)::int
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
WHERE me.user_id = @user_id AND me.hidden_at IS NULL
  AND (@kinds::text[] = '{}' OR c.kind::text = ANY(@kinds::text[]));

-- name: ConversationMessages :many
-- One thread, oldest first. A message deleted for everyone still comes back, as the
-- tombstone the design draws; one deleted for me does not (MS-04).
SELECT m.id, m.sender_id, m.kind, m.body, m.created_at, m.deleted_at, m.payload,
       u.display_name AS sender_name,
       q.body       AS quote_body,
       q.kind       AS quote_kind,
       qu.display_name AS quote_name,
       f.kind       AS file_kind,
       f.file_name  AS file_name,
       f.bytes      AS file_bytes,
       f.duration_ms AS file_duration,
       f.storage_key AS file_key
FROM messages m
LEFT JOIN users u      ON u.id = m.sender_id
LEFT JOIN messages q   ON q.id = m.reply_to_id
LEFT JOIN users qu     ON qu.id = q.sender_id
LEFT JOIN uploads f    ON f.id = m.upload_id
WHERE m.conversation_id = @conversation_id
  AND NOT EXISTS (SELECT 1 FROM message_hidden h WHERE h.message_id = m.id AND h.user_id = @user_id)
ORDER BY m.id
LIMIT @lim;

-- name: SendMessage :one
INSERT INTO messages (conversation_id, sender_id, kind, body, reply_to_id, upload_id, payload)
VALUES (@conversation_id, @sender_id, @kind, sqlc.narg('body'),
        sqlc.narg('reply_to_id'), sqlc.narg('upload_id'), @payload)
RETURNING *;

-- name: TouchConversation :exec
UPDATE conversations SET last_message_at = now() WHERE id = @id;

-- name: MarkConversationRead :exec
-- Message ids are uuidv7, so the newest is the largest when ordered as text; taking
-- the last row by id is the same thing and does not need max() on a uuid.
UPDATE conversation_members cm SET last_read_message_id = (
  SELECT m.id FROM messages m
  WHERE m.conversation_id = @conversation_id
  ORDER BY m.id DESC LIMIT 1
) WHERE cm.conversation_id = @conversation_id AND cm.user_id = @user_id;

-- name: MarkConversationUnread :exec
-- "Mark as unread": step the read mark back one message, so the thread shows a one
-- and the player finds it again (MS-01).
UPDATE conversation_members cm SET last_read_message_id = (
  SELECT m.id FROM messages m
  WHERE m.conversation_id = @conversation_id
  ORDER BY m.id DESC OFFSET 1 LIMIT 1
) WHERE cm.conversation_id = @conversation_id AND cm.user_id = @user_id;

-- name: SetConversationPinned :exec
UPDATE conversation_members SET pinned = @pinned
WHERE conversation_id = @conversation_id AND user_id = @user_id;

-- name: SetConversationMuted :exec
UPDATE conversation_members SET muted = @muted
WHERE conversation_id = @conversation_id AND user_id = @user_id;

-- name: HideConversation :exec
-- "Delete conversation": removed for this player only, as the dialog promises.
UPDATE conversation_members SET hidden_at = now()
WHERE conversation_id = @conversation_id AND user_id = @user_id;

-- name: UnreadConversations :one
SELECT count(*)::int FROM conversation_members me
WHERE me.user_id = @user_id AND me.hidden_at IS NULL
  AND EXISTS (
    SELECT 1 FROM messages m
    WHERE m.conversation_id = me.conversation_id AND m.deleted_at IS NULL
      AND m.sender_id IS DISTINCT FROM sqlc.arg('user_id')::uuid
      AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id)
  );

-- name: UnreadByKind :many
-- The number on each tab of the inbox (MS-01).
SELECT c.kind::text AS kind, count(*)::int AS threads
FROM conversation_members me
JOIN conversations c ON c.id = me.conversation_id
WHERE me.user_id = @user_id AND me.hidden_at IS NULL
  AND EXISTS (
    SELECT 1 FROM messages m
    WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
      AND m.sender_id IS DISTINCT FROM sqlc.arg('user_id')::uuid
      AND (me.last_read_message_id IS NULL OR m.id > me.last_read_message_id)
  )
GROUP BY c.kind;

-- name: ConversationMemberIDs :many
-- Who is in a conversation: the stream tells them a message arrived (MS-01).
SELECT user_id FROM conversation_members WHERE conversation_id = @conversation_id;

-- name: ConversationPeople :many
-- The names under a group's title: "Omar K., Khadija M., Sarah M. and you".
SELECT u.id, u.display_name, u.kind,
       COALESCE(u.last_seen_at > now() - interval '5 minutes', false)::boolean AS online
FROM conversation_members cm
JOIN users u ON u.id = cm.user_id
WHERE cm.conversation_id = @conversation_id
ORDER BY cm.joined_at;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = @id;

-- name: DeleteMessageForEveryone :exec
-- Only the writer, and the body goes with it: the row stays so the tombstone can.
UPDATE messages SET deleted_at = now(), body = NULL, upload_id = NULL
WHERE id = @id AND sender_id = @sender_id AND deleted_at IS NULL;

-- name: HideMessageForMe :exec
INSERT INTO message_hidden (message_id, user_id) VALUES (@message_id, @user_id)
ON CONFLICT DO NOTHING;

-- name: ReportMessage :exec
INSERT INTO message_reports (message_id, reporter_id, reason, note)
VALUES (@message_id, @reporter_id, @reason, sqlc.narg('note'));

-- name: CreateUpload :one
INSERT INTO uploads (owner_id, kind, mime, bytes, duration_ms, storage_key, file_name)
VALUES (@owner_id, @kind, @mime, @bytes, sqlc.narg('duration_ms'), @storage_key, sqlc.narg('file_name'))
RETURNING *;

-- name: ReadMarkers :many
-- How far every other member has read, so a sent message can show one tick or two
-- (MS-02). A thread of two has one row.
SELECT user_id, last_read_message_id FROM conversation_members
WHERE conversation_id = @conversation_id AND user_id <> @user_id;

-- name: FindSystemConversation :one
-- The "Sirāj team" thread: announcements and support replies, one per player (MS-03).
SELECT c.* FROM conversations c
JOIN conversation_members cm ON cm.conversation_id = c.id
WHERE c.kind = 'system' AND cm.user_id = @user_id
LIMIT 1;

-- name: CreateSystemConversation :one
INSERT INTO conversations (kind, title) VALUES ('system', @title) RETURNING *;

-- name: CreateGroup :one
-- Endpoint 76: a group a player starts with friends (MS-01).
INSERT INTO conversations (kind, title) VALUES ('group', @title)
RETURNING *;

-- name: SetConversationTitle :exec
-- Endpoint 78: renaming a group. Only a member may, and only a group has a name.
UPDATE conversations SET title = @title WHERE id = @id AND kind = 'group';

-- name: ConversationMembersFull :many
-- The group's people, as the members panel lists them: who they are, whether they
-- are about, and who owns the group.
SELECT u.id, u.display_name, u.kind, cm.role, cm.joined_at,
       COALESCE(u.last_seen_at > now() - interval '5 minutes', false)::boolean AS online
FROM conversation_members cm
JOIN users u ON u.id = cm.user_id
WHERE cm.conversation_id = @conversation_id
ORDER BY (cm.role = 'owner') DESC, cm.joined_at;

-- name: CountConversationMembers :one
SELECT count(*)::int FROM conversation_members WHERE conversation_id = @conversation_id;

-- name: AddGroupMember :exec
-- Adding somebody to a group: a member who had hidden the thread sees it again.
INSERT INTO conversation_members (conversation_id, user_id, role)
VALUES (@conversation_id, @user_id, 'member')
ON CONFLICT (conversation_id, user_id) DO UPDATE SET hidden_at = NULL;

-- name: RemoveConversationMember :execrows
DELETE FROM conversation_members
WHERE conversation_id = @conversation_id AND user_id = @user_id;

-- name: SetConversationOwner :exec
UPDATE conversation_members SET role = 'owner'
WHERE conversation_id = @conversation_id AND user_id = @user_id;

-- name: ConversationRole :one
SELECT role FROM conversation_members
WHERE conversation_id = @conversation_id AND user_id = @user_id;

-- name: SendChallengeCard :one
-- A challenge posted into a thread: the card the other side taps to play (MS-02).
INSERT INTO messages (conversation_id, sender_id, kind, body, payload)
VALUES (@conversation_id, @sender_id, 'challenge', NULL, @payload)
RETURNING *;

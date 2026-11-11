-- Module 10 · Notifications (OT-01, endpoints 87–89).

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE user_id = @user_id
  AND (NOT @unread_only::boolean OR read_at IS NULL)
ORDER BY id DESC
LIMIT @lim OFFSET @off;

-- name: CountNotifications :one
SELECT count(*) FROM notifications
WHERE user_id = @user_id AND (NOT @unread_only::boolean OR read_at IS NULL);

-- name: UnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = @user_id AND read_at IS NULL;

-- name: CreateNotification :one
INSERT INTO notifications (user_id, kind, payload, link)
VALUES (@user_id, @kind, @payload, sqlc.narg('link'))
RETURNING *;

-- name: MarkNotificationsRead :exec
UPDATE notifications SET read_at = now()
WHERE user_id = @user_id AND read_at IS NULL
  AND (cardinality(@ids::uuid[]) = 0 OR id = ANY (@ids));

-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = now() WHERE id = @id AND user_id = @user_id;

-- name: GetNotification :one
SELECT * FROM notifications WHERE id = @id AND user_id = @user_id;

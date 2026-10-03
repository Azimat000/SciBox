-- Уведомления на сайте и очередь писем (срез 8).

-- name: InsertNotification :exec
INSERT INTO notifications (user_id, kind, title, body, link, created_at)
VALUES (@user_id, @kind, @title, @body, @link, @now);

-- Письмо на любой адрес (например рекомендателю, у которого нет аккаунта).
-- name: EnqueueMail :exec
INSERT INTO outbox (to_email, subject, body, created_at, next_attempt_at)
VALUES (@to_email, @subject, @body, @now, @now);

-- Письмо на почту аккаунта: адрес берётся из таблицы, чужой адрес подставить нельзя.
-- name: EnqueueMailToUser :exec
INSERT INTO outbox (to_email, subject, body, created_at, next_attempt_at)
SELECT u.email, @subject, @body, @now, @now FROM users u WHERE u.id = @user_id;

-- name: ListNotifications :many
SELECT id, kind, title, body, link, created_at, read_at
FROM notifications
WHERE user_id = @user_id AND (NOT @unread_only::boolean OR read_at IS NULL)
ORDER BY created_at DESC, id
LIMIT @lim OFFSET @off;

-- name: CountNotifications :one
SELECT count(*)::bigint AS total, (count(*) FILTER (WHERE read_at IS NULL))::bigint AS unread
FROM notifications WHERE user_id = $1;

-- name: CountUnreadNotifications :one
SELECT count(*)::bigint FROM notifications WHERE user_id = $1 AND read_at IS NULL;

-- Отметить прочитанным можно только своё; чужое или несуществующее — 0 строк.
-- name: NotificationExists :one
SELECT EXISTS (SELECT 1 FROM notifications WHERE id = $1 AND user_id = $2);

-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = @now::timestamptz WHERE id = @id AND user_id = @user_id AND read_at IS NULL;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = @now::timestamptz WHERE user_id = @user_id AND read_at IS NULL;

-- Берём письма «в аренду»: пока аренда не истекла, другой отправитель их не возьмёт. Попытка засчитывается сразу.
-- name: ClaimOutbox :many
UPDATE outbox SET next_attempt_at = @lease_until, attempts = attempts + 1
WHERE id IN (
    SELECT o.id FROM outbox o
    WHERE o.sent_at IS NULL AND o.failed_at IS NULL AND o.next_attempt_at <= @now
    ORDER BY o.next_attempt_at, o.id
    LIMIT @batch
    FOR UPDATE SKIP LOCKED
)
RETURNING id, to_email, subject, body, attempts;

-- name: MarkOutboxSent :exec
UPDATE outbox SET sent_at = @now::timestamptz, last_error = '' WHERE id = @id;

-- name: MarkOutboxRetry :exec
UPDATE outbox SET next_attempt_at = @next_attempt_at, last_error = @last_error WHERE id = @id;

-- name: MarkOutboxFailed :exec
UPDATE outbox SET failed_at = @now::timestamptz, last_error = @last_error WHERE id = @id;

-- Старые отправленные и окончательно неудавшиеся письма больше не нужны.
-- name: DeleteOldOutbox :exec
DELETE FROM outbox WHERE (sent_at IS NOT NULL AND sent_at < @sent_before) OR (failed_at IS NOT NULL AND failed_at < @failed_before);

-- name: CountOutbox :one
SELECT
    (count(*) FILTER (WHERE sent_at IS NULL AND failed_at IS NULL))::bigint AS waiting,
    (count(*) FILTER (WHERE sent_at IS NOT NULL))::bigint AS sent,
    (count(*) FILTER (WHERE failed_at IS NOT NULL))::bigint AS failed
FROM outbox;

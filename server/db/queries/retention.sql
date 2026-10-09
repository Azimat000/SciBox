-- Сроки хранения (срез 22, T-19, T-35, D-151, D-152). Каждый запрос удаляет не больше max_rows строк: чистка идёт
-- партиями, чтобы не держать блокировки и не раздувать журнал транзакций. SKIP LOCKED не даёт двум копиям сервера
-- ждать друг друга.

-- Прочитанные уведомления старше срока.
-- name: PurgeReadNotifications :execrows
DELETE FROM notifications WHERE id IN (
    SELECT n.id FROM notifications n
    WHERE n.read_at IS NOT NULL AND n.read_at < sqlc.arg(cutoff)::timestamptz
    ORDER BY n.read_at LIMIT sqlc.arg(max_rows) FOR UPDATE SKIP LOCKED
);

-- Непрочитанные уведомления старше срока.
-- name: PurgeUnreadNotifications :execrows
DELETE FROM notifications WHERE id IN (
    SELECT n.id FROM notifications n
    WHERE n.read_at IS NULL AND n.created_at < sqlc.arg(cutoff)::timestamptz
    ORDER BY n.created_at LIMIT sqlc.arg(max_rows) FOR UPDATE SKIP LOCKED
);

-- Объявляет чистку журнала для триггера audit_log_guard: действует до конца транзакции.
-- name: AnnounceAuditPurge :exec
SELECT set_config('scibox.audit_purge', 'on', true);

-- Записи журнала старше срока. Триггер дополнительно не даст удалить запись моложе 90 дней.
-- name: PurgeAuditLog :execrows
DELETE FROM audit_log WHERE id IN (
    SELECT a.id FROM audit_log a
    WHERE a.at < sqlc.arg(cutoff)::timestamptz
    ORDER BY a.at LIMIT sqlc.arg(max_rows) FOR UPDATE SKIP LOCKED
);

-- Адреса людей в записях журнала старше срока: запись остаётся, адрес стирается (единственное разрешённое изменение).
-- name: ScrubOldAuditAddresses :execrows
UPDATE audit_log SET ip = NULL WHERE id IN (
    SELECT a.id FROM audit_log a
    WHERE a.ip IS NOT NULL AND a.at < sqlc.arg(cutoff)::timestamptz
    ORDER BY a.at LIMIT sqlc.arg(max_rows) FOR UPDATE SKIP LOCKED
);

-- Регистрации, которые так и не подтвердили почту: последняя активность старше срока и нет ни одной живой ссылки.
-- Войти в такой аккаунт нельзя, поэтому у него нет ничего, кроме почты, имени, хеша пароля и согласия.
-- name: PurgeUnconfirmedUsers :many
DELETE FROM users WHERE id IN (
    SELECT u.id FROM users u
    WHERE u.email_confirmed_at IS NULL AND u.deleted_at IS NULL AND u.updated_at < sqlc.arg(cutoff)::timestamptz
      AND NOT EXISTS (SELECT 1 FROM auth_tokens t WHERE t.user_id = u.id AND t.expires_at > sqlc.arg(now)::timestamptz)
    ORDER BY u.updated_at LIMIT sqlc.arg(max_rows) FOR UPDATE OF u SKIP LOCKED
)
RETURNING email::text AS email;

-- Следы удалённой регистрации: письма на её почту (в них ссылка) и счётчики попыток по ней.
-- name: PurgeOutboxTo :execrows
DELETE FROM outbox WHERE lower(to_email) = lower(sqlc.arg(email)::text);

-- name: PurgeRateEventsFor :execrows
DELETE FROM rate_events WHERE key = lower(sqlc.arg(email)::text);

-- Чистку каждого вида в каждый момент ведёт одна копия сервера: ключ блокировки действует до конца транзакции.
-- name: TryRetentionLock :one
SELECT pg_try_advisory_xact_lock(sqlc.arg(key)::bigint) AS acquired;

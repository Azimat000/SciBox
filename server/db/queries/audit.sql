-- Журнал действий (срез 21, T-17). Таблица только для дописывания: запросов на изменение и удаление записей нет,
-- кроме стирания адреса при удалении аккаунта.

-- Время не указано — берётся время транзакции. Подробности собирает пакет internal/audit.
-- name: InsertAuditEvent :exec
INSERT INTO audit_log (at, org_id, actor_id, subject_id, action, target_kind, target_id, target_label, details, ip)
VALUES (
    COALESCE(sqlc.narg(at)::timestamptz, now()), sqlc.narg(org_id), sqlc.narg(actor_id), sqlc.narg(subject_id), sqlc.arg(action),
    sqlc.arg(target_kind), sqlc.narg(target_id), sqlc.arg(target_label), sqlc.arg(details)::jsonb, sqlc.narg(ip)
);

-- Журнал организации, от новых к старым. before — номер записи, с которой продолжать (пусто — с самых новых).
-- name: ListOrgAudit :many
-- subject_masked: приглашённый человек с анонимным профилем, имя которого не открыто этой организации (срез 15, D-204):
-- пакет audit заменяет его имя и номер аккаунта.
SELECT a.id, a.at, a.action, a.target_kind, a.target_id, a.target_label, a.details,
       a.actor_id, actor.display_name AS actor_name,
       a.subject_id, subject.display_name AS subject_name, m.masked AS subject_masked
FROM audit_log a
LEFT JOIN users actor ON actor.id = a.actor_id
LEFT JOIN users subject ON subject.id = a.subject_id
CROSS JOIN LATERAL (
    SELECT COALESCE(a.action IN ('offer.sent', 'offer.canceled') AND EXISTS (
        SELECT 1 FROM profiles sp
        WHERE sp.user_id = a.subject_id AND sp.visibility = 'anonymous'
          AND NOT EXISTS (SELECT 1 FROM profile_reveals rv WHERE rv.profile_id = sp.id AND rv.org_id = a.org_id)), false)::bool AS masked
) m
WHERE a.org_id = sqlc.arg(org_id) AND (sqlc.narg(before)::bigint IS NULL OR a.id < sqlc.narg(before))
ORDER BY a.id DESC
LIMIT sqlc.arg(max_rows);

-- Адрес человека стирается из записей, когда стирается его аккаунт. Остальное в записях не меняется (триггер не даст).
-- name: ScrubAuditIP :execrows
UPDATE audit_log SET ip = NULL WHERE actor_id = $1 AND ip IS NOT NULL;

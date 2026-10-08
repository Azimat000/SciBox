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
SELECT a.id, a.at, a.action, a.target_kind, a.target_id, a.target_label, a.details,
       a.actor_id, actor.display_name AS actor_name, a.subject_id, subject.display_name AS subject_name
FROM audit_log a
LEFT JOIN users actor ON actor.id = a.actor_id
LEFT JOIN users subject ON subject.id = a.subject_id
WHERE a.org_id = sqlc.arg(org_id) AND (sqlc.narg(before)::bigint IS NULL OR a.id < sqlc.narg(before))
ORDER BY a.id DESC
LIMIT sqlc.arg(max_rows);

-- Адрес человека стирается из записей, когда стирается его аккаунт. Остальное в записях не меняется (триггер не даст).
-- name: ScrubAuditIP :execrows
UPDATE audit_log SET ip = NULL WHERE actor_id = $1 AND ip IS NOT NULL;

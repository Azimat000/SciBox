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
       a.actor_id, actor.display_name AS actor_name, a.subject_id, subject.display_name AS subject_name,
       -- Приглашённый соискатель в записях об отправке и отзыве приглашения: имя анонимного (или скрывшегося от организации)
       -- профиля не показывается, пока человек не открыл его откликом или ответом «Интересно» (D-199, D-200).
       COALESCE(a.action IN ('offer.sent', 'offer.canceled') AND sp.id IS NOT NULL
        AND (sp.visibility = 'anon' OR EXISTS (SELECT 1 FROM profile_hidden_orgs h WHERE h.profile_id = sp.id AND h.org_id = a.org_id)), false)::boolean AS subject_concealed,
       COALESCE(a.action IN ('offer.sent', 'offer.canceled') AND (
            EXISTS (SELECT 1 FROM vacancy_offers o JOIN vacancies ov ON ov.id = o.vacancy_id
                     WHERE o.user_id = a.subject_id AND o.status = 'interested' AND ov.org_id = a.org_id)
         OR EXISTS (SELECT 1 FROM applications ra JOIN vacancies rv ON rv.id = ra.vacancy_id
                     WHERE ra.user_id = a.subject_id AND ra.status <> 'withdrawn' AND rv.org_id = a.org_id)), false)::boolean AS subject_revealed
FROM audit_log a
LEFT JOIN users actor ON actor.id = a.actor_id
LEFT JOIN users subject ON subject.id = a.subject_id
LEFT JOIN profiles sp ON sp.user_id = a.subject_id
WHERE a.org_id = sqlc.arg(org_id) AND (sqlc.narg(before)::bigint IS NULL OR a.id < sqlc.narg(before))
ORDER BY a.id DESC
LIMIT sqlc.arg(max_rows);

-- Адрес человека стирается из записей, когда стирается его аккаунт. Остальное в записях не меняется (триггер не даст).
-- name: ScrubAuditIP :execrows
UPDATE audit_log SET ip = NULL WHERE actor_id = $1 AND ip IS NOT NULL;

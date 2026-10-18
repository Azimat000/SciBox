-- Отклики (срез 8).

-- Всё о вакансии, что нужно для отклика и для проверки прав.
-- name: GetVacancyForApply :one
SELECT id, org_id, unit_id, status, title, deadline, org_name, org_slug FROM vacancy_view WHERE id = $1;

-- name: GetActiveApplicationForVacancy :one
SELECT id, status FROM applications WHERE vacancy_id = $1 AND user_id = $2 AND status <> 'withdrawn';

-- name: InsertApplication :one
INSERT INTO applications (vacancy_id, user_id, cover_letter, contact_email, profile, created_at, updated_at, status_changed_at)
VALUES (@vacancy_id, @user_id, @cover_letter, @contact_email, @profile, @now, @now, @now)
RETURNING id;

-- name: InsertApplicationFile :one
INSERT INTO application_files (application_id, reference_id, kind, name, size, position, storage_key, sha256, scan_status, scanned_at, created_at)
VALUES (@application_id, @reference_id, @kind, @name, @size, @position, @storage_key, @sha256, @scan_status, @scanned_at, @now)
RETURNING id;

-- Карточка отклика с вакансией и именем соискателя.
-- name: GetApplication :one
SELECT a.id, a.vacancy_id, a.user_id, a.status, a.cover_letter, a.contact_email, a.profile, a.decision_note,
       a.created_at, a.updated_at, a.status_changed_at, (a.anonymized_at IS NOT NULL)::bool AS anonymized,
       u.display_name AS applicant_name,
       v.title AS vacancy_title, v.status AS vacancy_status, v.org_id, v.unit_id, v.org_name, v.org_slug, v.deadline
FROM applications a
JOIN users u ON u.id = a.user_id
JOIN vacancy_view v ON v.id = a.vacancy_id
WHERE a.id = $1;

-- «Мои отклики»: новые сверху, с числом рекомендаций.
-- name: ListMyApplications :many
SELECT a.id, a.status, a.created_at, a.status_changed_at,
       v.id AS vacancy_id, v.title AS vacancy_title, v.status AS vacancy_status, v.org_name, v.org_slug, v.deadline, v.city AS vacancy_city,
       (SELECT count(*) FROM reference_requests r WHERE r.application_id = a.id)::bigint AS refs_total,
       (SELECT count(*) FROM reference_requests r WHERE r.application_id = a.id AND r.status = 'received')::bigint AS refs_received
FROM applications a
JOIN vacancy_view v ON v.id = a.vacancy_id
WHERE a.user_id = @user_id
ORDER BY a.created_at DESC, a.id
LIMIT @lim OFFSET @off;

-- name: CountMyApplications :one
SELECT count(*)::bigint FROM applications WHERE user_id = $1;

-- Список файлов без содержимого; scan_status показывает, проверен ли файл (срез 23).
-- name: ListApplicationFiles :many
SELECT id, reference_id, kind, name, size, position, scan_status FROM application_files WHERE application_id = $1 ORDER BY kind, position, created_at;

-- name: GetApplicationFile :one
SELECT id, reference_id, kind, name, size, storage_key, scan_status FROM application_files WHERE id = @id AND application_id = @application_id;

-- Статус меняется только из ожидаемых: две одновременные смены не пройдут обе.
-- name: SetApplicationStatusFrom :execrows
UPDATE applications SET status = @to_status, updated_at = @now, status_changed_at = @now
WHERE id = @id AND status = ANY(@from_statuses::text[]);

-- Сколько откликов человек отправил за окно (ограничение частоты считается по rate_events, это для проверок).
-- name: CountApplicationsForVacancy :one
SELECT count(*)::bigint FROM applications WHERE vacancy_id = $1 AND status <> 'withdrawn';

-- Сотрудники организации (для рассылки уведомлений тем, кто видит отклики).
-- name: ListOrgMemberIDs :many
SELECT user_id FROM org_members WHERE org_id = $1 ORDER BY joined_at, user_id;

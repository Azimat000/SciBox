-- Разбор откликов организацией (срез 9). Приглашений внутри отклика нет с D-256.

-- Решение организации ----------------------------------------------------------------------------------------------

-- Статус меняется только из ожидаемых; записка и автор решения сохраняются вместе со статусом.
-- name: SetApplicationDecision :execrows
UPDATE applications
SET status = @to_status, decision_note = @note, decided_by = @decided_by, updated_at = @now::timestamptz, status_changed_at = @now::timestamptz
WHERE id = @id AND status = ANY(@from_statuses::text[]);

-- Список откликов для организации ----------------------------------------------------------------------------------

-- Отклики на вакансии, которые человек вправе разбирать: организации целиком и отдельные подразделения.
-- Нулевой номер вакансии и пустой статус значат «любые».
-- name: ListCandidates :many
SELECT a.id, a.status, a.created_at, a.status_changed_at, (a.anonymized_at IS NOT NULL)::bool AS anonymized,
       u.display_name AS applicant_name,
       COALESCE(a.profile->>'headline', '')::text AS headline,
       v.id AS vacancy_id, v.title AS vacancy_title, v.status AS vacancy_status, v.org_name, v.org_slug, v.unit_name,
       (SELECT count(*) FROM reference_requests r WHERE r.application_id = a.id)::bigint AS refs_total,
       (SELECT count(*) FROM reference_requests r WHERE r.application_id = a.id AND r.status = 'received')::bigint AS refs_received
FROM applications a
JOIN users u ON u.id = a.user_id
JOIN vacancy_view v ON v.id = a.vacancy_id
WHERE (v.org_id = ANY(@whole_orgs::uuid[]) OR v.unit_id = ANY(@units::uuid[]))
  AND (@vacancy_id::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR v.id = @vacancy_id::uuid)
  AND (@status::text = '' OR a.status = @status::text)
ORDER BY a.created_at DESC, a.id
LIMIT @row_limit OFFSET @row_offset;

-- name: CountCandidatesByStatus :many
SELECT a.status, count(*)::bigint AS total
FROM applications a
JOIN vacancy_view v ON v.id = a.vacancy_id
WHERE (v.org_id = ANY(@whole_orgs::uuid[]) OR v.unit_id = ANY(@units::uuid[]))
  AND (@vacancy_id::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR v.id = @vacancy_id::uuid)
GROUP BY a.status;

-- Вакансии, на которые есть отклики: для фильтра на странице откликов и для чисел в «Моих вакансиях».
-- Отозванные отклики в числа не входят.
-- name: ListCandidateVacancies :many
SELECT v.id, v.title, v.status, v.org_name, v.org_slug,
       (count(a.id) FILTER (WHERE a.status <> 'withdrawn'))::bigint AS total,
       (count(a.id) FILTER (WHERE a.status = 'sent'))::bigint AS new_count
FROM vacancy_view v
JOIN applications a ON a.vacancy_id = v.id
WHERE v.org_id = ANY(@whole_orgs::uuid[]) OR v.unit_id = ANY(@units::uuid[])
GROUP BY v.id, v.title, v.status, v.org_name, v.org_slug
ORDER BY max(a.created_at) DESC, v.id
LIMIT 300;

-- Выгрузка откликов в файл (срез 16, D-205): те же права и отборы, что у списка откликов, но без страниц и без чисел по
-- каждому отклику: одна строка на отклик, предел задаёт сервис (предел + 1, чтобы заметить «не поместилось»).
-- name: ExportCandidates :many
SELECT a.id, a.status, a.created_at, a.status_changed_at, (a.anonymized_at IS NOT NULL)::bool AS anonymized,
       u.display_name AS applicant_name, a.contact_email, a.profile,
       v.title AS vacancy_title, v.org_id, v.org_name, v.unit_name
FROM applications a
JOIN users u ON u.id = a.user_id
JOIN vacancy_view v ON v.id = a.vacancy_id
WHERE (v.org_id = ANY(@whole_orgs::uuid[]) OR v.unit_id = ANY(@units::uuid[]))
  AND (@vacancy_id::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR v.id = @vacancy_id::uuid)
  AND (@status::text = '' OR a.status = @status::text)
ORDER BY a.created_at DESC, a.id
LIMIT @row_limit;

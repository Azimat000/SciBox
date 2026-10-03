-- Разбор откликов организацией (срез 9).

-- Приглашения ------------------------------------------------------------------------------------------------------

-- name: InsertInvitation :one
INSERT INTO application_invitations (application_id, kind, status, message, starts_at, place_kind, place, contact_name, contact_email, contact_phone, created_by, created_at, updated_at)
VALUES (@application_id, @kind, @status, @message, @starts_at, @place_kind, @place, @contact_name, @contact_email, @contact_phone, @created_by, @now, @now)
RETURNING id;

-- name: ListInvitations :many
SELECT * FROM application_invitations WHERE application_id = $1 ORDER BY created_at, id;

-- name: GetInvitation :one
SELECT * FROM application_invitations WHERE id = @id AND application_id = @application_id;

-- name: CountInvitations :one
SELECT count(*)::bigint FROM application_invitations WHERE application_id = $1;

-- Статус приглашения меняется только из ожидаемых: две одновременные смены не пройдут обе.
-- name: SetInvitationStatusFrom :execrows
UPDATE application_invitations SET status = @to_status, updated_at = @now::timestamptz
WHERE id = @id AND status = ANY(@from_statuses::text[]);

-- Ответ соискателя: срабатывает один раз, пока приглашение ждёт ответа.
-- name: AnswerInvitation :execrows
UPDATE application_invitations
SET status = @to_status, answer_at = @answer_at, answer_note = @answer_note, answer_contact = @answer_contact, answer_time = @answer_time,
    answered_at = @now::timestamptz, updated_at = @now::timestamptz
WHERE id = @id AND status = 'pending';

-- Организация принимает время, которое предложил соискатель: оно становится временем собеседования.
-- name: AcceptProposedTime :execrows
UPDATE application_invitations SET status = 'confirmed', starts_at = answer_at, updated_at = @now::timestamptz
WHERE id = @id AND status = 'proposed' AND answer_at > @now::timestamptz;

-- Все открытые приглашения отклика закрываются (отказ, отзыв отклика).
-- name: CancelOpenInvitations :exec
UPDATE application_invitations SET status = 'cancelled', updated_at = @now::timestamptz
WHERE application_id = @application_id AND status IN ('pending', 'proposed', 'confirmed');

-- Новое собеседование заменяет прежние открытые.
-- name: CancelOpenInterviews :exec
UPDATE application_invitations SET status = 'cancelled', updated_at = @now::timestamptz
WHERE application_id = @application_id AND kind = 'interview' AND status IN ('pending', 'proposed', 'confirmed');

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
SELECT a.id, a.status, a.created_at, a.status_changed_at,
       u.display_name AS applicant_name,
       COALESCE(a.profile->>'headline', '')::text AS headline,
       v.id AS vacancy_id, v.title AS vacancy_title, v.status AS vacancy_status, v.org_name, v.org_slug, v.unit_name,
       (SELECT count(*) FROM reference_requests r WHERE r.application_id = a.id)::bigint AS refs_total,
       (SELECT count(*) FROM reference_requests r WHERE r.application_id = a.id AND r.status = 'received')::bigint AS refs_received,
       (SELECT count(*) FROM application_invitations i WHERE i.application_id = a.id AND i.status = 'pending')::bigint AS invites_pending,
       (SELECT count(*) FROM application_invitations i WHERE i.application_id = a.id AND i.status = 'proposed')::bigint AS invites_proposed
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

-- Приглашение переводит отклик в «приглашён» (из «отправлен» и «просмотрен»; уже приглашённому только обновляет строку и тем
-- самым блокирует её до конца транзакции: решение и приглашение одновременно не пройдут). Время статуса не сдвигается,
-- если отклик уже был приглашён.
-- name: MarkApplicationInvited :execrows
UPDATE applications
SET status = 'invited', updated_at = @now::timestamptz,
    status_changed_at = CASE WHEN status = 'invited' THEN status_changed_at ELSE @now::timestamptz END
WHERE id = @id AND status = ANY(@from_statuses::text[]);

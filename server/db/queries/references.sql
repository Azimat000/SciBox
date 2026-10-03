-- Рекомендательные письма (срез 8).

-- name: InsertReferenceRequest :one
INSERT INTO reference_requests (application_id, name, email, relation, token_hash, created_at, expires_at, last_sent_at)
VALUES (@application_id, @name, @email, @relation, @token_hash, @now, @expires_at, @now)
RETURNING id;

-- name: ListReferenceRequests :many
SELECT id, application_id, name, email, relation, status, letter_text, created_at, expires_at, last_sent_at, send_count, answered_at
FROM reference_requests WHERE application_id = $1 ORDER BY created_at, id;

-- name: CountReferenceRequests :one
SELECT count(*)::bigint FROM reference_requests WHERE application_id = $1;

-- Просьба вместе с откликом, вакансией и именем соискателя (для страницы рекомендателя).
-- name: GetReferenceByToken :one
SELECT r.id, r.application_id, r.name, r.email, r.relation, r.status, r.expires_at, r.answered_at,
       a.status AS application_status, a.user_id AS applicant_id,
       u.display_name AS applicant_name,
       v.title AS vacancy_title, v.org_name, v.org_id, v.unit_id, v.id AS vacancy_id
FROM reference_requests r
JOIN applications a ON a.id = r.application_id
JOIN users u ON u.id = a.user_id
JOIN vacancy_view v ON v.id = a.vacancy_id
WHERE r.token_hash = $1;

-- Просьба по номеру и отклику: блокируем строку, чтобы повторная отправка и ответ не наложились.
-- name: LockReferenceRequest :one
SELECT r.id, r.application_id, r.name, r.email, r.relation, r.status, r.expires_at, r.last_sent_at, r.send_count
FROM reference_requests r WHERE r.id = @id AND r.application_id = @application_id FOR UPDATE;

-- Ответ рекомендателя. Срабатывает один раз и только пока ссылка жива.
-- name: AnswerReferenceRequest :execrows
UPDATE reference_requests SET status = @status, letter_text = @letter_text, answered_at = @now::timestamptz
WHERE id = @id AND status = 'pending' AND expires_at > @now;

-- Новая ссылка: прежняя перестаёт работать.
-- name: RenewReferenceRequest :exec
UPDATE reference_requests SET token_hash = @token_hash, expires_at = @expires_at, last_sent_at = @now, send_count = send_count + 1
WHERE id = @id AND status = 'pending';

-- name: DeleteReferenceRequest :execrows
DELETE FROM reference_requests WHERE id = @id AND application_id = @application_id AND status = 'pending';

-- Блокировка отклика на время изменения числа просьб.
-- name: LockApplication :one
SELECT id FROM applications WHERE id = $1 FOR UPDATE;

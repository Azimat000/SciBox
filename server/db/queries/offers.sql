-- Приглашения учёных на вакансии (срез 10).

-- Профиль, на который хотят отправить приглашение: хозяин, режим приватности, имя.
-- name: GetProfileForOffer :one
SELECT p.id, p.user_id, p.visibility, u.display_name
FROM profiles p JOIN users u ON u.id = p.user_id
WHERE p.id = $1;

-- name: InsertOffer :one
INSERT INTO vacancy_offers (vacancy_id, user_id, invited_by, message, created_at, updated_at)
VALUES (@vacancy_id, @user_id, @invited_by, @message, @now, @now)
RETURNING id;

-- Приглашение с вакансией, организацией и учёным; application_id — живой отклик этого человека на эту вакансию.
-- name: GetOffer :one
SELECT o.id, o.vacancy_id, o.user_id, o.status, o.message, o.answer_note, o.answered_at, o.created_at, o.updated_at,
       u.display_name AS scientist_name, p.id AS profile_id,
       v.title AS vacancy_title, v.status AS vacancy_status, v.org_id, v.unit_id, v.org_name, v.org_slug, v.deadline,
       v.city AS vacancy_city, v.unit_name,
       a.id AS application_id
FROM vacancy_offers o
JOIN users u ON u.id = o.user_id
JOIN profiles p ON p.user_id = o.user_id
JOIN vacancy_view v ON v.id = o.vacancy_id
LEFT JOIN applications a ON a.vacancy_id = o.vacancy_id AND a.user_id = o.user_id AND a.status <> 'withdrawn'
WHERE o.id = $1;

-- Ответ учёного: срабатывает один раз, только пока приглашение ждёт ответа.
-- name: AnswerOffer :execrows
UPDATE vacancy_offers SET status = @status, answer_note = @note, answered_at = @now::timestamptz, updated_at = @now
WHERE id = @id AND user_id = @user_id AND status = 'pending';

-- Отзыв организацией: только пока ответа нет.
-- name: CancelOffer :execrows
UPDATE vacancy_offers SET status = 'cancelled', updated_at = @now WHERE id = @id AND status = 'pending';

-- «Мои приглашения» учёного: новые сверху; отозванные ему не показываются.
-- name: ListMyOffers :many
SELECT o.id, o.status, o.message, o.answer_note, o.answered_at, o.created_at,
       v.id AS vacancy_id, v.title AS vacancy_title, v.status AS vacancy_status, v.org_name, v.org_slug, v.deadline, v.city AS vacancy_city,
       a.id AS application_id,
       count(*) OVER () AS total
FROM vacancy_offers o
JOIN vacancy_view v ON v.id = o.vacancy_id
LEFT JOIN applications a ON a.vacancy_id = o.vacancy_id AND a.user_id = o.user_id AND a.status <> 'withdrawn'
WHERE o.user_id = @user_id AND o.status <> 'cancelled'
  AND (@status::text = '' OR o.status = @status::text)
ORDER BY o.created_at DESC, o.id
LIMIT @row_limit OFFSET @row_offset;

-- name: CountMyOffersByStatus :many
SELECT status, count(*) AS total FROM vacancy_offers WHERE user_id = $1 AND status <> 'cancelled' GROUP BY status;

-- «Отправленные приглашения» организации: по вакансиям организаций целиком (whole_orgs) и подразделений (units).
-- name: ListSentOffers :many
SELECT o.id, o.status, o.message, o.answer_note, o.answered_at, o.created_at,
       u.display_name AS scientist_name, p.id AS profile_id,
       v.id AS vacancy_id, v.title AS vacancy_title, v.status AS vacancy_status, v.org_name, v.org_slug, v.unit_name,
       count(*) OVER () AS total
FROM vacancy_offers o
JOIN vacancy_view v ON v.id = o.vacancy_id
JOIN users u ON u.id = o.user_id
JOIN profiles p ON p.user_id = o.user_id
WHERE (v.org_id = ANY(@whole_orgs::uuid[]) OR v.unit_id = ANY(@units::uuid[]))
  AND (sqlc.narg(vacancy_id)::uuid IS NULL OR o.vacancy_id = sqlc.narg(vacancy_id)::uuid)
  AND (@status::text = '' OR o.status = @status::text)
ORDER BY o.created_at DESC, o.id
LIMIT @row_limit OFFSET @row_offset;

-- name: CountSentOffersByStatus :many
SELECT o.status, count(*) AS total
FROM vacancy_offers o JOIN vacancy_view v ON v.id = o.vacancy_id
WHERE (v.org_id = ANY(@whole_orgs::uuid[]) OR v.unit_id = ANY(@units::uuid[]))
  AND (sqlc.narg(vacancy_id)::uuid IS NULL OR o.vacancy_id = sqlc.narg(vacancy_id)::uuid)
GROUP BY o.status;

-- Вакансии, на которые человек может пригласить этого учёного: опубликованные, срок не прошёл, в пределах прав.
-- offered — уже есть живое приглашение, applied — учёный уже откликнулся.
-- name: ListOfferTargets :many
SELECT v.id, v.title, v.org_name, v.org_slug, v.unit_name, v.deadline,
       EXISTS (SELECT 1 FROM vacancy_offers o WHERE o.vacancy_id = v.id AND o.user_id = @user_id AND o.status <> 'cancelled') AS offered,
       EXISTS (SELECT 1 FROM applications a WHERE a.vacancy_id = v.id AND a.user_id = @user_id AND a.status <> 'withdrawn') AS applied
FROM vacancy_view v
WHERE v.status = 'published'
  AND (v.deadline IS NULL OR v.deadline >= @today::date)
  AND (v.org_id = ANY(@whole_orgs::uuid[]) OR v.unit_id = ANY(@units::uuid[]))
ORDER BY lower(v.org_name), v.published_at DESC, v.id;

-- Избранное, сохранённые поиски, подбор, сроки, напоминания (срез 11).

-- ---- избранное ----

-- Блокировка строки человека на время изменения его избранного и поисков: предел числа записей не обойти двумя запросами сразу.
-- name: LockUser :one
SELECT id FROM users WHERE id = $1 FOR UPDATE;

-- Статус вакансии: избранным можно сделать только публичную (опубликованную или закрытую).
-- name: GetVacancyStatusByID :one
SELECT status FROM vacancies WHERE id = $1;

-- name: AddFavorite :execrows
INSERT INTO favorites (user_id, vacancy_id, created_at) VALUES (@user_id, @vacancy_id, @now::timestamptz) ON CONFLICT (user_id, vacancy_id) DO NOTHING;

-- name: RemoveFavorite :execrows
DELETE FROM favorites WHERE user_id = $1 AND vacancy_id = $2;

-- name: CountFavorites :one
SELECT count(*) FROM favorites WHERE user_id = $1;

-- Номера всех избранных (для закладок в списках).
-- name: ListFavoriteIDs :many
SELECT vacancy_id FROM favorites WHERE user_id = $1 ORDER BY created_at DESC, vacancy_id;

-- Список избранного: сначала те, на что ещё можно откликнуться, внутри групп новые добавления сверху.
-- Архивные вакансии не показываются (открыть их нельзя).
-- name: ListFavorites :many
SELECT sqlc.embed(v), f.created_at AS added_at, a.id AS application_id, count(*) OVER () AS total
FROM favorites f
JOIN vacancy_view v ON v.id = f.vacancy_id
LEFT JOIN applications a ON a.vacancy_id = v.id AND a.user_id = f.user_id AND a.status <> 'withdrawn'
WHERE f.user_id = @user_id AND v.status IN ('published', 'closed')
ORDER BY (v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= @today::date)) DESC, f.created_at DESC, v.id
LIMIT @row_limit OFFSET @row_offset;

-- Календарь сроков: избранные опубликованные вакансии, срок которых сегодня или позже.
-- name: ListFavoriteDeadlines :many
SELECT sqlc.embed(v), f.created_at AS added_at, a.id AS application_id
FROM favorites f
JOIN vacancy_view v ON v.id = f.vacancy_id
LEFT JOIN applications a ON a.vacancy_id = v.id AND a.user_id = f.user_id AND a.status <> 'withdrawn'
WHERE f.user_id = @user_id AND v.status = 'published' AND v.deadline >= @today::date
ORDER BY v.deadline, f.created_at, v.id;

-- Избранные без срока подачи (открытые): в календаре о них только число.
-- name: CountFavoritesWithoutDeadline :one
SELECT count(*) FROM favorites f JOIN vacancies v ON v.id = f.vacancy_id
WHERE f.user_id = $1 AND v.status = 'published' AND v.deadline IS NULL;

-- ---- сохранённые поиски ----

-- name: CountSavedSearches :one
SELECT count(*) FROM saved_searches WHERE user_id = $1;

-- name: InsertSavedSearch :one
INSERT INTO saved_searches (user_id, name, query, frequency, checked_at, next_run_at, created_at, updated_at)
VALUES (@user_id, @name, @query, @frequency, @now, @next_run_at, @now, @now)
RETURNING *;

-- name: ListSavedSearches :many
SELECT * FROM saved_searches WHERE user_id = $1 ORDER BY created_at DESC, id;

-- name: GetSavedSearch :one
SELECT * FROM saved_searches WHERE id = @id AND user_id = @user_id;

-- name: UpdateSavedSearch :one
UPDATE saved_searches SET name = @name, frequency = @frequency, next_run_at = @next_run_at, updated_at = @now,
    checked_at = COALESCE(sqlc.narg(checked_at)::timestamptz, checked_at)
WHERE id = @id AND user_id = @user_id
RETURNING *;

-- name: DeleteSavedSearch :execrows
DELETE FROM saved_searches WHERE id = @id AND user_id = @user_id;

-- Поиски, до которых дошла очередь. Берутся без блокировки: перед работой каждый блокируется отдельно.
-- name: ListDueSavedSearches :many
SELECT id FROM saved_searches WHERE frequency <> 'off' AND next_run_at <= @now ORDER BY next_run_at, id LIMIT @batch;

-- Блокировка поиска на время рассылки. Занятый другим запуском пропускается.
-- name: LockSavedSearchForRun :one
SELECT * FROM saved_searches WHERE id = @id AND frequency <> 'off' AND next_run_at <= @now FOR UPDATE SKIP LOCKED;

-- name: AdvanceSavedSearch :exec
UPDATE saved_searches SET
    checked_at = @checked_at, next_run_at = @next_run_at,
    last_sent_at = COALESCE(sqlc.narg(sent_at)::timestamptz, last_sent_at)
WHERE id = @id;

-- ---- напоминания о сроках ----

-- Кандидаты на напоминание (D-105): избранные опубликованные вакансии со сроком в ближайшие 7 дней, на которые человек
-- ещё не откликнулся. Ступень: остался день или меньше — 1, иначе 7. Напоминание этой ступени для этого срока ещё не
-- уходило, и человек добавил вакансию в избранное до начала окна ступени (полночь по Москве за 1 или 7 дней до срока):
-- кто добавил позже, только что сам смотрел на вакансию.
-- name: ListReminderCandidates :many
SELECT f.user_id, v.id AS vacancy_id, v.title, v.org_name,
       COALESCE(v.deadline, @today::date)::date AS deadline, -- срок есть всегда: условие ниже; COALESCE нужен только типам
       (v.deadline - @today::date)::int AS days_left,
       (CASE WHEN v.deadline - @today::date <= 1 THEN 1 ELSE 7 END)::smallint AS stage
FROM favorites f
JOIN vacancy_view v ON v.id = f.vacancy_id
WHERE v.status = 'published' AND v.deadline >= @today::date AND v.deadline <= @horizon::date
  AND NOT EXISTS (SELECT 1 FROM applications a WHERE a.vacancy_id = v.id AND a.user_id = f.user_id AND a.status <> 'withdrawn')
  AND NOT EXISTS (
        SELECT 1 FROM deadline_reminders r
        WHERE r.user_id = f.user_id AND r.vacancy_id = v.id AND r.deadline = v.deadline
          AND r.stage = (CASE WHEN v.deadline - @today::date <= 1 THEN 1 ELSE 7 END))
  AND f.created_at < ((v.deadline - (CASE WHEN v.deadline - @today::date <= 1 THEN 1 ELSE 7 END)::int)::timestamp AT TIME ZONE 'Europe/Moscow')
ORDER BY v.deadline, f.created_at, f.user_id
LIMIT @batch;

-- name: InsertReminder :execrows
INSERT INTO deadline_reminders (user_id, vacancy_id, stage, deadline, sent_at)
VALUES (@user_id, @vacancy_id, @stage, @deadline, @now)
ON CONFLICT DO NOTHING;

-- ---- подбор «подходящие вам» ----

-- Профиль для подбора: степень, звание, регион.
-- name: GetMatchProfile :one
SELECT id, degree, academic_title, region_code FROM profiles WHERE user_id = $1;

-- Коды специальностей профиля.
-- name: ListProfileSpecialtyCodes :many
SELECT specialty_code FROM profile_specialties WHERE profile_id = $1;

-- Кандидаты в подборку: опубликованные, срок не прошёл, хотя бы одна специальность из тех же групп ВАК, нет живого
-- отклика человека, не вакансии организаций, где человек сам разбирает отклики. Точные баллы считает код (matching.Score).
-- name: ListMatchCandidates :many
SELECT * FROM vacancy_view v
WHERE v.status = 'published'
  AND (v.deadline IS NULL OR v.deadline >= @today::date)
  AND EXISTS (
        SELECT 1 FROM vacancy_specialties vs
        WHERE vs.vacancy_id = v.id AND split_part(vs.specialty_code, '.', 1) || '.' || split_part(vs.specialty_code, '.', 2) = ANY(@groups::text[]))
  AND NOT EXISTS (SELECT 1 FROM applications a WHERE a.vacancy_id = v.id AND a.user_id = @user_id AND a.status <> 'withdrawn')
  AND NOT (v.org_id = ANY(@own_orgs::uuid[]) OR (v.unit_id IS NOT NULL AND v.unit_id = ANY(@own_units::uuid[])))
ORDER BY v.published_at DESC, v.id
LIMIT @batch;

-- Специальности вакансий пачкой (коды без названий).
-- name: ListSpecialtyCodesOfVacancies :many
SELECT vacancy_id, specialty_code FROM vacancy_specialties WHERE vacancy_id = ANY(@ids::uuid[]);

-- ---- настройки писем ----

-- name: GetNotificationSettings :one
SELECT email_new_vacancies, email_deadlines FROM notification_settings WHERE user_id = $1;

-- name: UpsertNotificationSettings :exec
INSERT INTO notification_settings (user_id, email_new_vacancies, email_deadlines, updated_at)
VALUES (@user_id, @email_new_vacancies, @email_deadlines, @now)
ON CONFLICT (user_id) DO UPDATE SET
    email_new_vacancies = EXCLUDED.email_new_vacancies, email_deadlines = EXCLUDED.email_deadlines, updated_at = EXCLUDED.updated_at;

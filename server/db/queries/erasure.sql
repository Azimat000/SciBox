-- Удаление аккаунта (срез 20, D-142, D-143). Все запросы вызывает пакет internal/erasure одной транзакцией.

-- Организации, где человек единственный владелец, и сколько там других сотрудников.
-- other_members > 0: удалять нельзя, пока он не назначит другого владельца;
-- other_members = 0: организация останется без владельца, её опубликованные вакансии закроются.
-- name: SoleOwnedOrganizations :many
SELECT o.id, o.slug, o.name,
       (SELECT count(*) FROM org_members x WHERE x.org_id = o.id AND x.user_id <> sqlc.arg(user_id))::int AS other_members
FROM org_members m
JOIN organizations o ON o.id = m.org_id
WHERE m.user_id = sqlc.arg(user_id) AND m.role = 'owner'
  AND NOT EXISTS (SELECT 1 FROM org_members y WHERE y.org_id = o.id AND y.role = 'owner' AND y.user_id <> sqlc.arg(user_id))
ORDER BY o.name, o.id;

-- Организации, где человек владелец, под блокировкой строки (по порядку номеров, чтобы два стирания не ждали друг друга).
-- Приём приглашения блокирует ту же строку: сотрудник не войдёт в организацию, пока решается, остаётся ли у неё владелец (T-34).
-- name: LockOrganizationsOwnedBy :many
SELECT o.id FROM organizations o
WHERE o.id IN (SELECT m.org_id FROM org_members m WHERE m.user_id = $1 AND m.role = 'owner')
ORDER BY o.id
FOR UPDATE OF o;

-- Одна организация под блокировкой (приём приглашения).
-- name: LockOrganization :one
SELECT id FROM organizations WHERE id = $1 FOR UPDATE;

-- Строка аккаунта человека под блокировкой: удаление, отмена и вход по ссылкам не идут одновременно.
-- name: LockUserRow :one
SELECT * FROM users WHERE id = $1 FOR UPDATE;

-- Скрыть профиль на срок отмены, запомнив прежний режим.
-- name: HideProfileForDeletion :exec
UPDATE profiles SET visibility_before_deletion = COALESCE(visibility_before_deletion, visibility), visibility = 'hidden', updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id);

-- Вернуть прежний режим, если человек передумал. Если за срок он сам выбрал другой режим, выбор остаётся.
-- name: RestoreProfileAfterDeletion :exec
UPDATE profiles SET
    visibility = CASE WHEN visibility = 'hidden' AND visibility_before_deletion IS NOT NULL THEN visibility_before_deletion ELSE visibility END,
    visibility_before_deletion = NULL,
    updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id);

-- Закрыть опубликованные вакансии организации, которая остаётся без сотрудников.
-- name: CloseOrphanVacancies :exec
UPDATE vacancies SET status = 'closed', closed_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
WHERE org_id = sqlc.arg(org_id) AND status = 'published';

-- name: EraseMemberships :exec
DELETE FROM org_members WHERE user_id = $1;

-- name: EraseUnitHeads :exec
UPDATE units SET head_user_id = NULL, updated_at = sqlc.arg(now)::timestamptz WHERE head_user_id = sqlc.arg(user_id);

-- Приглашения в организации на почту человека: адрес — его персональные данные.
-- name: EraseOrgInvitationsTo :exec
DELETE FROM org_invitations WHERE email = sqlc.arg(email)::citext;

-- Уведомления сотрудников организаций об откликах человека: в тексте его имя.
-- name: EraseCandidateNotifications :exec
DELETE FROM notifications
WHERE link IN (SELECT '/candidates/' || a.id::text FROM applications a WHERE a.user_id = $1);

-- Уведомления сотрудников об ответах человека на приглашения на вакансии: в тексте его имя, ссылка несёт номер приглашения.
-- name: EraseOfferAnswerNotifications :exec
DELETE FROM notifications
WHERE link IN (SELECT '/sent-offers?offer=' || o.id::text FROM vacancy_offers o WHERE o.user_id = $1);

-- Отклики: файлы, просьбы о рекомендациях (вместе с письмами рекомендателей) и приглашения стираются целиком.
-- name: EraseApplicationFiles :exec
DELETE FROM application_files WHERE application_id IN (SELECT id FROM applications WHERE user_id = $1);

-- История писем рекомендателям человека (почта третьего лица). Ожидающие письма не трогаем: они могут быть и по просьбе
-- другого соискателя к тому же рекомендателю. Вызывать до EraseReferenceRequests.
-- name: EraseRefereeMailHistory :exec
DELETE FROM outbox
WHERE (sent_at IS NOT NULL OR failed_at IS NOT NULL)
  AND lower(to_email) IN (
      SELECT lower(r.email::text) FROM reference_requests r JOIN applications a ON a.id = r.application_id WHERE a.user_id = $1
  );

-- name: EraseReferenceRequests :exec
DELETE FROM reference_requests WHERE application_id IN (SELECT id FROM applications WHERE user_id = $1);

-- name: EraseApplicationInvitations :exec
DELETE FROM application_invitations WHERE application_id IN (SELECT id FROM applications WHERE user_id = $1);

-- Обезличить отклики: остаются вакансия, даты и статус. Нерешённые становятся отозванными:
-- разбирать их больше некому, а окончательный статус не даёт организации писать стёртому аккаунту.
-- name: AnonymizeApplications :exec
UPDATE applications SET
    cover_letter = '', contact_email = '', profile = '{}'::jsonb, decision_note = '',
    status = CASE WHEN status IN ('sent', 'viewed', 'invited') THEN 'withdrawn' ELSE status END,
    status_changed_at = CASE WHEN status IN ('sent', 'viewed', 'invited') THEN sqlc.arg(now)::timestamptz ELSE status_changed_at END,
    anonymized_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
WHERE user_id = sqlc.arg(user_id);

-- name: EraseOffersTo :exec
DELETE FROM vacancy_offers WHERE user_id = $1;

-- Профиль вместе с записями разделов и специальностями (ON DELETE CASCADE).
-- name: EraseProfile :exec
DELETE FROM profiles WHERE user_id = $1;

-- name: EraseFavorites :exec
DELETE FROM favorites WHERE user_id = $1;

-- name: EraseSavedSearches :exec
DELETE FROM saved_searches WHERE user_id = $1;

-- name: EraseDeadlineReminders :exec
DELETE FROM deadline_reminders WHERE user_id = $1;

-- name: EraseNotificationSettings :exec
DELETE FROM notification_settings WHERE user_id = $1;

-- name: EraseNotifications :exec
DELETE FROM notifications WHERE user_id = $1;

-- name: EraseSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: EraseAuthTokens :exec
DELETE FROM auth_tokens WHERE user_id = $1;

-- name: EraseKnownDevices :exec
DELETE FROM known_devices WHERE user_id = $1;

-- Письма в очереди на почту человека (и отправленные, и нет): в их тексте его данные.
-- name: EraseOutboxTo :exec
DELETE FROM outbox WHERE lower(to_email) = lower(sqlc.arg(email)::text);

-- Счётчики попыток по почте и номеру аккаунта.
-- name: EraseRateEvents :exec
DELETE FROM rate_events WHERE key = lower(sqlc.arg(email)::text) OR key = sqlc.arg(user_key)::text;

-- От аккаунта остаётся строка без почты, имени и пароля. Адрес вида deleted+<номер>@deleted нельзя
-- ввести ни в одну форму (в домене нет точки), поэтому с ним нельзя ни войти, ни зарегистрироваться.
-- name: TombstoneUser :exec
UPDATE users SET
    email = ('deleted+' || id::text || '@deleted')::citext,
    display_name = 'Аккаунт удалён',
    password_hash = '!',
    email_confirmed_at = NULL,
    deletion_due_at = NULL,
    deleted_at = sqlc.arg(now)::timestamptz,
    updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id);

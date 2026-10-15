-- Чат, срез 38 (D-236…D-241): строки о событиях, связи откликов с переписками, блокировка, шаблоны, жалобы, перенос данных.

-- ---- переписка по отклику или приглашению ----

-- События откликов и приглашений находят переписку по ссылке (индексы chats_application_idx и chats_offer_idx).
-- name: FindChatByApplication :one
SELECT id FROM chats WHERE application_id = $1;

-- name: FindChatByOffer :one
SELECT id FROM chats WHERE offer_id = $1;

-- Что показать у отклика и приглашения (поле chat): переписка, её состояние и сколько в ней непрочитанного у смотрящего.
-- Берутся только переписки, на которые указали номера откликов и приглашений; права на сами отклики проверил вызывающий.
-- started — в переписке уже есть сообщения людей, а не только строки о событиях.
-- name: ListChatLinks :many
SELECT c.id, c.application_id, c.offer_id,
       a.status AS application_status, o.status AS offer_status,
       EXISTS (SELECT 1 FROM chat_blocks b WHERE b.user_id = c.user_id AND b.org_id = c.org_id)::boolean AS blocked,
       EXISTS (SELECT 1 FROM chat_messages m WHERE m.chat_id = c.id AND m.kind = 'text')::boolean AS started,
       (SELECT count(*) FROM (SELECT 1 FROM chat_messages x
                               WHERE x.chat_id = c.id AND x.seq > COALESCE(cp.read_seq, 0) AND x.to_side = @side::text
                               LIMIT 100) lim)::int AS unread
FROM chats c
LEFT JOIN applications a ON a.id = c.application_id
LEFT JOIN vacancy_offers o ON o.id = c.offer_id
LEFT JOIN chat_participants cp ON cp.chat_id = c.id AND cp.user_id = @viewer_id
WHERE c.application_id = ANY(@application_ids::uuid[]) OR c.offer_id = ANY(@offer_ids::uuid[]);

-- ---- блокировка организации учёным ----

-- Новая блокировка; ноль строк значит «уже заблокирована».
-- name: InsertChatBlock :execrows
INSERT INTO chat_blocks (user_id, org_id, created_at) VALUES (@user_id, @org_id, @now::timestamptz)
ON CONFLICT (user_id, org_id) DO NOTHING;

-- name: DeleteChatBlock :execrows
DELETE FROM chat_blocks WHERE user_id = $1 AND org_id = $2;

-- name: ListChatBlocks :many
SELECT b.org_id, o.name AS org_name, o.slug AS org_slug, b.created_at
FROM chat_blocks b JOIN organizations o ON o.id = b.org_id
WHERE b.user_id = $1
ORDER BY b.created_at DESC, b.org_id;

-- Заблокировал ли человек организацию: приглашать его она после этого не вправе.
-- name: IsOrgBlockedBy :one
SELECT EXISTS (SELECT 1 FROM chat_blocks WHERE user_id = $1 AND org_id = $2)::boolean;

-- Переписки человека с организацией и состояние их отклика и приглашения: блокировка и снятие дописывают строку в те,
-- что иначе открыты. Номера по возрастанию: так замки берутся в одном порядке.
-- name: ListPairChats :many
SELECT c.id, a.status AS application_status, o.status AS offer_status
FROM chats c
LEFT JOIN applications a ON a.id = c.application_id
LEFT JOIN vacancy_offers o ON o.id = c.offer_id
WHERE c.user_id = @user_id AND c.org_id = @org_id
ORDER BY c.id;

-- ---- шаблоны ответов организации ----

-- name: ListChatTemplates :many
SELECT id, title, body, updated_at FROM chat_templates WHERE org_id = $1 ORDER BY lower(title), id;

-- name: CountChatTemplates :one
SELECT count(*)::int FROM chat_templates WHERE org_id = $1;

-- name: InsertChatTemplate :one
INSERT INTO chat_templates (org_id, title, body, created_by, created_at, updated_at)
VALUES (@org_id, @title, @body, @created_by, @now::timestamptz, @now::timestamptz)
RETURNING id, title, body, updated_at;

-- name: UpdateChatTemplate :one
UPDATE chat_templates SET title = @title, body = @body, updated_at = @now::timestamptz
WHERE id = @id AND org_id = @org_id
RETURNING id, title, body, updated_at;

-- name: DeleteChatTemplate :execrows
DELETE FROM chat_templates WHERE id = $1 AND org_id = $2;

-- ---- жалобы ----

-- name: InsertChatComplaint :one
INSERT INTO chat_complaints (user_id, chat_id, org_id, vacancy_id, reason, note, created_at)
VALUES (@user_id, @chat_id, @org_id, @vacancy_id, @reason, @note, @now::timestamptz)
RETURNING id, created_at;

-- ---- перенос прежних откликов и приглашений ----

-- Пары «вакансия × учёный», у которых есть отклик или приглашение, но нет переписки. Обезличенные отклики (аккаунт стёрт)
-- не берутся. Листание по ключу пары: повторный проход после переноса ничего не находит.
-- name: ListChatBackfillPairs :many
SELECT p.vacancy_id, p.user_id FROM (
    SELECT a.vacancy_id, a.user_id FROM applications a WHERE a.anonymized_at IS NULL
    UNION
    SELECT f.vacancy_id, f.user_id FROM vacancy_offers f
) p
WHERE (p.vacancy_id, p.user_id) > (@after_vacancy::uuid, @after_user::uuid)
  AND NOT EXISTS (SELECT 1 FROM chats c WHERE c.vacancy_id = p.vacancy_id AND c.user_id = p.user_id)
ORDER BY p.vacancy_id, p.user_id
LIMIT @row_limit;

-- name: ListBackfillApplications :many
SELECT id, status, cover_letter, decision_note, created_at, status_changed_at, updated_at
FROM applications
WHERE vacancy_id = @vacancy_id AND user_id = @user_id AND anonymized_at IS NULL
ORDER BY created_at, id;

-- name: GetBackfillOffer :one
SELECT id, status, message, answer_note, answered_at, created_at, updated_at
FROM vacancy_offers WHERE vacancy_id = @vacancy_id AND user_id = @user_id;

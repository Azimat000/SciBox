-- Чат (срез 37, D-214, D-229…D-236): переписки учёного и организации по вакансии.

-- Переписка пары «учёный × вакансия»: заводится вместе с откликом или приглашением (срез 38), повтор находит ту же.
-- Если пара уже есть, ссылка на отклик или приглашение дополняется (приглашение, потом отклик в той же переписке).
-- name: OpenChat :one
INSERT INTO chats (vacancy_id, org_id, user_id, application_id, offer_id, created_at, updated_at)
SELECT v.id, v.org_id, @user_id, sqlc.narg(application_id), sqlc.narg(offer_id), @now::timestamptz, @now::timestamptz
FROM vacancies v WHERE v.id = @vacancy_id
ON CONFLICT (vacancy_id, user_id) DO UPDATE SET
    -- Новое приглашение после отозванного отклика становится главным в переписке: переписка следует за ним, а не за
    -- отзывом (иначе она осталась бы закрытой, пока приглашение ждёт ответа).
    application_id = CASE
        WHEN EXCLUDED.application_id IS NOT NULL THEN EXCLUDED.application_id
        WHEN EXCLUDED.offer_id IS NOT NULL AND EXISTS (SELECT 1 FROM applications wa WHERE wa.id = chats.application_id AND wa.status = 'withdrawn') THEN NULL
        ELSE chats.application_id END,
    offer_id       = COALESCE(EXCLUDED.offer_id, chats.offer_id)
RETURNING id;

-- Переписка со всем, что нужно шапке и проверке прав: вакансия, организация, состояние отклика и приглашения, учёный
-- (имя и признаки анонимности по правилам предложений организации), личные отметки смотрящего.
-- hidden_from: учёный скрылся от организации вакансии (D-200); revealed: он открыл ей имя откликом или ответом
-- «Интересно» (D-199). Решение о показе имени принимает privacy.ShowsName, здесь только факты.
-- name: GetChat :one
SELECT c.id, c.vacancy_id, c.user_id, c.application_id, c.offer_id,
       c.last_seq, c.last_for_scientist, c.last_for_org, c.scientist_read, c.org_read, c.updated_at,
       v.org_id, v.unit_id, v.title AS vacancy_title, v.status AS vacancy_status, v.deadline,
       o.name AS org_name, o.slug AS org_slug, un.name AS unit_name,
       a.status AS application_status, ofr.status AS offer_status,
       u.display_name AS scientist_name, p.id AS profile_id, p.visibility,
       rel.hidden_from, rel.revealed, rel.blocked,
       COALESCE(cp.read_seq, 0)::int AS read_seq, COALESCE(cp.archived, false)::boolean AS archived
FROM chats c
JOIN vacancies v ON v.id = c.vacancy_id
JOIN organizations o ON o.id = v.org_id
LEFT JOIN units un ON un.id = v.unit_id
LEFT JOIN applications a ON a.id = c.application_id
LEFT JOIN vacancy_offers ofr ON ofr.id = c.offer_id
JOIN users u ON u.id = c.user_id
LEFT JOIN profiles p ON p.user_id = c.user_id
LEFT JOIN chat_participants cp ON cp.chat_id = c.id AND cp.user_id = @viewer_id
CROSS JOIN LATERAL (
    SELECT EXISTS (SELECT 1 FROM profile_hidden_orgs h WHERE h.profile_id = p.id AND h.org_id = v.org_id)::boolean AS hidden_from,
           (EXISTS (SELECT 1 FROM applications ra JOIN vacancies rv ON rv.id = ra.vacancy_id
                     WHERE ra.user_id = c.user_id AND ra.status <> 'withdrawn' AND rv.org_id = v.org_id)
            OR EXISTS (SELECT 1 FROM vacancy_offers ro JOIN vacancies rv ON rv.id = ro.vacancy_id
                        WHERE ro.user_id = c.user_id AND ro.status = 'interested' AND rv.org_id = v.org_id))::boolean AS revealed,
           EXISTS (SELECT 1 FROM chat_blocks b WHERE b.user_id = c.user_id AND b.org_id = v.org_id)::boolean AS blocked
) rel
WHERE c.id = @id;

-- Замок на отклик и приглашение, которыми определяется, открыта ли переписка. Берётся до замка переписки: так же
-- действуют решения организации (отклик, потом строка в переписке), порядок один, ожидание в круг не замыкается (T-106).
-- name: LockApplicationStatus :one
SELECT status FROM applications WHERE id = $1 FOR SHARE;

-- name: LockOfferStatus :one
SELECT status FROM vacancy_offers WHERE id = $1 FOR SHARE;

-- Замок строки переписки: отправка в одну переписку идёт по очереди, номера не повторяются.
-- name: LockChat :one
SELECT application_id, offer_id, last_seq FROM chats WHERE id = $1 FOR UPDATE;

-- Заблокирована ли организация учёным этой переписки. Читается отдельным запросом после замка переписки: блокировка и
-- закрытие берут тот же замок, и запрос видит всё, что зафиксировано до его снятия (T-105, T-106).
-- name: IsChatBlocked :one
SELECT EXISTS (SELECT 1 FROM chat_blocks b WHERE b.user_id = c.user_id AND b.org_id = c.org_id)::boolean
FROM chats c WHERE c.id = $1;

-- Следующий номер: счётчики растут вместе с сообщением. to_side — кому оно адресовано.
-- name: BumpChat :one
UPDATE chats SET
    last_seq           = last_seq + 1,
    last_for_scientist = CASE WHEN @to_side::text = 'scientist' THEN last_seq + 1 ELSE last_for_scientist END,
    last_for_org       = CASE WHEN @to_side::text = 'org' THEN last_seq + 1 ELSE last_for_org END,
    updated_at         = @now::timestamptz
WHERE id = @id
RETURNING last_seq;

-- name: InsertChatMessage :exec
INSERT INTO chat_messages (chat_id, seq, kind, author_side, author_user_id, to_side, body, event_type, event_data, client_id, created_at)
VALUES (@chat_id, @seq, @kind, sqlc.narg(author_side), sqlc.narg(author_user_id), @to_side, @body, @event_type, @event_data, sqlc.narg(client_id), @created_at::timestamptz);

-- Новое сообщение возвращает переписку из архива у всех участников.
-- name: UnarchiveChat :exec
UPDATE chat_participants SET archived = false, updated_at = @now::timestamptz WHERE chat_id = @chat_id AND archived;

-- Уже отправленное сообщение по номеру, который выдал сайт: повтор отправки отдаёт его, а не создаёт второе.
-- name: FindChatMessageByClient :one
SELECT * FROM chat_messages WHERE chat_id = @chat_id AND author_user_id = @author_user_id AND client_id = @client_id;

-- name: GetChatMessage :one
SELECT * FROM chat_messages WHERE chat_id = @chat_id AND seq = @seq;

-- Лента: последние сообщения или более старые, чем before. Берётся на одно больше, чем просили: «есть ли ещё».
-- name: ListChatMessagesBefore :many
SELECT * FROM chat_messages
WHERE chat_id = @chat_id AND (sqlc.narg(before)::int IS NULL OR seq < sqlc.narg(before)::int)
ORDER BY seq DESC
LIMIT @row_limit;

-- Более новые, чем after (после переподключения).
-- name: ListChatMessagesAfter :many
SELECT * FROM chat_messages
WHERE chat_id = @chat_id AND seq > @after::int
ORDER BY seq ASC
LIMIT @row_limit;

-- Имена авторов ленты одним запросом. member: человек сейчас состоит в организации этой переписки.
-- name: ListChatAuthors :many
SELECT u.id, u.display_name, (om.user_id IS NOT NULL)::boolean AS member
FROM users u
LEFT JOIN org_members om ON om.org_id = @org_id AND om.user_id = u.id
WHERE u.id = ANY(@ids::uuid[]);

-- name: EditChatMessage :exec
UPDATE chat_messages SET body = @body, edited_at = @now::timestamptz WHERE chat_id = @chat_id AND seq = @seq;

-- Удаление стирает текст: на месте сообщения остаётся пометка «Сообщение удалено».
-- name: DeleteChatMessage :exec
UPDATE chat_messages SET body = '', deleted_at = @now::timestamptz WHERE chat_id = @chat_id AND seq = @seq;

-- Прочитано до номера включительно: у человека и у его стороны. Отметка только растёт и не уходит дальше последнего.
-- Возвращает отметку человека после записи: по ней видно, выросла ли она (срез 39, chat.read).
-- name: MarkChatRead :one
INSERT INTO chat_participants (chat_id, user_id, read_seq, updated_at)
VALUES (@chat_id, @user_id, (SELECT LEAST(@up_to::int, c.last_seq) FROM chats c WHERE c.id = @chat_id), @now::timestamptz)
ON CONFLICT (chat_id, user_id) DO UPDATE SET
    read_seq = GREATEST(chat_participants.read_seq, EXCLUDED.read_seq),
    updated_at = EXCLUDED.updated_at
RETURNING read_seq;

-- name: RaiseSideRead :exec
UPDATE chats SET
    scientist_read = CASE WHEN @side::text = 'scientist' THEN GREATEST(scientist_read, LEAST(@up_to::int, last_seq)) ELSE scientist_read END,
    org_read       = CASE WHEN @side::text = 'org' THEN GREATEST(org_read, LEAST(@up_to::int, last_seq)) ELSE org_read END
WHERE id = @id;

-- Возвращает число изменённых строк: повтор того же действия ничего не меняет и событий не рассылает (срез 40).
-- name: SetChatArchived :execrows
INSERT INTO chat_participants (chat_id, user_id, archived, updated_at)
VALUES (@chat_id, @user_id, @archived, @now::timestamptz)
ON CONFLICT (chat_id, user_id) DO UPDATE SET archived = EXCLUDED.archived, updated_at = EXCLUDED.updated_at
WHERE chat_participants.archived IS DISTINCT FROM EXCLUDED.archived;

-- Список переписок одной стороны, новые сверху, листание по ключу (updated_at, id).
-- Сторона учёного: свои переписки. Сторона организации: переписки по вакансиям в пределах прав, кроме тех, где сам
-- человек учёный. Права считает пакет access через orgs.ScopeOf; сервис вызывает запрос отдельно для организаций
-- целиком (whole_orgs, идёт по индексу chats_org_idx) и для подразделений (units, по вакансиям подразделения) и
-- сливает ответы: так каждая ветка останавливается на первой странице, а не читает все переписки базы. Пустая ветка
-- передаётся как NULL, и планировщик выбрасывает её до выполнения.
-- Сначала выбирается страница (page) только по дешёвым условиям, потом к ней присоединяются подробности: признаки
-- анонимности и непрочитанное считаются для 30 строк, а не для всех переписок.
-- Поиск идёт по названию вакансии и по собеседнику; имя учёного ищется, только если оно организации показывается
-- (privacy.ShowsName). unread — число сообщений, адресованных стороне, после отметки человека (не больше 100).
-- name: ListChats :many
WITH page AS (
    -- Только переписки и личные отметки: остальное присоединяется подзапросами там, где условие задано. Так при пустых
    -- поиске и отборе по подразделению планировщик идёт по индексу chats_org_idx или chats_user_idx в порядке времени и
    -- останавливается на первой странице.
    SELECT c.id
    FROM chats c
    LEFT JOIN chat_participants cp ON cp.chat_id = c.id AND cp.user_id = @viewer_id
    WHERE ((@side::text = 'scientist' AND c.user_id = @viewer_id)
           OR (@side::text = 'org' AND c.user_id <> @viewer_id
               AND ((sqlc.narg(whole_orgs)::uuid[] IS NOT NULL AND c.org_id = ANY(sqlc.narg(whole_orgs)::uuid[]))
                    OR (sqlc.narg(units)::uuid[] IS NOT NULL
                        AND c.vacancy_id IN (SELECT uv.id FROM vacancies uv WHERE uv.unit_id = ANY(sqlc.narg(units)::uuid[]))))))
      AND ((@box::text = 'archive' AND COALESCE(cp.archived, false))
           OR (@box::text = 'all' AND NOT COALESCE(cp.archived, false))
           OR (@box::text = 'unread' AND NOT COALESCE(cp.archived, false)
               AND (CASE WHEN @side::text = 'scientist' THEN c.last_for_scientist ELSE c.last_for_org END) > COALESCE(cp.read_seq, 0)))
      AND (sqlc.narg(vacancy_id)::uuid IS NULL OR c.vacancy_id = sqlc.narg(vacancy_id)::uuid)
      AND (@search::text = ''
           OR (@side::text = 'scientist'
               AND EXISTS (SELECT 1 FROM vacancies sv WHERE sv.id = c.vacancy_id AND sv.title ILIKE '%' || @search::text || '%'))
           OR (@side::text = 'org'
               AND c.vacancy_id IN (SELECT sv.id FROM vacancies sv
                                     WHERE (sv.org_id = ANY(COALESCE(sqlc.narg(whole_orgs)::uuid[], '{}'::uuid[]))
                                            OR sv.unit_id = ANY(COALESCE(sqlc.narg(units)::uuid[], '{}'::uuid[])))
                                       AND sv.title ILIKE '%' || @search::text || '%'))
           OR (@side::text = 'scientist'
               AND EXISTS (SELECT 1 FROM organizations so WHERE so.id = c.org_id AND so.name ILIKE '%' || @search::text || '%'))
           OR (@side::text = 'org'
               AND EXISTS (SELECT 1 FROM users su LEFT JOIN profiles sp ON sp.user_id = su.id
                            WHERE su.id = c.user_id AND su.display_name ILIKE '%' || @search::text || '%'
                              AND (NOT (COALESCE(sp.visibility NOT IN ('orgs', 'public'), false)
                                        OR EXISTS (SELECT 1 FROM profile_hidden_orgs h WHERE h.profile_id = sp.id AND h.org_id = c.org_id))
                                   OR EXISTS (SELECT 1 FROM applications ra JOIN vacancies rv ON rv.id = ra.vacancy_id
                                               WHERE ra.user_id = c.user_id AND ra.status <> 'withdrawn' AND rv.org_id = c.org_id)
                                   OR EXISTS (SELECT 1 FROM vacancy_offers ro JOIN vacancies rv ON rv.id = ro.vacancy_id
                                               WHERE ro.user_id = c.user_id AND ro.status = 'interested' AND rv.org_id = c.org_id)))))
      AND (sqlc.narg(cursor_at)::timestamptz IS NULL OR (c.updated_at, c.id) < (sqlc.narg(cursor_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
    ORDER BY c.updated_at DESC, c.id DESC
    LIMIT @row_limit
)
SELECT c.id, c.vacancy_id, c.user_id, c.application_id, c.offer_id, c.last_seq, c.updated_at,
       v.org_id, v.unit_id, v.title AS vacancy_title, v.status AS vacancy_status, v.deadline,
       o.name AS org_name, o.slug AS org_slug, un.name AS unit_name,
       a.status AS application_status, ofr.status AS offer_status,
       u.display_name AS scientist_name, p.id AS profile_id, p.visibility,
       rel.hidden_from, rel.revealed, rel.blocked,
       COALESCE(cp.read_seq, 0)::int AS read_seq, COALESCE(cp.archived, false)::boolean AS archived,
       (SELECT count(*) FROM (SELECT 1 FROM chat_messages x
                               WHERE x.chat_id = c.id AND x.seq > COALESCE(cp.read_seq, 0) AND x.to_side = @side::text
                               LIMIT 100) lim)::int AS unread,
       lm.kind AS last_kind, lm.author_side AS last_author_side, lm.author_user_id AS last_author_id, lm.body AS last_body,
       lm.event_type AS last_event_type, lm.deleted_at AS last_deleted_at, lm.created_at AS last_created_at,
       la.display_name AS last_author_name, (lom.user_id IS NOT NULL)::boolean AS last_author_member
FROM page
JOIN chats c ON c.id = page.id
JOIN vacancies v ON v.id = c.vacancy_id
JOIN organizations o ON o.id = c.org_id
LEFT JOIN units un ON un.id = v.unit_id
LEFT JOIN applications a ON a.id = c.application_id
LEFT JOIN vacancy_offers ofr ON ofr.id = c.offer_id
JOIN users u ON u.id = c.user_id
LEFT JOIN profiles p ON p.user_id = c.user_id
LEFT JOIN chat_participants cp ON cp.chat_id = c.id AND cp.user_id = @viewer_id
LEFT JOIN chat_messages lm ON lm.chat_id = c.id AND lm.seq = c.last_seq
LEFT JOIN users la ON la.id = lm.author_user_id
LEFT JOIN org_members lom ON lom.org_id = c.org_id AND lom.user_id = lm.author_user_id
CROSS JOIN LATERAL (
    SELECT EXISTS (SELECT 1 FROM profile_hidden_orgs h WHERE h.profile_id = p.id AND h.org_id = c.org_id)::boolean AS hidden_from,
           (EXISTS (SELECT 1 FROM applications ra JOIN vacancies rv ON rv.id = ra.vacancy_id
                     WHERE ra.user_id = c.user_id AND ra.status <> 'withdrawn' AND rv.org_id = c.org_id)
            OR EXISTS (SELECT 1 FROM vacancy_offers ro JOIN vacancies rv ON rv.id = ro.vacancy_id
                        WHERE ro.user_id = c.user_id AND ro.status = 'interested' AND rv.org_id = c.org_id))::boolean AS revealed,
           EXISTS (SELECT 1 FROM chat_blocks b WHERE b.user_id = c.user_id AND b.org_id = c.org_id)::boolean AS blocked
) rel
ORDER BY c.updated_at DESC, c.id DESC;

-- Числа вкладок стороны (без поиска и отбора): с непрочитанным вне архива и в архиве. Те же ветки, что у списка: сервис
-- вызывает запрос отдельно для организаций целиком и для подразделений и складывает ответы.
-- name: CountChatBoxes :one
SELECT (count(*) FILTER (WHERE NOT COALESCE(cp.archived, false)
                           AND (CASE WHEN @side::text = 'scientist' THEN c.last_for_scientist ELSE c.last_for_org END) > COALESCE(cp.read_seq, 0)))::int AS unread,
       (count(*) FILTER (WHERE COALESCE(cp.archived, false)))::int AS archive
FROM chats c
LEFT JOIN chat_participants cp ON cp.chat_id = c.id AND cp.user_id = @viewer_id
WHERE (@side::text = 'scientist' AND c.user_id = @viewer_id)
   OR (@side::text = 'org' AND c.user_id <> @viewer_id
       AND ((sqlc.narg(whole_orgs)::uuid[] IS NOT NULL AND c.org_id = ANY(sqlc.narg(whole_orgs)::uuid[]))
            OR (sqlc.narg(units)::uuid[] IS NOT NULL
                AND c.vacancy_id IN (SELECT uv.id FROM vacancies uv WHERE uv.unit_id = ANY(sqlc.narg(units)::uuid[])))));

-- Вакансии, по которым у организации есть переписки, для отбора в списке.
-- name: ListChatVacancies :many
SELECT v.id, v.title, o.name AS org_name, COALESCE(un.name, '')::text AS unit_name, count(*)::int AS chats
FROM chats c
JOIN vacancies v ON v.id = c.vacancy_id
JOIN organizations o ON o.id = c.org_id
LEFT JOIN units un ON un.id = v.unit_id
WHERE c.user_id <> @viewer_id
  AND ((sqlc.narg(whole_orgs)::uuid[] IS NOT NULL AND c.org_id = ANY(sqlc.narg(whole_orgs)::uuid[]))
       OR (sqlc.narg(units)::uuid[] IS NOT NULL AND v.unit_id = ANY(sqlc.narg(units)::uuid[])))
GROUP BY v.id, v.title, o.name, un.name
ORDER BY lower(v.title), v.id;

-- Сколько текстовых сообщений подряд написала эта сторона после последнего текста другой стороны (срез 40, T-99).
-- Удалённые считаются: удалить и написать снова не обнуляет счёт. Строки о событиях ответом не считаются.
-- name: CountUnansweredChatText :one
SELECT count(*)::bigint AS n
FROM chat_messages m
WHERE m.chat_id = @chat_id AND m.kind = 'text' AND m.author_side = @side::text
  AND m.seq > COALESCE((
      SELECT o.seq FROM chat_messages o
      WHERE o.chat_id = @chat_id AND o.kind = 'text' AND o.author_side <> @side::text
      ORDER BY o.seq DESC LIMIT 1), 0);

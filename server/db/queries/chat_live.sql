-- Живое обновление чата и письма о непрочитанном (срез 39, D-242…D-245).

-- Событие пишется в журнал и тут же объявляется: NOTIFY доходит только после фиксации транзакции, откат убирает и
-- строку, и объявление. Организация и подразделение берутся из переписки и вакансии. Нагрузка уведомления — вся строка
-- журнала (около 300 байт при пределе 8000): копии сервера отбирают получателей, не читая базу.
-- name: PublishChatEvent :one
WITH ev AS (
    INSERT INTO chat_events (kind, chat_id, user_id, org_id, unit_id, seq, actor_id, side, up_to, created_at)
    SELECT @kind::text, c.id, c.user_id, c.org_id, v.unit_id, @seq::int, sqlc.narg(actor_id), sqlc.narg(side), @up_to::int, @now::timestamptz
    FROM chats c JOIN vacancies v ON v.id = c.vacancy_id
    WHERE c.id = @chat_id
    RETURNING *
)
SELECT ev.id::bigint AS id FROM ev, LATERAL (SELECT pg_notify('chat_events', row_to_json(ev)::text)) n;

-- «Печатает» в базу не пишется: объявление несёт всё нужное само.
-- name: NotifyChatTyping :exec
SELECT pg_notify('chat_typing', @payload::text);

-- name: ListChatEventsByID :many
SELECT * FROM chat_events WHERE id = ANY(@ids::bigint[]) ORDER BY id;

-- Страховочный опрос: события новее последнего обработанного.
-- name: ListChatEventsAfter :many
SELECT * FROM chat_events WHERE id > @after::bigint ORDER BY id LIMIT @row_limit;

-- Добор после обрыва соединения LISTEN и повтор для переподключившегося потока: окно по времени.
-- name: ListChatEventsSince :many
SELECT * FROM chat_events WHERE created_at >= @since::timestamptz ORDER BY id LIMIT @row_limit;

-- Переподключение человека: события окна, которые ему положены по правам (учёному переписки, сотруднику организации или
-- подразделения). Фильтр здесь, а не в коде: чужие события окна не вытесняют из лимита его собственные.
-- name: ListChatEventsForReplay :many
SELECT * FROM chat_events
WHERE created_at >= @since::timestamptz
  AND (user_id = @user_id OR org_id = ANY(@orgs::uuid[]) OR unit_id = ANY(@units::uuid[]))
ORDER BY id
LIMIT @row_limit;

-- Время события, от которого переподключившийся поток начинает добор; нет строки — событие уже вышло из окна хранения.
-- name: GetChatEventTime :one
SELECT created_at FROM chat_events WHERE id = @id;

-- name: ChatEventHead :one
SELECT COALESCE(max(id), 0)::bigint FROM chat_events;

-- name: DeleteOldChatEvents :execrows
DELETE FROM chat_events WHERE id IN (
    SELECT e.id FROM chat_events e WHERE e.created_at < @before::timestamptz ORDER BY e.created_at LIMIT @batch::int
);

-- Сессия потока ещё жива: не удалена, не истекла, аккаунт цел.
-- name: ChatStreamSessionAlive :one
SELECT EXISTS (
    SELECT 1 FROM sessions s JOIN users u ON u.id = s.user_id
    WHERE s.id = @session_id AND s.user_id = @user_id AND s.expires_at > @now::timestamptz AND u.deleted_at IS NULL
)::boolean;

-- Письма о непрочитанном ---------------------------------------------------------------------------------------------

-- Новый текст сообщения стороне: строка появляется, у существующей растёт последний номер, время начала и отметка
-- отправки остаются.
-- name: UpsertChatUnreadMail :exec
INSERT INTO chat_unread_mail (chat_id, side, seq, since)
VALUES (@chat_id, @side::text, @seq::int, @now::timestamptz)
ON CONFLICT (chat_id, side) DO UPDATE SET seq = GREATEST(chat_unread_mail.seq, EXCLUDED.seq);

-- Сторона прочитала до последнего: ждать больше нечего.
-- name: ClearChatUnreadMail :exec
DELETE FROM chat_unread_mail m USING chats c
WHERE m.chat_id = c.id AND c.id = @chat_id AND m.side = @side::text
  AND m.seq <= CASE WHEN @side::text = 'scientist' THEN c.scientist_read ELSE c.org_read END;

-- Строки, которым пора. Берутся под замком и не больше batch: соседняя копия возьмёт другие, повтор не двоит письмо,
-- потому что отметка и письмо пишутся в одной транзакции.
-- name: ClaimChatUnreadMail :many
SELECT m.chat_id, m.side, m.seq, m.since,
       c.user_id, c.org_id, c.vacancy_id, c.scientist_read, c.org_read
FROM chat_unread_mail m
JOIN chats c ON c.id = m.chat_id
WHERE m.notified_at IS NULL AND m.since <= @due::timestamptz AND m.since > @oldest::timestamptz
ORDER BY m.since, m.chat_id
LIMIT @batch::int
FOR UPDATE OF m SKIP LOCKED;

-- Есть ли ещё непрочитанный текст стороне (правка и удаление могли убрать повод).
-- name: HasUnreadChatText :one
SELECT EXISTS (
    SELECT 1 FROM chat_messages x
    WHERE x.chat_id = @chat_id AND x.kind = 'text' AND x.to_side = @side::text AND x.deleted_at IS NULL
      AND x.seq > @read::int
)::boolean;

-- name: MarkChatUnreadMailNotified :exec
UPDATE chat_unread_mail SET notified_at = @now::timestamptz WHERE chat_id = @chat_id AND side = @side::text;

-- name: DeleteChatUnreadMail :exec
DELETE FROM chat_unread_mail WHERE chat_id = @chat_id AND side = @side::text;

-- Устаревшие (старше окна): писем о них не будет. Чистит задача.
-- name: DeleteStaleChatUnreadMail :execrows
DELETE FROM chat_unread_mail WHERE since <= @oldest::timestamptz;

-- Для метрик: сколько ждёт письма и как давно ждёт самое старое.
-- name: ChatUnreadMailStats :one
SELECT count(*)::bigint AS waiting, COALESCE(min(since), 'epoch'::timestamptz)::timestamptz AS oldest
FROM chat_unread_mail WHERE notified_at IS NULL;

-- Письмо на почту аккаунта о сообщении: тема и тело собирает код; выключатель email_messages (D-245).
-- name: EnqueueChatMailToUser :execrows
INSERT INTO outbox (to_email, subject, body, created_at, next_attempt_at)
SELECT u.email, @subject, @body, @now, @now
FROM users u LEFT JOIN notification_settings ns ON ns.user_id = u.id
WHERE u.id = @user_id AND COALESCE(ns.email_messages, true);

-- Данные для текста письма: вакансия, организация, имя учёного и признаки его анонимности (решает privacy).
-- name: GetChatMailFacts :one
SELECT v.title AS vacancy_title, o.name AS org_name, o.slug AS org_slug, v.unit_id,
       u.display_name AS scientist_name, p.visibility,
       (p.id IS NOT NULL AND EXISTS (SELECT 1 FROM profile_hidden_orgs h WHERE h.profile_id = p.id AND h.org_id = v.org_id))::boolean AS hidden_from,
       (EXISTS (SELECT 1 FROM applications ra JOIN vacancies rv ON rv.id = ra.vacancy_id
                 WHERE ra.user_id = c.user_id AND ra.status <> 'withdrawn' AND rv.org_id = v.org_id)
        OR EXISTS (SELECT 1 FROM vacancy_offers ro JOIN vacancies rv ON rv.id = ro.vacancy_id
                    WHERE ro.user_id = c.user_id AND ro.status = 'interested' AND rv.org_id = v.org_id))::boolean AS revealed
FROM chats c
JOIN vacancies v ON v.id = c.vacancy_id
JOIN organizations o ON o.id = v.org_id
JOIN users u ON u.id = c.user_id
LEFT JOIN profiles p ON p.user_id = c.user_id
WHERE c.id = @chat_id;

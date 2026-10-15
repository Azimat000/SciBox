-- Срез 39 (D-242…D-246): живое обновление чата и письма о непрочитанном. Миграция только добавляет таблицы (D-186).

-- +goose Up

-- Журнал событий чата для живого потока. Строка пишется в той же транзакции, что и сообщение, отметка прочтения или
-- строка о событии, и там же уходит `NOTIFY chat_events` с номером строки: копия сервера, которой подключён получатель,
-- берёт событие по номеру, отбирает тех, кому его можно показать, и собирает каждому его вид сообщения. Журнал нужен,
-- чтобы поток переподключался с `Last-Event-ID` без потери и чтобы копия добрала события за время обрыва своего
-- соединения LISTEN. Живёт 30 минут (задача `chat.events_cleanup`).
-- Ссылки на переписку и людей нет намеренно: строка короткоживущая и не должна мешать удалению переписки и аккаунта.
-- user_id — учёный переписки, org_id и unit_id — организация и подразделение вакансии: по ним получатель выбирается без
-- чтения переписки. actor_id — кто прочитал (chat.read) или положил в архив (chat.archived).
CREATE TABLE chat_events (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       text        NOT NULL CHECK (kind IN ('message.created', 'message.edited', 'message.deleted', 'chat.read', 'chat.archived')),
    chat_id    uuid        NOT NULL,
    user_id    uuid        NOT NULL,
    org_id     uuid        NOT NULL,
    unit_id    uuid,
    seq        integer     NOT NULL DEFAULT 0,
    actor_id   uuid,
    side       text        CHECK (side IN ('scientist', 'org')),
    up_to      integer     NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL
);
-- Чистка по времени и добор после обрыва соединения (окно по времени, а не по номеру: порядок фиксации не равен порядку номеров).
CREATE INDEX IF NOT EXISTS chat_events_created_idx ON chat_events (created_at);

-- Кому когда пора отправить письмо о непрочитанном сообщении (D-245): одна строка на пару «переписка, сторона». Строка
-- появляется с первым непрочитанным текстом, обновляет последний номер с каждым новым, исчезает, когда сторона
-- прочитала до него. notified_at отмечает, что письмо ушло: пока человек не открыл переписку, второго письма нет.
CREATE TABLE chat_unread_mail (
    chat_id     uuid        NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    side        text        NOT NULL CHECK (side IN ('scientist', 'org')),
    seq         integer     NOT NULL CHECK (seq > 0),
    since       timestamptz NOT NULL,
    notified_at timestamptz,
    PRIMARY KEY (chat_id, side)
);
-- Задача берёт только ещё не отправленные, самые давние первыми.
CREATE INDEX IF NOT EXISTS chat_unread_mail_due_idx ON chat_unread_mail (since) WHERE notified_at IS NULL;

-- +goose Down
DROP TABLE chat_unread_mail;
DROP TABLE chat_events;

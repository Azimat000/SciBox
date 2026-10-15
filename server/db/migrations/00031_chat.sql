-- Срез 37 (D-214, D-229…D-236): чат. Переписка учёного и организации по вакансии, сообщения, отметки прочтения и личный архив.
-- Миграция только добавляет таблицы и индексы (D-186).

-- +goose Up

-- Одна переписка на пару «учёный × вакансия». Живёт, пока жив отклик или приглашение, из которых выросла (CASCADE).
-- Закрыта она или открыта, в таблице не хранится: это следует из состояния отклика и приглашения (пакет chat, closure.go),
-- поэтому решение организации закрывает переписку без второй записи, которую можно забыть.
--
-- Счётчики на строке нужны, чтобы список и число в шапке не считали сообщения:
--   last_seq            номер последнего сообщения (по нему выдаётся следующий номер, замок строки упорядочивает отправку);
--   last_for_scientist  номер последнего сообщения, обращённого к учёному (текст организации, строка о событии для него);
--   last_for_org        то же для стороны организации;
--   scientist_read      до какого номера прочитал учёный (для галочек у собеседника);
--   org_read            до какого номера прочитал хоть один сотрудник организации.
CREATE TABLE chats (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    vacancy_id         uuid        NOT NULL REFERENCES vacancies (id) ON DELETE RESTRICT,
    -- Организация вакансии: переписывается из вакансии при создании (организация вакансии не меняется). Нужна, чтобы
    -- список организации шёл по индексу в порядке времени, не перебирая переписки всей базы.
    org_id             uuid        NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    user_id            uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    application_id     uuid        REFERENCES applications (id) ON DELETE CASCADE,
    offer_id           uuid        REFERENCES vacancy_offers (id) ON DELETE CASCADE,
    last_seq           integer     NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    last_for_scientist integer     NOT NULL DEFAULT 0 CHECK (last_for_scientist BETWEEN 0 AND last_seq),
    last_for_org       integer     NOT NULL DEFAULT 0 CHECK (last_for_org BETWEEN 0 AND last_seq),
    scientist_read     integer     NOT NULL DEFAULT 0 CHECK (scientist_read BETWEEN 0 AND last_seq),
    org_read           integer     NOT NULL DEFAULT 0 CHECK (org_read BETWEEN 0 AND last_seq),
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS chats_pair_uidx ON chats (vacancy_id, user_id);
-- Список учёного: свои переписки, новые сверху, листание по ключу (updated_at, id).
CREATE INDEX IF NOT EXISTS chats_user_idx ON chats (user_id, updated_at DESC, id DESC);
-- Список организации (сотрудник с правами на всю организацию): все её переписки, новые сверху.
CREATE INDEX IF NOT EXISTS chats_org_idx ON chats (org_id, updated_at DESC, id DESC);
-- Список руководителя подразделения и отбор по вакансии: переписки одной вакансии.
CREATE INDEX IF NOT EXISTS chats_vacancy_idx ON chats (vacancy_id, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS chats_application_idx ON chats (application_id) WHERE application_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS chats_offer_idx ON chats (offer_id) WHERE offer_id IS NOT NULL;

-- Сообщения. Текст человека (kind = text) или строка о событии (kind = system: отклик, приглашение, решение).
-- to_side — кому сообщение адресовано: так считаются непрочитанные. У текста это противоположная сторона, у строки о
-- событии сторона, которой событие сообщается. Номер seq растёт на 1 внутри переписки; по нему листание и прочтение.
-- Удалённое сообщение остаётся строкой без текста (deleted_at), собеседник видит «Сообщение удалено».
-- author_user_id становится пустым, когда аккаунт сотрудника удалён: подпись «Сотрудник организации».
CREATE TABLE chat_messages (
    chat_id        uuid        NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    seq            integer     NOT NULL CHECK (seq > 0),
    kind           text        NOT NULL CHECK (kind IN ('text', 'system')),
    author_side    text        CHECK (author_side IN ('scientist', 'org')),
    author_user_id uuid        REFERENCES users (id) ON DELETE SET NULL,
    to_side        text        NOT NULL CHECK (to_side IN ('scientist', 'org')),
    body           text        NOT NULL DEFAULT '' CHECK (char_length(body) <= 4000),
    event_type     text        NOT NULL DEFAULT '' CHECK (event_type IN (
        '', 'application.sent', 'application.viewed', 'invitation.created', 'invitation.answered',
        'invitation.proposal_accepted', 'invitation.cancelled', 'application.decided', 'application.withdrawn',
        'offer.created', 'offer.answered', 'offer.cancelled', 'chat.blocked', 'chat.unblocked')),
    event_data     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    client_id      uuid,
    created_at     timestamptz NOT NULL,
    edited_at      timestamptz,
    deleted_at     timestamptz,
    PRIMARY KEY (chat_id, seq),
    CHECK ((kind = 'text') = (author_side IS NOT NULL)),
    CHECK ((kind = 'system') = (event_type <> ''))
);
-- Повтор отправки с тем же client_id не создаёт второе сообщение (T-101). У строки о событии client_id пуст.
CREATE UNIQUE INDEX IF NOT EXISTS chat_messages_client_uidx ON chat_messages (chat_id, author_user_id, client_id) WHERE client_id IS NOT NULL;

-- Что личное у каждого участника: докуда он прочитал и лежит ли переписка у него в архиве. Строки заводятся при первом
-- прочтении или архивировании; нет строки значит «ничего не прочитано, не в архиве». Сторона организации читает
-- каждым сотрудником отдельно: у коллеги число непрочитанных своё.
CREATE TABLE chat_participants (
    chat_id    uuid        NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    read_seq   integer     NOT NULL DEFAULT 0 CHECK (read_seq >= 0),
    archived   boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (chat_id, user_id)
);
-- Вкладка «Архив» и число архивных у человека.
CREATE INDEX IF NOT EXISTS chat_participants_archive_idx ON chat_participants (user_id, chat_id) WHERE archived;

-- +goose Down
DROP TABLE chat_participants;
DROP TABLE chat_messages;
DROP TABLE chats;

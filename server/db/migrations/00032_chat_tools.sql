-- Срез 38 (D-214, D-236…D-241): инструменты чата. Шаблоны ответов организации, блокировка организации учёным, жалобы
-- и настройка писем о сообщениях. Миграция только добавляет (D-186).

-- +goose Up

-- Шаблоны ответов: общие на организацию, до 50 штук (предел держит сервис под замком организации). created_by очищается,
-- когда аккаунт автора удалён: шаблон принадлежит организации, а не человеку.
CREATE TABLE chat_templates (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
    body       text        NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    created_by uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
-- Список шаблонов организации по названию.
CREATE INDEX IF NOT EXISTS chat_templates_org_idx ON chat_templates (org_id, lower(title), id);

-- Блокировка организации учёным: одна строка на пару. Закрытие переписок следует из строки (пакет chat, closure.go),
-- поэтому снять блокировку значит удалить её, а не восстанавливать состояние переписок.
CREATE TABLE chat_blocks (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    org_id     uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, org_id)
);
-- Переписки организации проверяют блокировку по паре (user_id, org_id): первичный ключ. Этот индекс нужен только для
-- удаления организации.
CREATE INDEX IF NOT EXISTS chat_blocks_org_idx ON chat_blocks (org_id);

-- Жалобы учёного на организацию в переписке. Хранятся до модерации (срез 30). chat_id очищается, если переписка ушла;
-- организация и вакансия записаны отдельно, чтобы жалоба не зависела от судьбы переписки. Текст удалённых сообщений в
-- жалобу не попадает (D-232): пояснение пишет сам человек.
CREATE TABLE chat_complaints (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    chat_id    uuid        REFERENCES chats (id) ON DELETE SET NULL,
    org_id     uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    vacancy_id uuid        REFERENCES vacancies (id) ON DELETE SET NULL,
    reason     text        NOT NULL CHECK (reason IN ('spam', 'fraud', 'abuse', 'other')),
    note       text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    created_at timestamptz NOT NULL,
    CHECK (reason <> 'other' OR note <> '')
);
-- Очередь модерации (срез 30): новые сверху; жалобы одного человека на одну организацию считаются по паре.
CREATE INDEX IF NOT EXISTS chat_complaints_created_idx ON chat_complaints (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS chat_complaints_user_idx ON chat_complaints (user_id, org_id, created_at DESC);

-- Письма о непрочитанных сообщениях (рассылка в срезе 39): по умолчанию включены, как прежние.
ALTER TABLE notification_settings ADD COLUMN IF NOT EXISTS email_messages boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE notification_settings DROP COLUMN email_messages;
DROP TABLE chat_complaints;
DROP TABLE chat_blocks;
DROP TABLE chat_templates;

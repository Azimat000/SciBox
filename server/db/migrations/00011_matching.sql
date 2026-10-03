-- Избранное, сохранённые поиски, напоминания о сроках и настройки писем (срез 11).

-- +goose Up

-- Избранные вакансии человека (D-107). Вакансия — только публичная (опубликована или закрыта); вакансия, ставшая архивной,
-- остаётся в таблице и вернётся в список, если её откроют снова.
CREATE TABLE favorites (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    vacancy_id uuid        NOT NULL REFERENCES vacancies (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, vacancy_id)
);
CREATE INDEX favorites_user_idx ON favorites (user_id, created_at DESC);
CREATE INDEX favorites_vacancy_idx ON favorites (vacancy_id);

-- Сохранённый поиск (D-108): строка запроса в том виде, как она стоит в адресе страницы поиска (без страницы).
-- checked_at — «всё опубликованное до этого момента уже учтено»; next_run_at — когда фоновый цикл посмотрит в следующий раз.
CREATE TABLE saved_searches (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    query        text        NOT NULL CHECK (char_length(query) <= 2000),
    frequency    text        NOT NULL DEFAULT 'daily' CHECK (frequency IN ('instant', 'daily', 'weekly', 'off')),
    checked_at   timestamptz NOT NULL,
    next_run_at  timestamptz NOT NULL,
    last_sent_at timestamptz,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);
CREATE INDEX saved_searches_user_idx ON saved_searches (user_id, created_at DESC);
CREATE INDEX saved_searches_due_idx ON saved_searches (next_run_at) WHERE frequency <> 'off';

-- Отправленные напоминания о сроке (D-105): одно на человека, вакансию, ступень (7 или 1 день) и конкретный срок.
CREATE TABLE deadline_reminders (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    vacancy_id uuid        NOT NULL REFERENCES vacancies (id) ON DELETE CASCADE,
    stage      smallint    NOT NULL CHECK (stage IN (1, 7)),
    deadline   date        NOT NULL,
    sent_at    timestamptz NOT NULL,
    PRIMARY KEY (user_id, vacancy_id, stage, deadline)
);

-- Какие письма человек получает (D-106). Нет строки — всё включено. Уведомления на сайте это не отключает.
CREATE TABLE notification_settings (
    user_id             uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    email_new_vacancies boolean     NOT NULL DEFAULT true,
    email_deadlines     boolean     NOT NULL DEFAULT true,
    updated_at          timestamptz NOT NULL
);

-- +goose Down
DROP TABLE notification_settings;
DROP TABLE deadline_reminders;
DROP TABLE saved_searches;
DROP TABLE favorites;

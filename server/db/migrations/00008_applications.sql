-- Отклики, рекомендательные письма, уведомления и очередь писем (срез 8).

-- +goose Up

-- Отклик на вакансию. profile — снимок профиля на момент отправки (то, что человек отправил,
-- остаётся таким, даже если он потом поправит или скроет профиль, D-072).
-- status: sent → viewed → invited → accepted | rejected; withdrawn — отозван самим соискателем.
CREATE TABLE applications (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    vacancy_id        uuid        NOT NULL REFERENCES vacancies (id) ON DELETE RESTRICT,
    user_id           uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status            text        NOT NULL DEFAULT 'sent' CHECK (status IN ('sent', 'viewed', 'invited', 'rejected', 'accepted', 'withdrawn')),
    cover_letter      text        NOT NULL CHECK (char_length(cover_letter) BETWEEN 1 AND 6000),
    contact_email     text        NOT NULL CHECK (char_length(contact_email) BETWEEN 3 AND 254),
    profile           jsonb       NOT NULL,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,
    status_changed_at timestamptz NOT NULL
);
-- Один живой отклик на вакансию от человека; после отзыва можно откликнуться заново.
CREATE UNIQUE INDEX applications_one_active ON applications (vacancy_id, user_id) WHERE status <> 'withdrawn';
CREATE INDEX applications_user_idx ON applications (user_id, created_at DESC);
CREATE INDEX applications_vacancy_idx ON applications (vacancy_id, created_at DESC);

-- Просьба о рекомендательном письме. В базе только хеш одноразовой ссылки.
-- pending → received (письмо загружено) | declined (рекомендатель отказался).
CREATE TABLE reference_requests (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    name           text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    email          citext      NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254),
    relation       text        NOT NULL DEFAULT '' CHECK (char_length(relation) <= 120),
    token_hash     bytea       NOT NULL UNIQUE,
    status         text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'received', 'declined')),
    letter_text    text        NOT NULL DEFAULT '' CHECK (char_length(letter_text) <= 12000),
    created_at     timestamptz NOT NULL,
    expires_at     timestamptz NOT NULL,
    last_sent_at   timestamptz NOT NULL,
    send_count     smallint    NOT NULL DEFAULT 1,
    answered_at    timestamptz
);
CREATE UNIQUE INDEX reference_requests_one_per_email ON reference_requests (application_id, email);

-- Файлы отклика. Все лежат в базе (D-080): PDF резюме, приложенные файлы и PDF рекомендательных писем.
-- Кто какой вид видит, решает пакет internal/files (письма соискателю не отдаются).
CREATE TABLE application_files (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    reference_id   uuid        REFERENCES reference_requests (id) ON DELETE CASCADE,
    kind           text        NOT NULL CHECK (kind IN ('cv', 'attachment', 'reference_letter')),
    name           text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    size           integer     NOT NULL CHECK (size > 0),
    position       smallint    NOT NULL DEFAULT 0,
    data           bytea       NOT NULL,
    created_at     timestamptz NOT NULL,
    CHECK ((kind = 'reference_letter') = (reference_id IS NOT NULL))
);
CREATE INDEX application_files_application_idx ON application_files (application_id);

-- Уведомления на сайте (колокольчик). Тексты хранятся готовыми.
CREATE TABLE notifications (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL CHECK (char_length(kind) BETWEEN 1 AND 60),
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    body       text        NOT NULL DEFAULT '',
    link       text        NOT NULL DEFAULT '' CHECK (char_length(link) <= 300),
    created_at timestamptz NOT NULL,
    read_at    timestamptz
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

-- Очередь писем. Письмо кладётся в ту же транзакцию, что и событие, поэтому не теряется при падении сервера.
-- Отправитель берёт письмо «в аренду» (next_attempt_at сдвигается), отправляет и ставит sent_at;
-- не вышло — повтор позже с растущей паузой; после 8 неудач письмо помечается failed_at и не повторяется.
CREATE TABLE outbox (
    id              bigserial   PRIMARY KEY,
    to_email        text        NOT NULL,
    subject         text        NOT NULL,
    body            text        NOT NULL,
    created_at      timestamptz NOT NULL,
    next_attempt_at timestamptz NOT NULL,
    attempts        integer     NOT NULL DEFAULT 0,
    last_error      text        NOT NULL DEFAULT '',
    sent_at         timestamptz,
    failed_at       timestamptz
);
CREATE INDEX outbox_due_idx ON outbox (next_attempt_at) WHERE sent_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP TABLE outbox;
DROP TABLE notifications;
DROP TABLE application_files;
DROP TABLE reference_requests;
DROP TABLE applications;

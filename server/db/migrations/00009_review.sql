-- Разбор откликов организацией (срез 9): решения и приглашения.

-- +goose Up

-- Записка к решению (принят / отказ): организация пишет её соискателю; кто принял решение запоминается.
ALTER TABLE applications
    ADD COLUMN decision_note text NOT NULL DEFAULT '' CHECK (char_length(decision_note) <= 1000),
    ADD COLUMN decided_by    uuid REFERENCES users (id) ON DELETE SET NULL;

-- Приглашение к отклику. Три вида (D-011):
--   interview        собеседование: дата и время (starts_at), формат (online: ссылка / onsite: адрес) и место;
--   contacts         организация сообщает свои контакты (отвечать не нужно, статус shared);
--   request_contacts организация просит оставить контакты и удобное время.
-- status: pending (ждёт ответа) → confirmed | proposed (соискатель предложил другое время) | answered;
-- proposed → confirmed (организация приняла предложенное время); pending | proposed | confirmed → cancelled.
CREATE TABLE application_invitations (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    kind           text        NOT NULL CHECK (kind IN ('interview', 'contacts', 'request_contacts')),
    status         text        NOT NULL CHECK (status IN ('pending', 'confirmed', 'proposed', 'answered', 'shared', 'cancelled')),
    message        text        NOT NULL DEFAULT '' CHECK (char_length(message) <= 2000),
    starts_at      timestamptz,
    place_kind     text        CHECK (place_kind IN ('online', 'onsite')),
    place          text        NOT NULL DEFAULT '' CHECK (char_length(place) <= 500),
    contact_name   text        NOT NULL DEFAULT '' CHECK (char_length(contact_name) <= 120),
    contact_email  text        NOT NULL DEFAULT '' CHECK (char_length(contact_email) <= 254),
    contact_phone  text        NOT NULL DEFAULT '' CHECK (char_length(contact_phone) <= 40),
    -- ответ соискателя
    answer_at      timestamptz,
    answer_note    text        NOT NULL DEFAULT '' CHECK (char_length(answer_note) <= 1000),
    answer_contact text        NOT NULL DEFAULT '' CHECK (char_length(answer_contact) <= 300),
    answer_time    text        NOT NULL DEFAULT '' CHECK (char_length(answer_time) <= 300),
    answered_at    timestamptz,
    created_by     uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    CHECK ((kind = 'interview') = (starts_at IS NOT NULL AND place_kind IS NOT NULL AND place <> ''))
);
CREATE INDEX application_invitations_application_idx ON application_invitations (application_id, created_at);

-- +goose Down
DROP TABLE application_invitations;
ALTER TABLE applications DROP COLUMN decided_by, DROP COLUMN decision_note;

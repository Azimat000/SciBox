-- Каталог учёных и приглашения на вакансию (срез 10).

-- +goose Up

-- Приглашение учёного на конкретную вакансию (D-095, D-096). Организация зовёт, учёный отвечает один раз.
-- status: pending (ждёт ответа) → interested | declined (ответил учёный); pending → cancelled (отозвала организация).
-- Живое приглашение на пару «вакансия + человек» может быть только одно; после отзыва можно пригласить снова.
CREATE TABLE vacancy_offers (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    vacancy_id  uuid        NOT NULL REFERENCES vacancies (id) ON DELETE RESTRICT,
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    invited_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    status      text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'interested', 'declined', 'cancelled')),
    message     text        NOT NULL DEFAULT '' CHECK (char_length(message) <= 1000),
    answer_note text        NOT NULL DEFAULT '' CHECK (char_length(answer_note) <= 1000),
    answered_at timestamptz,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    CHECK ((status IN ('interested', 'declined')) = (answered_at IS NOT NULL))
);
CREATE UNIQUE INDEX vacancy_offers_one_live ON vacancy_offers (vacancy_id, user_id) WHERE status <> 'cancelled';
CREATE INDEX vacancy_offers_user_idx ON vacancy_offers (user_id, created_at DESC);
CREATE INDEX vacancy_offers_vacancy_idx ON vacancy_offers (vacancy_id, created_at DESC);

-- +goose Down
DROP TABLE vacancy_offers;

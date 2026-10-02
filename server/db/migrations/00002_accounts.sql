-- Аккаунты (срез 3): пользователи, сессии, одноразовые ссылки из писем, события для ограничения частоты.

-- +goose Up
CREATE TABLE users (
    id                     uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email                  citext      NOT NULL UNIQUE,
    display_name           text        NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 120),
    password_hash          text        NOT NULL,
    email_confirmed_at     timestamptz,
    -- Согласие на обработку персональных данных (152-ФЗ): когда и на какую версию текста.
    privacy_consent_at     timestamptz NOT NULL,
    privacy_policy_version text        NOT NULL,
    created_at             timestamptz NOT NULL,
    updated_at             timestamptz NOT NULL
);

-- В базе лежит только хеш токена из cookie: утечка таблицы не даёт войти в чужой аккаунт.
CREATE TABLE sessions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    user_agent   text        NOT NULL DEFAULT '',
    ip           text        NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Одноразовые ссылки из писем: подтверждение почты и сброс пароля. Тоже только хеш.
CREATE TABLE auth_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text        NOT NULL CHECK (purpose IN ('confirm_email', 'reset_password')),
    token_hash bytea       NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);
CREATE INDEX auth_tokens_user_purpose_idx ON auth_tokens (user_id, purpose);

-- События для ограничения частоты: неудачные входы (по почте и по адресу), запросы писем с одного адреса.
-- kind — что считаем, key — по чему считаем (почта, IP, id пользователя).
CREATE TABLE rate_events (
    id   bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text        NOT NULL,
    key  text        NOT NULL,
    at   timestamptz NOT NULL
);
CREATE INDEX rate_events_lookup_idx ON rate_events (kind, key, at);
CREATE INDEX rate_events_at_idx ON rate_events (at);

-- +goose Down
DROP TABLE rate_events;
DROP TABLE auth_tokens;
DROP TABLE sessions;
DROP TABLE users;

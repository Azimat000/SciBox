-- Организации и подразделения (срез 4): организации, сотрудники с ролями, подразделения, приглашения по почте.

-- +goose Up
CREATE TABLE organizations (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Адрес страницы: латиницей, из названия при создании; после этого не меняется, чтобы ссылки не ломались.
    slug        text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 80),
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 2 AND 200),
    kind        text        NOT NULL CHECK (kind IN ('university', 'institute', 'science_center', 'rd_company', 'technopark', 'other')),
    city        text        NOT NULL CHECK (char_length(city) BETWEEN 1 AND 100),
    website     text        NOT NULL DEFAULT '' CHECK (char_length(website) <= 300),
    description text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 5000),
    created_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);
CREATE INDEX organizations_name_trgm_idx ON organizations USING gin (name gin_trgm_ops);
CREATE INDEX organizations_kind_idx ON organizations (kind);

-- Роль человека в организации. Один человек: одна роль в одной организации.
--   owner      всё: данные организации, подразделения, сотрудники, все вакансии;
--   hr         кадровик: вакансии и отклики всех подразделений;
--   unit_head  руководитель подразделения: вакансии и отклики только своих подразделений (units.head_user_id).
CREATE TABLE org_members (
    org_id    uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id   uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role      text        NOT NULL CHECK (role IN ('owner', 'hr', 'unit_head')),
    joined_at timestamptz NOT NULL,
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX org_members_user_idx ON org_members (user_id);

CREATE TABLE units (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name         text        NOT NULL CHECK (char_length(name) BETWEEN 2 AND 200),
    kind         text        NOT NULL CHECK (kind IN ('department', 'laboratory', 'division', 'shared_facility')),
    description  text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 3000),
    -- Научные темы подразделения: короткие фразы.
    topics       text[]      NOT NULL DEFAULT '{}',
    -- Руководитель: сотрудник организации. Сервис следит, чтобы он оставался её сотрудником.
    head_user_id uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);
CREATE INDEX units_org_idx ON units (org_id);
CREATE INDEX units_head_idx ON units (head_user_id) WHERE head_user_id IS NOT NULL;

-- Приглашение сотрудника по почте. В базе только хеш токена из ссылки (как у сессий).
CREATE TABLE org_invitations (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email       citext      NOT NULL,
    role        text        NOT NULL CHECK (role IN ('owner', 'hr', 'unit_head')),
    -- Для руководителя: подразделение, которым он станет руководить (если у него ещё нет руководителя).
    unit_id     uuid        REFERENCES units (id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL UNIQUE,
    invited_by  uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz
);
CREATE INDEX org_invitations_org_idx ON org_invitations (org_id);
CREATE INDEX org_invitations_email_idx ON org_invitations (email);

-- +goose Down
DROP TABLE org_invitations;
DROP TABLE units;
DROP TABLE org_members;
DROP TABLE organizations;

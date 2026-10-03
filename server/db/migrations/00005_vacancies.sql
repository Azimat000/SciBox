-- Вакансии (срез 5).

-- +goose Up

-- Вакансия. Жизненный цикл (status): draft → published → closed → archived (переходы проверяет сервис internal/vacancies).
-- Черновик может быть неполным, поэтому почти все поля допускают «пусто»; что обязательно для публикации, решает сервис.
CREATE TABLE vacancies (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- Подразделение с вакансиями удалить нельзя (D-051): пусть владелец сначала закроет и перенесёт вакансии.
    unit_id        uuid        REFERENCES units (id) ON DELETE RESTRICT,
    created_by     uuid        REFERENCES users (id) ON DELETE SET NULL,
    status         text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'closed', 'archived')),
    title          text        NOT NULL CHECK (char_length(title) BETWEEN 3 AND 200),
    position_code  text        NOT NULL REFERENCES positions (code),
    summary        text        NOT NULL DEFAULT '' CHECK (char_length(summary) <= 600),
    description    text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
    requirements   text        NOT NULL DEFAULT '' CHECK (char_length(requirements) <= 5000),
    -- Что предстоит делать: дисциплины (ППС), тема исследования (аспирантура, постдок), проект (научные должности), сфера ответственности (управление).
    focus          text        NOT NULL DEFAULT '' CHECK (char_length(focus) <= 300),
    -- Уровень по рамке EURAXESS: 1 — R1 … 4 — R4.
    career_level   smallint    CHECK (career_level BETWEEN 1 AND 4),
    work_format    text        CHECK (work_format IN ('onsite', 'hybrid', 'remote')),
    region_code    text        REFERENCES regions (code),
    city           text        NOT NULL DEFAULT '' CHECK (char_length(city) <= 100),
    housing        text        NOT NULL DEFAULT 'none' CHECK (housing IN ('none', 'dormitory', 'service', 'compensation')),
    -- Ставка в процентах от полной.
    rate_percent   smallint    CHECK (rate_percent IN (25, 50, 75, 100)),
    -- Рублей в месяц до вычета налога; у аспирантуры и постдока это стипендия. Необязательны (D-014).
    salary_from    integer     CHECK (salary_from > 0),
    salary_to      integer     CHECK (salary_to > 0),
    contract_type  text        CHECK (contract_type IN ('permanent', 'fixed')),
    contract_months smallint   CHECK (contract_months BETWEEN 1 AND 120),
    funding_source text        CHECK (funding_source IN ('budget', 'grant', 'contract', 'own')),
    funding_note   text        NOT NULL DEFAULT '' CHECK (char_length(funding_note) <= 200),
    degree_required text       NOT NULL DEFAULT 'none' CHECK (degree_required IN ('none', 'candidate', 'doctor')),
    title_required text        NOT NULL DEFAULT 'none' CHECK (title_required IN ('none', 'docent', 'professor')),
    is_competition boolean     NOT NULL DEFAULT false,
    -- Последний день приёма откликов (включительно, по московскому времени).
    deadline       date,
    published_at   timestamptz,
    closed_at      timestamptz,
    archived_at    timestamptz,
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    CONSTRAINT vacancies_salary_order CHECK (salary_to IS NULL OR salary_from IS NULL OR salary_to >= salary_from),
    CONSTRAINT vacancies_contract_months_only_fixed CHECK (contract_months IS NULL OR contract_type = 'fixed')
);
CREATE INDEX vacancies_org_status_idx ON vacancies (org_id, status);
CREATE INDEX vacancies_unit_idx ON vacancies (unit_id) WHERE unit_id IS NOT NULL;
CREATE INDEX vacancies_published_idx ON vacancies (published_at DESC) WHERE status = 'published';

-- Научные специальности вакансии (область науки для поиска).
CREATE TABLE vacancy_specialties (
    vacancy_id     uuid NOT NULL REFERENCES vacancies (id) ON DELETE CASCADE,
    specialty_code text NOT NULL REFERENCES specialties (code),
    PRIMARY KEY (vacancy_id, specialty_code)
);
CREATE INDEX vacancy_specialties_code_idx ON vacancy_specialties (specialty_code);

-- Вакансия вместе с названиями из справочников, организацией и подразделением: из неё читают и страницы, и списки.
-- Звёздочка раскрывается при создании представления: добавили колонку в vacancies — пересоздайте представление.
CREATE VIEW vacancy_view AS
SELECT v.*,
       p.name          AS position_name,
       p.position_type AS position_type,
       r.name          AS region_name,
       o.slug          AS org_slug,
       o.name          AS org_name,
       o.kind          AS org_kind,
       o.city          AS org_city,
       u.name          AS unit_name
FROM vacancies v
JOIN positions p ON p.code = v.position_code
JOIN organizations o ON o.id = v.org_id
LEFT JOIN regions r ON r.code = v.region_code
LEFT JOIN units u ON u.id = v.unit_id;

-- +goose Down
DROP VIEW vacancy_view;
DROP TABLE vacancy_specialties;
DROP TABLE vacancies;

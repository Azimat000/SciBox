-- Профиль учёного (срез 7).

-- +goose Up

-- Профиль: один на человека, создаётся при первом заходе в «Мой профиль». Основные поля лежат колонками;
-- разделы (образование, опыт, публикации, гранты, патенты, преподавание) — в profile_items.
-- Форматы идентификаторов и допустимые годы проверяет сервис (internal/profiles); здесь только длины и перечисления.
CREATE TABLE profiles (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               uuid        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    -- Кому виден профиль (D-010): hidden — никому, orgs — сотрудникам организаций, public — всем. Решает internal/privacy.
    visibility            text        NOT NULL DEFAULT 'hidden' CHECK (visibility IN ('hidden', 'orgs', 'public')),
    open_to_offers        boolean     NOT NULL DEFAULT false,
    headline              text        NOT NULL DEFAULT '' CHECK (char_length(headline) <= 200),
    city                  text        NOT NULL DEFAULT '' CHECK (char_length(city) <= 100),
    region_code           text        REFERENCES regions (code),
    about                 text        NOT NULL DEFAULT '' CHECK (char_length(about) <= 3000),
    degree                text        NOT NULL DEFAULT 'none' CHECK (degree IN ('none', 'candidate', 'doctor')),
    degree_specialty_code text        REFERENCES specialties (code),
    degree_year           smallint    CHECK (degree_year BETWEEN 1900 AND 2100),
    degree_institution    text        NOT NULL DEFAULT '' CHECK (char_length(degree_institution) <= 200),
    dissertation_title    text        NOT NULL DEFAULT '' CHECK (char_length(dissertation_title) <= 500),
    academic_title        text        NOT NULL DEFAULT 'none' CHECK (academic_title IN ('none', 'docent', 'professor')),
    academic_title_year   smallint    CHECK (academic_title_year BETWEEN 1900 AND 2100),
    orcid                 text        NOT NULL DEFAULT '' CHECK (char_length(orcid) <= 19),
    spin                  text        NOT NULL DEFAULT '' CHECK (char_length(spin) <= 12),
    scopus_id             text        NOT NULL DEFAULT '' CHECK (char_length(scopus_id) <= 16),
    wos_id                text        NOT NULL DEFAULT '' CHECK (char_length(wos_id) <= 20),
    -- h-index по базам, вводится вручную.
    h_rsci                smallint    CHECK (h_rsci BETWEEN 0 AND 300),
    h_scopus              smallint    CHECK (h_scopus BETWEEN 0 AND 300),
    h_wos                 smallint    CHECK (h_wos BETWEEN 0 AND 300),
    h_scholar             smallint    CHECK (h_scholar BETWEEN 0 AND 300),
    -- Контактная почта для организаций; почта аккаунта наружу не отдаётся.
    contact_email         text        NOT NULL DEFAULT '' CHECK (char_length(contact_email) <= 254),
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL
);
CREATE INDEX profiles_visibility_idx ON profiles (visibility);

-- Научные специальности учёного (по ним организации будут искать, срез 10).
CREATE TABLE profile_specialties (
    profile_id     uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    specialty_code text NOT NULL REFERENCES specialties (code),
    PRIMARY KEY (profile_id, specialty_code)
);
CREATE INDEX profile_specialties_code_idx ON profile_specialties (specialty_code);

-- Записи разделов профиля. kind: education, experience, publication, grant, patent, teaching.
-- data — поля записи (JSON), которые проверил сервис; sort_year — по нему записи идут от новых к старым
-- (для незавершённых: 9999, они вверху).
CREATE TABLE profile_items (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id uuid        NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    kind       text        NOT NULL CHECK (kind IN ('education', 'experience', 'publication', 'grant', 'patent', 'teaching')),
    sort_year  smallint    NOT NULL DEFAULT 0,
    data       jsonb       NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX profile_items_profile_idx ON profile_items (profile_id, kind, sort_year DESC, created_at DESC);
-- Одна публикация с одним DOI на профиль (DOI хранится в нижнем регистре).
CREATE UNIQUE INDEX profile_items_doi_idx ON profile_items (profile_id, (data ->> 'doi'))
    WHERE kind = 'publication' AND data ->> 'doi' IS NOT NULL;

-- +goose Down
DROP TABLE profile_items;
DROP TABLE profile_specialties;
DROP TABLE profiles;

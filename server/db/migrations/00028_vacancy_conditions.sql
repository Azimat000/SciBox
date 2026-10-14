-- Срез 16 (D-203): условия для учёного в вакансии: доступ к оборудованию и ЦКП, оплата публикаций, поездки на конференции.
--
-- Только добавление: столбцы с значением по умолчанию, поэтому старая версия сервера, которая их не знает, продолжает
-- вставлять и читать вакансии (миграция входит в правило «расширить, потом сжать», D-186). Представление vacancy_view
-- пересоздаётся командой CREATE OR REPLACE: старые столбцы стоят на прежних местах, новые добавлены в конец. Звёздочка
-- раскрывалась при создании, поэтому старый список столбцов выписан явно.
--
-- Проверку «описание и подразделение допустимы только при отметке про оборудование» делает сервис; в базе только длина
-- (ограничение стоит прямо на столбце: так оно не считается отдельным ограничением таблицы).

-- +goose Up
ALTER TABLE vacancies ADD COLUMN equipment_access boolean NOT NULL DEFAULT false;
ALTER TABLE vacancies ADD COLUMN equipment_note text NOT NULL DEFAULT '' CHECK (char_length(equipment_note) <= 300);
-- Подразделение-ЦКП той же организации: если подразделение удалят, ссылка пропадёт, описание останется.
ALTER TABLE vacancies ADD COLUMN equipment_unit_id uuid REFERENCES units (id) ON DELETE SET NULL;
ALTER TABLE vacancies ADD COLUMN publications_paid boolean NOT NULL DEFAULT false;
ALTER TABLE vacancies ADD COLUMN conference_travel boolean NOT NULL DEFAULT false;

CREATE OR REPLACE VIEW vacancy_view AS
SELECT v.id, v.org_id, v.unit_id, v.created_by, v.status, v.title, v.position_code, v.summary, v.description,
       v.requirements, v.focus, v.career_level, v.work_format, v.region_code, v.city, v.housing, v.rate_percent,
       v.salary_from, v.salary_to, v.contract_type, v.contract_months, v.funding_source, v.funding_note,
       v.degree_required, v.title_required, v.is_competition, v.deadline, v.published_at, v.closed_at, v.archived_at,
       v.created_at, v.updated_at,
       p.name          AS position_name,
       p.position_type AS position_type,
       r.name          AS region_name,
       o.slug          AS org_slug,
       o.name          AS org_name,
       o.kind          AS org_kind,
       o.city          AS org_city,
       u.name          AS unit_name,
       v.equipment_access, v.equipment_note, v.equipment_unit_id, v.publications_paid, v.conference_travel,
       eu.name         AS equipment_unit_name
FROM vacancies v
JOIN positions p ON p.code = v.position_code
JOIN organizations o ON o.id = v.org_id
LEFT JOIN regions r ON r.code = v.region_code
LEFT JOIN units u ON u.id = v.unit_id
LEFT JOIN units eu ON eu.id = v.equipment_unit_id;

-- +goose Down
DROP VIEW vacancy_view;
ALTER TABLE vacancies DROP COLUMN conference_travel;
ALTER TABLE vacancies DROP COLUMN publications_paid;
ALTER TABLE vacancies DROP COLUMN equipment_unit_id;
ALTER TABLE vacancies DROP COLUMN equipment_note;
ALTER TABLE vacancies DROP COLUMN equipment_access;
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

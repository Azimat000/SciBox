-- Семь видов вакансий вместо четырёх (D-134): научный работник, преподаватель, административный сотрудник, аспирантура,
-- магистратура, проектная работа, стажировка. Прежний вид early_career делится на аспирантуру, магистратуру и стажировку
-- (постдок становится научной должностью), management становится административным сотрудником (admin).
-- Коды должностей — первичный ключ, на них ссылаются вакансии; поэтому строки «Другая…» не переименовываются,
-- а заменяются новыми: вакансии переводятся на новый код, старый удаляется.

-- +goose Up
ALTER TABLE positions DROP CONSTRAINT positions_position_type_check;
-- name UNIQUE: новые названия ставим после временных, чтобы не столкнуться со старыми.
UPDATE positions SET name = name || ' (old)', sort = sort + 100;

INSERT INTO positions (code, position_type, name, sort) VALUES
    ('admin_other', 'admin', 'Другая административная должность', 0),
    ('internship_other', 'internship', 'Другая стажировка', 0),
    ('methodist', 'admin', 'Методист', 0),
    ('admin_specialist', 'admin', 'Специалист учебного или научного отдела', 0),
    ('grant_executor', 'project', 'Исполнитель по гранту', 0),
    ('project_specialist', 'project', 'Специалист проекта', 0),
    ('project_other', 'project', 'Другая проектная работа', 0),
    ('student_intern', 'internship', 'Стажировка для студентов', 0);

UPDATE vacancies SET position_code = 'admin_other' WHERE position_code = 'management_other';
UPDATE vacancies SET position_code = 'internship_other' WHERE position_code = 'early_career_other';
DELETE FROM positions WHERE code IN ('management_other', 'early_career_other');

UPDATE positions AS p SET position_type = n.position_type, name = n.name, sort = n.sort
FROM (VALUES
    ('research_lab_assistant', 'research', 'Лаборант-исследователь', 1),
    ('research_engineer', 'research', 'Инженер-исследователь', 2),
    ('junior_researcher', 'research', 'Младший научный сотрудник', 3),
    ('researcher', 'research', 'Научный сотрудник', 4),
    ('senior_researcher', 'research', 'Старший научный сотрудник', 5),
    ('leading_researcher', 'research', 'Ведущий научный сотрудник', 6),
    ('chief_researcher', 'research', 'Главный научный сотрудник', 7),
    ('lab_head', 'research', 'Заведующий лабораторией (отделом)', 8),
    ('postdoc', 'research', 'Постдок', 9),
    ('research_other', 'research', 'Другая научная должность', 10),
    ('assistant', 'teaching', 'Ассистент', 11),
    ('lecturer', 'teaching', 'Преподаватель', 12),
    ('senior_lecturer', 'teaching', 'Старший преподаватель', 13),
    ('docent', 'teaching', 'Доцент', 14),
    ('professor', 'teaching', 'Профессор', 15),
    ('department_head', 'teaching', 'Заведующий кафедрой', 16),
    ('teaching_other', 'teaching', 'Другая должность ППС', 17),
    ('methodist', 'admin', 'Методист', 18),
    ('admin_specialist', 'admin', 'Специалист учебного или научного отдела', 19),
    ('grant_manager', 'admin', 'Грант-менеджер', 20),
    ('tech_transfer', 'admin', 'Специалист по трансферу технологий', 21),
    ('shared_facility_head', 'admin', 'Руководитель ЦКП', 22),
    ('dean', 'admin', 'Декан факультета (директор института)', 23),
    ('science_vice_rector', 'admin', 'Проректор по науке', 24),
    ('admin_other', 'admin', 'Другая административная должность', 25),
    ('phd_student', 'phd', 'Аспирантура', 26),
    ('master_student', 'masters', 'Магистратура', 27),
    ('grant_executor', 'project', 'Исполнитель по гранту', 28),
    ('project_specialist', 'project', 'Специалист проекта', 29),
    ('project_other', 'project', 'Другая проектная работа', 30),
    ('intern_researcher', 'internship', 'Стажёр-исследователь', 31),
    ('student_intern', 'internship', 'Стажировка для студентов', 32),
    ('internship_other', 'internship', 'Другая стажировка', 33)
) AS n (code, position_type, name, sort)
WHERE p.code = n.code;

ALTER TABLE positions ADD CONSTRAINT positions_position_type_check
    CHECK (position_type IN ('research', 'teaching', 'admin', 'phd', 'masters', 'project', 'internship'));

-- Сохранённые поиски хранят фильтры строкой адреса: старые виды заменяем новыми, иначе поиск перестанет открываться.
UPDATE saved_searches SET query = regexp_replace(query, '(^|&)type=early_career(?=&|$)', '\1type=phd&type=masters&type=internship', 'g')
WHERE query ~ '(^|&)type=early_career(&|$)';
UPDATE saved_searches SET query = regexp_replace(query, '(^|&)type=management(?=&|$)', '\1type=admin', 'g')
WHERE query ~ '(^|&)type=management(&|$)';

-- Название должности входит в поисковый текст вакансии.
SELECT refresh_vacancy_search(id) FROM vacancies;

UPDATE reference_sources SET
    title = 'Квалификационные характеристики должностей: научные работники (Постановление Президиума РАН от 25.03.2008 № 196; ЕКС, приказ Минтруда России) и ППС (приказ Минздравсоцразвития России от 11.01.2011 № 1н); административные должности, проектная работа, аспирантура, магистратура и стажировки — по практике вузов',
    edition = 'Список составлен по результатам исследования 2026-10-03, виды вакансий изменены 2026-10-07 (D-134). Строки «Другая…» не официальные: они нужны, чтобы вакансию можно было опубликовать, если должности нет в списке.',
    checked_on = '2026-10-07'
WHERE catalog = 'positions';

-- +goose Down
ALTER TABLE positions DROP CONSTRAINT positions_position_type_check;
UPDATE positions SET name = name || ' (new)', sort = sort + 100;

INSERT INTO positions (code, position_type, name, sort) VALUES
    ('management_other', 'management', 'Другая должность в управлении наукой', 0),
    ('early_career_other', 'early_career', 'Другая программа для начинающих', 0);
UPDATE vacancies SET position_code = 'management_other' WHERE position_code IN ('admin_other', 'methodist', 'admin_specialist');
UPDATE vacancies SET position_code = 'early_career_other' WHERE position_code IN ('internship_other', 'student_intern');
UPDATE vacancies SET position_code = 'research_other' WHERE position_code IN ('grant_executor', 'project_specialist', 'project_other');
DELETE FROM positions WHERE code IN ('admin_other', 'internship_other', 'methodist', 'admin_specialist', 'grant_executor',
                                     'project_specialist', 'project_other', 'student_intern');

UPDATE positions AS p SET position_type = n.position_type, name = n.name, sort = n.sort
FROM (VALUES
    ('research_lab_assistant', 'research', 'Лаборант-исследователь', 1),
    ('research_engineer', 'research', 'Инженер-исследователь', 2),
    ('junior_researcher', 'research', 'Младший научный сотрудник', 3),
    ('researcher', 'research', 'Научный сотрудник', 4),
    ('senior_researcher', 'research', 'Старший научный сотрудник', 5),
    ('leading_researcher', 'research', 'Ведущий научный сотрудник', 6),
    ('chief_researcher', 'research', 'Главный научный сотрудник', 7),
    ('lab_head', 'research', 'Заведующий лабораторией (отделом)', 8),
    ('research_other', 'research', 'Другая научная должность', 9),
    ('assistant', 'teaching', 'Ассистент', 10),
    ('lecturer', 'teaching', 'Преподаватель', 11),
    ('senior_lecturer', 'teaching', 'Старший преподаватель', 12),
    ('docent', 'teaching', 'Доцент', 13),
    ('professor', 'teaching', 'Профессор', 14),
    ('department_head', 'teaching', 'Заведующий кафедрой', 15),
    ('teaching_other', 'teaching', 'Другая должность ППС', 16),
    ('master_student', 'early_career', 'Магистратура', 17),
    ('phd_student', 'early_career', 'Аспирантура', 18),
    ('postdoc', 'early_career', 'Постдок', 19),
    ('intern_researcher', 'early_career', 'Стажёр-исследователь', 20),
    ('early_career_other', 'early_career', 'Другая программа для начинающих', 21),
    ('grant_manager', 'management', 'Грант-менеджер', 22),
    ('tech_transfer', 'management', 'Специалист по трансферу технологий', 23),
    ('shared_facility_head', 'management', 'Руководитель ЦКП', 24),
    ('dean', 'management', 'Декан факультета (директор института)', 25),
    ('science_vice_rector', 'management', 'Проректор по науке', 26),
    ('management_other', 'management', 'Другая должность в управлении наукой', 27)
) AS n (code, position_type, name, sort)
WHERE p.code = n.code;

ALTER TABLE positions ADD CONSTRAINT positions_position_type_check
    CHECK (position_type IN ('research', 'teaching', 'early_career', 'management'));

UPDATE saved_searches SET query = regexp_replace(query, '(^|&)type=(phd|masters|internship)(?=&|$)', '\1type=early_career', 'g');
UPDATE saved_searches SET query = regexp_replace(query, '(^|&)type=(admin|project)(?=&|$)', '\1type=management', 'g');

SELECT refresh_vacancy_search(id) FROM vacancies;

UPDATE reference_sources SET
    title = 'Квалификационные характеристики должностей: научные работники (Постановление Президиума РАН от 25.03.2008 № 196; ЕКС, приказ Минтруда России) и ППС (приказ Минздравсоцразвития России от 11.01.2011 № 1н); позиции начинающих исследователей и управление наукой — по практике вузов',
    edition = 'Список составлен по результатам исследования 2026-10-03. Строки «Другая…» не официальные: они нужны, чтобы вакансию можно было опубликовать, если должности нет в списке.',
    checked_on = '2026-10-03'
WHERE catalog = 'positions';

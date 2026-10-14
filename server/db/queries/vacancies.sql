-- Вакансии (срез 5).

-- name: CreateVacancy :one
INSERT INTO vacancies (
    org_id, unit_id, created_by, title, position_code, summary, description, requirements, focus, career_level,
    work_format, region_code, city, housing, rate_percent, salary_from, salary_to, contract_type, contract_months,
    funding_source, funding_note, degree_required, title_required, is_competition, deadline,
    equipment_access, equipment_note, equipment_unit_id, publications_paid, conference_travel, created_at, updated_at
) VALUES (
    @org_id, @unit_id, @created_by, @title, @position_code, @summary, @description, @requirements, @focus, @career_level,
    @work_format, @region_code, @city, @housing, @rate_percent, @salary_from, @salary_to, @contract_type, @contract_months,
    @funding_source, @funding_note, @degree_required, @title_required, @is_competition, @deadline,
    @equipment_access, @equipment_note, @equipment_unit_id, @publications_paid, @conference_travel, @now, @now
) RETURNING id;

-- name: UpdateVacancy :execrows
UPDATE vacancies SET
    unit_id = @unit_id, title = @title, position_code = @position_code, summary = @summary, description = @description,
    requirements = @requirements, focus = @focus, career_level = @career_level, work_format = @work_format,
    region_code = @region_code, city = @city, housing = @housing, rate_percent = @rate_percent,
    salary_from = @salary_from, salary_to = @salary_to, contract_type = @contract_type, contract_months = @contract_months,
    funding_source = @funding_source, funding_note = @funding_note, degree_required = @degree_required,
    title_required = @title_required, is_competition = @is_competition, deadline = @deadline,
    equipment_access = @equipment_access, equipment_note = @equipment_note, equipment_unit_id = @equipment_unit_id,
    publications_paid = @publications_paid, conference_travel = @conference_travel, updated_at = @now
WHERE id = @id;

-- Строка с блокировкой: два одновременных изменения одной вакансии выстроятся в очередь.
-- name: LockVacancy :one
SELECT * FROM vacancies WHERE id = $1 FOR UPDATE;

-- Смена статуса. Срабатывает, только если статус всё ещё from_status (кто-то мог изменить раньше).
-- name: SetVacancyStatus :execrows
UPDATE vacancies SET
    status = @to_status,
    updated_at = @now,
    published_at = CASE WHEN @to_status::text = 'published' AND published_at IS NULL THEN @now ELSE published_at END,
    closed_at = CASE WHEN @to_status::text = 'closed' THEN @now ELSE closed_at END,
    archived_at = CASE WHEN @to_status::text = 'archived' THEN @now ELSE archived_at END
WHERE id = @id AND status = @from_status;

-- name: DeleteDraftVacancy :execrows
DELETE FROM vacancies WHERE id = $1 AND status = 'draft';

-- name: DeleteVacancySpecialties :exec
DELETE FROM vacancy_specialties WHERE vacancy_id = $1;

-- name: AddVacancySpecialties :exec
INSERT INTO vacancy_specialties (vacancy_id, specialty_code) SELECT @vacancy_id, unnest(@codes::text[]);

-- name: GetVacancyView :one
SELECT * FROM vacancy_view WHERE id = $1;

-- Карточки опубликованных вакансий пачкой, в порядке переданных номеров (срез 27). Закрытая или снятая между двумя
-- запросами вакансия пропадает из ответа.
-- name: ListPublishedVacancyViewsByIDs :many
SELECT sqlc.embed(v)
FROM unnest(@ids::uuid[]) WITH ORDINALITY AS u(id, ord)
JOIN vacancy_view v ON v.id = u.id
WHERE v.status = 'published'
ORDER BY u.ord;

-- name: ListVacancySpecialties :many
SELECT vs.vacancy_id, s.code, s.name
FROM vacancy_specialties vs JOIN specialties s ON s.code = vs.specialty_code
WHERE vs.vacancy_id = ANY(@ids::uuid[])
ORDER BY vs.vacancy_id, string_to_array(s.code, '.')::int[];

-- Поиск среди опубликованных вакансий (срез 6). Вакансии с прошедшим сроком подачи не показываются (today — сегодня по Москве).
-- Пустой список или пустая строка у фильтра значит «любые». Найденных берётся не больше row_cap: сервис передаёт предел
-- выдачи + 1 (max_results + 1), и если total получился больше max_results, значит «больше предела»; листать можно только
-- первые max_results (срез 24, D-161).
-- fuzzy включает поиск с опечатками вместо словоформ: сервис включает его, когда точных совпадений нет.
--
-- Слова поиска сначала выбирают подходящие вакансии по индексу (hits: по тексту со словоформами или, с fuzzy, по триграммам
-- названия; только открытые, условие s.open нужно индексам, они построены по ним), и только потом к ним применяются остальные
-- условия. Без этого планировщик, не умея оценить частоту слова, обходил все открытые вакансии и сверял каждую с текстом
-- (около секунды). Если подходящих больше hit_cap, берутся самые свежие: порядок «по совпадению» считается среди них (стоимость
-- не растёт с частотой слова). Без слов hits пуст и не стоит ничего. Порог сходства оператора <% задан на базу в миграции 00021.
-- Устройство: found выбирает первые row_cap подходящих вакансий в нужном порядке (только номера и ключи порядка, без
-- склейки с названиями), page считает число найденных и отрезает страницу, и только страница подтягивает карточки.
-- Ключи порядка k_* пусты, когда порядок не выбран: планировщик (план под значения, plan_cache_mode) выбрасывает их
-- и идёт по индексу порядка «новые сверху», останавливаясь на row_cap-й строке.
-- Отбор (found) идёт по самой таблице vacancies, без соединения с организациями, должностями, регионами и подразделениями
-- (срез 27): они нужны только карточкам страницы, которые берутся в конце; тип позиции и вид организации проверяются
-- подзапросом, только если такой фильтр задан. Раньше каждая из тысячи найденных вакансий тянула четыре соединения.
-- name: SearchVacancies :many
WITH hits AS MATERIALIZED (
  SELECT m.vacancy_id, ts_rank_cd(m.doc, websearch_to_tsquery('russian', @q::text)) AS rank
  FROM (
    SELECT s.vacancy_id, s.doc FROM vacancy_search s
    WHERE @q::text <> '' AND NOT @fuzzy::bool AND s.open AND s.doc @@ websearch_to_tsquery('russian', @q::text)
    ORDER BY s.published_at DESC, s.vacancy_id
    LIMIT @hit_cap
  ) m
  UNION ALL
  SELECT f.vacancy_id, word_similarity(@q::text, f.fuzzy_text) AS rank
  FROM (
    SELECT s.vacancy_id, s.fuzzy_text FROM vacancy_search s
    WHERE @q::text <> '' AND @fuzzy::bool AND s.open AND @q::text <% s.fuzzy_text
    ORDER BY s.published_at DESC, s.vacancy_id
    LIMIT @hit_cap
  ) f
), found AS (
  SELECT fv.id, fv.published_at,
         CASE WHEN @sort::text = 'relevance' AND @q::text <> '' THEN h.rank END AS k_rank,
         CASE WHEN @sort::text = 'deadline' THEN fv.deadline END AS k_deadline,
         CASE WHEN @sort::text = 'salary' THEN COALESCE(fv.salary_to, fv.salary_from) END AS k_pay
  FROM vacancies fv
  LEFT JOIN hits h ON h.vacancy_id = fv.id
  LEFT JOIN vacancy_search s ON s.vacancy_id = fv.id
  WHERE fv.status = 'published'
    AND (fv.deadline IS NULL OR fv.deadline >= @today::date)
    AND (sqlc.narg(org_id)::uuid IS NULL OR fv.org_id = sqlc.narg(org_id)::uuid)
    AND (sqlc.narg(unit_id)::uuid IS NULL OR fv.unit_id = sqlc.narg(unit_id)::uuid)
    AND (@q::text = '' OR h.vacancy_id IS NOT NULL)
    AND (cardinality(@fields::text[]) = 0 OR (s.open AND s.codes && @fields::text[]))
    AND (@region::text = '' OR fv.region_code = @region::text)
    AND (cardinality(@formats::text[]) = 0 OR fv.work_format = ANY(@formats::text[]))
    AND (cardinality(@types::text[]) = 0 OR EXISTS (
           SELECT 1 FROM positions pos WHERE pos.code = fv.position_code AND pos.position_type = ANY(@types::text[])))
    AND (cardinality(@levels::int[]) = 0 OR fv.career_level = ANY(@levels::int[]))
    AND (cardinality(@degrees::text[]) = 0 OR fv.degree_required = ANY(@degrees::text[]))
    AND (cardinality(@org_kinds::text[]) = 0 OR EXISTS (
           SELECT 1 FROM organizations org WHERE org.id = fv.org_id AND org.kind = ANY(@org_kinds::text[])))
    AND (cardinality(@fundings::text[]) = 0 OR fv.funding_source = ANY(@fundings::text[]))
    AND (cardinality(@rates::int[]) = 0 OR fv.rate_percent = ANY(@rates::int[]))
    AND (cardinality(@terms::text[]) = 0 OR (CASE
           WHEN fv.contract_type = 'permanent' THEN 'permanent'
           WHEN fv.contract_months <= 12 THEN 'short'
           WHEN fv.contract_months <= 36 THEN 'medium'
           WHEN fv.contract_months > 36 THEN 'long'
         END) = ANY(@terms::text[]))
    AND (@salary_min::int = 0 OR COALESCE(fv.salary_to, fv.salary_from) >= @salary_min::int)
    AND (NOT @housing::bool OR fv.housing <> 'none')
    AND (NOT @competition::bool OR fv.is_competition)
    AND (NOT @equipment::bool OR fv.equipment_access)
    AND (NOT @publications::bool OR fv.publications_paid)
    AND (NOT @conferences::bool OR fv.conference_travel)
    AND (NOT @no_deadline::bool OR fv.deadline IS NULL)
    AND (sqlc.narg(deadline_to)::date IS NULL OR fv.deadline <= sqlc.narg(deadline_to)::date)
    AND (sqlc.narg(published_after)::timestamptz IS NULL OR fv.published_at > sqlc.narg(published_after)::timestamptz)
    AND (sqlc.narg(published_until)::timestamptz IS NULL OR fv.published_at <= sqlc.narg(published_until)::timestamptz)
  ORDER BY k_rank DESC NULLS LAST, k_deadline ASC NULLS LAST, k_pay DESC NULLS LAST, fv.published_at DESC, fv.id
  LIMIT @row_cap
), page AS (
  SELECT f.id, count(*) OVER () AS total,
         row_number() OVER (ORDER BY f.k_rank DESC NULLS LAST, f.k_deadline ASC NULLS LAST, f.k_pay DESC NULLS LAST, f.published_at DESC, f.id) AS ord
  FROM found f
)
SELECT sqlc.embed(v), p.total
FROM (SELECT * FROM page WHERE ord <= @max_results::int ORDER BY ord LIMIT @row_limit OFFSET @row_offset) p
JOIN vacancy_view v ON v.id = p.id
ORDER BY p.ord;

-- «Мои вакансии»: вся организация (whole_orgs) или только перечисленные подразделения (units).
-- name: ListMyVacancies :many
SELECT * FROM vacancy_view
WHERE (org_id = ANY(@whole_orgs::uuid[]) OR unit_id = ANY(@units::uuid[]))
  AND (@status::text = '' OR status = @status::text)
ORDER BY updated_at DESC, id
LIMIT @row_limit OFFSET @row_offset;

-- name: CountMyVacanciesByStatus :many
SELECT status, count(*) AS total FROM vacancies
WHERE org_id = ANY(@whole_orgs::uuid[]) OR unit_id = ANY(@units::uuid[])
GROUP BY status;

-- Подразделения организации с названиями: куда человек может поставить вакансию.
-- name: ListUnitNames :many
SELECT id, name FROM units WHERE org_id = $1 ORDER BY lower(name), id;

-- Вакансии (срез 5).

-- name: CreateVacancy :one
INSERT INTO vacancies (
    org_id, unit_id, created_by, title, position_code, summary, description, requirements, focus, career_level,
    work_format, region_code, city, housing, rate_percent, salary_from, salary_to, contract_type, contract_months,
    funding_source, funding_note, degree_required, title_required, is_competition, deadline, created_at, updated_at
) VALUES (
    @org_id, @unit_id, @created_by, @title, @position_code, @summary, @description, @requirements, @focus, @career_level,
    @work_format, @region_code, @city, @housing, @rate_percent, @salary_from, @salary_to, @contract_type, @contract_months,
    @funding_source, @funding_note, @degree_required, @title_required, @is_competition, @deadline, @now, @now
) RETURNING id;

-- name: UpdateVacancy :execrows
UPDATE vacancies SET
    unit_id = @unit_id, title = @title, position_code = @position_code, summary = @summary, description = @description,
    requirements = @requirements, focus = @focus, career_level = @career_level, work_format = @work_format,
    region_code = @region_code, city = @city, housing = @housing, rate_percent = @rate_percent,
    salary_from = @salary_from, salary_to = @salary_to, contract_type = @contract_type, contract_months = @contract_months,
    funding_source = @funding_source, funding_note = @funding_note, degree_required = @degree_required,
    title_required = @title_required, is_competition = @is_competition, deadline = @deadline, updated_at = @now
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

-- name: ListVacancySpecialties :many
SELECT vs.vacancy_id, s.code, s.name
FROM vacancy_specialties vs JOIN specialties s ON s.code = vs.specialty_code
WHERE vs.vacancy_id = ANY(@ids::uuid[])
ORDER BY vs.vacancy_id, string_to_array(s.code, '.')::int[];

-- Поиск среди опубликованных вакансий (срез 6). Вакансии с прошедшим сроком подачи не показываются (today — сегодня по Москве).
-- Пустой список или пустая строка у фильтра значит «любые». total — сколько всего нашлось (до LIMIT).
-- fuzzy включает поиск с опечатками вместо словоформ: сервис включает его, когда точных совпадений нет.
-- name: SearchVacancies :many
SELECT sqlc.embed(v), count(*) OVER () AS total
FROM vacancy_view v
JOIN vacancy_search s ON s.vacancy_id = v.id
WHERE v.status = 'published'
  AND (v.deadline IS NULL OR v.deadline >= @today::date)
  AND (sqlc.narg(org_id)::uuid IS NULL OR v.org_id = sqlc.narg(org_id)::uuid)
  AND (sqlc.narg(unit_id)::uuid IS NULL OR v.unit_id = sqlc.narg(unit_id)::uuid)
  AND (@q::text = '' OR CASE WHEN @fuzzy::bool
         THEN word_similarity(@q::text, v.title || ' ' || v.position_name || ' ' || v.org_name) >= 0.35
         ELSE s.doc @@ websearch_to_tsquery('russian', @q::text) END)
  AND (cardinality(@fields::text[]) = 0 OR EXISTS (
         SELECT 1 FROM vacancy_specialties vs, unnest(@fields::text[]) f
         WHERE vs.vacancy_id = v.id AND (vs.specialty_code = f OR vs.specialty_code LIKE f || '.%')))
  AND (@region::text = '' OR v.region_code = @region::text)
  AND (cardinality(@formats::text[]) = 0 OR v.work_format = ANY(@formats::text[]))
  AND (cardinality(@types::text[]) = 0 OR v.position_type = ANY(@types::text[]))
  AND (cardinality(@levels::int[]) = 0 OR v.career_level = ANY(@levels::int[]))
  AND (cardinality(@degrees::text[]) = 0 OR v.degree_required = ANY(@degrees::text[]))
  AND (cardinality(@org_kinds::text[]) = 0 OR v.org_kind = ANY(@org_kinds::text[]))
  AND (cardinality(@fundings::text[]) = 0 OR v.funding_source = ANY(@fundings::text[]))
  AND (cardinality(@rates::int[]) = 0 OR v.rate_percent = ANY(@rates::int[]))
  AND (cardinality(@terms::text[]) = 0 OR (CASE
         WHEN v.contract_type = 'permanent' THEN 'permanent'
         WHEN v.contract_months <= 12 THEN 'short'
         WHEN v.contract_months <= 36 THEN 'medium'
         WHEN v.contract_months > 36 THEN 'long'
       END) = ANY(@terms::text[]))
  AND (@salary_min::int = 0 OR COALESCE(v.salary_to, v.salary_from) >= @salary_min::int)
  AND (NOT @housing::bool OR v.housing <> 'none')
  AND (NOT @competition::bool OR v.is_competition)
  AND (NOT @no_deadline::bool OR v.deadline IS NULL)
  AND (sqlc.narg(deadline_to)::date IS NULL OR v.deadline <= sqlc.narg(deadline_to)::date)
  AND (sqlc.narg(published_after)::timestamptz IS NULL OR v.published_at > sqlc.narg(published_after)::timestamptz)
  AND (sqlc.narg(published_until)::timestamptz IS NULL OR v.published_at <= sqlc.narg(published_until)::timestamptz)
ORDER BY
  CASE WHEN @sort::text = 'relevance' AND @q::text <> '' THEN
    CASE WHEN @fuzzy::bool
      THEN word_similarity(@q::text, v.title || ' ' || v.position_name || ' ' || v.org_name)
      ELSE ts_rank_cd(s.doc, websearch_to_tsquery('russian', @q::text)) END
  END DESC NULLS LAST,
  CASE WHEN @sort::text = 'deadline' THEN v.deadline END ASC NULLS LAST,
  CASE WHEN @sort::text = 'salary' THEN COALESCE(v.salary_to, v.salary_from) END DESC NULLS LAST,
  v.published_at DESC, v.id
LIMIT @row_limit OFFSET @row_offset;

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

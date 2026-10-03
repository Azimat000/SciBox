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

-- Опубликованные вакансии (публично). org_id и unit_id, если нужна одна организация или одно подразделение.
-- name: ListPublishedVacancies :many
SELECT * FROM vacancy_view
WHERE status = 'published'
  AND (sqlc.narg(org_id)::uuid IS NULL OR org_id = sqlc.narg(org_id)::uuid)
  AND (sqlc.narg(unit_id)::uuid IS NULL OR unit_id = sqlc.narg(unit_id)::uuid)
ORDER BY published_at DESC, id
LIMIT @row_limit OFFSET @row_offset;

-- name: CountPublishedVacancies :one
SELECT count(*) FROM vacancy_view
WHERE status = 'published'
  AND (sqlc.narg(org_id)::uuid IS NULL OR org_id = sqlc.narg(org_id)::uuid)
  AND (sqlc.narg(unit_id)::uuid IS NULL OR unit_id = sqlc.narg(unit_id)::uuid);

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

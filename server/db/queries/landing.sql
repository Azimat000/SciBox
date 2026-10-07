-- Числа для главной страницы (срез 12). «Открытая» вакансия — опубликованная, срок подачи которой не прошёл (today — сегодня по Москве),
-- то есть та же, что находит поиск (D-066).

-- name: LandingCounts :one
SELECT
  (SELECT count(*) FROM vacancies v
    WHERE v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= @today::date))::bigint AS open_vacancies,
  (SELECT count(DISTINCT v.org_id) FROM vacancies v
    WHERE v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= @today::date))::bigint AS hiring_organizations,
  (SELECT count(*) FROM profiles p
    WHERE p.visibility = ANY(@modes::text[]) AND p.headline <> '')::bigint AS scientists;

-- Открытые вакансии по областям науки. Вакансия с несколькими специальностями одной области считается один раз,
-- а со специальностями разных областей — в каждой.
-- name: LandingVacanciesByField :many
SELECT f.field_code, f.vacancies
FROM (
  SELECT split_part(vs.specialty_code, '.', 1)::text AS field_code, count(DISTINCT v.id)::bigint AS vacancies
  FROM vacancies v JOIN vacancy_specialties vs ON vs.vacancy_id = v.id
  WHERE v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= @today::date)
  GROUP BY 1
) f
ORDER BY f.field_code::int;

-- Открытые вакансии по видам (научный работник, преподаватель, административный сотрудник, аспирантура, магистратура,
-- проектная работа, стажировка) в порядке справочника должностей.
-- name: LandingVacanciesByType :many
SELECT p.position_type AS position_type, count(*)::bigint AS vacancies
FROM vacancies v JOIN positions p ON p.code = v.position_code
WHERE v.status = 'published' AND (v.deadline IS NULL OR v.deadline >= @today::date)
GROUP BY 1
ORDER BY min(p.sort);

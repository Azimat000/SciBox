-- Если адрес уже занят, строка не создаётся (сервис пробует следующий вариант адреса).
-- name: CreateOrganization :one
INSERT INTO organizations (slug, name, kind, city, website, description, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (slug) DO NOTHING
RETURNING *;

-- name: GetOrganizationBySlug :one
SELECT * FROM organizations WHERE slug = $1;

-- name: UpdateOrganization :one
UPDATE organizations
SET name = $2, kind = $3, city = $4, website = $5, description = $6, updated_at = $7
WHERE id = $1
RETURNING *;

-- Каталог: поиск по названию и городу без учёта регистра, фильтр по типу ('' = любой).
-- name: ListOrganizations :many
SELECT o.id, o.slug, o.name, o.kind, o.city, o.website, o.description,
       (SELECT count(*) FROM units u WHERE u.org_id = o.id)::bigint AS unit_count
FROM organizations o
WHERE (sqlc.arg(kind)::text = '' OR o.kind = sqlc.arg(kind)::text)
  AND (sqlc.arg(pattern)::text = '' OR o.name ILIKE sqlc.arg(pattern)::text OR o.city ILIKE sqlc.arg(pattern)::text)
ORDER BY lower(o.name), o.id
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: CountOrganizations :one
SELECT count(*)::bigint
FROM organizations o
WHERE (sqlc.arg(kind)::text = '' OR o.kind = sqlc.arg(kind)::text)
  AND (sqlc.arg(pattern)::text = '' OR o.name ILIKE sqlc.arg(pattern)::text OR o.city ILIKE sqlc.arg(pattern)::text);

-- Организации человека вместе с его ролью.
-- name: ListOrganizationsOfUser :many
SELECT o.id, o.slug, o.name, o.kind, o.city, m.role
FROM org_members m JOIN organizations o ON o.id = m.org_id
WHERE m.user_id = $1
ORDER BY lower(o.name), o.id;

-- name: GetOrganizationByID :one
SELECT * FROM organizations WHERE id = $1;

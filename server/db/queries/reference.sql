-- Справочники (срез 5). Порядок: по числам в коде (1.2.10 после 1.2.9), регионы по названию.

-- name: ListScienceFields :many
SELECT code, name FROM science_fields ORDER BY code::int;

-- name: ListScienceGroups :many
SELECT code, field_code, name FROM science_groups ORDER BY string_to_array(code, '.')::int[];

-- name: ListSpecialties :many
SELECT code, group_code, name FROM specialties ORDER BY string_to_array(code, '.')::int[];

-- name: ListRegions :many
SELECT code, name FROM regions ORDER BY name;

-- name: ListPositions :many
SELECT code, position_type, name, sort FROM positions ORDER BY sort;

-- name: ListReferenceSources :many
SELECT catalog, title, url, edition, checked_on FROM reference_sources ORDER BY catalog;

-- name: GetPosition :one
SELECT code, position_type, name, sort FROM positions WHERE code = $1;

-- name: RegionExists :one
SELECT EXISTS (SELECT 1 FROM regions WHERE code = $1);

-- Какие из этих кодов специальностей настоящие (остальные отбрасывает проверка).
-- name: ExistingSpecialtyCodes :many
SELECT code FROM specialties WHERE code = ANY($1::text[]);

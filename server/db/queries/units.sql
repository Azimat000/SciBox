-- name: CreateUnit :one
INSERT INTO units (org_id, name, kind, description, topics, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetUnitInOrg :one
SELECT * FROM units WHERE id = $1 AND org_id = $2;

-- Подразделения организации с именем руководителя.
-- name: ListUnits :many
SELECT u.id, u.org_id, u.name, u.kind, u.description, u.topics, u.head_user_id, h.display_name AS head_name
FROM units u LEFT JOIN users h ON h.id = u.head_user_id
WHERE u.org_id = $1
ORDER BY lower(u.name), u.id;

-- name: GetUnitWithHead :one
SELECT u.id, u.org_id, u.name, u.kind, u.description, u.topics, u.head_user_id, h.display_name AS head_name
FROM units u LEFT JOIN users h ON h.id = u.head_user_id
WHERE u.id = $1 AND u.org_id = $2;

-- name: UpdateUnit :one
UPDATE units SET name = $3, kind = $4, description = $5, topics = $6, updated_at = $7
WHERE id = $1 AND org_id = $2
RETURNING *;

-- name: SetUnitHead :execrows
UPDATE units SET head_user_id = $3, updated_at = $4 WHERE id = $1 AND org_id = $2;

-- Назначить руководителя, только если места ещё никто не занимает (для принятия приглашения).
-- name: SetUnitHeadIfVacant :execrows
UPDATE units SET head_user_id = $3, updated_at = $4 WHERE id = $1 AND org_id = $2 AND head_user_id IS NULL;

-- name: DeleteUnit :execrows
DELETE FROM units WHERE id = $1 AND org_id = $2;

-- Подразделения, которыми руководит человек.
-- name: ListUnitIDsHeadedBy :many
SELECT id FROM units WHERE org_id = $1 AND head_user_id = $2 ORDER BY id;

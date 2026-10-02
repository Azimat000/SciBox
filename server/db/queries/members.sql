-- name: AddMember :exec
INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, $3, $4);

-- name: GetMember :one
SELECT * FROM org_members WHERE org_id = $1 AND user_id = $2;

-- Сотрудники организации с именем и почтой (почту видит только владелец: решает сервис).
-- name: ListMembers :many
SELECT m.user_id, m.role, m.joined_at, u.display_name, u.email::text AS email
FROM org_members m JOIN users u ON u.id = m.user_id
WHERE m.org_id = $1
ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'hr' THEN 1 ELSE 2 END, lower(u.display_name), m.user_id;

-- name: SetMemberRole :execrows
UPDATE org_members SET role = $3 WHERE org_id = $1 AND user_id = $2;

-- name: RemoveMember :execrows
DELETE FROM org_members WHERE org_id = $1 AND user_id = $2;

-- Владельцы с блокировкой строк: два одновременных запроса не смогут оба убрать «последнего» владельца.
-- name: LockOwners :many
SELECT user_id FROM org_members WHERE org_id = $1 AND role = 'owner' FOR UPDATE;

-- Человек перестал быть сотрудником: он больше не руководит подразделениями этой организации.
-- name: ClearHeadOfUser :exec
UPDATE units SET head_user_id = NULL, updated_at = $3 WHERE org_id = $1 AND head_user_id = $2;

-- Работает ли человек с этой почтой в организации.
-- name: GetMemberIDByEmail :one
SELECT m.user_id FROM org_members m JOIN users u ON u.id = m.user_id WHERE m.org_id = $1 AND u.email = $2;

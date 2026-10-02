-- name: CreateInvitation :one
INSERT INTO org_invitations (org_id, email, role, unit_id, token_hash, invited_by, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id;

-- Новое приглашение той же почте отменяет прежние, ещё не принятые.
-- name: RevokePendingInvitations :exec
UPDATE org_invitations SET revoked_at = sqlc.arg(at)::timestamptz
WHERE org_id = sqlc.arg(org_id) AND email = sqlc.arg(email) AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: ListPendingInvitations :many
SELECT i.id, i.email::text AS email, i.role, i.unit_id, un.name AS unit_name, i.created_at, i.expires_at
FROM org_invitations i LEFT JOIN units un ON un.id = i.unit_id
WHERE i.org_id = sqlc.arg(org_id) AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > sqlc.arg(now)::timestamptz
ORDER BY i.created_at DESC, i.id;

-- name: RevokeInvitation :execrows
UPDATE org_invitations SET revoked_at = sqlc.arg(at)::timestamptz
WHERE id = sqlc.arg(id) AND org_id = sqlc.arg(org_id) AND accepted_at IS NULL AND revoked_at IS NULL;

-- Посмотреть живое приглашение по ссылке, не принимая его.
-- name: PeekInvitation :one
SELECT i.id, i.org_id, i.email::text AS email, i.role, i.unit_id, un.name AS unit_name,
       o.slug AS org_slug, o.name AS org_name
FROM org_invitations i
JOIN organizations o ON o.id = i.org_id
LEFT JOIN units un ON un.id = i.unit_id
WHERE i.token_hash = sqlc.arg(token_hash) AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > sqlc.arg(now)::timestamptz;

-- Принять приглашение по ссылке: «гашение» одним запросом, чтобы две одновременные попытки не прошли обе.
-- name: ConsumeInvitation :one
UPDATE org_invitations SET accepted_at = sqlc.arg(now)::timestamptz
WHERE token_hash = sqlc.arg(token_hash) AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
RETURNING id, org_id, role, unit_id, email::text AS email;

-- Живые приглашения на почту человека (показываем их ему в «Организации», не только в письме).
-- name: ListInvitationsForEmail :many
SELECT i.id, i.role, i.unit_id, un.name AS unit_name, o.slug AS org_slug, o.name AS org_name, i.created_at, i.expires_at
FROM org_invitations i
JOIN organizations o ON o.id = i.org_id
LEFT JOIN units un ON un.id = i.unit_id
WHERE i.email = sqlc.arg(email) AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > sqlc.arg(now)::timestamptz
ORDER BY i.created_at DESC, i.id;

-- Приглашения, срок которых вышел давно (before), больше не нужны.
-- name: DeleteOldInvitations :exec
DELETE FROM org_invitations WHERE expires_at <= $1;

-- То же по номеру приглашения: так принимают приглашение из списка «Мои приглашения», где ссылки нет.
-- name: ConsumeInvitationByID :one
UPDATE org_invitations SET accepted_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id) AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
RETURNING id, org_id, role, unit_id, email::text AS email;

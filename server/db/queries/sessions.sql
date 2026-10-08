-- reauth_at: при входе по паролю сессия сразу считается «со свежим паролем» (тот же момент, что created_at);
-- при входе по ссылке из письма пароля не было, reauth_at пустой.
-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at, user_agent, ip, reauth_at)
VALUES ($1, $2, $3, $3, $4, $5, $6, sqlc.narg(reauth_at))
RETURNING id;

-- Стёртый аккаунт (deleted_at) не входит, даже если сессия почему-то осталась.
-- name: GetSessionByTokenHash :one
SELECT s.id AS session_id, s.last_seen_at, s.expires_at, s.reauth_at,
       u.id AS user_id, u.email, u.display_name, u.email_confirmed_at, u.created_at AS user_created_at,
       u.deletion_due_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND u.deleted_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = $2, expires_at = $3 WHERE id = $1;

-- name: MarkSessionReauth :exec
UPDATE sessions SET reauth_at = $2 WHERE id = $1;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteOtherUserSessions :exec
DELETE FROM sessions WHERE user_id = $1 AND id <> $2;

-- Завершить одну свою сессию: чужую не найдёт (0 строк).
-- name: DeleteUserSession :execrows
DELETE FROM sessions WHERE user_id = $1 AND id = $2;

-- Живые сессии человека, последние сверху (список «Где вы вошли»).
-- name: ListUserSessions :many
SELECT id, created_at, last_seen_at, user_agent
FROM sessions
WHERE user_id = $1 AND expires_at > sqlc.arg(now)::timestamptz
ORDER BY last_seen_at DESC, created_at DESC, id
LIMIT 50;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= $1;

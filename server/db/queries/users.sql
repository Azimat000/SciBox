-- name: CreateUser :one
INSERT INTO users (email, display_name, password_hash, privacy_consent_at, privacy_policy_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $4, $4)
ON CONFLICT (email) DO NOTHING
RETURNING *;

-- Повторная регистрация на непотверждённую почту: побеждает последний, кто зарегистрировался.
-- Подтвердить такую почту всё равно сможет только её настоящий владелец.
-- name: ReregisterUnconfirmedUser :one
UPDATE users
SET display_name = $2, password_hash = $3, privacy_consent_at = $4, privacy_policy_version = $5, updated_at = $4
WHERE id = $1 AND email_confirmed_at IS NULL
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: ConfirmUserEmail :exec
UPDATE users
SET email_confirmed_at = COALESCE(email_confirmed_at, sqlc.arg(at)::timestamptz), updated_at = sqlc.arg(at)::timestamptz
WHERE id = sqlc.arg(id);

-- name: UpdateUserName :one
UPDATE users SET display_name = $2, updated_at = $3 WHERE id = $1 RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1;

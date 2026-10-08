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
WHERE id = $1 AND email_confirmed_at IS NULL AND deleted_at IS NULL
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

-- Сменить почту аккаунта. Занятый адрес даёт ошибку уникальности (её разбирает сервис).
-- name: ChangeUserEmail :one
UPDATE users SET email = sqlc.arg(email)::citext, updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;

-- Есть ли живой аккаунт с этой почтой (кроме самого человека).
-- name: EmailTakenByOther :one
SELECT EXISTS (SELECT 1 FROM users WHERE email = sqlc.arg(email)::citext AND id <> sqlc.arg(id)) AS taken;

-- Просьба удалить аккаунт: срок, после которого данные сотрутся. Повторная просьба срок не сдвигает.
-- name: ScheduleUserDeletion :one
UPDATE users SET deletion_due_at = COALESCE(deletion_due_at, sqlc.arg(due)::timestamptz), updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING deletion_due_at;

-- name: CancelUserDeletion :execrows
UPDATE users SET deletion_due_at = NULL, updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id) AND deletion_due_at IS NOT NULL AND deleted_at IS NULL;

-- Аккаунты, чей срок отмены прошёл. SKIP LOCKED: две копии сервера не возьмут один аккаунт.
-- name: ClaimDueDeletions :many
SELECT id FROM users
WHERE deletion_due_at IS NOT NULL AND deletion_due_at <= sqlc.arg(now)::timestamptz AND deleted_at IS NULL
ORDER BY deletion_due_at
LIMIT sqlc.arg(max_rows)
FOR UPDATE SKIP LOCKED;

-- name: CreateAuthToken :exec
INSERT INTO auth_tokens (user_id, purpose, token_hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- Ссылка подтверждения почты, привязанная к браузеру регистрации (хеш случайной cookie; срез 29).
-- name: CreateBoundAuthToken :exec
INSERT INTO auth_tokens (user_id, purpose, token_hash, created_at, expires_at, binding_hash)
VALUES ($1, $2, $3, $4, $5, $6);

-- Погашение ссылки подтверждения: вместе с привязкой, чтобы решить, входить ли в аккаунт.
-- name: ConsumeConfirmToken :one
UPDATE auth_tokens SET used_at = sqlc.arg(now)::timestamptz
WHERE token_hash = sqlc.arg(token_hash) AND purpose = 'confirm_email'
  AND used_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
RETURNING user_id, binding_hash;

-- Старые неиспользованные ссылки того же назначения перестают работать.
-- name: SupersedeAuthTokens :exec
UPDATE auth_tokens SET used_at = sqlc.arg(at)::timestamptz
WHERE user_id = sqlc.arg(user_id) AND purpose = sqlc.arg(purpose) AND used_at IS NULL;

-- Ссылка одноразовая: проверка и «погашение» одним запросом, чтобы два одновременных запроса не прошли оба.
-- name: ConsumeAuthToken :one
UPDATE auth_tokens SET used_at = sqlc.arg(now)::timestamptz
WHERE token_hash = sqlc.arg(token_hash) AND purpose = sqlc.arg(purpose)
  AND used_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
RETURNING user_id;

-- name: DeleteExpiredAuthTokens :exec
DELETE FROM auth_tokens WHERE expires_at <= $1;

-- Посмотреть ссылку, не погашая её (чтобы проверить новый пароль до того, как ссылка сгорит).
-- name: PeekAuthToken :one
SELECT u.id AS user_id, u.email
FROM auth_tokens t JOIN users u ON u.id = t.user_id
WHERE t.token_hash = sqlc.arg(token_hash) AND t.purpose = sqlc.arg(purpose)
  AND t.used_at IS NULL AND t.expires_at > sqlc.arg(now)::timestamptz;

-- Когда последний раз выдавали ссылку этого назначения (пауза между письмами).
-- name: LatestAuthTokenAt :one
SELECT COALESCE(max(created_at), 'epoch'::timestamptz)::timestamptz AS at
FROM auth_tokens WHERE user_id = $1 AND purpose = $2;

-- Ссылка смены почты (change_email) или отката (revert_email): new_email — какой станет почта аккаунта.
-- name: CreateEmailToken :exec
INSERT INTO auth_tokens (user_id, purpose, token_hash, created_at, expires_at, new_email)
VALUES ($1, $2, $3, $4, $5, sqlc.arg(new_email)::citext);

-- Погасить ссылку смены почты одним запросом (как ConsumeAuthToken) и узнать адрес.
-- name: ConsumeEmailToken :one
UPDATE auth_tokens SET used_at = sqlc.arg(now)::timestamptz
WHERE token_hash = sqlc.arg(token_hash) AND purpose = sqlc.arg(purpose)
  AND used_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
RETURNING user_id, new_email::text AS new_email;

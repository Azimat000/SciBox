-- Браузер уже знаком: обновить время. 0 строк — браузер новый для этого человека.
-- name: TouchKnownDevice :execrows
UPDATE known_devices SET last_seen_at = $3 WHERE user_id = $1 AND device_hash = $2;

-- name: AddKnownDevice :exec
INSERT INTO known_devices (user_id, device_hash, first_seen_at, last_seen_at)
VALUES ($1, $2, $3, $3)
ON CONFLICT (user_id, device_hash) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at;

-- Браузеры, с которых не входили дольше срока cookie, забываются.
-- name: DeleteOldKnownDevices :exec
DELETE FROM known_devices WHERE last_seen_at <= $1;

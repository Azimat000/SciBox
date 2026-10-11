-- +goose Up
-- Неактивные аккаунты (срез 29, D-192, T-38): данные не хранят дольше цели обработки (152-ФЗ). Аккаунт, в который не
-- входили 3 года, получает письмо за 30 дней и затем стирается так же, как при просьбе человека.
-- Существующие аккаунты получают сегодняшнюю дату: никого не удалит в день выкладки.
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_active_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE users ADD COLUMN IF NOT EXISTS inactive_warned_at timestamptz;
CREATE INDEX IF NOT EXISTS users_inactive_idx ON users (last_active_at) WHERE deleted_at IS NULL AND email_confirmed_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS users_inactive_idx;
ALTER TABLE users DROP COLUMN IF EXISTS inactive_warned_at;
ALTER TABLE users DROP COLUMN IF EXISTS last_active_at;

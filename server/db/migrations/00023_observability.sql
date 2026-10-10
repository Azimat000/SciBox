-- +goose Up
-- Метрики очереди писем (срез 26) считают недоставленные письма каждые несколько секунд из каждой копии. Без индекса это
-- чтение всей таблицы вместе с отправленными письмами; с частичным индексом по недоставленным (их немного, живут 30
-- дней) подсчёт идёт по нескольким строкам. Ждущие письма уже покрывает outbox_due_idx.
CREATE INDEX IF NOT EXISTS outbox_failed_idx ON outbox (failed_at) WHERE failed_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS outbox_failed_idx;

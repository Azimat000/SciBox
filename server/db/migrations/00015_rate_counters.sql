-- Счётчики ограничения частоты для публичных и тяжёлых адресов (срез 19, T-07, D-141).
-- Один счётчик на вид лимита, ключ (IP-адрес гостя или аккаунт) и окно времени: запрос увеличивает число
-- одной командой INSERT … ON CONFLICT, поэтому лимит общий для всех копий сервера. Таблица без журнала (UNLOGGED):
-- запись быстрее, а после сбоя базы счётчики обнуляются, что для минутных лимитов не страшно.

-- +goose Up
CREATE UNLOGGED TABLE rate_counters (
    kind         text        NOT NULL,
    key          text        NOT NULL,
    window_start timestamptz NOT NULL,
    hits         integer     NOT NULL CHECK (hits > 0),
    PRIMARY KEY (kind, key, window_start)
);
CREATE INDEX rate_counters_window_idx ON rate_counters (window_start);

-- +goose Down
DROP TABLE rate_counters;

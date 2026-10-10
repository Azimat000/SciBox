-- Срез 25 (D-167, T-23): общее расписание фоновых задач для всех копий сервера и воркера.
-- Одна строка на задачу «на весь кластер» (чистка, рассылка по поискам, напоминания, проверка осиротевших файлов):
-- копия, которая успела занять задачу, исполняет её, остальные видят «ещё не пора» или «уже занята». После сбоя пауза
-- растёт и общая для всех копий. Таблица маленькая (по строке на задачу), в ней нет персональных данных.

-- +goose Up
CREATE TABLE job_runs (
    name         text        PRIMARY KEY,
    -- Когда задачу пора запускать снова.
    next_run_at  timestamptz NOT NULL,
    -- Пока задача занята копией, до этого момента другая её не возьмёт; упавшая копия не держит задачу вечно.
    leased_until timestamptz,
    holder       text        NOT NULL DEFAULT '',
    started_at   timestamptz,
    finished_at  timestamptz,
    last_ok_at   timestamptz,
    -- Сколько проходов подряд закончились ошибкой; нужно для нарастающей паузы.
    failures     integer     NOT NULL DEFAULT 0 CHECK (failures >= 0),
    last_error   text        NOT NULL DEFAULT '',
    runs         bigint      NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE job_runs;

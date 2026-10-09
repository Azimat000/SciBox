-- Файлы откликов переезжают из базы в S3-совместимое хранилище (срез 23, T-21, D-157).
--
-- Делается в два шага без остановки сервера:
--  1. Эта миграция добавляет столбцы для хранилища и проверки на вирусы и переименовывает `data` в `legacy_data`.
--     Новые строки хранят только ключ объекта (`storage_key`), контрольную сумму и итог проверки; содержимое в базу
--     больше не пишется (проверка `application_files_no_content`: она не касается старых строк, пока их не трогают,
--     а любая запись в строку требует `legacy_data IS NULL`).
--  2. Фоновая задача `filework` переносит старые файлы в хранилище, сверяя SHA-256 после чтения обратно, и в одном
--     запросе записывает ключ и обнуляет `legacy_data`. Сам столбец удаляется в срезе 28 (после выкладки и проверки).
-- Старые файлы никогда не проверялись на вирусы: они получают статус `pending` и недоступны, пока не будут проверены.

-- +goose Up

ALTER TABLE application_files RENAME COLUMN data TO legacy_data;
ALTER TABLE application_files ALTER COLUMN legacy_data DROP NOT NULL;

ALTER TABLE application_files
    ADD COLUMN storage_key    text,
    ADD COLUMN sha256         bytea,
    ADD COLUMN scan_status    text        NOT NULL DEFAULT 'pending',
    ADD COLUMN scanned_at     timestamptz,
    ADD COLUMN scan_signature text,
    ADD COLUMN scan_attempts  integer     NOT NULL DEFAULT 0,
    ADD COLUMN scan_next_at   timestamptz;

ALTER TABLE application_files
    ADD CONSTRAINT application_files_scan_status CHECK (scan_status IN ('pending', 'clean', 'infected')),
    ADD CONSTRAINT application_files_sha256_len CHECK (sha256 IS NULL OR octet_length(sha256) = 32),
    ADD CONSTRAINT application_files_signature_len CHECK (scan_signature IS NULL OR char_length(scan_signature) <= 200),
    -- Проверенный файл имеет дату проверки; заражённый не хранит содержимого нигде.
    ADD CONSTRAINT application_files_clean_has_date CHECK (scan_status <> 'clean' OR scanned_at IS NOT NULL),
    ADD CONSTRAINT application_files_infected_is_gone CHECK (scan_status <> 'infected' OR (storage_key IS NULL AND legacy_data IS NULL)),
    -- Содержимое есть либо в хранилище, либо (до переноса) в старом столбце; без содержимого бывает только заражённый.
    ADD CONSTRAINT application_files_has_content CHECK (storage_key IS NOT NULL OR legacy_data IS NOT NULL OR scan_status = 'infected'),
    -- Содержимое в базу больше не пишется. NOT VALID: старые строки не проверяются, пока их не изменят.
    ADD CONSTRAINT application_files_no_content CHECK (legacy_data IS NULL) NOT VALID;

CREATE UNIQUE INDEX application_files_storage_key_idx ON application_files (storage_key) WHERE storage_key IS NOT NULL;
-- Фоновая проверка: файлы, которым пора на проверку.
CREATE INDEX application_files_scan_due_idx ON application_files (scan_next_at NULLS FIRST, created_at) WHERE scan_status = 'pending' AND storage_key IS NOT NULL;
-- Перенос старых файлов: пустой индекс, когда переносить нечего.
CREATE INDEX application_files_legacy_idx ON application_files (id) WHERE legacy_data IS NOT NULL;

-- Очередь на удаление объектов из хранилища. Стирание аккаунта кладёт сюда ключи в той же транзакции, что и удаляет
-- строки, а фоновая задача удаляет объекты и вычёркивает ключи: хранилище недоступно — ключи ждут, ничего не теряется.
CREATE TABLE blob_deletions (
    storage_key text        PRIMARY KEY,
    queued_at   timestamptz NOT NULL
);
CREATE INDEX blob_deletions_queued_idx ON blob_deletions (queued_at);

-- +goose Down

-- Откат возвращает содержимое в базу только там, где оно ещё там. Строки, чьё содержимое живёт лишь в хранилище,
-- удаляются (объекты остаются в хранилище, их уберёт чистка осиротевших): как и откат таблицы в 00008, это потеря
-- данных, поэтому `migrate down` — команда администратора, не часть выкладки.
DELETE FROM application_files WHERE legacy_data IS NULL;
DROP TABLE blob_deletions;
DROP INDEX application_files_legacy_idx;
DROP INDEX application_files_scan_due_idx;
DROP INDEX application_files_storage_key_idx;
ALTER TABLE application_files
    DROP CONSTRAINT application_files_no_content,
    DROP CONSTRAINT application_files_has_content,
    DROP CONSTRAINT application_files_infected_is_gone,
    DROP CONSTRAINT application_files_clean_has_date,
    DROP CONSTRAINT application_files_signature_len,
    DROP CONSTRAINT application_files_sha256_len,
    DROP CONSTRAINT application_files_scan_status;
ALTER TABLE application_files
    DROP COLUMN scan_next_at,
    DROP COLUMN scan_attempts,
    DROP COLUMN scan_signature,
    DROP COLUMN scanned_at,
    DROP COLUMN scan_status,
    DROP COLUMN sha256,
    DROP COLUMN storage_key;
ALTER TABLE application_files ALTER COLUMN legacy_data SET NOT NULL;
ALTER TABLE application_files RENAME COLUMN legacy_data TO data;

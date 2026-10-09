-- Фоновая работа с файлами (срез 23): проверка на вирусы, перенос старых файлов, чистка осиротевших объектов.

-- Берёт пачку файлов, которым пора на проверку, и «арендует» их до lease_until: если задача упадёт, файлы вернутся
-- в очередь сами. Две копии сервера не берут одни и те же строки.
-- name: ClaimFilesToScan :many
WITH due AS (
    SELECT d.id FROM application_files d
    WHERE d.scan_status = 'pending' AND d.storage_key IS NOT NULL AND (d.scan_next_at IS NULL OR d.scan_next_at <= sqlc.arg(now)::timestamptz)
    ORDER BY d.scan_next_at NULLS FIRST, d.created_at
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
)
UPDATE application_files f SET scan_next_at = sqlc.arg(lease_until)::timestamptz, scan_attempts = f.scan_attempts + 1
FROM due WHERE f.id = due.id
RETURNING f.id, f.storage_key, f.scan_attempts;

-- name: MarkFileClean :execrows
UPDATE application_files SET scan_status = 'clean', scanned_at = sqlc.arg(now)::timestamptz, scan_next_at = NULL
WHERE id = @id AND storage_key = @storage_key AND scan_status = 'pending';

-- Заражённый файл: ключ стирается из строки (содержимое удаляется из хранилища следом), сигнатура остаётся для журнала.
-- name: MarkFileInfected :execrows
UPDATE application_files SET scan_status = 'infected', scanned_at = sqlc.arg(now)::timestamptz, scan_signature = sqlc.arg(signature)::text, storage_key = NULL, scan_next_at = NULL
WHERE id = @id AND storage_key = @storage_key AND scan_status = 'pending';

-- Проверить не удалось: попробовать позже.
-- name: DeferFileScan :exec
UPDATE application_files SET scan_next_at = sqlc.arg(next_at)::timestamptz WHERE id = @id AND scan_status = 'pending';

-- Следующий старый файл с содержимым в базе (под блокировкой строки, чтобы две копии не переносили один файл).
-- name: NextLegacyFile :one
SELECT id, name, size, legacy_data FROM application_files
WHERE legacy_data IS NOT NULL AND storage_key IS NULL
ORDER BY id LIMIT 1
FOR UPDATE SKIP LOCKED;

-- Запись ключа и обнуление содержимого — одним запросом: ограничение no_content разрешает запись в строку только так.
-- name: FinishLegacyFile :execrows
UPDATE application_files SET storage_key = @storage_key, sha256 = @sha256, legacy_data = NULL
WHERE id = @id AND legacy_data IS NOT NULL AND storage_key IS NULL;

-- Сколько старых файлов ещё ждёт переноса.
-- name: CountLegacyFiles :one
SELECT count(*)::bigint FROM application_files WHERE legacy_data IS NOT NULL;

-- Из этих ключей оставляет те, на которые есть строка в базе (остальные объекты осиротели).
-- name: ExistingFileKeys :many
SELECT storage_key FROM application_files WHERE storage_key = ANY(@keys::text[]);

-- Сколько файлов ждёт проверки (для журнала фоновой задачи).
-- name: CountPendingFiles :one
SELECT count(*)::bigint FROM application_files WHERE scan_status = 'pending' AND storage_key IS NOT NULL;

-- Очередь удаления объектов (стирание аккаунта кладёт сюда ключи): пачка под блокировкой, вычёркивается после удаления.
-- name: ClaimBlobDeletions :many
SELECT storage_key FROM blob_deletions ORDER BY queued_at, storage_key LIMIT @lim FOR UPDATE SKIP LOCKED;

-- name: DoneBlobDeletions :exec
DELETE FROM blob_deletions WHERE storage_key = ANY(@keys::text[]);

-- name: CountBlobDeletions :one
SELECT count(*)::bigint FROM blob_deletions;

-- Кто владелец файла и что за отклик: нужно, чтобы сообщить об отвергнутом файле.
-- name: GetFileNotice :one
SELECT f.name, f.kind, a.user_id, a.id AS application_id, v.title AS vacancy_title
FROM application_files f
JOIN applications a ON a.id = f.application_id
JOIN vacancies v ON v.id = a.vacancy_id
WHERE f.id = $1;

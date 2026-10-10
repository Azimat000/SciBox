-- name: ServerVersion :one
SELECT current_setting('server_version')::text AS server_version;

-- Размер базы и предел соединений для метрик (срез 26). Число занятых соединений читает opsmetrics запросом к
-- pg_stat_activity напрямую: системные представления sqlc не знает.
-- name: DatabaseStats :one
SELECT
    pg_database_size(current_database())::bigint AS size_bytes,
    current_setting('max_connections')::bigint AS max_connections;

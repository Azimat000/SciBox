-- name: ServerVersion :one
SELECT current_setting('server_version')::text AS server_version;

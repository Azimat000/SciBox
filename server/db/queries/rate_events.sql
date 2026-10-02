-- name: RecordRateEvent :exec
INSERT INTO rate_events (kind, key, at) VALUES ($1, $2, $3);

-- Сколько событий этого вида по этому ключу после момента since и когда было самое раннее из них.
-- name: CountRateEvents :one
SELECT count(*)::bigint AS events, COALESCE(min(at), 'epoch'::timestamptz)::timestamptz AS oldest
FROM rate_events WHERE kind = $1 AND key = $2 AND at > $3;

-- name: ClearRateEvents :exec
DELETE FROM rate_events WHERE kind = $1 AND key = $2;

-- name: DeleteOldRateEvents :exec
DELETE FROM rate_events WHERE at <= $1;

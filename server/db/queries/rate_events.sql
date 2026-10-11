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

-- Замок на ключ счётчика до конца транзакции: проверка «не больше N» и запись события идут одна за другой, а не вместе
-- с другими такими же запросами (срез 29, CON-1, CON-2). Без замка пачка параллельных запросов проходит проверку с нулём.
-- name: LockRateKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(key)::text, 0));

-- Возвращает одну попытку (вход удался: не считаем его в лимит по адресу).
-- name: RefundRateEvent :exec
DELETE FROM rate_events WHERE ctid = (
    SELECT e.ctid FROM rate_events e WHERE e.kind = sqlc.arg(kind)::text AND e.key = sqlc.arg(key)::text ORDER BY e.at DESC LIMIT 1
);

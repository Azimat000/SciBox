-- Засчитать запрос в окне и вернуть, сколько их стало (срез 19).
-- name: HitRateCounter :one
INSERT INTO rate_counters (kind, key, window_start, hits) VALUES ($1, $2, $3, 1)
ON CONFLICT (kind, key, window_start) DO UPDATE SET hits = rate_counters.hits + 1
RETURNING hits;

-- name: DeleteOldRateCounters :exec
DELETE FROM rate_counters WHERE window_start < $1;

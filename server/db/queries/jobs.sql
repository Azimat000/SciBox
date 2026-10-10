-- Расписание фоновых задач на весь кластер (срез 25, T-23). Одна строка на задачу: имя, когда пора, кто держит.

-- Занять задачу. Первая копия создаёт строку и сразу получает задачу; дальше задача берётся, только если пора и она не занята
-- (или аренда истекла: копия упала). Нет строки в ответе — не пора или занята другой копией. В ответе число сбоев подряд.
-- name: ClaimJob :one
INSERT INTO job_runs (name, next_run_at, leased_until, holder, started_at, runs)
VALUES (@name, @now::timestamptz, @leased_until::timestamptz, @holder, @now::timestamptz, 1)
ON CONFLICT (name) DO UPDATE SET
    leased_until = EXCLUDED.leased_until, holder = EXCLUDED.holder, started_at = EXCLUDED.started_at, runs = job_runs.runs + 1
WHERE job_runs.next_run_at <= @now::timestamptz AND (job_runs.leased_until IS NULL OR job_runs.leased_until <= @now::timestamptz)
RETURNING failures;

-- Задача выполнена: следующий запуск через обычный срок. Строка меняется, только если задачу всё ещё держит эта копия.
-- name: FinishJobOK :execrows
UPDATE job_runs SET next_run_at = @next_run_at::timestamptz, leased_until = NULL, holder = '', finished_at = @now::timestamptz, last_ok_at = @now::timestamptz,
                    failures = 0, last_error = ''
WHERE name = @name AND holder = @holder;

-- Задача упала: следующий запуск после нарастающей паузы.
-- name: FinishJobFailed :execrows
UPDATE job_runs SET next_run_at = @next_run_at::timestamptz, leased_until = NULL, holder = '', finished_at = @now::timestamptz,
                    failures = failures + 1, last_error = @last_error
WHERE name = @name AND holder = @holder;

-- Копию остановили посреди задачи: задача возвращается другим копиям сразу и сбоем не считается.
-- name: ReleaseJob :execrows
UPDATE job_runs SET next_run_at = @now::timestamptz, leased_until = NULL, holder = '', finished_at = @now::timestamptz
WHERE name = @name AND holder = @holder;

-- name: ListJobRuns :many
SELECT name, next_run_at, leased_until, holder, started_at, finished_at, last_ok_at, failures, last_error, runs
FROM job_runs ORDER BY name;

-- Время базы: по нему копия сверяет свои часы. Расписание сравнивает сроки с часами копии, и копия с часами, ушедшими на
-- дни вперёд, могла бы поставить задачу «на потом» для всех (срез 25).
-- name: DatabaseNow :one
SELECT now()::timestamptz;

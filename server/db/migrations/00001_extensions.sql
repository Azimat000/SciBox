-- Расширения PostgreSQL, нужные следующим срезам:
-- citext: регистронезависимые адреса почты (аккаунты, срез 3);
-- pg_trgm: нечёткий поиск по названиям (поиск вакансий, срез 6).

-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS citext;

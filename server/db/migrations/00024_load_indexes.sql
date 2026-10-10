-- +goose Up
-- Числа главной (срез 27) считали вакансии и профили чтением таблиц: на половине объёма «вся наука РФ» это 1,2 секунды
-- (24 тысячи страниц профилей с диска). Частичные индексы, в которых есть всё нужное счёту, дают подсчёт по индексу без
-- обращения к таблицам. На большой базе в бою индексы создаются заранее командой CREATE INDEX CONCURRENTLY (docs/SCALING.md).
CREATE INDEX IF NOT EXISTS profiles_visible_idx ON profiles (visibility) WHERE headline <> '';

-- Порядок «новые сверху» для открытых вакансий (00021) получает срок и организацию внутри индекса: подсчёт открытых
-- вакансий и организаций идёт по индексу без таблицы. Отдельный индекс по сроку не годится: планировщик начинал брать
-- им все открытые вакансии вместо хода по порядку «новые сверху» (поиск без слов стал в десять раз медленнее).
DROP INDEX IF EXISTS vacancies_open_order_idx;
CREATE INDEX vacancies_open_order_idx ON vacancies (published_at DESC, id) INCLUDE (deadline, org_id) WHERE status = 'published';

-- +goose Down
DROP INDEX IF EXISTS vacancies_open_order_idx;
CREATE INDEX vacancies_open_order_idx ON vacancies (published_at DESC, id) WHERE status = 'published';
DROP INDEX IF EXISTS profiles_visible_idx;

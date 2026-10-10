-- Срез 24 (D-161): поиск вакансий смотрит только открытые вакансии, а таких около десятой части (остальные закрыты и в архиве
-- за годы). Индекс по тексту всех вакансий отдавал тысячи чужих строк, которые потом отбрасывались по статусу. Теперь у
-- строки текста есть отметка «опубликована», а индекс по тексту построен только по таким строкам.
--
-- Боевая выкладка: индекс сначала создать вручную CREATE INDEX CONCURRENTLY с тем же именем (docs/SCALING.md).

-- +goose Up

-- Порог сходства для поиска с опечатками: оператор <% (индекс по триграммам) сравнивает с этим значением. Было 0,35 (D-065):
-- на таком пороге одно искажённое слово находило треть профилей и сотни вакансий, и поиск с опечатками работал сотни
-- миллисекунд; на 0,5 опечатка в одну-две буквы всё так же находит исходное слово, а случайные созвучия отсеиваются (D-163).
-- Порог задан на базу, поэтому не зависит от того, каким пулом открыто соединение.
-- +goose StatementBegin
DO $$ BEGIN EXECUTE format('ALTER DATABASE %I SET pg_trgm.word_similarity_threshold = 0.5', current_database()); END $$;
-- +goose StatementEnd
ALTER TABLE vacancy_search ADD COLUMN open boolean NOT NULL DEFAULT false;
-- Дата публикации нужна поиску по словам: из тысяч подходящих текстов он берёт самые свежие (D-161), не заглядывая в таблицу вакансий.
ALTER TABLE vacancy_search ADD COLUMN published_at timestamptz;
-- Название, должность и организация для поиска с опечатками (триграммы): раньше сходство считалось по всем открытым вакансиям.
ALTER TABLE vacancy_search ADD COLUMN fuzzy_text text NOT NULL DEFAULT '';
-- Коды специальностей вакансии вместе с кодами групп и областей (1.4.4 даёт 1, 1.4 и 1.4.4): фильтр «область науки» идёт по индексу.
ALTER TABLE vacancy_search ADD COLUMN codes text[] NOT NULL DEFAULT '{}';

UPDATE vacancy_search s SET open = (v.status = 'published'), published_at = v.published_at,
       fuzzy_text = v.title || ' ' || p.name || ' ' || o.name,
       codes = COALESCE((SELECT array_agg(DISTINCT c) FROM vacancy_specialties vs,
                             LATERAL unnest(ARRAY[split_part(vs.specialty_code, '.', 1),
                                                  split_part(vs.specialty_code, '.', 1) || '.' || split_part(vs.specialty_code, '.', 2),
                                                  vs.specialty_code]) c
                          WHERE vs.vacancy_id = v.id), '{}')
  FROM vacancies v JOIN positions p ON p.code = v.position_code JOIN organizations o ON o.id = v.org_id
 WHERE v.id = s.vacancy_id;

CREATE INDEX IF NOT EXISTS vacancy_search_open_doc_idx ON vacancy_search USING gin (doc) WHERE open;
CREATE INDEX IF NOT EXISTS vacancy_search_fuzzy_idx ON vacancy_search USING gin (fuzzy_text gin_trgm_ops) WHERE open;
CREATE INDEX IF NOT EXISTS vacancy_search_codes_idx ON vacancy_search USING gin (codes) WHERE open;
DROP INDEX vacancy_search_doc_idx;

-- Порядок «новые сверху» целиком в индексе (вместе с номером для одинаковых дат): страница берётся с начала индекса, и сервер
-- останавливается на нужной строке, не сортируя все открытые вакансии. Прежний индекс только по дате становится лишним.
CREATE INDEX IF NOT EXISTS vacancies_open_order_idx ON vacancies (published_at DESC, id) WHERE status = 'published';
DROP INDEX vacancies_published_idx;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION refresh_vacancy_search(vid uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO vacancy_search (vacancy_id, doc, open, published_at, fuzzy_text, codes)
    SELECT v.id,
           setweight(to_tsvector('russian', v.title || ' ' || p.name), 'A')
        || setweight(to_tsvector('russian', concat_ws(' ', v.summary, v.focus, o.name, u.name,
                (SELECT string_agg(s.name, ' ')
                   FROM vacancy_specialties vs JOIN specialties s ON s.code = vs.specialty_code
                  WHERE vs.vacancy_id = v.id))), 'B')
        || setweight(to_tsvector('russian', concat_ws(' ', v.city, r.name, o.city)), 'C')
        || setweight(to_tsvector('russian', concat_ws(' ', v.description, v.requirements)), 'D'),
           v.status = 'published', v.published_at, v.title || ' ' || p.name || ' ' || o.name,
           COALESCE((SELECT array_agg(DISTINCT c) FROM vacancy_specialties vs,
                         LATERAL unnest(ARRAY[split_part(vs.specialty_code, '.', 1),
                                              split_part(vs.specialty_code, '.', 1) || '.' || split_part(vs.specialty_code, '.', 2),
                                              vs.specialty_code]) c
                      WHERE vs.vacancy_id = v.id), '{}')
      FROM vacancies v
      JOIN positions p ON p.code = v.position_code
      JOIN organizations o ON o.id = v.org_id
      LEFT JOIN units u ON u.id = v.unit_id
      LEFT JOIN regions r ON r.code = v.region_code
     WHERE v.id = vid
    ON CONFLICT (vacancy_id) DO UPDATE SET doc = EXCLUDED.doc, open = EXCLUDED.open, published_at = EXCLUDED.published_at, fuzzy_text = EXCLUDED.fuzzy_text, codes = EXCLUDED.codes;
END
$$;

-- Смена статуса не меняет текст, только отметку и дату: текст заново не собирается.
CREATE FUNCTION vacancy_search_after_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE vacancy_search SET open = (NEW.status = 'published'), published_at = NEW.published_at
     WHERE vacancy_id = NEW.id AND (open <> (NEW.status = 'published') OR published_at IS DISTINCT FROM NEW.published_at);
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER vacancy_search_status
    AFTER UPDATE OF status, published_at ON vacancies FOR EACH ROW EXECUTE FUNCTION vacancy_search_after_status();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN EXECUTE format('ALTER DATABASE %I RESET pg_trgm.word_similarity_threshold', current_database()); END $$;
-- +goose StatementEnd
CREATE INDEX vacancies_published_idx ON vacancies (published_at DESC) WHERE status = 'published';
DROP INDEX vacancies_open_order_idx;
DROP TRIGGER vacancy_search_status ON vacancies;
DROP FUNCTION vacancy_search_after_status();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION refresh_vacancy_search(vid uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO vacancy_search (vacancy_id, doc)
    SELECT v.id,
           setweight(to_tsvector('russian', v.title || ' ' || p.name), 'A')
        || setweight(to_tsvector('russian', concat_ws(' ', v.summary, v.focus, o.name, u.name,
                (SELECT string_agg(s.name, ' ')
                   FROM vacancy_specialties vs JOIN specialties s ON s.code = vs.specialty_code
                  WHERE vs.vacancy_id = v.id))), 'B')
        || setweight(to_tsvector('russian', concat_ws(' ', v.city, r.name, o.city)), 'C')
        || setweight(to_tsvector('russian', concat_ws(' ', v.description, v.requirements)), 'D')
      FROM vacancies v
      JOIN positions p ON p.code = v.position_code
      JOIN organizations o ON o.id = v.org_id
      LEFT JOIN units u ON u.id = v.unit_id
      LEFT JOIN regions r ON r.code = v.region_code
     WHERE v.id = vid
    ON CONFLICT (vacancy_id) DO UPDATE SET doc = EXCLUDED.doc;
END
$$;
-- +goose StatementEnd
CREATE INDEX vacancy_search_doc_idx ON vacancy_search USING gin (doc);
DROP INDEX vacancy_search_codes_idx;
DROP INDEX vacancy_search_fuzzy_idx;
DROP INDEX vacancy_search_open_doc_idx;
ALTER TABLE vacancy_search DROP COLUMN codes;
ALTER TABLE vacancy_search DROP COLUMN fuzzy_text;
ALTER TABLE vacancy_search DROP COLUMN published_at;
ALTER TABLE vacancy_search DROP COLUMN open;

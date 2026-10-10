-- Срез 24 (D-161, D-162): база под объём «вся наука РФ». Каталог учёных получает индексируемый текст для поиска и готовый счётчик
-- статей в Q1–Q2; для списков, которые читаются чаще всего, добавлены недостающие индексы.
--
-- Боевая выкладка на больших таблицах: перед миграцией создать индексы вручную командой CREATE INDEX CONCURRENTLY с теми же
-- именами (docs/SCALING.md, «Как выкладывать индексы»); IF NOT EXISTS пропустит уже готовые, и миграция не будет держать
-- блокировку таблицы. Обычный CREATE INDEX тоже работает, но на 5 миллионах строк блокирует запись на минуты.

-- +goose Up

-- Текст профиля для поиска лежит отдельно (как у вакансий, D-064): раньше он собирался на лету для каждого профиля при каждом
-- запросе, и индекс не мог его использовать (D-098). Состав текста тот же: имя, должность, город, регион, организация степени,
-- специальности, навыки, «о себе». Таблицу пересобирают триггеры.
CREATE TABLE profile_search (
    profile_id uuid        PRIMARY KEY REFERENCES profiles (id) ON DELETE CASCADE,
    doc        tsvector    NOT NULL,
    -- Имя и должность для поиска с опечатками (триграммы), дата обновления для выбора самых свежих совпадений.
    fuzzy_text text        NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Коды специальностей профиля вместе с кодами их групп и областей (1.4.4 даёт 1, 1.4 и 1.4.4): фильтр «область науки» стал
    -- проверкой «есть ли пересечение со списком» и идёт по индексу, без разбора кодов на лету.
    codes      text[]      NOT NULL DEFAULT '{}'
);
CREATE INDEX profile_search_doc_idx ON profile_search USING gin (doc);
CREATE INDEX profile_search_fuzzy_idx ON profile_search USING gin (fuzzy_text gin_trgm_ops);
CREATE INDEX profile_search_codes_idx ON profile_search USING gin (codes);

-- +goose StatementBegin
CREATE FUNCTION refresh_profile_search(pid uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO profile_search (profile_id, doc, fuzzy_text, updated_at, codes)
    SELECT p.id,
           to_tsvector('russian', u.display_name || ' ' || p.headline || ' ' || p.city || ' ' || COALESCE(r.name, '') || ' '
               || p.degree_institution || ' '
               || COALESCE((SELECT string_agg(s.name, ' ')
                              FROM profile_specialties ps JOIN specialties s ON s.code = ps.specialty_code
                             WHERE ps.profile_id = p.id), '') || ' '
               || array_to_string(p.research_skills, ' ') || ' ' || array_to_string(p.general_skills, ' ') || ' ' || p.about),
           u.display_name || ' ' || p.headline, p.updated_at,
           COALESCE((SELECT array_agg(DISTINCT c) FROM profile_specialties ps,
                         LATERAL unnest(ARRAY[split_part(ps.specialty_code, '.', 1),
                                              split_part(ps.specialty_code, '.', 1) || '.' || split_part(ps.specialty_code, '.', 2),
                                              ps.specialty_code]) c
                      WHERE ps.profile_id = p.id), '{}')
      FROM profiles p
      JOIN users u ON u.id = p.user_id
      LEFT JOIN regions r ON r.code = p.region_code
     WHERE p.id = pid
    ON CONFLICT (profile_id) DO UPDATE SET doc = EXCLUDED.doc, fuzzy_text = EXCLUDED.fuzzy_text, updated_at = EXCLUDED.updated_at, codes = EXCLUDED.codes;
END
$$;

CREATE FUNCTION profile_search_after_profile() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM refresh_profile_search(NEW.id);
    RETURN NULL;
END
$$;

CREATE FUNCTION profile_search_after_specialty() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM refresh_profile_search(OLD.profile_id);
    ELSE
        PERFORM refresh_profile_search(NEW.profile_id);
    END IF;
    RETURN NULL;
END
$$;

CREATE FUNCTION profile_search_after_user() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM refresh_profile_search(id) FROM profiles WHERE user_id = NEW.id;
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER profile_search_profile
    AFTER INSERT OR UPDATE OF headline, city, region_code, degree_institution, about, research_skills, general_skills, updated_at
    ON profiles FOR EACH ROW EXECUTE FUNCTION profile_search_after_profile();
CREATE TRIGGER profile_search_specialty
    AFTER INSERT OR DELETE ON profile_specialties FOR EACH ROW EXECUTE FUNCTION profile_search_after_specialty();
CREATE TRIGGER profile_search_user
    AFTER UPDATE OF display_name ON users FOR EACH ROW EXECUTE FUNCTION profile_search_after_user();

-- Профили, созданные до этой миграции.
SELECT refresh_profile_search(id) FROM profiles;

-- Статьи в журналах Q1–Q2 (срез 14) считались подзапросом на каждый профиль при каждом показе каталога. Теперь число лежит
-- в столбце профиля. Его пересчитывает триггер при любой правке публикаций, а при смене справочника журналов
-- (`scibox journals load`) его пересчитывает загрузка справочника. «За последние пять лет» зависит от текущего года и считается
-- только для профилей текущей страницы.
ALTER TABLE profiles ADD COLUMN q12_total smallint NOT NULL DEFAULT 0 CHECK (q12_total >= 0);

-- +goose StatementBegin
CREATE FUNCTION profile_q12_total(pid uuid) RETURNS smallint LANGUAGE sql STABLE AS $$
    SELECT count(*)::smallint
      FROM profile_items i
      JOIN journal_issns x ON x.issn = i.data ->> 'issn'
      JOIN journals j ON j.id = x.journal_id
     WHERE i.profile_id = pid AND i.kind = 'publication' AND j.quartile <= 2
$$;

CREATE FUNCTION profile_q12_after_item() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' AND OLD.kind = 'publication' THEN
        UPDATE profiles SET q12_total = profile_q12_total(OLD.profile_id) WHERE id = OLD.profile_id AND q12_total <> profile_q12_total(OLD.profile_id);
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.kind = 'publication' THEN
        UPDATE profiles SET q12_total = profile_q12_total(NEW.profile_id) WHERE id = NEW.profile_id AND q12_total <> profile_q12_total(NEW.profile_id);
    END IF;
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER profile_q12_item
    AFTER INSERT OR DELETE OR UPDATE OF kind, data, profile_id ON profile_items
    FOR EACH ROW EXECUTE FUNCTION profile_q12_after_item();

UPDATE profiles p SET q12_total = c.n
  FROM (SELECT i.profile_id, count(*)::smallint AS n
          FROM profile_items i
          JOIN journal_issns x ON x.issn = i.data ->> 'issn'
          JOIN journals j ON j.id = x.journal_id
         WHERE i.kind = 'publication' AND j.quartile <= 2
         GROUP BY i.profile_id) c
 WHERE c.profile_id = p.id AND p.q12_total <> c.n;

-- Лучшее из четырёх h-index готовым столбцом: порядок «по h-index» идёт по индексу, а не сортирует весь каталог.
ALTER TABLE profiles ADD COLUMN h_max smallint GENERATED ALWAYS AS
    (GREATEST(COALESCE(h_rsci, 0), COALESCE(h_scopus, 0), COALESCE(h_wos, 0), COALESCE(h_scholar, 0))) STORED NOT NULL;
CREATE INDEX IF NOT EXISTS profiles_h_idx ON profiles (h_max DESC NULLS LAST, updated_at DESC, id) WHERE headline <> '';
-- Фильтр по региону вместе с порядком «недавно обновлены».
CREATE INDEX IF NOT EXISTS profiles_region_idx ON profiles (region_code, updated_at DESC, id) WHERE headline <> '';
-- Порядок «по алфавиту».
CREATE INDEX IF NOT EXISTS users_display_name_idx ON users (display_name, id);

-- Список профилей каталога в порядке «недавно обновлены»: берётся с начала индекса, пока не наберётся страница.
CREATE INDEX IF NOT EXISTS profiles_catalog_idx ON profiles (updated_at DESC, id) WHERE headline <> '';

-- +goose Down
DROP INDEX IF EXISTS profiles_catalog_idx;
DROP INDEX IF EXISTS users_display_name_idx;
DROP INDEX IF EXISTS profiles_region_idx;
DROP INDEX IF EXISTS profiles_h_idx;
ALTER TABLE profiles DROP COLUMN h_max;
DROP TRIGGER profile_q12_item ON profile_items;
DROP FUNCTION profile_q12_after_item();
DROP FUNCTION profile_q12_total(uuid);
ALTER TABLE profiles DROP COLUMN q12_total;
DROP TRIGGER profile_search_user ON users;
DROP TRIGGER profile_search_specialty ON profile_specialties;
DROP TRIGGER profile_search_profile ON profiles;
DROP FUNCTION profile_search_after_user();
DROP FUNCTION profile_search_after_specialty();
DROP FUNCTION profile_search_after_profile();
DROP FUNCTION refresh_profile_search(uuid);
DROP TABLE profile_search;

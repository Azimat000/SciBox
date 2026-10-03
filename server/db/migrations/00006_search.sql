-- Поиск вакансий (срез 6): текст для полнотекстового поиска по-русски.

-- +goose Up

-- Текст вакансии для поиска лежит отдельно от вакансии: так он не попадает в каждую выдачу (vacancy_view тянет «v.*»).
-- Вес слов: A — название и должность; B — аннотация, тема, организация, подразделение, специальности; C — город и регион;
-- D — описание и требования.
CREATE TABLE vacancy_search (
    vacancy_id uuid     PRIMARY KEY REFERENCES vacancies (id) ON DELETE CASCADE,
    doc        tsvector NOT NULL
);
CREATE INDEX vacancy_search_doc_idx ON vacancy_search USING gin (doc);

-- +goose StatementBegin
-- Пересобирает текст одной вакансии. Если вакансии нет (её только что удалили), ничего не делает.
CREATE FUNCTION refresh_vacancy_search(vid uuid) RETURNS void LANGUAGE plpgsql AS $$
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

CREATE FUNCTION vacancy_search_after_vacancy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM refresh_vacancy_search(NEW.id);
    RETURN NULL;
END
$$;

CREATE FUNCTION vacancy_search_after_specialty() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM refresh_vacancy_search(OLD.vacancy_id);
    ELSE
        PERFORM refresh_vacancy_search(NEW.vacancy_id);
    END IF;
    RETURN NULL;
END
$$;

CREATE FUNCTION vacancy_search_after_org() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM refresh_vacancy_search(id) FROM vacancies WHERE org_id = NEW.id;
    RETURN NULL;
END
$$;

CREATE FUNCTION vacancy_search_after_unit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM refresh_vacancy_search(id) FROM vacancies WHERE unit_id = NEW.id;
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER vacancy_search_vacancy
    AFTER INSERT OR UPDATE OF unit_id, title, position_code, summary, description, requirements, focus, region_code, city
    ON vacancies FOR EACH ROW EXECUTE FUNCTION vacancy_search_after_vacancy();
CREATE TRIGGER vacancy_search_specialty
    AFTER INSERT OR DELETE ON vacancy_specialties FOR EACH ROW EXECUTE FUNCTION vacancy_search_after_specialty();
CREATE TRIGGER vacancy_search_org
    AFTER UPDATE OF name, city ON organizations FOR EACH ROW EXECUTE FUNCTION vacancy_search_after_org();
CREATE TRIGGER vacancy_search_unit
    AFTER UPDATE OF name ON units FOR EACH ROW EXECUTE FUNCTION vacancy_search_after_unit();

-- Вакансии, созданные до этой миграции.
SELECT refresh_vacancy_search(id) FROM vacancies;

-- +goose Down
DROP TRIGGER vacancy_search_unit ON units;
DROP TRIGGER vacancy_search_org ON organizations;
DROP TRIGGER vacancy_search_specialty ON vacancy_specialties;
DROP TRIGGER vacancy_search_vacancy ON vacancies;
DROP FUNCTION vacancy_search_after_unit();
DROP FUNCTION vacancy_search_after_org();
DROP FUNCTION vacancy_search_after_specialty();
DROP FUNCTION vacancy_search_after_vacancy();
DROP FUNCTION refresh_vacancy_search(uuid);
DROP TABLE vacancy_search;

-- Справочник навыков для подсказок в профиле (срез 33, D-211). Профиль по-прежнему хранит навыки строками
-- (profiles.research_skills, general_skills; D-136), поэтому замена справочника профили не ломает: строка либо
-- совпадает с каноническим написанием из справочника, либо остаётся «своим» навыком человека (D-212).
--
-- Только добавление таблиц (правило «расширить, потом сжать», D-186). Строки сюда кладёт команда `scibox skills load`
-- (пакет internal/skills) из файла, встроенного в сервер; источник и выпуск записываются в reference_sources
-- (каталог «skills»).
--
-- skills: один навык с каноническим написанием (name), видом (научный или общий) и группой для подписи в подсказке.
-- skill_terms: все написания навыка, включая каноническое (is_name): по ним ищут подсказки и сводят синонимы.
-- key — ключ сравнения (skills.Key: без регистра, «е» вместо «ё», одинаковые тире, один пробел); внутри вида
-- один ключ принадлежит одному навыку. words — тот же ключ со знаками препинания, заменёнными пробелами (поиск по
-- началу слова); tsv — основы слов для поиска по словоформам.

-- +goose Up
CREATE TABLE skills (
    id   integer PRIMARY KEY,                                  -- порядковый номер в файле при загрузке; профили на него не ссылаются
    kind text NOT NULL CHECK (kind IN ('research', 'general')),
    grp  text NOT NULL CHECK (char_length(grp) BETWEEN 1 AND 60),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60)
);

CREATE TABLE skill_terms (
    kind     text NOT NULL CHECK (kind IN ('research', 'general')),
    key      text NOT NULL CHECK (key <> ''),
    term     text NOT NULL CHECK (char_length(term) BETWEEN 1 AND 60),
    words    text NOT NULL,
    skill_id integer NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    is_name  boolean NOT NULL,
    tsv      tsvector GENERATED ALWAYS AS (to_tsvector('russian', term)) STORED,
    PRIMARY KEY (kind, key)
);
CREATE INDEX IF NOT EXISTS skill_terms_skill_idx ON skill_terms (skill_id);
CREATE INDEX IF NOT EXISTS skill_terms_key_prefix_idx ON skill_terms (kind, key text_pattern_ops);
CREATE INDEX IF NOT EXISTS skill_terms_tsv_idx ON skill_terms USING gin (tsv);
CREATE INDEX IF NOT EXISTS skill_terms_key_trgm_idx ON skill_terms USING gin (key gin_trgm_ops);

-- +goose Down
DROP TABLE skill_terms;
DROP TABLE skills;
DELETE FROM reference_sources WHERE catalog = 'skills';

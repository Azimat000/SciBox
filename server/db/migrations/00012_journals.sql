-- Справочник журналов с квартилями (срез 14, D-126, D-127). Источник: SCImago Journal Rank (SJR), лучший квартиль журнала
-- по его предметным категориям. Строки сюда кладёт команда `scibox journals load` (пакет internal/journals) из файла,
-- встроенного в сервер; источник и выпуск записываются в reference_sources (каталог «journals»). Миграция данных не несёт:
-- справочник большой (~30 тысяч журналов) и обновляется раз в год заменой целиком.
-- Публикация профиля ссылается на журнал своим ISSN (profile_items.data->>'issn'), а не номером строки: при замене
-- справочника ссылки не ломаются.

-- +goose Up
CREATE TABLE journals (
    id        bigint PRIMARY KEY,                               -- Sourceid SCImago
    title     text NOT NULL CHECK (title <> ''),
    publisher text NOT NULL DEFAULT '',
    quartile  smallint CHECK (quartile BETWEEN 1 AND 4),        -- NULL: у журнала нет квартиля (новый, мало данных)
    sjr       real,                                             -- значение SJR: порядок в поиске по справочнику
    data_year smallint NOT NULL                                 -- за какой год квартили
);
CREATE INDEX journals_title_trgm_idx ON journals USING gin (title gin_trgm_ops);

CREATE TABLE journal_issns (
    issn       text PRIMARY KEY CHECK (issn ~ '^[0-9]{4}-[0-9]{3}[0-9X]$'),
    journal_id bigint NOT NULL REFERENCES journals (id) ON DELETE CASCADE,
    position   smallint NOT NULL                                -- порядок ISSN в файле: первый показываем у журнала
);
CREATE INDEX journal_issns_journal_idx ON journal_issns (journal_id, position);

-- Публикации профиля по ISSN журнала: счётчик «статей в Q1–Q2» в каталоге.
CREATE INDEX profile_items_issn_idx ON profile_items ((data ->> 'issn')) WHERE kind = 'publication';

-- +goose Down
DROP INDEX profile_items_issn_idx;
DROP TABLE journal_issns;
DROP TABLE journals;
DELETE FROM reference_sources WHERE catalog = 'journals';

-- Справочник журналов (срез 14). Строки грузит internal/journals (COPY), здесь чтение и служебное.

-- Какой выпуск справочника загружен (нет строки — справочник пуст).
-- name: JournalsEdition :one
SELECT edition FROM reference_sources WHERE catalog = 'journals';

-- name: ClearJournals :exec
DELETE FROM journals;

-- name: SaveJournalsSource :exec
INSERT INTO reference_sources (catalog, title, url, edition, checked_on)
VALUES ('journals', @title, @url, @edition, @checked_on)
ON CONFLICT (catalog) DO UPDATE
SET title = EXCLUDED.title, url = EXCLUDED.url, edition = EXCLUDED.edition, checked_on = EXCLUDED.checked_on;

-- Поиск журнала для формы публикации: по ISSN (@issn) или по словам названия (@patterns: каждое слово «%слово%»).
-- Сначала точное название, потом начинающиеся с запроса, потом по SJR. Журналы без ISSN в справочник не попадают.
-- name: SearchJournals :many
SELECT j.id, j.title, j.publisher, j.quartile, j.data_year,
       (SELECT array_agg(x.issn ORDER BY x.position) FROM journal_issns x WHERE x.journal_id = j.id)::text[] AS issns
FROM journals j
WHERE CASE WHEN @issn::text <> ''
        THEN EXISTS (SELECT 1 FROM journal_issns x WHERE x.journal_id = j.id AND x.issn = @issn::text)
        ELSE j.title ILIKE ALL (@patterns::text[]) AND EXISTS (SELECT 1 FROM journal_issns x WHERE x.journal_id = j.id) END
ORDER BY lower(j.title) = lower(@q::text) DESC, j.title ILIKE @prefix::text DESC, j.sjr DESC NULLS LAST, j.title, j.id
LIMIT @row_limit;

-- Журналы по ISSN публикаций (профиль, найденная по DOI работа).
-- name: JournalsByISSN :many
SELECT x.issn, j.id, j.title, j.quartile, j.data_year
FROM journal_issns x JOIN journals j ON j.id = x.journal_id
WHERE x.issn = ANY(@issns::text[]);

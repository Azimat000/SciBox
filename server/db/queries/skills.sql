-- Справочник навыков (срез 33, D-211). Строки грузит internal/skills (COPY), здесь чтение и служебное.

-- Какой выпуск справочника загружен (нет строки — справочник пуст).
-- name: SkillsEdition :one
SELECT edition FROM reference_sources WHERE catalog = 'skills';

-- terms уходят вместе со skills (ON DELETE CASCADE).
-- name: ClearSkills :exec
DELETE FROM skills;

-- name: SaveSkillsSource :exec
INSERT INTO reference_sources (catalog, title, url, edition, checked_on)
VALUES ('skills', @title, @url, @edition, @checked_on)
ON CONFLICT (catalog) DO UPDATE
SET title = EXCLUDED.title, url = EXCLUDED.url, edition = EXCLUDED.edition, checked_on = EXCLUDED.checked_on;

-- Подсказки навыков. @key — ключ запроса целиком, @prefix — он же с экранированием для LIKE и знаком «%» в конце,
-- @words — слова запроса, @tsq — запрос по основам слов («слово:* & слово:*», только буквы и цифры) или пусто.
-- Ярусы: 0 — точное написание, 1 — начало написания, 2 — начало каждого слова, 3 — словоформы. На каждый навык
-- берётся лучшее совпадение (название выигрывает у синонима), порядок: ярус, короткое название раньше, по алфавиту.
-- name: SearchSkills :many
WITH hit AS (
    SELECT t.skill_id, t.term, t.is_name,
           CASE
               WHEN t.key = @key::text THEN 0
               WHEN t.key LIKE @prefix::text THEN 1
               WHEN cardinality(@words::text[]) > 0 AND NOT EXISTS (
                        SELECT 1 FROM unnest(@words::text[]) w WHERE strpos(' ' || t.words, ' ' || w) = 0) THEN 2
               ELSE 3
           END AS rank
    FROM skill_terms t
    WHERE t.kind = @kind::text
      AND (t.key = @key::text
           OR t.key LIKE @prefix::text
           OR (cardinality(@words::text[]) > 0 AND NOT EXISTS (
                   SELECT 1 FROM unnest(@words::text[]) w WHERE strpos(' ' || t.words, ' ' || w) = 0))
           OR (@tsq::text <> '' AND t.tsv @@ to_tsquery('russian', @tsq::text)))
)
SELECT s.id, s.name, s.grp, h.term AS via, h.is_name, h.rank::int AS rank
FROM (SELECT DISTINCT ON (skill_id) skill_id, term, is_name, rank FROM hit ORDER BY skill_id, rank, is_name DESC, term) h
JOIN skills s ON s.id = h.skill_id
ORDER BY h.rank, char_length(s.name), s.name
LIMIT @row_limit;

-- Запасной поиск по похожим написаниям (опечатки): только когда обычный ничего не нашёл.
-- name: SearchSkillsFuzzy :many
WITH hit AS (
    SELECT t.skill_id, t.term, t.is_name,
           greatest(similarity(t.key, @key::text), word_similarity(@key::text, t.key)) AS score
    FROM skill_terms t
    WHERE t.kind = @kind::text
      AND (t.key % @key::text OR word_similarity(@key::text, t.key) >= 0.6)
)
SELECT s.id, s.name, s.grp, h.term AS via, h.is_name
FROM (SELECT DISTINCT ON (skill_id) skill_id, term, is_name, score FROM hit ORDER BY skill_id, score DESC, is_name DESC, term) h
JOIN skills s ON s.id = h.skill_id
ORDER BY h.score DESC, char_length(s.name), s.name
LIMIT @row_limit;

-- Каноническое написание для введённых человеком навыков: по ключу (skills.Key) находится название из справочника.
-- name: CanonicalSkills :many
SELECT t.key, s.name
FROM skill_terms t JOIN skills s ON s.id = t.skill_id
WHERE t.kind = @kind::text AND t.key = ANY(@keys::text[]);

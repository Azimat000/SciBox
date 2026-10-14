-- Срез 32 (D-210): ссылка на GitHub среди идентификаторов профиля.
--
-- Только добавление: столбец со значением по умолчанию, поэтому старая версия сервера, которая его не знает,
-- продолжает читать и обновлять профили (правило «расширить, потом сжать», D-186). В столбце лежит нормализованный
-- адрес https://github.com/<логин>; формат проверяет сервис (profiles.normalizeGitHub), в базе только длина:
-- «https://github.com/» (19 знаков) плюс логин не длиннее 39.

-- +goose Up
ALTER TABLE profiles ADD COLUMN github_url text NOT NULL DEFAULT '' CHECK (char_length(github_url) <= 60);

-- +goose Down
ALTER TABLE profiles DROP COLUMN github_url;

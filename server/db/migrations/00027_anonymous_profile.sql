-- Срез 15 (D-199, D-200): анонимный режим профиля и «скрыть от моей организации».
--
-- Четвёртый режим приватности `anon`: сотрудники организаций видят научные данные без имени, места работы и всего, по чему
-- человека легко узнать. Текст профиля для поиска в этом режиме собирается без имени, должности, города, организации степени,
-- навыков и «о себе»: иначе поиск по фамилии находил бы «анонимную» карточку и подтверждал, чья она.
--
-- scibox:contract Старая версия сервера не записывает режим anon и читает его как «скрыт» (privacy.Parse не знает строки,
-- CanView закрыт всем, кроме владельца), поэтому ослабление проверки безопасно для отката версии.

-- +goose Up
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_check CHECK (visibility IN ('hidden', 'anon', 'orgs', 'public'));
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_before_deletion_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_before_deletion_check
    CHECK (visibility_before_deletion IN ('hidden', 'anon', 'orgs', 'public'));

-- Организации, от сотрудников которых человек скрыл профиль. Сотрудник такой организации не видит профиль ни в каком режиме.
CREATE TABLE profile_hidden_orgs (
    profile_id uuid        NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    org_id     uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (profile_id, org_id)
);
-- Какие профили скрыты от организации (проверка «сколько человек скрылось от нас», чистка при удалении организации).
CREATE INDEX profile_hidden_orgs_org_idx ON profile_hidden_orgs (org_id);

-- Поиск: у анонимного профиля в тексте только то, что показывается в безымянной карточке (регион и специальности).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION refresh_profile_search(pid uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO profile_search (profile_id, doc, fuzzy_text, updated_at, codes)
    SELECT p.id,
           CASE WHEN p.visibility = 'anon' THEN
               to_tsvector('russian', COALESCE(r.name, '') || ' '
                   || COALESCE((SELECT string_agg(s.name, ' ')
                                  FROM profile_specialties ps JOIN specialties s ON s.code = ps.specialty_code
                                 WHERE ps.profile_id = p.id), ''))
           ELSE
               to_tsvector('russian', u.display_name || ' ' || p.headline || ' ' || p.city || ' ' || COALESCE(r.name, '') || ' '
                   || p.degree_institution || ' '
                   || COALESCE((SELECT string_agg(s.name, ' ')
                                  FROM profile_specialties ps JOIN specialties s ON s.code = ps.specialty_code
                                 WHERE ps.profile_id = p.id), '') || ' '
                   || array_to_string(p.research_skills, ' ') || ' ' || array_to_string(p.general_skills, ' ') || ' ' || p.about)
           END,
           CASE WHEN p.visibility = 'anon' THEN '' ELSE u.display_name || ' ' || p.headline END,
           p.updated_at,
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
-- +goose StatementEnd

-- Смена режима пересобирает текст для поиска (правка профиля делает то же по другим столбцам).
CREATE TRIGGER profile_search_visibility
    AFTER UPDATE OF visibility ON profiles FOR EACH ROW EXECUTE FUNCTION profile_search_after_profile();

-- +goose Down
DROP TRIGGER IF EXISTS profile_search_visibility ON profiles;
DROP TABLE IF EXISTS profile_hidden_orgs;
UPDATE profiles SET visibility = 'hidden' WHERE visibility = 'anon';
UPDATE profiles SET visibility_before_deletion = 'hidden' WHERE visibility_before_deletion = 'anon';
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_before_deletion_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_before_deletion_check CHECK (visibility_before_deletion IN ('hidden', 'orgs', 'public'));
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_check CHECK (visibility IN ('hidden', 'orgs', 'public'));
-- Функция остаётся новой: у профилей не-anon она даёт прежний результат.

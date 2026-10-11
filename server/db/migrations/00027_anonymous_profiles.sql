-- +goose Up
-- Анонимный режим и «скрыть от организаций» (срез 15, D-199…D-205).
-- scibox:contract расширяет список допустимых значений режима: прежняя версия сервера значение «anonymous» не пишет, а читая, разбирает его как «неизвестный режим» и прячет профиль от всех, кроме владельца. Новое ограничение шире прежнего, поэтому выкладка одним выпуском безопасна.
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_check CHECK (visibility IN ('hidden', 'anonymous', 'orgs', 'public'));
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_before_deletion_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_before_deletion_check
    CHECK (visibility_before_deletion IN ('hidden', 'anonymous', 'orgs', 'public'));

-- Организации, от сотрудников которых учёный скрыл профиль (в любом режиме).
CREATE TABLE IF NOT EXISTS profile_hidden_orgs (
    profile_id uuid        NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    org_id     uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (profile_id, org_id)
);
CREATE INDEX IF NOT EXISTS profile_hidden_orgs_org_idx ON profile_hidden_orgs (org_id);

-- Организации, которым открыто имя анонимного профиля: учёный откликнулся на их вакансию или ответил «Интересно» на
-- их приглашение. Запись не снимается: организация уже узнала имя.
CREATE TABLE IF NOT EXISTS profile_reveals (
    profile_id uuid        NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    org_id     uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    reason     text        NOT NULL CHECK (reason IN ('application', 'offer_interested')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (profile_id, org_id)
);
CREATE INDEX IF NOT EXISTS profile_reveals_org_idx ON profile_reveals (org_id);

-- +goose Down
DROP TABLE profile_reveals;
DROP TABLE profile_hidden_orgs;
UPDATE profiles SET visibility = 'hidden' WHERE visibility = 'anonymous';
UPDATE profiles SET visibility_before_deletion = 'hidden' WHERE visibility_before_deletion = 'anonymous';
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_before_deletion_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_before_deletion_check CHECK (visibility_before_deletion IN ('hidden', 'orgs', 'public'));
ALTER TABLE profiles DROP CONSTRAINT profiles_visibility_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_visibility_check CHECK (visibility IN ('hidden', 'orgs', 'public'));

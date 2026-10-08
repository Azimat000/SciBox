-- Аккаунты: усиление (срез 20, T-10…T-14, D-142…D-146).
-- Свежий пароль для опасных действий, смена почты со ссылкой отката, известные устройства (письмо о входе
-- с нового), удаление аккаунта со сроком на отмену и обезличивание откликов.

-- +goose Up

-- Когда в этой сессии последний раз вводили пароль (вход или повторный ввод). Опасные действия
-- (смена почты, удаление аккаунта, назначение владельца) разрешены только вскоре после него.
ALTER TABLE sessions ADD COLUMN reauth_at timestamptz;

-- Ссылки смены почты: change_email уходит на новый адрес, revert_email на старый («это не я»).
-- new_email — адрес, который станет почтой аккаунта, если ссылка сработает.
ALTER TABLE auth_tokens ADD COLUMN new_email citext;
ALTER TABLE auth_tokens DROP CONSTRAINT auth_tokens_purpose_check;
ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_purpose_check
    CHECK (purpose IN ('confirm_email', 'reset_password', 'change_email', 'revert_email'));
ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_new_email_check
    CHECK ((purpose IN ('change_email', 'revert_email')) = (new_email IS NOT NULL));

-- Браузеры, с которых человек уже входил. Браузер помнит случайный токен в долгой cookie, в базе только его хеш.
-- Вход из браузера, которого здесь нет, присылает письмо «вход с нового устройства».
CREATE TABLE known_devices (
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_hash   bytea       NOT NULL,
    first_seen_at timestamptz NOT NULL,
    last_seen_at  timestamptz NOT NULL,
    PRIMARY KEY (user_id, device_hash)
);
CREATE INDEX known_devices_last_seen_idx ON known_devices (last_seen_at);

-- Удаление аккаунта (D-142): deletion_due_at — когда данные сотрутся (до этого можно отменить);
-- deleted_at — когда стёрты. От стёртого аккаунта остаётся пустая строка без почты и имени,
-- чтобы обезличенные отклики и записи «кто создал» не потеряли ссылку.
ALTER TABLE users
    ADD COLUMN deletion_due_at timestamptz,
    ADD COLUMN deleted_at      timestamptz;
CREATE INDEX users_deletion_due_idx ON users (deletion_due_at) WHERE deletion_due_at IS NOT NULL AND deleted_at IS NULL;

-- На время срока отмены профиль скрыт; прежний режим возвращается, если человек передумал.
ALTER TABLE profiles ADD COLUMN visibility_before_deletion text
    CHECK (visibility_before_deletion IN ('hidden', 'orgs', 'public'));

-- Обезличенный отклик: письмо, контакты и снимок профиля стёрты, остались вакансия, даты и статус.
ALTER TABLE applications ADD COLUMN anonymized_at timestamptz;
ALTER TABLE applications DROP CONSTRAINT applications_cover_letter_check;
ALTER TABLE applications DROP CONSTRAINT applications_contact_email_check;
ALTER TABLE applications ADD CONSTRAINT applications_cover_letter_check
    CHECK (char_length(cover_letter) <= 6000 AND (anonymized_at IS NOT NULL OR char_length(cover_letter) >= 1));
ALTER TABLE applications ADD CONSTRAINT applications_contact_email_check
    CHECK (char_length(contact_email) <= 254 AND (anonymized_at IS NOT NULL OR char_length(contact_email) >= 3));

-- +goose Down
ALTER TABLE applications DROP CONSTRAINT applications_contact_email_check;
ALTER TABLE applications DROP CONSTRAINT applications_cover_letter_check;
DELETE FROM applications WHERE anonymized_at IS NOT NULL;
ALTER TABLE applications ADD CONSTRAINT applications_cover_letter_check CHECK (char_length(cover_letter) BETWEEN 1 AND 6000);
ALTER TABLE applications ADD CONSTRAINT applications_contact_email_check CHECK (char_length(contact_email) BETWEEN 3 AND 254);
ALTER TABLE applications DROP COLUMN anonymized_at;
ALTER TABLE profiles DROP COLUMN visibility_before_deletion;
DROP INDEX users_deletion_due_idx;
ALTER TABLE users DROP COLUMN deleted_at, DROP COLUMN deletion_due_at;
DROP TABLE known_devices;
DELETE FROM auth_tokens WHERE purpose IN ('change_email', 'revert_email');
ALTER TABLE auth_tokens DROP CONSTRAINT auth_tokens_new_email_check;
ALTER TABLE auth_tokens DROP CONSTRAINT auth_tokens_purpose_check;
ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_purpose_check CHECK (purpose IN ('confirm_email', 'reset_password'));
ALTER TABLE auth_tokens DROP COLUMN new_email;
ALTER TABLE sessions DROP COLUMN reauth_at;

-- D-256: внутри отклика нет приглашений (собеседование, контакты организации, просьба оставить контакты). Их придумали,
-- когда чата не было (D-011); теперь о времени, ссылке и контактах договариваются в переписке, которая открыта с
-- отклика до решения и после «принят». Статуса отклика «приглашён» тоже нет: такие отклики становятся «просмотрен».
--
-- Строки приглашений стираются: показать их больше негде, а в них контакты и записки людей. Строки о приглашениях в
-- переписках остаются как история (это снимки, таблица им не нужна). Сама таблица application_invitations удаляется
-- следующим выпуском, когда прежней версии сервера, которая её читает, уже не будет.
--
-- scibox:contract Прежняя версия сервера может попытаться пригласить кандидата: проверка статуса в базе откажет, и на
-- время выкладки это одно действие ответит ошибкой. Карточку отклика прежняя версия читает как обычно, приглашений в ней
-- просто нет.

-- +goose Up
UPDATE applications SET status = 'viewed' WHERE status = 'invited';
DELETE FROM application_invitations;
ALTER TABLE applications DROP CONSTRAINT applications_status_check;
ALTER TABLE applications ADD CONSTRAINT applications_status_check
    CHECK (status IN ('sent', 'viewed', 'rejected', 'accepted', 'withdrawn'));

-- +goose Down
ALTER TABLE applications DROP CONSTRAINT applications_status_check;
ALTER TABLE applications ADD CONSTRAINT applications_status_check
    CHECK (status IN ('sent', 'viewed', 'invited', 'rejected', 'accepted', 'withdrawn'));

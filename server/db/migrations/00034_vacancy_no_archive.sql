-- D-256: у вакансии нет статуса «В архиве». Он отличался от «Закрыта» только тем, что прятал страницу по ссылке от всех,
-- кроме сотрудников, и требовал второго шага после закрытия. Архивные вакансии становятся закрытыми: страница снова
-- открывается по ссылке, в поиске и списках их по-прежнему нет. Столбец archived_at остаётся (его читает прежняя версия
-- сервера через vacancy_view); новые значения в него не пишутся.
--
-- scibox:contract Прежняя версия сервера может попытаться убрать вакансию в архив: проверка в базе откажет, и на время
-- выкладки это одно действие ответит ошибкой. Остальное прежняя версия читает как обычно: закрытые вакансии она знает.

-- +goose Up
UPDATE vacancies SET status = 'closed', closed_at = COALESCE(closed_at, archived_at, updated_at) WHERE status = 'archived';
ALTER TABLE vacancies DROP CONSTRAINT vacancies_status_check;
ALTER TABLE vacancies ADD CONSTRAINT vacancies_status_check CHECK (status IN ('draft', 'published', 'closed'));

-- +goose Down
ALTER TABLE vacancies DROP CONSTRAINT vacancies_status_check;
ALTER TABLE vacancies ADD CONSTRAINT vacancies_status_check CHECK (status IN ('draft', 'published', 'closed', 'archived'));

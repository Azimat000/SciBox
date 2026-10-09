-- Сроки хранения (срез 22, T-19, T-35, D-151, D-152).
-- 1. Журнал действий можно чистить по сроку, но только записи старше 90 дней и только внутри транзакции, которая
--    объявила чистку (`scibox.audit_purge`). Первое условие — пол, который не зависит от настройки срока в коде:
--    ошибка в расчёте даты не сотрёт свежую историю. TRUNCATE и любое изменение записей по-прежнему запрещены.
-- 2. Индексы, без которых чистка по сроку читала бы всю таблицу каждый час.

-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_log_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Единственное разрешённое изменение: убрать адрес. Всё остальное в записи должно остаться как было.
        IF NEW.ip IS NULL
           AND (NEW.id, NEW.at, NEW.org_id, NEW.actor_id, NEW.subject_id, NEW.action, NEW.target_kind, NEW.target_id, NEW.target_label, NEW.details)
               IS NOT DISTINCT FROM
               (OLD.id, OLD.at, OLD.org_id, OLD.actor_id, OLD.subject_id, OLD.action, OLD.target_kind, OLD.target_id, OLD.target_label, OLD.details)
        THEN
            RETURN NEW;
        END IF;
    ELSIF TG_OP = 'DELETE' THEN
        -- Удаление по сроку хранения: объявленная чистка и запись старше пола в 90 дней.
        IF current_setting('scibox.audit_purge', true) = 'on' AND OLD.at < now() - interval '90 days' THEN
            RETURN OLD;
        END IF;
    END IF;
    RAISE EXCEPTION 'audit_log is append-only' USING ERRCODE = 'integrity_constraint_violation';
END;
$$;
-- +goose StatementEnd

-- Записи, у которых ещё стоит адрес: чистка адресов не перечитывает уже очищенное.
CREATE INDEX audit_log_purge_ip_idx ON audit_log (at) WHERE ip IS NOT NULL;
-- Прочитанные и непрочитанные уведомления стареют по-разному.
CREATE INDEX notifications_purge_read_idx ON notifications (read_at) WHERE read_at IS NOT NULL;
CREATE INDEX notifications_purge_unread_idx ON notifications (created_at) WHERE read_at IS NULL;
-- Неподтверждённые регистрации.
CREATE INDEX users_purge_unconfirmed_idx ON users (updated_at) WHERE email_confirmed_at IS NULL AND deleted_at IS NULL;

-- +goose Down

DROP INDEX users_purge_unconfirmed_idx;
DROP INDEX notifications_purge_unread_idx;
DROP INDEX notifications_purge_read_idx;
DROP INDEX audit_log_purge_ip_idx;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_log_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.ip IS NULL
           AND (NEW.id, NEW.at, NEW.org_id, NEW.actor_id, NEW.subject_id, NEW.action, NEW.target_kind, NEW.target_id, NEW.target_label, NEW.details)
               IS NOT DISTINCT FROM
               (OLD.id, OLD.at, OLD.org_id, OLD.actor_id, OLD.subject_id, OLD.action, OLD.target_kind, OLD.target_id, OLD.target_label, OLD.details)
        THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'audit_log is append-only' USING ERRCODE = 'integrity_constraint_violation';
END;
$$;
-- +goose StatementEnd

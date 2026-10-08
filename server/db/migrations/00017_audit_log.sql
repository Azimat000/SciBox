-- Журнал действий (срез 21, T-17, D-147…D-150).
-- Кто, что, над чем и когда сделал. Пишется в той же транзакции, что и само действие, и только дописывается:
-- запись нельзя ни изменить, ни удалить. Единственное исключение: адрес человека (ip) стирается, когда стирается
-- его аккаунт (D-143). Удаление по сроку хранения появится в срезе 22 отдельной миграцией.

-- +goose Up

CREATE TABLE audit_log (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at           timestamptz NOT NULL,
    -- Организация, чьё это действие. NULL у действий над своим аккаунтом (вход, пароль, почта): их видит только сам человек.
    org_id       uuid        REFERENCES organizations (id),
    -- Кто сделал. NULL: система или человек без аккаунта (рекомендатель). Аккаунты не удаляются (остаётся строка-«надгробие»).
    actor_id     uuid        REFERENCES users (id),
    -- Над каким человеком (кому сменили роль, кого пригласили, чей это отклик). Имя берётся из аккаунта при показе,
    -- поэтому после стирания аккаунта в журнале видно «Аккаунт удалён», а не имя.
    subject_id   uuid        REFERENCES users (id),
    -- Что сделал: вид.действие, например vacancy.published. Список известных действий хранит пакет internal/audit.
    action       text        NOT NULL CHECK (action ~ '^[a-z]+\.[a-z_]+$' AND char_length(action) <= 60),
    -- Над чем: вид объекта, его номер и подпись на момент действия (название вакансии потом могут изменить).
    target_kind  text        NOT NULL DEFAULT '' CHECK (char_length(target_kind) <= 30),
    target_id    uuid,
    target_label text        NOT NULL DEFAULT '' CHECK (char_length(target_label) <= 300),
    -- Мелкие подробности (прежняя и новая роль, статусы). Без имён, писем и текстов людей.
    details      jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(details) = 'object' AND octet_length(details::text) <= 2000),
    -- Откуда: адрес человека. Стирается вместе с аккаунтом.
    ip           text        CHECK (char_length(ip) <= 45)
);

-- Журнал организации листается от новых к старым по номеру записи (без OFFSET).
CREATE INDEX audit_log_org_idx ON audit_log (org_id, id DESC) WHERE org_id IS NOT NULL;
-- Действия человека: стирание адреса при удалении аккаунта и его собственный журнал.
CREATE INDEX audit_log_actor_idx ON audit_log (actor_id, id DESC) WHERE actor_id IS NOT NULL;
-- Чистка по сроку хранения (срез 22).
CREATE INDEX audit_log_at_idx ON audit_log (at);

-- +goose StatementBegin
CREATE FUNCTION audit_log_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Единственное разрешённое изменение: убрать адрес. Всё остальное в записи должно остаться как было.
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

CREATE TRIGGER audit_log_no_change BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_guard();
CREATE TRIGGER audit_log_no_truncate BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_guard();

-- +goose Down
DROP TABLE audit_log;
DROP FUNCTION audit_log_guard();

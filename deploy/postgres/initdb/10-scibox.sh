#!/bin/bash
# Первая инициализация базы (срез 28): выполняется один раз, когда каталог данных пуст (docker-entrypoint.sh,
# от пользователя postgres, на временном сервере без сети).
#
# 1. непрерывный архив журнала изменений и шифрование соединений (настройки пишутся в postgresql.conf: параметры
#    архива должны быть в файле, а не в команде запуска, иначе временный сервер инициализации тоже начал бы
#    архивировать в хранилище, которого ещё нет);
# 2. правила входа: из сети только роль приложения и только с шифрованием; суперпользователь из сети не входит;
# 3. роль приложения без прав суперпользователя и её база.
set -euo pipefail

cat >> "$PGDATA/postgresql.conf" <<CONF

# --- ИОН: срез 28 (deploy/postgres/initdb/10-scibox.sh) ---
wal_level = replica
archive_mode = on
archive_command = 'pgbackrest --stanza=main archive-push %p'
# Журнал отправляется в архив не реже чем раз в минуту, даже когда записей мало: так потеря при аварии — минуты.
archive_timeout = 60
ssl = on
password_encryption = scram-sha-256
CONF

# Сертификат для шифрования соединений внутри сети контейнеров. Приложение подключается с sslmode=require (проверка
# боевого режима): канал шифруется, подлинность сервера обеспечивает закрытая сеть compose.
openssl req -new -x509 -days 3650 -nodes -subj "/CN=postgres" \
    -out "$PGDATA/server.crt" -keyout "$PGDATA/server.key" 2>/dev/null
chmod 600 "$PGDATA/server.key"

cat > "$PGDATA/pg_hba.conf" <<'HBA'
# TYPE   DATABASE  USER    ADDRESS      METHOD
# Внутри контейнера (pgBackRest, psql, проверки восстановления): без пароля, снаружи по сокету не достучаться.
local    all       all                  trust
# Из сети: только приложение и только по шифрованному каналу.
hostssl  scibox    scibox  0.0.0.0/0    scram-sha-256
hostssl  scibox    scibox  ::/0         scram-sha-256
# Всё остальное, включая суперпользователя из сети, отказ.
host     all       all     0.0.0.0/0    reject
host     all       all     ::/0         reject
HBA

: "${SCIBOX_DB_PASSWORD:?SCIBOX_DB_PASSWORD_FROM_FILE не задан: пароль роли приложения}"
psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres --set=pw="$SCIBOX_DB_PASSWORD" <<'SQL'
CREATE ROLE scibox LOGIN PASSWORD :'pw';
CREATE DATABASE scibox OWNER scibox;
SQL
# Статистика запросов для разбора медленных мест (срез 27): расширение ставит суперпользователь, читает владелец базы.
psql -v ON_ERROR_STOP=1 --username postgres --dbname scibox <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
GRANT pg_read_all_stats TO scibox;
SQL

# Образ базы ИОН (срез 28, T-26): PostgreSQL с pgBackRest (непрерывный архив журнала изменений и полные копии в
# S3-хранилище) и rclone (копия файлов откликов). Тот же образ работает как сервер базы, как «копировщик» (backup) и
# как проверка восстановления (restore-check): команды в /usr/local/bin.
#
# Базовые образы заданы переменными на случай, если Docker Hub недоступен (docs/DEPLOY.md).
ARG POSTGRES_IMAGE=postgres:16.15
ARG RCLONE_IMAGE=rclone/rclone:1.75.2

FROM ${RCLONE_IMAGE} AS rclone

FROM ${POSTGRES_IMAGE}
# jq читает состояние копий (pgbackrest info --output=json); openssl делает сертификат базы при первом запуске.
RUN apt-get update \
 && apt-get install -y --no-install-recommends pgbackrest jq openssl ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=rclone /usr/local/bin/rclone /usr/local/bin/rclone
COPY initdb/ /docker-entrypoint-initdb.d/
COPY bin/ /usr/local/bin/
RUN chmod 0755 /usr/local/bin/scibox-* /docker-entrypoint-initdb.d/*.sh
# Контейнер начинает работу от root только чтобы прочитать файлы секретов; процесс базы и копий работает от postgres
# (scibox-entrypoint делает gosu).
ENTRYPOINT ["scibox-entrypoint"]
CMD ["postgres"]

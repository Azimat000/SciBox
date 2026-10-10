#!/bin/sh
# Раскладывает секреты из deploy/secrets/ (смонтированы в /in только для чтения) по томам с нужным владельцем и
# правами 600 (срез 28). Работает от root внутри контейнера secrets-init. Повторяем: файлы перезаписываются.
#
#   app         uid 65532 (сервер, воркер, шаг выкладки): database_url, s3_*, smtp_password, ops_token
#   monitoring  uid 65534 (Prometheus, Alertmanager): ops_token, smtp_password
#   grafana     uid 472:  grafana_admin_password
set -eu

put() { # put <каталог тома> <uid> <имя файла>...
    dir=$1; uid=$2; shift 2
    install -d -m 0700 -o "$uid" -g "$uid" "$dir"
    for name in "$@"; do
        if [ ! -s "/in/$name" ]; then
            echo "init-secrets: нет файла deploy/secrets/$name или он пуст (docs/DEPLOY.md, «Секреты»)" >&2
            exit 1
        fi
        install -m 0600 -o "$uid" -g "$uid" "/in/$name" "$dir/$name"
    done
}

put /out/app 65532 database_url s3_access_key s3_secret_key smtp_password ops_token
put /out/monitoring 65534 ops_token smtp_password
put /out/grafana 472 grafana_admin_password
echo "init-secrets: готово"

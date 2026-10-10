#!/bin/sh
# Собирает deploy/generated/alertmanager.yml из настроек почты в env-файле (срез 28, T-53): Alertmanager сам
# переменные окружения не подставляет. Пароль почты в файл не попадает: Alertmanager читает его из тома секретов.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
if [ "${SCIBOX_DEPLOY_LOCAL:-}" = 1 ]; then envfile="$here/local/local.env"; else envfile="$here/.env"; fi

val() { # val ИМЯ [по умолчанию]: значение из env-файла без кавычек и комментария в конце строки
    v=$(sed -n "s/^$1=//p" "$envfile" | tail -1)
    [ -n "$v" ] || v=${2:-}
    printf '%s' "$v"
}
smtp_addr=$(val SMTP_ADDR)
smtp_user=$(val SMTP_USER)
mail_from=$(val MAIL_FROM)
to=$(val ALERT_EMAIL_TO)
[ -n "$smtp_addr" ] && [ -n "$mail_from" ] && [ -n "$to" ] || { echo "render-alertmanager: нужны SMTP_ADDR, MAIL_FROM и ALERT_EMAIL_TO в $envfile" >&2; exit 1; }
# Адрес отправителя без имени: «ИОН <a@b>» → a@b
from_addr=$(printf '%s' "$mail_from" | sed -n 's/.*<\(.*\)>.*/\1/p')
[ -n "$from_addr" ] || from_addr=$mail_from

mkdir -p "$here/generated"
umask 077
{
    cat <<YAML
# Создан deploy/render-alertmanager.sh: не править руками.
global:
  smtp_smarthost: '$smtp_addr'
  smtp_from: '$from_addr'
  smtp_require_tls: true
YAML
    if [ -n "$smtp_user" ]; then
        cat <<YAML
  smtp_auth_username: '$smtp_user'
  smtp_auth_password_file: /run/secrets/smtp_password
YAML
    fi
    cat <<YAML

route:
  receiver: email
  group_by: [alertname]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  routes:
    # Служебная тревога «система тревог жива» никому не отправляется.
    - matchers: [severity="none"]
      receiver: nobody
    - matchers: [severity="critical"]
      receiver: email
      repeat_interval: 4h

receivers:
  - name: nobody
  - name: email
    email_configs:
      - to: '$to'
        send_resolved: true
YAML
} > "$here/generated/alertmanager.yml"
chmod 644 "$here/generated/alertmanager.yml"
echo "render-alertmanager: deploy/generated/alertmanager.yml (получатель $to)"

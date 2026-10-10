#!/bin/sh
# Готовит репетицию (срез 28): сертификаты, тестовые секреты и бакеты. Повторный запуск безопасен: готовое не трогает.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(dirname "$(dirname "$here")")
# macOS: iCloud Drive выгружает файлы папки «Документы» (флаг dataless), и контейнер читает их с ошибкой «Resource
# deadlock avoided». Читаем такие файлы на самом компьютере: они снова становятся настоящими. На сервере не нужно.
if [ "$(uname)" = Darwin ]; then
    find "$root/deploy" "$root/monitoring" "$root/docker" "$root/server" "$root/web" "$root/config" "$root/Dockerfile" "$root/.dockerignore" \
        -path "$root/web/node_modules" -prune -o -type f -flags +dataless -exec cat {} + >/dev/null 2>&1 || true
fi
deploy=$(dirname "$here")
"$here/make-pki.sh"
"$deploy/init-secrets.sh" >/dev/null
if [ "$(uname)" = Darwin ]; then
    find "$root/deploy" -type f -flags +dataless -exec cat {} + >/dev/null 2>&1 || true
fi
cd "$deploy/secrets"
umask 077
# Ключи хранилища репетиции совпадают с теми, что заданы у SeaweedFS в docker-compose.local.yml.
[ -s s3_access_key ] || printf 'rehearsal-access-key' > s3_access_key
[ -s s3_secret_key ] || printf 'rehearsal-secret-key-1234567890' > s3_secret_key
[ -s backup_s3_access_key ] || printf 'rehearsal-access-key' > backup_s3_access_key
[ -s backup_s3_secret_key ] || printf 'rehearsal-secret-key-1234567890' > backup_s3_secret_key
[ -s smtp_password ] || printf 'rehearsal-mail-password' > smtp_password
chmod 600 ./*
echo "репетиция подготовлена"

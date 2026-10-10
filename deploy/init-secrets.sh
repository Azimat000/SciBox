#!/bin/sh
# Создаёт файлы секретов в deploy/secrets/ (срез 28). Случайные секреты генерируются здесь; те, что выдают
# провайдеры (ключи хранилищ, пароль почты), создаёт человек сам. Повторный запуск не трогает готовые файлы.
#
# ВАЖНО: backup_cipher_pass и backup_files_crypt_pass шифруют копии. Потеряете их вместе с сервером — копии не
# прочитать никогда. Сохраните оба файла отдельно от сервера (менеджер паролей, сейф).
set -eu
cd "$(dirname "$0")"
umask 077
mkdir -p secrets
chmod 700 secrets

random() { # random <байт>: строка из букв и цифр (безопасна в адресе подключения)
    LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c "$1"
}
make() { # make <имя> <длина>
    if [ -s "secrets/$1" ]; then
        echo "уже есть: secrets/$1"
    else
        random "$2" > "secrets/$1"
        echo "создан:   secrets/$1"
    fi
}

make db_admin_password 40
make db_password 40
make ops_token 48
make backup_cipher_pass 48
make backup_files_crypt_pass 48
make grafana_admin_password 24

if [ ! -s secrets/database_url ]; then
    printf 'postgres://scibox:%s@postgres:5432/scibox?sslmode=require' "$(cat secrets/db_password)" > secrets/database_url
    echo "создан:   secrets/database_url"
else
    echo "уже есть: secrets/database_url"
fi
chmod 600 secrets/*

echo
echo "Остались файлы, которые выдаёт провайдер (создайте сами, без лишних пробелов):"
for name in s3_access_key s3_secret_key smtp_password backup_s3_access_key backup_s3_secret_key; do
    if [ -s "secrets/$name" ]; then
        echo "  готово:    secrets/$name"
    else
        echo "  НЕ ХВАТАЕТ: secrets/$name   (пример: printf '%s' 'значение' > secrets/$name && chmod 600 secrets/$name)"
    fi
done
echo
echo "Сохраните вне сервера: secrets/backup_cipher_pass и secrets/backup_files_crypt_pass."

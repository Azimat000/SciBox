#!/bin/sh
# Свой маленький центр сертификации для репетиции боевого стенда на этом компьютере (срез 28). Выпускает сертификаты
# для хранилища (s3.scibox.test) и почтового приёмника (mail.scibox.test); приложение и pgBackRest доверяют только
# этому центру. Файлы лежат в deploy/local/pki/ (в git не попадают). Повторный запуск ничего не меняет.
set -eu
dir=$(cd "$(dirname "$0")" && pwd)/pki
mkdir -p "$dir/ca" "$dir/leaf"
if [ -s "$dir/ca/ca.crt" ] && [ -s "$dir/leaf/s3.crt" ] && [ -s "$dir/leaf/mail.crt" ]; then
    exit 0
fi
cd "$dir"
openssl genrsa -out ca/ca.key 2048 2>/dev/null
openssl req -x509 -new -nodes -key ca/ca.key -sha256 -days 825 -subj "/CN=SciBox local rehearsal CA" -out ca/ca.crt
for name in s3 mail; do
    host="$name.scibox.test"
    openssl genrsa -out "leaf/$name.key" 2048 2>/dev/null
    openssl req -new -key "leaf/$name.key" -subj "/CN=$host" -out "leaf/$name.csr"
    printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\n' "$host" > "leaf/$name.ext"
    openssl x509 -req -in "leaf/$name.csr" -CA ca/ca.crt -CAkey ca/ca.key -CAcreateserial -out "leaf/$name.crt" \
        -days 825 -sha256 -extfile "leaf/$name.ext" 2>/dev/null
    rm -f "leaf/$name.csr" "leaf/$name.ext"
done
rm -f ca/ca.srl
# Ключи читает процесс в контейнере под другим пользователем: права шире 600, потому что ключи одноразовые, только для
# репетиции на этом компьютере и в git не попадают.
chmod 644 leaf/*.key
echo "готово: $dir"

#!/bin/sh
# Репетиция боевого стенда на этом компьютере одной командой (срез 28): подготовка, сборка, выкладка, демо-данные.
#   make prod-up   или   deploy/local/up.sh [--no-seed]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
deploy=$(dirname "$here")
export SCIBOX_DEPLOY_LOCAL=1
# Базовые образы уже скачаны: репетиция не должна падать из-за медленного соединения с реестром.
export SCIBOX_DOCKER_BUILD_ARGS="${SCIBOX_DOCKER_BUILD_ARGS:---pull=false}"
"$here/prepare.sh"

echo "== хранилище и бакеты"
"$deploy/compose" up -d --no-deps --wait s3 s3-tls mailpit
"$deploy/compose" run --rm --no-deps -T --entrypoint sh backup -c '
    export RCLONE_CONFIG_S_TYPE=s3 RCLONE_CONFIG_S_PROVIDER=Other RCLONE_CONFIG_S_ENDPOINT=http://s3:8333
    export RCLONE_CONFIG_S_ACCESS_KEY_ID=rehearsal-access-key RCLONE_CONFIG_S_SECRET_ACCESS_KEY=rehearsal-secret-key-1234567890
    rclone mkdir S:scibox-files-rehearsal && rclone mkdir S:scibox-backup-rehearsal' 2>/dev/null

"$deploy/deploy.sh" --build

if [ "${1:-}" != "--no-seed" ]; then
    echo "== демо-данные"
    "$deploy/compose" --profile tools run --rm -T seed
fi
echo
echo "Репетиция боевого стенда готова: https://localhost:8443 (сертификат самоподписанный, браузер спросит подтверждение)."
echo "Письма: http://localhost:8026. Остановить: make prod-down"

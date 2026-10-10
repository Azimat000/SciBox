#!/bin/sh
# Выкладка версии (срез 28): без простоя, с возможностью отката.
#
#   deploy/deploy.sh --build            собрать образ из этого каталога (версия из git) и выложить
#   deploy/deploy.sh scibox:1.4.0       выложить уже готовый образ (локальный или из реестра)
#   deploy/deploy.sh --rollback         вернуть предыдущую версию (миграции не откатываются: они только добавляют)
#   deploy/deploy.sh --check            только проверки, ничего не менять
#
# Порядок (если шаг не прошёл, выкладка останавливается, прежняя версия продолжает работать):
#   1. проверки: файлы секретов, окружение, конфигурация compose;
#   2. образ: сборка или скачивание;
#   3. секреты раскладываются по томам;
#   4. база, копии, антивирус, мониторинг поднимаются, если ещё не работают;
#   5. шаг `release`: миграции и справочник журналов (только добавляют, поэтому старая версия работает рядом);
#   6. копии сервера заменяются по одной; следующая — только когда прежняя стала готовой (Caddy тем временем шлёт
#      запросы второй), потом воркер;
#   7. проверка снаружи.
# Репетиция на этом компьютере: SCIBOX_DEPLOY_LOCAL=1 deploy/deploy.sh --build
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(dirname "$here")
dc="$here/compose"
state="$here/.state"
mkdir -p "$state"

mode=deploy
build=0
image=""
for arg in "$@"; do
    case "$arg" in
        --build) build=1 ;;
        --rollback) mode=rollback ;;
        --check) mode=check ;;
        -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
        -*) echo "неизвестный параметр $arg" >&2; exit 2 ;;
        *) image=$arg ;;
    esac
done

say() { printf '\n== %s\n' "$*"; }
die() { printf 'ОШИБКА: %s\n' "$*" >&2; exit 1; }

# --- 1. проверки ---
say "проверки"
command -v docker >/dev/null || die "нет docker"
for name in db_admin_password db_password database_url s3_access_key s3_secret_key smtp_password ops_token \
    backup_s3_access_key backup_s3_secret_key backup_cipher_pass backup_files_crypt_pass grafana_admin_password; do
    [ -s "$here/secrets/$name" ] || die "нет файла deploy/secrets/$name (deploy/init-secrets.sh, docs/DEPLOY.md «Секреты»)"
done

case "$mode" in
    rollback)
        [ -s "$state/previous-image" ] || die "нет записи о предыдущей версии (deploy/.state/previous-image)"
        image=$(cat "$state/previous-image")
        echo "возврат на версию $image"
        ;;
    *)
        if [ "$build" = 1 ]; then
            version=$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)
            image="scibox:$version"
        fi
        [ -n "$image" ] || die "укажите образ или --build (deploy/deploy.sh --help)"
        ;;
esac
export SCIBOX_IMAGE=$image

"$dc" config -q || die "конфигурация compose не прошла проверку (не хватает переменных в deploy/.env?)"
[ "$mode" = check ] && { echo "проверки пройдены, образ: $image"; exit 0; }

# --- 2. образ ---
if [ "$build" = 1 ]; then
    say "сборка образа $image"
    # shellcheck disable=SC2086
    docker build ${SCIBOX_DOCKER_BUILD_ARGS:-} --build-arg "VERSION=$version" -t "$image" "$root"
elif [ "${SCIBOX_DEPLOY_LOCAL:-}" != 1 ] && [ "$mode" != rollback ]; then
    say "скачивание образа $image"
    "$dc" pull release || die "образ $image не скачался: он есть в реестре? для локального образа используйте --build"
fi
docker image inspect "$image" >/dev/null 2>&1 || die "образа $image нет на этом сервере"

say "образ базы и копий"
# shellcheck disable=SC2086
docker build ${SCIBOX_DOCKER_BUILD_ARGS:-} -t "${DB_IMAGE:-scibox-postgres:16.15}" "$here/postgres"

# --- 3. секреты ---
say "секреты"
"$dc" run --rm --no-deps secrets-init

# --- 4. база, копии, антивирус ---
say "база, антивирус, копии"
"$dc" up -d --wait --no-deps postgres
"$dc" up -d --no-deps backup clamav

# --- 5. миграции ---
say "миграции и справочники (release)"
"$dc" run --rm --no-deps release || die "шаг release не прошёл: версия не заменена, прежняя продолжает работать"

# --- 6. копии сервера по одной ---
# Запоминаем версию, которая работала до выкладки: её вернёт `--rollback`.
current=$(docker inspect -f '{{.Config.Image}}' "$("$dc" ps -q app1 2>/dev/null | head -1)" 2>/dev/null || true)
if [ -n "$current" ] && [ "$current" != "$image" ] && [ "$mode" != rollback ]; then
    printf '%s' "$current" > "$state/previous-image"
fi
for svc in app1 app2 worker; do
    say "замена $svc"
    "$dc" up -d --no-deps --wait --wait-timeout 180 "$svc" || die "$svc не стал готовым: оставшиеся копии продолжают работать; журнал: ./deploy/compose logs $svc"
done
say "вход (Caddy) и остальное"
"$dc" up -d --no-deps caddy
# Мониторинг (Prometheus, Alertmanager, Grafana, node-exporter) работает по умолчанию; выключается MONITORING=off в
# env-файле (на очень маленьком сервере; в репетиции на компьютере по умолчанию выключен). Порты только на 127.0.0.1 сервера (docs/DEPLOY.md, «Мониторинг»).
if [ "${SCIBOX_DEPLOY_LOCAL:-}" = 1 ]; then envfile="$here/local/local.env"; default_monitoring=off; else envfile="$here/.env"; default_monitoring=on; fi
monitoring=${MONITORING:-$(sed -n 's/^MONITORING=//p' "$envfile" | tail -1)}
if [ "${monitoring:-$default_monitoring}" != off ]; then
    say "мониторинг"
    "$here/render-alertmanager.sh"
    "$dc" --profile monitoring up -d --no-deps prometheus alertmanager node-exporter grafana
fi

# --- 7. проверка снаружи ---
say "проверка снаружи"
site=$("$dc" config 2>/dev/null | sed -n 's/^ *SCIBOX_PUBLIC_URL: https:\/\/\(.*\)$/\1/p' | head -1)
url="https://${site:-localhost}/api/health"
insecure=""
[ "${SCIBOX_DEPLOY_LOCAL:-}" = 1 ] && insecure="-k"
ok=0
for _ in $(seq 1 30); do
    if curl -fsS $insecure -o /dev/null --max-time 5 "$url"; then ok=1; break; fi
    sleep 2
done
[ "$ok" = 1 ] || die "$url не отвечает после выкладки: ./deploy/compose ps и ./deploy/compose logs caddy app1"
printf '%s' "$image" > "$state/current-image"
echo
echo "Готово: версия $image работает, $url отвечает."
echo "Вернуть предыдущую версию: deploy/deploy.sh --rollback"

# HANDOFF: записка следующей сессии

Обновлено: 2026-10-11. Срез 28 «Сборка и выкладка» готов (`docs/slices/28-build-and-deploy.md`, D-184…D-189, метка `slice-28`). Идёт серверная очередь 18–29 (D-138), потом продуктовые 15–17. `ТЕКУЩИЙ: 29` (итоговый аудит безопасности).

## Где остановились
- **Образ и стенд:** `Dockerfile` (distroless, не root, только чтение, 39 МБ), `deploy/` (боевой compose: Caddy, две копии, воркер, PostgreSQL с WAL-архивом pgBackRest, ClamAV, копии базы и файлов, мониторинг профилем), `deploy/deploy.sh` (выкладка без простоя, `--rollback`), `docs/DEPLOY.md` (чек-лист сервера, секреты, копии, восстановление, мониторинг). Хостинг пользователь пока не выбрал: настоящую выкладку, сертификат, полный ClamAV и `make load-test` на боевой машине делает он по DEPLOY.md.
- **Репетиция на Mac:** `make prod-up` (https://localhost:8443, письма http://localhost:8026), `make prod-e2e` (6 из 6), `make prod-restore-check`, `make prod-down`. Нужно ~3 ГБ памяти Docker и остановленный dev-стек (`docker compose down` в корне), перед `make test` наоборот поднять `make db-up`.
- **Копии:** решения пользователя D-184 (минуты) и D-185 (30 дней). Пароли шифрования копий (`secrets/backup_cipher_pass`, `backup_files_crypt_pass`) пользователь должен хранить вне сервера.

## Что дальше: срез 29 (аудит)
`audit-context-building` → `golang-security` (5 направлений) → `semgrep` → фаззинг и `property-based-testing` разборщиков → OWASP ZAP baseline против репетиции боевого стенда (`make prod-up`) → чек-лист ASVS L2 в SECURITY.md. Открыто в реестре: T-28, T-29 (принята), T-32, T-36, T-38, T-41, T-51, T-55, T-56 и принятые с пересмотром при аудите.

## Как запустить
- `make dev` (сервер + воркер + сайт; http://localhost:5173), вход `elena.orlova@demo.example.ru` (владелец), `dmitry.korolev@demo.example.ru` (соискатель), пароль в `server/seed/seed.go`.
- `make test` (линтер, vulncheck, `monitoring-check`, сервер с порогами, сайт; ≈15 минут), `make e2e`, `make load-test` (после `make load-data`).
- Выкладка и откат: `deploy/deploy.sh --build`, `deploy/deploy.sh --rollback`; проверка копий `deploy/restore-check.sh`.

## Неочевидные вещи
- **iCloud Drive:** папка проекта в «Документах»; Mac «выгружает» файлы (флаг `dataless`), и `docker build`, `git`, контейнеры зависают («Resource deadlock avoided»). Лечение: `find deploy server web config -path '*/node_modules' -prune -o -type f -flags +dataless -exec cat {} +`; если зависло всё, перезапустить процесс `bird` (`kill`), для tracked-файлов `git show HEAD:путь > файл`. Свободного места держать не меньше ~12 ГБ. `git add -A` может зависнуть: добавлять каталоги явно (`.claude` не трогать).
- Каждая сборка Docker даёт новый номер образа: имя образа базы содержит отпечаток (`deploy/db-image-tag`), иначе compose перезапускает базу при каждой выкладке.
- Правка `deploy/postgres/**` меняет имя образа базы: следующая выкладка перезапустит базу (несколько секунд ошибок).
- `scibox-*` и `pgbackrest`/`rclone`/`psql` в контейнерах базы вызывать через `scibox-entrypoint` (`./compose exec backup scibox-entrypoint pgbackrest info`): значения секретов в окружение кладёт он.
- Миграции после 00024 только добавляют; удаляющие помечаются `-- scibox:contract` (D-186). `application_files.legacy_data` ещё не удалён (отдельным выпуском).
- Тест `TestSeveralCopiesNeverSendTwice` был зависим от времени суток (до полудня по Москве падал); исправлено в этом срезе (`quietDemoPeople`).
- Mac: Docker (Colima) сейчас 5 ядер (на время замеров среза 27); вернуть: `colima stop && colima start --cpu 2 --memory 4`. `go build`/`go test` без git-индекса: `GOFLAGS=-buildvcs=false`. Порт базы 5433. Ошибки 500 только через `apierr.WriteInternal`. Тексты сайта только в `web/src/i18n/ru.ts`. `npx prettier` в `web/` не запускать.

## Известные проблемы и долги
- Не сделано в срезе 28: автоматический повтор удалений после восстановления (T-55), закрепление образов по хэшам (T-56), удаление `legacy_data`, полный ClamAV и Let's Encrypt проверены только по документации (T-41), цель 500 зап/с не проверена на боевой машине (`docs/SCALING.md`).
- Открытые находки прежних срезов: T-30/T-45 (принята/снижена), T-51, T-38, T-36, T-32; окно «свежих подходящих» до проверки приватности (находка 1 в `docs/slices/24-differential-review.md`); D-124 (два «Войдите…»), политика `/privacy` черновик; сквозные тесты не входят в CI (D-140).

## Окружение
Go 1.26.9 (образ `golang:1.26.9-trixie`), golangci-lint 2.12.2, govulncheck v1.8.0, k6 2.1.0, Node 26.8 (образ `node:24-trixie-slim`), Docker 29 (Colima 5 ядер, 3,8 ГБ), PostgreSQL 16.15, pgBackRest 2.59.3, rclone 1.75.2, Caddy 2.11.7, ClamAV 1.5.4, Mailpit 1.31.3, SeaweedFS 4.48, trivy 0.72, Playwright 1.63.0; мониторинг: Prometheus 3.15.0, Alertmanager 0.28.1, Grafana 13.2.3, node-exporter 1.10.2, GlitchTip 6.2.6.

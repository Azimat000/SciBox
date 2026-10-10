# HANDOFF: записка следующей сессии

Обновлено: 2026-10-10. Срез 26 «Наблюдаемость» готов (`docs/slices/26-observability.md`, D-172…D-176). Идёт серверная очередь 18–29 (D-138), потом продуктовые 15–17. `ТЕКУЩИЙ: 27` (нагрузочный тест и бюджеты).

## Где остановились
- **Внутренний порт** у каждого процесса: сервер `127.0.0.1:9100`, воркер `127.0.0.1:9101` (`SCIBOX_OPS_ADDR`, `off` выключает): `/livez`, `/readyz` (база, миграции, у воркера часы), `/metrics`, `/debug/pprof/` (в бою выключен, с секретом `SCIBOX_OPS_TOKEN`). `internal/opsserver`, сборка в `cli/observability.go`.
- **Метрики:** `internal/metrics` (счётчики событий, метки конечны: шаблон маршрута), `internal/opsmetrics` (состояние: пул, очереди писем и файлов, задачи, предохранители; кеш 5 с). Метка задач `task`, не `job`.
- **Журнал:** JSON в бою, номер запроса (`internal/reqid`) в каждой записи с контекстом запроса: пишите `logger.ErrorContext(r.Context(), …)`. Записи уровня Error уходят в GlitchTip (`internal/errreport`, `SCIBOX_ERROR_DSN`), только разрешённые поля (`allowedKeys`).
- **Мониторинг:** `docker-compose.monitoring.yml` (Prometheus 9190, Grafana 3100, node-exporter; GlitchTip 8100 в профиле `errors`), панель `monitoring/grafana/dashboards/scibox.json` (собирается скриптом `monitoring/grafana/build-dashboard.py`: правьте скрипт, не JSON), правила `monitoring/prometheus/rules/scibox.yml` и тесты `tests/scibox_test.yml`.

## Что дальше
`ТЕКУЩИЙ: 27`: сценарий смешанной нагрузки (k6 или vegeta, решение записать), `make load-test` на базе `scibox_load`, до 500 запросов в секунду на двух копиях сервера и воркере. Теперь есть чем измерять: метрики и панель, pprof на 9100. Перед замером: в `docs/SCALING.md` строки с пометкой «срез 27» (отклики лидера и `applications.org_id`, поиск по частому слову T-45, скорость отправки писем, запись счётчиков частоты, журнал действий, опрос метрик на объёме `outbox`, T-30, T-50). Базы `scibox_load` нет (удалена, ≈ 7 ГБ); перед пересозданием освободить ≥ 10 ГБ.

## Как запустить
- `make dev` (сервер + воркер + сайт; http://localhost:5173), вход `elena.orlova@demo.example.ru` (владелец), `dmitry.korolev@demo.example.ru` (соискатель), пароль в `server/seed/seed.go`. `make worker`, `make share`.
- Панель: `make monitoring-up`, http://localhost:3100. Ошибки: `make errors-up`, затем `make glitchtip-setup` (печатает `SCIBOX_ERROR_DSN`, пароль в `monitoring/glitchtip-login.txt`, не в git); проверка `scibox error-test`.
- `make test` (линтер, vulncheck, `monitoring-check`, сервер с порогами, сайт, около 15 минут), `make e2e` (внутренний порт выключен в конфиге).
- Встроенный браузер Claude не запускает `make dev` из «Документов»: `cd server && go build -o bin/scibox ./cmd/api`, затем `SCIBOX_BACKGROUND=off ./bin/scibox serve`, `./bin/scibox worker`.

## Неочевидные вещи
- **Память Docker (3,8 ГБ на все проекты, вместе с чужими контейнерами):** ClamAV + Grafana + GlitchTip вместе не помещаются: антивирус становится `unhealthy`, тест `TestFilesThroughTheWholeServerOnTheRealStorageAndAntivirus` краснеет. Лечение: `make monitoring-down`, `docker compose restart clamav` (минута). Постоянное: больше памяти Colima (`colima stop && colima start --memory 6`), решает пользователь.
- **Диск:** кеш сборки Go разрастается до 9 ГБ (`go clean -cache` безопасно); на 2026-10-10 свободно 15 ГБ. Папка проекта в iCloud: `git stash` один раз упал на `index.lock` по таймауту (рабочее дерево цело); держать ≥ 10 ГБ свободно.
- Ответ, написанный промежуточным обработчиком раньше выбора маршрута (проверка входа, 429), считается по своему адресу через `RouteNamer.Finder` (`Mux.Find`); `api_unknown` — только действительно неизвестный адрес.
- Ключи журнала со словами `token`, `secret`, `password`, `dsn` маска скрывает целиком: не называйте так безобидные поля (в срезе пришлось переименовать `token` в `protected`).
- GlitchTip: проект ищется по имени (`slug` GlitchTip сам меняет). `ENABLE_USER_REGISTRATION=False` в 6.2.6 не мешает первому входу.
- Новая фоновая задача (из среза 25): `cli/jobs.go`, имя `раздел.действие`; метрики и тревоги по ней появятся сами (`task=`), но тревога `JobNotRunning` ждёт «три срока плюс 10 минут».
- `go build` и `go test` без git-индекса: `GOFLAGS=-buildvcs=false`. Порт базы 5433. Ошибки 500 только через `apierr.WriteInternal`. Тексты сайта только в `web/src/i18n/ru.ts`. `npx prettier` в `web/` не запускать. zsh: `grep --include=*.go` в кавычках.

## Известные проблемы и долги
- T-52 (мониторинг в бою без входа и TLS), T-53 (тревоги ни к кому не приходят: нужен Alertmanager с получателем), T-51 (свободный текст ошибки в GlitchTip, принято): `docs/SECURITY.md`; T-41, T-43, T-45…T-47, T-50 из прежних срезов открыты.
- Организации с тысячами откликов: список откликов 37 мс / 70 мс (`offset=3000`), лечится `applications.org_id` (срез 27 по результатам).
- Окно «самые свежие подходящие по слову» берётся до проверки приватности (находка 1 в `docs/slices/24-differential-review.md`). `golang.org/x/net` v0.58.0 с находками в модуле (код их не вызывает): обновить до v0.60.0.
- Скорость отправки писем на настоящем почтовом сервисе не замерена: срез 27.
- Сквозные тесты не входят в CI (D-140, срез 28). D-124: два почти одинаковых «Войдите…», кнопки «скоро» у входа через сервисы. Политика `/privacy` черновик.
- `docs/DEPLOY.md` ещё нет (срез 28): туда записать выкладку индексов (`docs/SCALING.md`), `SCIBOX_BACKGROUND=off` и отдельный `worker`, `SCIBOX_SHUTDOWN_TIMEOUT`, NTP, внутренний порт и секрет (`SCIBOX_OPS_*`, Prometheus с `credentials_file`), `SCIBOX_ERROR_DSN`, вход в Grafana и закрытые порты мониторинга, Alertmanager, срок хранения событий GlitchTip, срок и порядок копий (T-40).

## Окружение
Go 1.26.9, golangci-lint 2.12.2, govulncheck v1.8.0, Node 26.8, Docker 29 (3,8 ГБ памяти, 2 ядра), PostgreSQL 16.15, Mailpit 1.31.3, SeaweedFS 4.48, ClamAV 1.5.4, Playwright 1.63.0; мониторинг: Prometheus 3.15.0, Grafana 13.2.3, node-exporter 1.10.2, GlitchTip 6.2.6 (PostgreSQL 18), prometheus/client_golang v1.24.1.

# HANDOFF: записка следующей сессии

Обновлено: 2026-10-10. Срез 27 «Нагрузочный тест и бюджеты» готов (`docs/slices/27-load-test.md`, D-177…D-183). Идёт серверная очередь 18–29 (D-138), потом продуктовые 15–17. `ТЕКУЩИЙ: 28` (сборка и выкладка).

## Где остановились
- **Нагрузочный тест:** `make load-test` (ступени 100…600 зап/с, `STEPS=`, `DURATION=`), `make load-smoke`. k6 + `loadtest/scenario.js` + `loadtest/fixtures.sql` + `scripts/load-test`; две копии сервера (:8091, :8092) и воркер на базе `scibox_load`, результаты в `loadtest/.run/<время>/` (не в git): таблицы, профили pprof, `/metrics`, нагрузка машины. Условия и цифры: `docs/SCALING.md`, раздел «Нагрузочный тест».
- **Итог на Mac:** предел около 130 зап/с (процессор базы; Go-сервер занимает 17% ядра на копию). Цель 500 зап/с не проверена на нормальном железе: **пользователь сказал, что сервер будет на платном виртуальном сервере, после переезда повторить `make load-test`** (на нём `make load-data SCALE=1`, `pg_stat_statements` включить в базе).
- **Исправлено:** подбор «Подходящие вам» 154 → 14 мс; числа главной по индексу (миграция 00024) и в фоне (`httpcache.Keyed.WithStale`); отбор поиска без лишних соединений; кеш ответов поиска вакансий и каталога гостя (`SCIBOX_SEARCH_CACHE_TTL=15s`, `off` выключает).
- **Дымовой тест:** `cli.TestMixedLoadStaysHealthy` в `make test` (≈20 с).

## Что дальше
`ТЕКУЩИЙ: 28`: Dockerfile (distroless, не root, только чтение), `deploy/docker-compose.prod.yml` (Caddy, две копии сервера, воркер, PostgreSQL, ClamAV), миграции без простоя, резервные копии (T-26), `docs/DEPLOY.md`. Хостинг пользователь пока не выбрал, но сказал, что будет платный виртуальный сервер, не Mac. В `docs/DEPLOY.md` обязательно записать: `docs/SCALING.md` «Как выкладывать индексы», `SCIBOX_BACKGROUND=off` и `worker`, `SCIBOX_SHUTDOWN_TIMEOUT`, NTP, `SCIBOX_OPS_*`, `SCIBOX_ERROR_DSN`, `SCIBOX_SEARCH_CACHE_TTL`, `shared_buffers` не меньше четверти памяти, `pg_stat_statements`, Alertmanager (T-53), закрытые порты мониторинга (T-52), срок копий (T-40), повторить `make load-test`.

## Как запустить
- `make dev` (сервер + воркер + сайт; http://localhost:5173), вход `elena.orlova@demo.example.ru` (владелец), `dmitry.korolev@demo.example.ru` (соискатель), пароль в `server/seed/seed.go`. `make worker`, `make share`.
- Нагрузка: базы `scibox_load` сейчас нет (удалена, 7 ГБ; на Mac при свободном месте меньше 8 ГБ iCloud выгружает файлы проекта). `make load-data` (≈40 минут, 7 ГБ диска; перед пересозданием свободно ≥ 10 ГБ), затем `make load-smoke` (прогрев), `make load-test`. После перезапуска Docker кеш базы холодный: первая минута медленная.
- Панель: `make monitoring-up`, http://localhost:3100; ошибки: `make errors-up`, `make glitchtip-setup`.
- `make test` (линтер, vulncheck, `monitoring-check`, сервер с порогами, сайт, около 15 минут), `make e2e`.
- Встроенный браузер не запускает `make dev` из «Документов»: `cd server && go build -o bin/scibox ./cmd/api`, затем `SCIBOX_BACKGROUND=off ./bin/scibox serve`, `./bin/scibox worker`.

## Неочевидные вещи
- **Docker (Colima) сейчас 5 ядер** (по согласию пользователя на время замеров); вернуть: `colima stop && colima start --cpu 2 --memory 4`. Памяти у Docker 3,8 ГБ на все проекты (supabase и др. чужие): ClamAV + Grafana + GlitchTip вместе не помещаются; `scripts/load-test` антивирус не поднимает (`CLAMAV=1` — с ним). `docker-compose.yml` теперь с `pg_stat_statements`; `make db-up` берёт кеш базы 256 МБ, `scripts/load-test` 1 ГБ.
- **Урок индексов:** лишний индекс может поменять план чужого запроса (00024: отдельный индекс по сроку сделал поиск без слов в десять раз медленнее). После любого нового индекса на `vacancies`, `vacancy_search`, `profiles` проверять `make load-check` и `EXPLAIN` поиска.
- **Ранг поиска** (`ts_rank_cd` ≈ 9 мкс на текст) — пол стоимости поиска по слову; короткая копия текста выигрыша не дала (D-182), не повторять.
- `go build`/`go test` без git-индекса: `GOFLAGS=-buildvcs=false`. Порт базы 5433. Ошибки 500 только через `apierr.WriteInternal`. Тексты сайта только в `web/src/i18n/ru.ts`. `npx prettier` в `web/` не запускать. zsh: `grep --include=*.go` в кавычках.
- Новая фоновая задача: `cli/jobs.go`, имя `раздел.действие`; кеш ответов нового публичного адреса: `httpcache.Keyed` (только то, что одинаково для всех, I-73).
- Диск Mac после удаления базы возвращается командой `colima ssh -- sudo fstrim -v /var/lib/docker` (не `/`: данные Docker лежат на отдельном диске); если `vitest` падает «Timeout waiting for worker» — iCloud выгрузил файлы `node_modules`, прочитать их: `find web/node_modules -type f -flags +dataless | xargs cat >/dev/null`.
- Ключи журнала со словами `token`, `secret`, `password`, `dsn` маска скрывает целиком.

## Известные проблемы и долги
- Цель 500 зап/с и p95 поиска по слову (236–242 мс на Mac) — после переезда на платный сервер; список узких мест с цифрами в `docs/SCALING.md`: поиск по слову, список откликов лидера (`applications.org_id`), скорость очереди писем (5,7 письма в секунду на ответ 25 мс), `LandingVacanciesByField`.
- T-52 (мониторинг в бою без входа и TLS), T-53 (тревоги ни к кому не приходят), T-51 (свободный текст ошибки), T-30 и T-45 принята/снижена, T-41, T-43, T-46, T-47, T-50 открыты: `docs/SECURITY.md`.
- Окно «самые свежие подходящие по слову» берётся до проверки приватности (находка 1 в `docs/slices/24-differential-review.md`). `golang.org/x/net` v0.58.0 с находками в модуле (код их не вызывает): обновить до v0.60.0.
- Сквозные тесты не входят в CI (D-140, срез 28). D-124: два почти одинаковых «Войдите…», кнопки «скоро» у входа через сервисы. Политика `/privacy` черновик.

## Окружение
Go 1.26.9, golangci-lint 2.12.2, govulncheck v1.8.0, k6 2.1.0, Node 26.8, Docker 29 (Colima 5 ядер, 3,8 ГБ), PostgreSQL 16.15, Mailpit 1.31.3, SeaweedFS 4.48, ClamAV 1.5.4, Playwright 1.63.0; мониторинг: Prometheus 3.15.0, Grafana 13.2.3, node-exporter 1.10.2, GlitchTip 6.2.6, prometheus/client_golang v1.24.1.

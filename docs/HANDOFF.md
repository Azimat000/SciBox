# HANDOFF: записка следующей сессии

Обновлено: 2026-10-02, после среза 1.

## Где остановились
Срез 1 «Фундамент» готов. Работают сервер на Go с `/api/health`, база PostgreSQL в Docker, сайт на React с временной стартовой страницей, `make dev` и `make test` с порогами покрытия. Отчёт: `docs/slices/01-foundation.md`.

## Что дальше
Срез 2 «Визуальный мир и дизайн-система» (`docs/ROADMAP.md`):
- `PRODUCT.md` уже есть, `/impeccable init` повторно не нужен. Запустить `impeccable context`, затем `reference/new-work.md`.
- Спросить пользователя `buildPath` (comp-first / code-first) перед выбором направления, если доступна генерация картинок. Ответ писать в `.impeccable/config.json`.
- Показать 2–3 направления (D-024), пользователь выбирает. Шрифт с кириллицей, не Inter.
- Заменить `web/src/index.css` и стартовую страницу (D-034): сейчас там системный шрифт и временная палитра.
- Тексты только в `web/src/i18n/ru.ts`.

## Как запустить
- `make dev`: база + Mailpit, миграции, сервер (127.0.0.1:8080) и сайт (http://localhost:5173). Остановка: Ctrl+C.
- `make test`: всё с проверкой покрытия. Сначала сам поднимает Docker-контейнеры.
- `make db-reset`, `make migrate`, `make seed` (пока заглушка), `make sqlc` (после правки `server/db/queries`).
- Docker Desktop должен быть запущен (`open -a Docker`).

## Известные проблемы и долги
- Встроенный браузер Claude не может сам выполнить `make dev` из папки «Документы» (macOS: `getcwd: Operation not permitted`). Обход: запустить `make dev` через Bash в фоне, потом `preview_start` с `url: http://localhost:5173`. После проверки остановить: `pkill -f "bin/scibox serve"; pkill -f "vite --strictPort"`.
- Непокрытые строки сервера (честно, не исключены): ветка ошибки `srv.Serve` и `Shutdown` в `internal/cli`, ветка ошибки миграции в `internal/testdb`, ошибка `NewProvider` в `migrate.Command` (миграции встроены, недостижимо).
- `seed` пока выводит «Демо-данных пока нет».

## Неочевидные вещи
- Порт базы **5433**, на 5432 у пользователя своя PostgreSQL (D-028).
- Тесты с базой: `testdb.New(t)` / `testdb.Create(t, migrated)` создают отдельную базу на тест (D-031). Пакеты, которые импортирует testdb (сейчас `migrate`), тестируются из внешнего пакета `*_test` + `export_test.go`, иначе цикл импортов.
- Критичные зоны сервера = имена пакетов в `server/coverage.conf` (D-033). Код аккаунтов класть в `internal/auth`, права в `internal/access` и т.д.; порог 97% включится сам. На фронтенде блок порогов для папки добавлять в `web/vite.config.ts`.
- Название продукта меняется в одном месте: `config/product.json` (D-029).
- React Router 8: `RouterProvider` импортировать из `react-router/dom`. `react/only-export-components` в oxlint включён с `--deny-warnings`: вспомогательные функции держать в отдельных `.ts`.
- Ошибки API: `{"error":{"code","message"}}` (D-032). На сайте `apiGet` отличает «сервер лежит» (`unreachable`) от ошибки в формате API.

## Окружение
Go 1.26.5, Node 26.8, npm 11.19, Docker 29, Mailpit 1.31.3, PostgreSQL 16.15 (проверено 2026-10-02).

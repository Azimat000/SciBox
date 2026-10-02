# Архитектура

Состояние: после среза 2 (дизайн-система). Правило: если этот файл расходится с кодом, прав код, а файл чинится в том же коммите.

## Структура папок

```
SciBox/
├── config/product.json   название продукта; читают и сервер, и сайт (D-029)
├── docker-compose.yml    PostgreSQL 16.15 (порт 5433, D-028), Mailpit v1.31.3 (8025 / 1025)
├── Makefile              dev, test, test-server, test-web, db-up, db-down, migrate, seed, db-reset, sqlc
├── scripts/
│   ├── dev.sh            собирает сервер, запускает сервер и Vite вместе
│   └── coverage-check    go vet + go test -race с покрытием + проверка порогов
├── PRODUCT.md            описание продукта для impeccable (ведёт impeccable)
├── server/               Go 1.26, модуль scibox/server
│   ├── cmd/api/          точка входа: команды serve | migrate up|down|reset|status | seed | version
│   ├── cmd/covercheck/   проверка порогов покрытия (логика в internal/covercheck)
│   ├── db/migrations/    goose, встроены в бинарник (db/embed.go)
│   ├── db/queries/       SQL для sqlc
│   ├── sqlc.yaml         sqlc запускается через `go tool sqlc` (D-030)
│   ├── coverage.conf     пороги покрытия и критичные пакеты
│   └── internal/
│       ├── cli/          разбор команд, запуск HTTP-сервера, мягкая остановка
│       ├── config/       переменные окружения + config/product.json
│       ├── covercheck/   чтение профиля покрытия и сверка с coverage.conf
│       ├── dbgen/        код sqlc (сгенерирован, не править руками)
│       ├── health/       проверка базы для /api/health
│       ├── httpapi/      роутер chi, обработчики, единый формат ошибок
│       ├── migrate/      обёртка над goose
│       └── testdb/       временные базы для тестов (D-031)
└── web/                  Vite 8 + React 19 + TypeScript 6
    ├── vite.config.ts    прокси /api → 127.0.0.1:8080; Vitest и пороги покрытия
    └── src/
        ├── app/          App (провайдеры), routes, Layout (шапка, подвал, тосты, режим), CrashPage, queryClient
        ├── api/          client.ts (apiGet, ApiError), health.ts (useHealth)
        ├── config/       product.ts (имя продукта из config/product.json)
        ├── features/
        │   ├── shell/    шапка, мобильное меню, подвал, переключатель «Ищу работу / Нанимаю» (RoleProvider), nav.ts, «Раздел готовится»
        │   ├── status/   стартовая страница (проверка сервера), 404
        │   └── styleguide/  служебная страница /styleguide (вне покрытия)
        ├── i18n/         ru.ts (все тексты), index.ts (t)
        ├── lib/          plural, deadline (срок подачи словами), normalize (поиск без регистра и ё)
        ├── styles/       tokens.css (цвета, шкалы), base.css (шрифты, сброс), layout.css
        ├── ui/           Button, Field/TextField/TextArea/Select, Combobox, Tag/FilterChip, Deadline,
        │                 VacancyEntry, Modal, ToastProvider/useToast, EmptyState, Skeleton, icons; CSS рядом с компонентом
        └── test/         setup.ts, render.tsx (renderApp, jsonResponse)
```

Позже появятся: `server/seed/` (демо-данные), `storage/uploads/` (файлы пользователей, в git не попадает), папки `features/*` по разделам.

## Порты
- web (Vite): 5173 · api: 127.0.0.1:8080 · Postgres в Docker: **5433** · Mailpit UI: 8025, SMTP: 1025

## Переменные окружения сервера
| Переменная | По умолчанию |
|---|---|
| `SCIBOX_HTTP_ADDR` | `127.0.0.1:8080` |
| `DATABASE_URL` | `postgres://scibox:scibox@localhost:5433/scibox?sslmode=disable` |
| `SCIBOX_PRODUCT_CONFIG` | `../config/product.json` (путь от папки `server/`) |
| `TEST_DATABASE_URL` (тесты) | `postgres://scibox:scibox@localhost:5433/postgres?sslmode=disable` |

## Таблицы БД
Своих таблиц пока нет.
- Миграция `00001_extensions.sql`: расширения `citext` (почты, срез 3) и `pg_trgm` (поиск, срез 6).
- Служебная таблица goose: `goose_db_version`.

## API
Формат ошибки для всех адресов (D-032): `{"error": {"code": "...", "message": "..."}}`.
Коды: `not_found` (404), `method_not_allowed` (405), `internal` (500, паника в обработчике), `database_unavailable` (503).

| Метод и адрес | Ответ |
|---|---|
| `GET /api/health` | 200 `{"status":"ok","product":"SciBox","version":"dev","database":{"schema_version":1,"server_version":"16.15"}}`; 503 `database_unavailable`, если база не ответила за 2 с |

## Страницы фронтенда
| Адрес | Что |
|---|---|
| `/` | Стартовая (в новом оформлении, содержимое пока служебное): статус сервера и базы (проверяем / работает / база недоступна / сервер не отвечает), кнопка «Проверить ещё раз» |
| `/styleguide` | Служебно: все компоненты со всеми состояниями (в меню нет) |
| `/vacancies`, `/scientists`, `/organizations`, `/favorites`, `/my-vacancies`, `/applications`, `/candidates`, `/my-organization`, `/login`, `/privacy` | «Раздел готовится» (заглушки до своих срезов) |
| `*` | 404 «Такой страницы нет» со ссылкой на главную |
| (ошибка отрисовки) | `CrashPage` через `errorElement` роутера |

## Как фронтенд отличает «сервер лежит» от «база лежит»
`apiGet` превращает любую неудачу в `ApiError`. Если ответ в формате API, код берётся из него (`database_unavailable` → сервер жив, база нет). Если ответ не в нашем формате (прокси Vite отдаёт пустой 500, когда сервер не запущен) или сеть упала, код `unreachable`.

## Дизайн-система (срез 2)
- Мир «Журнал» (D-035, D-036): белая страница, чернильно-синий `--blue` для действий, жёлтый маркер `--mark` как единственный акцент (близкий срок, активный режим и пункт меню). Заголовки и аннотации Literata, интерфейс Golos Text (`@fontsource-variable`, лежат в проекте).
- Все значения берутся из `src/styles/tokens.css`; в компонентах своих цветов и размеров нет. Тёмной темы нет.
- Режим «Ищу работу / Нанимаю» хранится в `localStorage` (`scibox.role`), пока влияет только на меню (D-038).
- Срок подачи: `describeDeadline` (`src/lib/deadline.ts`), «близкий» = до 14 дней включительно.
- Подробное описание визуальной системы: `DESIGN.md` (ведёт impeccable).

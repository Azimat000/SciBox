# Архитектура

Состояние: после среза 3 (аккаунты). Правило: если этот файл расходится с кодом, прав код, а файл чинится в том же коммите.

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
│       ├── apierr/       единый формат ответов и ошибок API (D-032), коды
│       ├── auth/         аккаунты (критичная зона): пароли argon2id, регистрация, вход, сессии, сброс пароля, письма, HTTP-обработчики, проверка Origin
│       ├── mail/         отправка писем по SMTP (Mailpit) и `Memory` для тестов
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
        │   ├── auth/     страницы входа, регистрации, сброса пароля, подтверждения, настроек, политики; меню пользователя; useMe, useForm
        │   ├── shell/    шапка, мобильное меню, подвал, переключатель «Ищу работу / Нанимаю» (RoleProvider), nav.ts, «Раздел готовится»
        │   ├── status/   стартовая страница (проверка сервера), 404
        │   └── styleguide/  служебная страница /styleguide (вне покрытия)
        ├── i18n/         ru.ts (все тексты), index.ts (t)
        ├── lib/          plural, deadline (срок подачи словами), normalize (поиск без регистра и ё)
        ├── styles/       tokens.css (цвета, шкалы), base.css (шрифты, сброс), layout.css
        ├── ui/           Button, Field/TextField/TextArea/Select, Combobox, Tag/FilterChip, Deadline,
        │                 VacancyEntry, Modal, ToastProvider/useToast, EmptyState, Skeleton, icons; CSS рядом с компонентом
        └── test/         setup.ts, render.tsx (renderApp, jsonResponse), api.ts (stubApi: подставной сервер), forms.ts
```

Позже появятся: `server/seed/` (демо-данные), `storage/uploads/` (файлы пользователей, в git не попадает), папки `features/*` по разделам.

## Порты
- web (Vite): 5173 · api: 127.0.0.1:8080 · Postgres в Docker: **5433** · Mailpit UI: 8025, SMTP: 1025

## Переменные окружения сервера
| Переменная | По умолчанию |
|---|---|
| `SCIBOX_SMTP_ADDR` | `localhost:1025` (Mailpit) |
| `SCIBOX_MAIL_FROM` | `SciBox <no-reply@scibox.local>` |
| `SCIBOX_PUBLIC_URL` | `http://localhost:5173` (из него строятся ссылки в письмах; https включает Secure у cookie) |
| `SCIBOX_HTTP_ADDR` | `127.0.0.1:8080` |
| `DATABASE_URL` | `postgres://scibox:scibox@localhost:5433/scibox?sslmode=disable` |
| `SCIBOX_PRODUCT_CONFIG` | `../config/product.json` (путь от папки `server/`) |
| `TEST_DATABASE_URL` (тесты) | `postgres://scibox:scibox@localhost:5433/postgres?sslmode=disable` |

## Таблицы БД
- Миграция `00001_extensions.sql`: расширения `citext` и `pg_trgm` (поиск, срез 6).
- Миграция `00002_accounts.sql`:
  - `users` (id uuid, email citext уникальный, display_name, password_hash, email_confirmed_at, privacy_consent_at, privacy_policy_version, created_at, updated_at);
  - `sessions` (id, user_id, token_hash, created_at, last_seen_at, expires_at, user_agent, ip);
  - `auth_tokens` (id, user_id, purpose `confirm_email|reset_password`, token_hash, created_at, expires_at, used_at);
  - `rate_events` (kind, key, at): счётчики для ограничения частоты.
- Служебная таблица goose: `goose_db_version`. Очистка устаревшего (сессии, ссылки, счётчики) раз в час в процессе сервера.

## API
Формат ошибки для всех адресов (D-032): `{"error": {"code": "...", "message": "..."}}`.
Коды: `not_found` (404), `method_not_allowed` (405), `internal` (500), `database_unavailable` (503), `bad_request` (400), `validation_failed` (422, с `fields` по полям), `unauthorized` (401), `forbidden` (403, чужой источник запроса), `rate_limited` (429, `retry_after` в секундах и заголовок Retry-After), `invalid_credentials` (401), `email_not_confirmed` (403), `invalid_token` (400), `unsupported_media_type` (415). Все ответы `/api/*` с `Cache-Control: no-store`. Запросы, меняющие данные, принимают только JSON и только со своего сайта (D-040).

| Метод и адрес | Ответ |
|---|---|
| `GET /api/auth/me` | 200 `{"user": null | {id, email, name, email_confirmed, created_at}}` |
| `POST /api/auth/register` | 202 `{email}`; 422 с полями; пишет письмо в фоне |
| `POST /api/auth/confirm-email` `{token}` | 200 `{user}` + cookie (вход) |
| `POST /api/auth/resend-confirmation` `{email}` | всегда 202 |
| `POST /api/auth/login` `{email,password}` | 200 `{user}` + cookie; 401/403/429 |
| `POST /api/auth/logout` | 204, cookie стирается |
| `POST /api/auth/forgot-password` `{email}` | всегда 202 |
| `POST /api/auth/reset-password` `{token,password}` | 204, все сессии завершаются |
| `PATCH /api/account` `{name}` | 200 `{user}` (нужен вход) |
| `POST /api/account/password` `{current_password,new_password}` | 204; другие сессии завершаются |
| `POST /api/account/sessions/revoke-others` | 204 |
| `GET /api/health` | 200 `{"status":"ok","product":"SciBox","version":"dev","database":{"schema_version":1,"server_version":"16.15"}}`; 503 `database_unavailable`, если база не ответила за 2 с |

## Страницы фронтенда
| Адрес | Что |
|---|---|
| `/` | Стартовая (в новом оформлении, содержимое пока служебное): статус сервера и базы (проверяем / работает / база недоступна / сервер не отвечает), кнопка «Проверить ещё раз» |
| `/styleguide` | Служебно: все компоненты со всеми состояниями (в меню нет) |
| `/login` (`?next=` — куда вернуть после входа), `/register`, `/forgot-password`, `/reset-password?token=`, `/confirm-email?token=`, `/account`, `/privacy` | Настоящие страницы аккаунтов (срез 3); `/account` без входа ведёт на `/login` |
| `/vacancies`, `/scientists`, `/organizations`, `/favorites`, `/my-vacancies`, `/applications`, `/candidates`, `/my-organization` | «Раздел готовится» (заглушки до своих срезов) |
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

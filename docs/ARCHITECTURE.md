# Архитектура

Состояние: после среза 6 (поиск вакансий). Правило: если этот файл расходится с кодом, прав код, а файл чинится в том же коммите.

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
│   ├── seed/             демо-данные (вымышленные организации, люди и вакансии; `profiles.go` и `generated.go` собирают 22 организации и ~190 вакансий из научных направлений); вне покрытия, проверяется тестом в internal/cli
│   └── internal/
│       ├── access/       права по ролям организации (критичная зона): Actor.Can(действие, подразделение), без базы и HTTP
│       ├── orgs/         организации, подразделения, сотрудники, приглашения, каталог (критичная зона): сервис, письма, HTTP-обработчики
│       ├── vacancies/    вакансии (критичная зона): поля и проверки по типу позиции, жизненный цикл (таблица переходов), права через access, поиск (`search.go`), «мои вакансии», HTTP
│       ├── refdata/      справочники (специальности ВАК, регионы, должности, источники): `GET /api/reference`
│       ├── apierr/       единый формат ответов и ошибок API (D-032), коды, DecodeJSON
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
        │   ├── orgs/     каталог, страницы организации и подразделения, «Организация» (мои организации и приглашения), управление (данные, подразделения, сотрудники), принятие приглашения; api.ts, labels.ts, RequireUser
        │   ├── vacancies/ страницы «Мои вакансии», вакансия (статья + управление), форма (по типу позиции), список для страниц организаций; api.ts (+ справочники useReference), labels.ts, formValues.ts
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

Страницы организации и подразделения показывают список опубликованных вакансий (`VacancyList`).

Позже появятся: `storage/uploads/` (файлы пользователей, в git не попадает), папки `features/*` по разделам.

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
- Миграция `00003_organizations.sql`:
  - `organizations` (id, slug уникальный, name, kind, city, website, description, created_by, created_at, updated_at; индекс pg_trgm по названию);
  - `org_members` (org_id, user_id, role `owner|hr|unit_head`, joined_at; ключ org_id+user_id);
  - `units` (id, org_id, name, kind `department|laboratory|division|shared_facility`, description, topics text[], head_user_id, created_at, updated_at);
  - `org_invitations` (id, org_id, email, role, unit_id, token_hash, invited_by, created_at, expires_at, accepted_at, revoked_at).
  - Счётчики частоты в общей `rate_events`: `org_create` (5 за сутки на человека), `org_invite` (30 за час).
- Миграция `00004_reference.sql`: `reference_sources` (источники), `science_fields` / `science_groups` / `specialties` (номенклатура ВАК, 5 / 35 / 350), `regions` (89), `positions` (27, с типом). Данные внутри миграции (docs/REFERENCE.md).
- Миграция `00005_vacancies.sql`:
  - `vacancies` (id, org_id, unit_id → `ON DELETE RESTRICT`, created_by, status `draft|published|closed|archived`, title, position_code, summary, description, requirements, focus, career_level 1–4, work_format, region_code, city, housing, rate_percent, salary_from/to, contract_type, contract_months, funding_source, funding_note, degree_required, title_required, is_competition, deadline date, published_at, closed_at, archived_at, created_at, updated_at; CHECK на значения);
  - `vacancy_specialties` (vacancy_id, specialty_code);
  - представление `vacancy_view` (вакансия + названия должности, региона, организации, подразделения; при добавлении колонок в `vacancies` пересоздать).
  - Счётчик частоты `vacancy_create` (100 в сутки на человека) в общей `rate_events`.
- Миграция `00006_search.sql`: `vacancy_search` (vacancy_id, doc tsvector; индекс GIN), функция `refresh_vacancy_search(uuid)` и триггеры: на `vacancies` (при вставке и смене текстовых полей), `vacancy_specialties`, `organizations` (название, город), `units` (название). Текст в `vacancy_view` не входит.
- Служебная таблица goose: `goose_db_version`. Очистка устаревшего (сессии, ссылки, счётчики; приглашения старше 30 дней после срока) раз в час в процессе сервера.

## API
Формат ошибки для всех адресов (D-032): `{"error": {"code": "...", "message": "..."}}`.
Коды: `unit_has_vacancies` (409, удаление подразделения с вакансиями), `invalid_status_change` / `vacancy_not_draft` / `vacancy_changed` (409), `not_found` (404), `method_not_allowed` (405), `internal` (500), `database_unavailable` (503), `bad_request` (400), `validation_failed` (422, с `fields` по полям), `unauthorized` (401), `forbidden` (403, чужой источник запроса), `rate_limited` (429, `retry_after` в секундах и заголовок Retry-After), `invalid_credentials` (401), `email_not_confirmed` (403), `invalid_token` (400), `unsupported_media_type` (415). Все ответы `/api/*` с `Cache-Control: no-store`. Запросы, меняющие данные, принимают только JSON и только со своего сайта (D-040).

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
| `GET /api/organizations?q=&kind=&limit=&offset=` | публичный каталог: `{items:[{id,slug,name,kind,city,summary,unit_count}], total}`; поиск по названию и городу без учёта регистра |
| `GET /api/organizations/{slug}` | публично: `{organization, units:[…], viewer}`; `viewer` (роль и права) только у вошедшего; номера аккаунтов руководителей только сотрудникам |
| `GET /api/organizations/{slug}/units/{id}` | публично: страница подразделения |
| `POST /api/organizations` | 201, создатель становится владельцем; 422 с полями; 429 при пятой в сутки |
| `PATCH /api/organizations/{slug}` | владелец |
| `POST /api/organizations/{slug}/units`, `PATCH …/units/{id}`, `DELETE …/units/{id}`, `PUT …/units/{id}/head {user_id|null}` | создание, удаление и назначение руководителя: владелец; правка: владелец или руководитель этого подразделения |
| `GET /api/organizations/{slug}/members` | владелец: сотрудники с почтами и ожидающие приглашения |
| `PATCH …/members/{userId} {role}`, `DELETE …/members/{userId}` | владелец; уйти (DELETE себя) может любой сотрудник; последний владелец 409 `last_owner` |
| `POST …/invitations {email,role,unit_id}`, `DELETE …/invitations/{id}` | владелец; письмо в фоне |
| `POST /api/invitations/lookup {token}` | без входа: что за приглашение (и совпадает ли почта с вошедшим) |
| `POST /api/invitations/accept {token}`, `POST /api/invitations/{id}/accept` | вошедший; чужая почта 403 `invitation_wrong_email`, устарело 400 `invalid_invitation`, уже в организации 409 `already_member` |
| `GET /api/my/organizations` | `{organizations:[…с ролью], invitations:[…на почту человека]}` |
| `GET /api/reference` | публично: `{science:[область → группы → специальности], regions, positions:[{code,type,name}], sources}` |
| `GET /api/vacancies?q=&field=&region=&format=&type=&level=&degree=&org_kind=&funding=&rate=&term=&salary_min=&housing=&competition=&deadline=&sort=&org=&unit=&limit=&offset=` | публичный поиск: опубликованные вакансии без прошедшего срока: `{items:[карточка], total, fuzzy}`; все параметры необязательны, повторяющиеся (`field`, `format`, `type`, `level`, `degree`, `org_kind`, `funding`, `rate`, `term`) значат «или»; неизвестное значение 422 с `fields`; `limit` до 100, по умолчанию 50 |
| `POST /api/vacancies` `{organization: slug, …поля}` | 201 `{vacancy}`: черновик; ведущий вакансии этого подразделения; 422 с полями; 429 после 100 в сутки |
| `GET /api/vacancies/{id}` | опубликованную и закрытую видят все; черновик и архив только ведущие вакансии (остальным 404); в ответе `viewer:{can_manage, transitions}` |
| `PATCH /api/vacancies/{id}` | правка (черновик проверяется мягко, остальное строго); перенос в другое подразделение нужно право на оба |
| `POST /api/vacancies/{id}/status` `{status}` | смена статуса по таблице переходов; публикация проверяет готовность; 409 `invalid_status_change`, `vacancy_changed` |
| `DELETE /api/vacancies/{id}` | только черновик; иначе 409 `vacancy_not_draft` |
| `GET /api/my/vacancies?status=&limit=&offset=` | «Мои вакансии»: `{items,total,counts по статусам}` в пределах прав |
| `GET /api/my/vacancy-targets` | где человек может создать вакансию: `[{organization, whole_org, units}]` |
| `GET /api/health` | 200 `{"status":"ok","product":"SciBox","version":"dev","database":{"schema_version":1,"server_version":"16.15"}}`; 503 `database_unavailable`, если база не ответила за 2 с |

## Страницы фронтенда
| Адрес | Что |
|---|---|
| `/`, `/vacancies` | Поиск вакансий (срез 6): строка поиска и регион, колонка фильтров (на узком экране раскрывается кнопкой), выбранные фильтры тегами, порядок, список, страницы по 20. Всё выбранное в адресе: `q`, `field`, `region`, `type`, `level`, `format`, `degree`, `org_kind`, `funding`, `rate`, `term`, `salary_min`, `housing`, `competition`, `deadline`, `sort`, `page` |
| `/status` | Статус сервера и базы (проверяем / работает / база недоступна / сервер не отвечает), кнопка «Проверить ещё раз» (бывшая стартовая) |
| `/styleguide` | Служебно: все компоненты со всеми состояниями (в меню нет) |
| `/login` (`?next=` — куда вернуть после входа), `/register`, `/forgot-password`, `/reset-password?token=`, `/confirm-email?token=`, `/account`, `/privacy` | Настоящие страницы аккаунтов (срез 3); `/account` без входа ведёт на `/login` |
| `/organizations` (поиск и тип в адресе, страницы по 20), `/organizations/new`, `/organizations/:slug`, `/organizations/:slug/units/:unitId` | Каталог, создание, публичные страницы организации и подразделения (срез 4) |
| `/my-organization` | Мои организации и приглашения; без входа ведёт на `/login?next=` |
| `/my-organization/:slug` (+ `/units`, `/units/new`, `/units/:unitId`, `/members`) | Управление: данные, подразделения, сотрудники и приглашения (вкладка «Сотрудники» только владельцу) |
| `/invitations/accept?token=` | Страница по ссылке из письма |
| `/my-vacancies` (`?status=`, `?page=`) | Мои вакансии: вкладки по статусам со счётчиками, действия; без входа ведёт на `/login?next=` |
| `/my-vacancies/new` (`?org=&unit=`), `/my-vacancies/:id/edit` | Форма вакансии; поля зависят от типа позиции |
| `/vacancies/:id` | Страница вакансии (статья); тем, кто ведёт вакансию, сверху управление: править, сменить статус, удалить черновик |
| `/scientists`, `/favorites`, `/applications`, `/candidates` | «Раздел готовится» (заглушки до своих срезов) |
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

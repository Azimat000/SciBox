# Архитектура

Состояние: после среза 11 (избранное, сохранённые поиски, подбор, сроки и напоминания). Правило: если этот файл расходится с кодом, прав код, а файл чинится в том же коммите.

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
│   ├── seed/             демо-данные (вымышленные организации, люди и вакансии; `profiles.go` и `generated.go` собирают 22 организации и ~190 вакансий из научных направлений; `scientists.go` — 14 профилей учёных через сервис профилей; `applications.go` — 14 откликов через сервис откликов; `matching.go` — избранное и сохранённые поиски через сервис подбора); вне покрытия, проверяется тестом в internal/cli
│   └── internal/
│       ├── access/       права по ролям организации (критичная зона): Actor.Can(действие, подразделение), без базы и HTTP
│       ├── orgs/         организации, подразделения, сотрудники, приглашения, каталог (критичная зона): сервис, письма, HTTP-обработчики
│       ├── vacancies/    вакансии (критичная зона): поля и проверки по типу позиции, жизненный цикл (таблица переходов), права через access, поиск (`search.go`; разбор и запись условий в адресе: `query.go`), «мои вакансии», HTTP
│       ├── privacy/      приватность профиля (критичная зона): режимы скрыт / организациям / публичный, кто видит профиль и контакты; без базы и HTTP
│       ├── profiles/     профиль учёного (критичная зона): основные поля, записи разделов, приватность, поиск по DOI, сборка резюме, каталог учёных (`catalog.go`), HTTP
│       ├── applications/ отклики (критичная зона): отправка (снимок профиля, резюме, PDF-файлы, рекомендатели), «Мои отклики», карточка глазами соискателя и организации, выдача файлов, отзыв, «можно ли откликнуться»; разбор организацией (`review.go`: «просмотрен» при открытии, решение, приглашения и ответы на них, список откликов), таблицы переходов (`transitions.go`), проверка приглашений (`invitation_input.go`), тексты уведомлений (`notices.go`), HTTP
│       ├── references/   рекомендательные письма (критичная зона): просьбы, одноразовые ссылки, письмо текстом/PDF, отказ, повтор и отмена, HTTP (в том числе страница рекомендателя без входа)
│       ├── files/        файлы откликов (критичная зона): проверка PDF, имена, multipart-запрос, таблица «вид файла → кто видит», безопасная выдача
│       ├── offers/       приглашения учёных на вакансии (критичная зона): отправка (права `ManageVacancies`, приватность профиля, лимит), ответ «Интересно / Не сейчас», отзыв, списки обеих сторон, «куда можно пригласить», тексты уведомлений (`notices.go`), HTTP
│       ├── matching/     избранное, сохранённые поиски, подбор, календарь сроков (критичная зона): правила подбора (`rules.go`, без базы), расписание (`schedule.go`), сервис (`service.go`), фоновая рассылка по поискам (`digest.go`) и напоминания о сроках (`reminders.go`), тексты писем (`notices.go`), HTTP
│       ├── notifications/ уведомления и очередь писем (критичная зона): колокольчик, письма через `outbox`, отправитель с повторами, переключатели писем (`settings.go`), HTTP
│       ├── testkit/      помощники для тестов с базой (временная база, сервисы, люди, организация со всеми ролями, «поломка» n-го запроса); вне покрытия
│       ├── crossref/     клиент Crossref (поиск публикации по DOI), нормализация DOI
│       ├── cv/           отрисовка PDF по описанию документа (fpdf, шрифты в `fonts/`, лицензия OFL)
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
        │   ├── applications/ форма отклика, кнопка на вакансии (`ApplyBlock`), «Мои отклики», отклик глазами соискателя (рекомендации: повторить, отменить, добавить), список откликов организации (`CandidatesPage`), карточка отклика для организации (панель решения), приглашения (`Invitations`, окна `InviteModal`, `DecisionModal`, `AnswerModals`), страница рекомендателя; api.ts, labels.ts, review.css
        │   ├── catalog/  каталог учёных: страница `/scientists` (поиск, фильтры, чипы, порядок, страницы), запись `ScientistEntry`, фильтры, адрес (`params.ts`)
        │   ├── matching/ избранное, подбор, сохранённые поиски, сроки: четыре страницы-вкладки (`FavoritesPage`, `MatchesPage`, `SavedSearchesPage`, `DeadlinesPage`), закладка `FavoriteButton`, «Сохранить поиск» (`SaveSearch.tsx`), открытие поиска из уведомления (`OpenSavedSearchPage`), письма в настройках аккаунта (`MailSettings`); api.ts, labels.ts, matching.css
        │   ├── offers/   приглашения на вакансии: окно «Пригласить» и кнопка, «Приглашения» учёного (`/offers`, `/offers/:id`), «Отправленные приглашения» организации (`/sent-offers`), вкладки «Отклики / Приглашения»; api.ts, labels.ts
        │   ├── notifications/ колокольчик в шапке (окно с последними, число непрочитанных раз в минуту), страница «Уведомления»; api.ts
        │   ├── profile/  «Мой профиль» (правка разделов в окнах, приватность), форма основного, страница учёного для других, разметка профиля `ProfileView`, поля записей по описанию (`sections.ts`)
        │   ├── shell/    шапка, мобильное меню, подвал, переключатель «Ищу работу / Нанимаю» (RoleProvider), nav.ts
        │   ├── status/   стартовая страница (проверка сервера), 404
        │   └── styleguide/  служебная страница /styleguide (вне покрытия)
        ├── i18n/         ru.ts (все тексты), index.ts (t)
        ├── lib/          plural, deadline (срок подачи словами), normalize (поиск без регистра и ё)
        ├── styles/       tokens.css (цвета, шкалы), base.css (шрифты, сброс), layout.css
        ├── ui/           Button, Field/TextField/TextArea/Select (с группами пунктов), FilterGroup/ScienceFilter (группы фильтров и области науки; поиск и каталог), Combobox, Tag/FilterChip, FilePicker (выбор PDF), Deadline,
        │                 VacancyEntry (слот `action` под сроком: закладка), Modal, ToastProvider/useToast, EmptyState, Skeleton, icons; CSS рядом с компонентом
        └── test/         setup.ts, render.tsx (renderApp, jsonResponse), api.ts (stubApi: подставной сервер), forms.ts
```

Страницы организации и подразделения показывают список опубликованных вакансий (`VacancyList`).

Файлы откликов лежат в базе (D-080), папки `storage/uploads/` нет.

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
| `SCIBOX_CROSSREF_URL` | `https://api.crossref.org` (поиск публикаций по DOI) |
| `SCIBOX_CROSSREF_MAILTO` | пусто (почта для «вежливого пула» Crossref, необязательно) |
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
- Миграция `00007_profiles.sql`:
  - `profiles` (id, user_id уникальный → `users` `ON DELETE CASCADE`, visibility `hidden|orgs|public`, open_to_offers, headline, city, region_code, about, degree `none|candidate|doctor`, degree_specialty_code, degree_year, degree_institution, dissertation_title, academic_title `none|docent|professor`, academic_title_year, orcid, spin, scopus_id, wos_id, h_rsci, h_scopus, h_wos, h_scholar, contact_email, created_at, updated_at; CHECK на длины и перечисления, форматы проверяет сервис);
  - `profile_specialties` (profile_id, specialty_code);
  - `profile_items` (id, profile_id, kind `education|experience|publication|grant|patent|teaching`, sort_year, data jsonb, created_at, updated_at; уникальный индекс по `(profile_id, data->>'doi')` для публикаций).
  - Счётчик частоты `doi_lookup` (60 в час на человека) в общей `rate_events`.
- Миграция `00008_applications.sql`:
  - `applications` (id, vacancy_id → `vacancies` `ON DELETE RESTRICT`, user_id → `users` `ON DELETE CASCADE`, status `sent|viewed|invited|rejected|accepted|withdrawn`, cover_letter, contact_email, profile jsonb (снимок профиля), created_at, updated_at, status_changed_at; уникальный индекс `(vacancy_id, user_id) WHERE status <> 'withdrawn'`);
  - `reference_requests` (id, application_id `CASCADE`, name, email citext, relation, token_hash уникальный, status `pending|received|declined`, letter_text, created_at, expires_at, last_sent_at, send_count, answered_at; уникальный `(application_id, email)`);
  - `application_files` (id, application_id `CASCADE`, reference_id → `reference_requests` `CASCADE` (только у `reference_letter`), kind `cv|attachment|reference_letter`, name, size, position, data bytea, created_at);
  - `notifications` (id, user_id `CASCADE`, kind, title, body, link, created_at, read_at);
  - `outbox` (id bigserial, to_email, subject, body, created_at, next_attempt_at, attempts, last_error, sent_at, failed_at; индекс по `next_attempt_at` для неотправленных).
  - Счётчики частоты в общей `rate_events`: `apply` (20 в сутки на человека), `reference_request` (15 в сутки на человека).
- Миграция `00009_review.sql`:
  - `applications` + `decision_note` (записка к решению, до 1000 знаков), `decided_by` (кто решил, `ON DELETE SET NULL`);
  - `application_invitations` (id, application_id `CASCADE`, kind `interview|contacts|request_contacts`, status `pending|confirmed|proposed|answered|shared|cancelled`, message, starts_at, place_kind `online|onsite`, place, contact_name, contact_email, contact_phone, answer_at (предложенное время), answer_note, answer_contact, answer_time, answered_at, created_by, created_at, updated_at; CHECK: поля собеседования заполнены ровно у `interview`).
- Миграция `00010_offers.sql`: `vacancy_offers` (id, vacancy_id → `vacancies` `ON DELETE RESTRICT`, user_id → `users` `ON DELETE CASCADE`, invited_by → `users` `ON DELETE SET NULL`, status `pending|interested|declined|cancelled`, message, answer_note (до 1000 знаков), answered_at, created_at, updated_at; CHECK: `answered_at` есть ровно у ответивших; уникальный индекс `(vacancy_id, user_id) WHERE status <> 'cancelled'`). Счётчик частоты `offer` (20 в сутки на человека) в общей `rate_events`.
- Миграция `00011_matching.sql`:
  - `favorites` (user_id → `users` `CASCADE`, vacancy_id → `vacancies` `CASCADE`, created_at; ключ user_id+vacancy_id);
  - `saved_searches` (id, user_id `CASCADE`, name до 120, query до 2000 знаков в каноническом виде, frequency `instant|daily|weekly|off`, checked_at (граница «всё до неё учтено»), next_run_at, last_sent_at, created_at, updated_at; индекс по `next_run_at` для включённых);
  - `deadline_reminders` (user_id, vacancy_id, stage 1 или 7, deadline, sent_at; ключ из четырёх полей: одно напоминание на человека, вакансию, ступень и срок);
  - `notification_settings` (user_id, email_new_vacancies, email_deadlines, updated_at).
- Служебная таблица goose: `goose_db_version`. Очистка устаревшего (сессии, ссылки, счётчики; приглашения старше 30 дней после срока) раз в час в процессе сервера.

## API
Формат ошибки для всех адресов (D-032): `{"error": {"code": "...", "message": "..."}}`.
Коды: `invalid_invitation_state` / `too_many_invitations` (409), `unit_has_vacancies` (409, удаление подразделения с вакансиями), `invalid_status_change` / `vacancy_not_draft` / `vacancy_changed` (409), `not_found` (404), `method_not_allowed` (405), `internal` (500), `database_unavailable` (503), `bad_request` (400), `validation_failed` (422, с `fields` по полям), `unauthorized` (401), `forbidden` (403, чужой источник запроса), `rate_limited` (429, `retry_after` в секундах и заголовок Retry-After), `invalid_credentials` (401), `email_not_confirmed` (403), `invalid_token` (400), `unsupported_media_type` (415), `payload_too_large` (413, запрос с файлами больше предела). Все ответы `/api/*` с `Cache-Control: no-store`. Запросы, меняющие данные, принимают только JSON и только со своего сайта (D-040).

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
| `GET /api/profile` | вошедший: свой профиль (создаётся пустым и скрытым): `{profile, viewer:{is_owner, can_see_contacts}}`; в `profile` есть `visibility`, `contact_email`, разделы `sections` (всегда списки) |
| `PUT /api/profile` | основные поля целиком (`headline, city, region_code, about, degree, degree_specialty_code, degree_year, degree_institution, dissertation_title, academic_title, academic_title_year, orcid, spin, scopus_id, wos_id, h_rsci, h_scopus, h_wos, h_scholar, contact_email, specialties`); 422 с полями; приватность и записи не трогает |
| `PUT /api/profile/privacy` `{visibility, open_to_offers}` | 200 `{profile, viewer}`; 422 при неизвестном режиме |
| `POST /api/profile/items` `{kind, …поля вида}` | 201 `{item}`; виды `education, experience, publication, grant, patent, teaching`; 422 с полями; 409 `too_many_items` |
| `PUT /api/profile/items/{id}` `{kind?, …поля}` | 200 `{item}`; чужая или несуществующая запись 404; сменить вид нельзя (422) |
| `DELETE /api/profile/items/{id}` | 204; чужая запись 404 |
| `GET /api/profile/doi?doi=` | 200 `{work:{doi,title,authors,venue,year,type,volume,issue,pages}}` из Crossref; 422 неверный или уже добавленный DOI; 404 `doi_not_found`; 502 `doi_unavailable`; 429 после 60 в час |
| `GET /api/profile/cv` | PDF собственного резюме (`Content-Disposition: attachment`) |
| `GET /api/scientists/{id}` | публично, с учётом приватности: `{profile, viewer}`; скрытый или неизвестный 404; контакты только владельцу и сотрудникам организаций, `visibility` только владельцу |
| `GET /api/scientists/{id}/cv` | PDF резюме по тем же правилам (нужен вход) |
| `POST /api/applications` (multipart: часть `data` — JSON `{vacancy_id, contact_email, cover_letter, referees:[{name,email,relation}]}`, части `file` — PDF) | 201 `{application}`; 422 с полями (`contact_email`, `cover_letter`, `referees`, `files`, `profile`); 409 `vacancy_closed`, `deadline_passed`, `already_applied`; 403 `own_vacancy`; 404; 429 |
| `GET /api/applications?limit=&offset=` | «Мои отклики»: `{items:[{id,status,created_at,status_changed_at,vacancy,references:{total,received}}], total}` |
| `GET /api/applications/for-vacancy/{vacancyId}` | `{can_apply, reason?: own_vacancy|closed|deadline_passed|applied, application}` для вошедшего |
| `GET /api/applications/{id}` | карточка: `{application}` с `viewer` `{role, can_withdraw, decisions, can_invite}`, `decision_note`, `invitations`; `applicant` (рекомендации — только статусы) или `staff` (с письмами; первое открытие ставит «просмотрен» и уведомляет соискателя); чужой и несуществующий — одинаковый 404 |
| `GET /api/applications/{id}/files/{fileId}` | PDF как скачивание; письмо рекомендателя соискателю — 404 |
| `POST /api/applications/{id}/withdraw` | 204; 409 `invalid_status_change` |
| `POST /api/applications/{id}/references` `{name,email,relation}` | 201 `{reference}`; 409 `too_many_references`, `application_closed` |
| `POST …/references/{refId}/resend`, `DELETE …/references/{refId}` | повтор (429 `resend_too_soon` пока не прошли сутки) и отмена (409 `reference_not_pending`) |
| `POST /api/recommendations/lookup {token}` | без входа: `{request:{referee_name,relation,applicant_name,vacancy_title,org_name,status,expires_at}}`; 404 `invalid_link`, 410 `link_expired`, 410 `application_withdrawn` |
| `POST /api/recommendations/submit` (multipart: `data` `{token,text}`, не более одной части `file`) | 200; 422 (`text`, `file`); 409 `already_answered` |
| `POST /api/recommendations/decline {token}` | 200; те же ошибки |
| `GET /api/my/candidates?vacancy=&status=&limit=&offset=` | отклики на вакансии, которые вошедший вправе разбирать (`ViewApplications`: организации целиком и подразделения, которыми руководит): `{items:[{id,status,created_at,applicant_name,headline,vacancy,unit_name,references,pending_invitations,proposed_invitations}], total, counts}`; `counts` по всем статусам (при выбранной вакансии только по ней); 422 на неизвестный статус и неверный номер вакансии; чужая вакансия даёт пустой список |
| `GET /api/my/candidate-vacancies` | `{items:[{id,title,status,org_name,org_slug,total,new}]}`: вакансии с откликами (отозванные не считаются, `new` = «отправлен») |
| `POST /api/applications/{id}/status` `{status: accepted|rejected, note}` | 204; только сотрудник с правом (иначе 404); 409 `invalid_status_change` из недопустимого статуса; 422 `status`, `note`; отказ закрывает открытые приглашения |
| `POST /api/applications/{id}/invitations` `{kind, message, starts_at, place_kind, place, contact_name, contact_email, contact_phone}` | 201 `{invitation}`; 422 по полям (`starts_at` RFC 3339, будущее, не дальше года); 409 `invalid_status_change` (отклик решён/отозван), `too_many_invitations` (10); отклик становится «приглашён», новое собеседование заменяет прежнее открытое |
| `DELETE /api/applications/{id}/invitations/{invId}` | 204; 409 `invalid_invitation_state` |
| `POST /api/applications/{id}/invitations/{invId}/accept-proposal` | 204: время, предложенное соискателем, становится временем собеседования; 409 `invalid_invitation_state` (нет предложения или оно прошло) |
| `POST /api/applications/{id}/invitations/{invId}/answer` `{action: confirm|propose|reply, proposed_at, note, contact, time}` | 204; только автор отклика (иначе 404); один раз, пока приглашение `pending`; 422 по полям; 409 `invalid_invitation_state` |
| `GET /api/notifications?limit=&offset=&unread=1` | `{items:[{id,kind,title,body,link,created_at,read}], total, unread}` (только свои) |
| `GET /api/notifications/unread-count` | `{unread}` |
| `POST /api/notifications/{id}/read`, `POST /api/notifications/read-all` | 204; чужое уведомление 404 |
| `GET /api/scientists?q=&field=&region=&degree=&title=&open=1&h_min=&sort=&limit=&offset=` | публично, с учётом приватности (D-098): `{items:[{id,name,headline,city,region,degree,academic_title,open_to_offers,h_index,publications,specialties,updated_at}], total, fuzzy}`; повторяющиеся `field`, `degree`, `title` значат «или»; `sort`: `relevance`, `updated`, `h_index`, `name`; `limit` до 50, по умолчанию 20; неизвестное значение и не число 422 с `fields`; контактов в ответе нет |
| `GET /api/scientists/{id}/offer-targets` | вошедший: `{items:[{id,title,org_name,org_slug,unit_name,deadline,offered,applied}]}` — вакансии, на которые он может пригласить этого учёного (опубликованные, срок не прошёл, в пределах `ManageVacancies`); закрытый приватностью профиль 404; не сотрудник организации получает пустой список |
| `POST /api/offers` `{vacancy_id, profile_id, message}` | 201 `{offer}`; 404 (нет прав на вакансию или профиль закрыт: неотличимо от «нет такого»); 409 `vacancy_closed`, `deadline_passed`, `already_offered`, `already_applied`, `invitee_is_staff`; 422 `message`; 429 после 20 в сутки |
| `GET /api/offers?status=&limit=&offset=` | «Приглашения» учёного: `{items:[offer], total, counts:{pending,interested,declined}, pending}`; отозванные не показываются; 422 на неизвестное состояние |
| `GET /api/offers/{id}` | `{offer}` для приглашённого (с `application_id`, `can_answer`); чужое, отозванное и несуществующее 404 |
| `POST /api/offers/{id}/answer` `{action: interested|declined, note}` | 204; только приглашённый, один раз; 422 `action`, `note`; 409 `invalid_offer_state` |
| `POST /api/offers/{id}/cancel` | 204; ведущий вакансию; чужое 404; 409 `invalid_offer_state`, если уже ответили или отозвали |
| `GET /api/my/sent-offers?vacancy=&status=&limit=&offset=` | «Отправленные приглашения» организации: `{items:[offer со scientist{profile_id,name} и can_cancel], total, counts по четырём состояниям}` в пределах `ManageVacancies`; 422 на неверные `vacancy` и `status` |
| `GET /api/favorites?limit=&offset=` | вошедший: `{items:[{vacancy: карточка, added_at, state: open|expired|closed, application_id}], total}`: сначала открытые, внутри групп новые добавления сверху; архивные не показываются; `limit` до 50, по умолчанию 20 |
| `GET /api/favorites/ids` | `{ids:[…]}`: номера всех избранных вакансий (для закладок в списках) |
| `PUT /api/favorites/{vacancyId}` | 204; повтор ничего не меняет; черновик, архив и неизвестная вакансия 404; 409 `too_many_favorites` (предел 200) |
| `DELETE /api/favorites/{vacancyId}` | 204, даже если вакансии в избранном не было |
| `GET /api/saved-searches` | `{items:[{id,name,query,frequency,created_at,last_sent_at}]}`: свои, новые сверху |
| `POST /api/saved-searches` `{name, query, frequency}` | 201 `{search}`; `query` — строка запроса страницы поиска (проверяется и записывается в каноническом виде); 422 по полям `name`, `frequency`, `query` (в том числе «условий нет»); 409 `too_many_searches` (предел 20) |
| `GET /api/saved-searches/{id}`, `PATCH …` `{name, frequency}`, `DELETE …` | свой поиск; чужой и несуществующий 404; правка меняет название и частоту (условия нет); включение после «не сообщать» начинает счёт новых заново |
| `GET /api/matches?limit=&offset=` | «Подходящие вам»: `{items:[{vacancy, score, reasons:[коды]}], total, ready, basis:{specialties, level, has_region, has_degree}}`; `ready: false` без областей науки в профиле |
| `GET /api/deadlines` | календарь сроков избранного: `{items:[{vacancy, days_left, application_id}], without_deadline}` (опубликованные, срок сегодня или позже, ближайшие первыми) |
| `GET /api/notification-settings`, `PUT …` `{email_new_vacancies, email_deadlines}` | какие письма присылать (оба поля обязательны); нет строки — всё включено |
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
| `/profile` | Мой профиль: кнопки «Изменить основное», «Скачать резюме», «Как видят другие», блок приватности, профиль с «Добавить / Изменить / Удалить» у разделов; без входа ведёт на `/login?next=` |
| `/profile/edit` | Форма основного: кто вы, степень и звание, специальности, ORCID/SPIN/Scopus/WoS, h-index, контактная почта |
| `/scientists/:id` | Страница учёного для других (приватность решает сервер; «нет профиля» и «скрыт» выглядят одинаково); у вошедшего в режиме «Нанимаю» кнопка «Пригласить на вакансию» |
| `/vacancies/:id/apply` | Форма отклика (письмо, файлы, рекомендатели); без входа ведёт на вход и обратно |
| `/applications`, `/applications/:id` | «Мои отклики» (страницы по 20, отметка «Ждёт вашего ответа») и отклик глазами соискателя: решение, приглашения с ответами (если открыл сотрудник организации — переход на `/candidates/:id`) |
| `/candidates` (`?vacancy=&status=&page=`) | Список откликов организации: отбор по вакансии, вкладки по статусам со счётчиками, страницы по 20 |
| `/candidates/:id` | Карточка отклика для организации: решение (пригласить, принять, отказать), приглашения, письмо, файлы, письма рекомендателей, снимок профиля; если открыл автор отклика — переход на `/applications/:id` |
| `/recommend?token=` | Страница рекомендателя без входа: письмо текстом и/или PDF, отказ |
| `/notifications` | Все уведомления страницами; колокольчик в шапке у вошедших |
| `/scientists` (`?q=&field=&region=&degree=&title=&open=&h_min=&sort=&page=`) | Каталог учёных (срез 10): строка поиска и регион, фильтры, чипы, порядок, страницы по 20; у вошедшего в режиме «Нанимаю» на каждой записи «Пригласить на вакансию» |
| `/offers`, `/offers/:id` | «Приглашения» учёного: список; страница приглашения с ответом «Интересно / Не сейчас» (один раз, с запиской), вакансия и кнопка «Откликнуться»; без входа ведут на `/login?next=` |
| `/sent-offers` (`?status=&page=`) | «Отправленные приглашения» организации: вкладки по состояниям со счётчиками, ответы и записки учёных, «Отозвать» |
| `/favorites` (`?page=`) | «Избранное» (срез 11): вакансии с отметками «набор закончен» / «срок прошёл» / «вы откликнулись», «Убрать» с кнопкой «Вернуть», страницы по 20; вкладки раздела |
| `/matches` (`?page=`) | «Подходящие вам»: на что опирается подбор, вакансии с причинами; «подбирать пока не по чему», если в профиле нет областей науки |
| `/saved-searches`, `/saved-searches/:id` | «Поиски»: условия тегами, частота прямо в списке, «Переименовать», «Удалить»; адрес с номером открывает поиск (из уведомления) с новыми вакансиями вперёд |
| `/deadlines` | «Сроки подачи»: избранное по месяцам и датам, маркер у сроков в пределах недели, примечание о вакансиях без срока |
| `*` | 404 «Такой страницы нет» со ссылкой на главную |
| (ошибка отрисовки) | `CrashPage` через `errorElement` роутера |

## Как фронтенд отличает «сервер лежит» от «база лежит»
`apiGet` превращает любую неудачу в `ApiError`. Если ответ в формате API, код берётся из него (`database_unavailable` → сервер жив, база нет). Если ответ не в нашем формате (прокси Vite отдаёт пустой 500, когда сервер не запущен) или сеть упала, код `unreachable`.

## Дизайн-система (срез 2)
- Мир «Журнал» (D-035, D-036): белая страница, чернильно-синий `--blue` для действий, жёлтый маркер `--mark` как единственный акцент (близкий срок, активный режим и пункт меню). Заголовки и аннотации Literata, интерфейс Golos Text (`@fontsource-variable`, лежат в проекте).
- Все значения берутся из `src/styles/tokens.css`; в компонентах своих цветов и размеров нет. Тёмной темы нет.
- Режим «Ищу работу / Нанимаю» хранится в `localStorage` (`scibox.role`), влияет на меню и на то, кому показаны закладка «В избранное» и «Сохранить поиск» (D-038, D-112).
- Срок подачи: `describeDeadline` (`src/lib/deadline.ts`), «близкий» = до 14 дней включительно.
- Подробное описание визуальной системы: `DESIGN.md` (ведёт impeccable).

## Фоновая работа в процессе сервера
`cli.serve` запускает рядом с HTTP: отправитель писем из `outbox` (раз в 2 с), очистку аккаунтов и организаций (раз в час) и `matching.Service.Run` (раз в 5 минут): рассылка по сохранённым поискам (`SendDigests`) и напоминания о сроках (`SendReminders`, не раньше 9:00 МСК). Каждый проход самостоятелен, ошибки пишутся в журнал, следующий проход повторяет недоделанное; двойной отправки нет (блокировка строки поиска `SKIP LOCKED`, запись о напоминании и уведомление в одной транзакции).

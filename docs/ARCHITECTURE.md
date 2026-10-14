# Архитектура

Состояние: после среза 16 (научные условия вакансии и выгрузка откликов в CSV: миграция `00028`, `applications/export.go`, политика периметра `export_file`; до этого срез 15: анонимный режим профиля и скрытие от организаций; до этого срез 29 (итоговый аудит: привязка ссылки подтверждения к браузеру, неделимые лимиты, `internal/textsafe`, неактивные аккаунты; до этого срез 28: сборка и выкладка: образ сервера, боевой compose с Caddy, двумя копиями, воркером, базой с непрерывным архивом и копиями, мониторингом; до этого срез 26: наблюдаемость: номер запроса в каждой строке журнала, JSON-журнал в бою, метрики Prometheus и внутренний порт `/livez` `/readyz` `/metrics` `pprof`, отправка ошибок в GlitchTip без личных данных, панель Grafana и правила тревог; до этого срез 25: несколько копий сервера и отдельный процесс фоновых задач `worker`: общее расписание в базе, предохранитель и ограничитель у Crossref и почты, мягкая остановка; до этого срез 24: база под объём «вся наука РФ»: пул со сроками, индексируемый поиск, предел выдачи, кеш; срез 23: файлы в S3-совместимом хранилище с проверкой на вирусы, срез 22: персональные данные, срез 21: журнал действий и таблица доступа на каждом адресе). Правило: если этот файл расходится с кодом, прав код, а файл чинится в том же коммите.

## Структура папок

```
SciBox/
├── .github/             автоматические проверки в GitHub (срез 18): `workflows/ci.yml` — тесты сервера с -race и порогами на PostgreSQL-сервисе, golangci-lint, govulncheck, тесты сайта; `dependabot.yml` — еженедельные обновления Go, npm (web, e2e) и Actions с выдержкой 7 дней
├── config/product.json   название продукта; читают и сервер, и сайт (D-029)
├── docker-compose.yml    PostgreSQL 16.15 (порт 5433, D-028; настройки памяти и стоимости чтения в `command`, срез 24), Mailpit v1.31.3 (8025 / 1025), хранилище файлов SeaweedFS 4.48 (S3, порт 8333, D-157), ClamAV 1.5.4 облегчённый (порт 3310, D-158)
├── docker-compose.monitoring.yml  наблюдение (срез 26, D-176): Prometheus, Grafana, node-exporter, GlitchTip (профиль `errors`)
├── monitoring/           `prometheus/` (prometheus.yml, rules/scibox.yml — 31 правило тревог, tests/scibox_test.yml — их тесты promtool), `grafana/` (provisioning, build-dashboard.py собирает dashboards/scibox.json — панель «ИОН: сервер»)
├── docker/clamav-dev/    сигнатура тестового вируса EICAR для облегчённого ClamAV (`eicar.ndb`)
├── Dockerfile            образ сервера (срез 28, D-187): Node → сайт, Go → бинарник, итог distroless static debian13 nonroot, 39 МБ; `.dockerignore`
├── deploy/               выкладка (срез 28, D-188, D-189): `docker-compose.prod.yml` (боевой стенд), `docker-compose.local.yml` (репетиция на компьютере), `caddy/` (Caddyfile, Caddyfile.local, Caddyfile.s3, site.caddy), `postgres/` (образ базы с pgBackRest и rclone, `initdb/10-scibox.sh`, `bin/scibox-*`: entrypoint, backup, archive-now, restore-check, restore), `monitoring/prometheus.yml`, `deploy.sh` (выкладка и откат), `compose` (обёртка), `init-secrets.sh`, `init-secrets-volumes.sh`, `render-alertmanager.sh`, `restore-check.sh`, `db-image-tag`, `env.example`, `local/` (`up.sh`, `prepare.sh`, `make-pki.sh`, `local.env`); `secrets/`, `.env`, `generated/`, `.state/`, `local/pki/` в git не попадают
├── README.md             запуск, демо-аккаунты, дорожки «я учёный» и «я организация», проверки
├── Makefile              dev (сервер + воркер + сайт), worker, monitoring-up, monitoring-down, monitoring-check, errors-up, glitchtip-setup, share, test (= lint + vulncheck + test-server + test-web), test-server, test-web, lint, lint-new, vulncheck, e2e, db-up, db-down, migrate, seed, db-reset, sqlc, load-data, load-check, prod-up, prod-down, prod-e2e, prod-restore-check, prod-config
├── scripts/
│   ├── dev.sh            собирает сервер, запускает сервер (без фоновых задач), воркер и Vite вместе
│   ├── glitchtip-setup   создаёт в локальном GlitchTip администратора, организацию и проект, печатает SCIBOX_ERROR_DSN (срез 26)
│   ├── coverage-check    go vet + go test -race с покрытием + проверка порогов
│   ├── load-test         нагрузочный тест (срез 27, D-177): две копии сервера и воркер на базе scibox_load, k6, ступени скорости, профили и метрики в `loadtest/.run/` (`make load-test`, `make load-smoke`)
│   ├── load-data         пересоздаёт базу scibox_load и заливает объёмные данные по масштабу (срез 24; генератор `server/loadgen/generate.sql`)
│   └── e2e-db            пересоздаёт базу scibox_e2e и заливает демо-данные (для `make e2e`)
├── loadtest/             нагрузочный сценарий k6 `scenario.js` (смесь запросов, бюджеты) и `fixtures.sql` (сеансы, пары «человек — вакансия»); результаты прогонов в `.run/` не в git (срез 27)
├── e2e/                  сквозные тесты Playwright (свой package.json): playwright.config.ts поднимает сервер на 8090
│                         и Vite на 5174 с базой scibox_e2e; tests/helpers.ts (письма из Mailpit, вход, режим, слежка за 5xx);
│                         сценарии scientist-and-organization и new-organization, каждый на компьютере и телефоне (Pixel 7)
├── PRODUCT.md            описание продукта для impeccable (ведёт impeccable)
├── server/               Go 1.26 (строка `toolchain go1.26.6` в go.mod закрепляет версию), модуль scibox/server
│   ├── cmd/api/          точка входа: команды serve | worker | release | healthcheck [serve|worker] | migrate up|down|reset|status | journals load [--force] [файл] | seed | error-test | version
│   ├── cmd/covercheck/   проверка порогов покрытия (логика в internal/covercheck)
│   ├── cmd/smtpsink/     приёмник писем с настраиваемой паузой на ответ для замеров очереди писем (срез 27, вне покрытия)
│   ├── db/migrations/    goose, встроены в бинарник (db/embed.go)
│   ├── db/queries/       SQL для sqlc
│   ├── sqlc.yaml         sqlc запускается через `go tool sqlc` (D-030)
│   ├── coverage.conf     пороги покрытия и критичные пакеты
│   ├── .golangci.yml     линтер: корректность и безопасность (gosec, gocritic diagnostic, revive об ошибках и экспорте, nolintlint), `make lint` / `make lint-new` (D-137, срез 18); 0 замечаний, входит в `make test`
│   ├── loadgen/          генератор объёмных данных для замеров (срез 24): `generate.sql`; запускает `scripts/load-data`, вне покрытия
│   ├── seed/             демо-данные (у демо-публикаций настоящие ISSN журналов из SCImago, `journalISSN` в `scientists.go`) (вымышленные организации, люди и вакансии; `profiles.go` и `generated.go` собирают 22 организации и ~190 вакансий из научных направлений; `scientists.go` — 14 профилей учёных через сервис профилей; `applications.go` — 14 откликов через сервис откликов; `matching.go` — избранное и сохранённые поиски через сервис подбора); вне покрытия, проверяется тестом в internal/cli
│   └── internal/
│       ├── access/       права по ролям организации (критичная зона): Actor.Can(действие, подразделение), без базы и HTTP
│       ├── orgs/         организации, подразделения, сотрудники, приглашения, каталог (критичная зона): сервис, письма, HTTP-обработчики
│       ├── vacancies/    вакансии (критичная зона): поля и проверки по типу позиции, жизненный цикл (таблица переходов), права через access, поиск (`search.go`; разбор и запись условий в адресе: `query.go`), «мои вакансии», HTTP
│       ├── privacy/      приватность профиля (критичная зона): режимы скрыт / анонимно / организациям / публичный, скрытие от организаций, открытое имя; кто видит профиль, контакты и с какой подробностью; без базы и HTTP
│       ├── profiles/     профиль (критичная зона): основные поля и навыки, записи разделов, приватность, поиск по DOI, сборка резюме, раздел «Соискатели» (`catalog.go`), HTTP
│       ├── applications/ отклики (критичная зона): отправка (снимок профиля, резюме, PDF-файлы, рекомендатели), «Мои отклики», карточка глазами соискателя и организации, выдача файлов, отзыв, «можно ли откликнуться»; разбор организацией (`review.go`: «просмотрен» при открытии, решение, приглашения и ответы на них, список откликов), таблицы переходов (`transitions.go`), проверка приглашений (`invitation_input.go`), тексты уведомлений (`notices.go`), HTTP
│       ├── references/   рекомендательные письма (критичная зона): просьбы, одноразовые ссылки, письмо текстом/PDF, отказ, повтор и отмена, HTTP (в том числе страница рекомендателя без входа)
│       ├── files/        файлы откликов (критичная зона): склад `Vault` (`vault.go`: приём потоком, проверка PDF по первым байтам до записи, SHA-256, проверка на вирусы, пауза после сбоя антивируса), multipart-запрос с проверкой полей до файлов (`form.go`), имена, таблица «вид файла → кто видит», безопасная выдача потоком
│       ├── blobstore/    хранилище объектов (срез 23, критичная зона): интерфейс `Store`, `S3` на minio-go (любой S3: Yandex, Selectel, VK Cloud, SeaweedFS), `Memory` для тестов
│       ├── antivirus/    проверка на вирусы (срез 23, критичная зона): клиент clamd по TCP (INSTREAM), «не знаем» это `ErrUnavailable`; `Fake` (находит EICAR) и `Skip` (без проверки, только разработка)
│       ├── filework/     фоновая работа с файлами (срез 23, критичная зона): проверка тех, кого не успели проверить при загрузке, перенос старых файлов из базы, удаление объектов по очереди `blob_deletions`, чистка осиротевших
│       ├── offers/       приглашения учёных на вакансии (критичная зона): отправка (права `ManageVacancies`, приватность профиля, лимит), ответ «Интересно / Не сейчас», отзыв, списки обеих сторон, «куда можно пригласить», тексты уведомлений (`notices.go`), HTTP
│       ├── matching/     избранное, сохранённые поиски, подбор, календарь сроков (критичная зона): правила подбора (`rules.go`, без базы), расписание (`schedule.go`), сервис (`service.go`), фоновая рассылка по поискам (`digest.go`) и напоминания о сроках (`reminders.go`), тексты писем (`notices.go`), HTTP
│       ├── journals/     справочник журналов SCImago (срез 14): разбор выгрузки (`parse.go`), загрузка заменой целиком (`load.go`, файл `data/scimagojr.csv.gz` встроен в сервер), поиск по названию и ISSN (`search.go`), HTTP
│       ├── num/          перевод int в int16/int32/uint32 с прижатием к границе типа вместо переворота (срез 18): все параметры sqlc с меньшим типом идут через него
│       ├── issn/         проверка и запись ISSN (`1234-567X`, контрольная цифра); общий для журналов и профилей
│       ├── jobs/         фоновые задачи (срез 25, критичная зона): `Runner` исполняет задачи «на весь кластер» (расписание в таблице `job_runs`, один исполнитель на срок, аренда, общая нарастающая пауза после сбоя) и «в каждой копии» (свой таймер); перехват паники, мягкая остановка (`Draining`, `Grace`), `Snapshot` для метрик
│       ├── resilience/   защита от внешних сервисов (срез 25, критичная зона): `Breaker` (закрыт / открыт / проба), `Limiter` (число одновременных, ожидание ограничено), `Gate`
│       ├── reqid/        номер запроса (срез 26): создаёт только сервер, `X-Request-Id` в ответе, обработчик журнала дописывает его в каждую запись с контекстом запроса
│       ├── metrics/      метрики Prometheus (срез 26): запросы по шаблону адреса, время, паники, записи журнала по уровню, запросы к базе, проходы задач; метки конечны (`RouteNamer`)
│       ├── opsmetrics/   метрики состояния на момент опроса (срез 26): пул, очереди писем и файлов, задачи кластера, предохранители; кеш 5 с, сбой источника даёт `scibox_source_up=0`
│       ├── opsserver/    внутренний порт (срез 26, критичная зона): `/livez`, `/readyz`, `/metrics`, `/debug/pprof`; секрет `Authorization: Bearer`, pprof выключен по умолчанию в бою
│       ├── errreport/    отправка ошибок в GlitchTip (срез 26, критичная зона): записи уровня Error, разрешённые поля, текст базы сведён к коду, очередь, предел, сворачивание повторов
│       ├── dbpool/       пул соединений с PostgreSQL (срез 24, T-22): размер, жизнь соединений, `statement_timeout`, `idle_in_transaction_session_timeout`, `plan_cache_mode`, `random_page_cost`, журнал медленных запросов без значений параметров (`SlowQueryTracer`), `IsTimeout`
│       ├── httpcache/    короткий кеш публичных ответов (срез 24): `Cache` на срок в памяти копии (`refdata`), `Serve` с `ETag` и `Cache-Control`, 304 на `If-None-Match`; срез 27 (D-179, D-181): `Keyed` — кеш по ключу запроса с одним сбором на одинаковые запросы, пределом записей, сбором без отмены посетителя и «запасом» (`WithStale`: старый ответ, пока он обновляется в фоне); используют поиск вакансий, каталог гостя и `landing`
│       ├── landing/      числа для главной (`GET /api/landing`): открытые вакансии, организации, учёные для гостя, разбивка по областям и видам позиций; HTTP
│       ├── notifications/ уведомления и очередь писем (критичная зона): колокольчик, письма через `outbox`, отправитель с повторами (`worker.go`: виды сбоя, предохранитель, возврат при остановке, `QueueStats`), переключатели писем (`settings.go`), HTTP
│       ├── testkit/      помощники для тестов с базой (временная база, сервисы, люди, организация со всеми ролями, «поломка» n-го запроса); вне покрытия
│       ├── crossref/     клиент Crossref (поиск публикации по DOI) под `resilience.Gate` (ограничитель, предохранитель), нормализация DOI
│       ├── cv/           отрисовка PDF по описанию документа: синяя плашка с именем без акцентов, строка идентификаторов (h-index в ней обычной строкой, только если указан), разделы (D-154, D-209; fpdf, шрифты в `fonts/`, лицензия OFL)
│       ├── refdata/      справочники (специальности ВАК, регионы, должности, источники): `GET /api/reference`
│       ├── apierr/       единый формат ответов и ошибок API (D-032), коды, DecodeJSON; `WriteInternal` (500 или 499, если запрос оборвал сам человек, D-119)
│       ├── auth/         аккаунты (критичная зона): пароли argon2id, регистрация, вход, сессии, сброс пароля, письма, HTTP-обработчики, проверка Origin; срез 20: `sessions.go` (список и завершение сеансов, свежий пароль `Reauth`), `devices.go` (User-Agent, знакомые браузеры), `email_change.go` (смена почты и откат), `deletion.go` (удаление со сроком отмены, фоновое стирание), `breached.go` + `data/` (список частых паролей NCSC, README с источником)
│       ├── mail/         отправка писем по SMTP (Mailpit), ошибки с видом сбоя (`Error`, `KindOf`: служба / временный / постоянный отказ) и `Memory` для тестов
│       ├── cli/          разбор команд, запуск HTTP-сервера и воркера, мягкая остановка; `services` (`newServices`) собирает все сервисы, из них `router` (HTTP) и `jobs` (список фоновых задач, `jobs.go`); `newApp` — то же для тестов (его проверяет `TestPerimeter`); `copies_test.go` — тесты нескольких копий
│       ├── audit/        журнал действий (срез 21, критичная зона): действия и область (`audit.go`: `Record` в транзакции действия, адрес из контекста запроса, скрытая почта), чтение страницами для владельца (`list.go`)
│       ├── testaudit/    помощники тестов журнала (прочитать записи, «сломать» журнал); вне покрытия
│       ├── logmask/      маска журнала сервера (срез 22, критичная зона): `slog.Handler` скрывает почты, адреса людей, токены, пароли (`logmask.go`); логгер создаётся только в `cli.newLogger`
│       ├── retention/    сроки хранения (срез 22, критичная зона): `Policy` (сроки и полы), `Service.Pass` (раз в час: уведомления, журнал действий, адреса в журнале, регистрации без подтверждённой почты), партии под блокировкой базы
│       ├── dataexport/   выгрузка своих данных (срез 22, критичная зона): `Prepare` (свежий пароль, лимит, запись в журнал, сбор `data.json`), `Write` (ZIP потоком), HTTP `GET /api/account/export`
│       ├── erasure/      что стирается и обезличивается при удалении аккаунта, проверка «единственный владелец» (срез 20, критичная зона)
│       ├── config/       переменные окружения + config/product.json; боевой режим `SCIBOX_ENV=production` (`checkProduction`, критичная зона)
│       ├── covercheck/   чтение профиля покрытия и сверка с coverage.conf
│       ├── dbgen/        код sqlc (сгенерирован, не править руками)
│       ├── health/       проверка базы для /api/health
│       ├── httpapi/      роутер chi, обработчики, единый формат ошибок; `perimeter.go` — таблица особых адресов периметра (`PerimeterRoutes`: лимиты тела, долгие передачи, политики частоты)
│       ├── textsafe/     проверка текста человека (срез 29, критичная зона): `Valid`, `ValidRequestURL`, `ValidValue` (обход тела запроса); нулевой знак и невалидный UTF-8 не доходят до базы
│       ├── httpguard/    периметр HTTP (срез 19, критичная зона): `NewServer` (сроки соединения, `MaxHeaderBytes`), `Perimeter` (настоящий IP за доверенными прокси, заголовки защиты с CSP, лимит тела, сроки долгих передач), `Throttle` (ограничение частоты: гость по IP, вошедший по аккаунту), счётчики `PostgresStore` (таблица `rate_counters`) и `MemoryStore`
│       ├── migrate/      обёртка над goose
│       └── testdb/       временные базы для тестов (D-031)
└── web/                  Vite 8 + React 19 + TypeScript 6
    ├── vite.config.ts    прокси /api → `SCIBOX_API_URL` (по умолчанию 127.0.0.1:8080); Vitest и пороги покрытия
    └── src/
        ├── app/          App (провайдеры), routes (у маршрутов `handle.title`), Layout (шапка, подвал, тосты, режим), pageTitle.ts (заголовок вкладки, D-121), CrashPage, queryClient
        ├── api/          client.ts (apiGet, ApiError), health.ts (useHealth)
        ├── config/       product.ts (имя продукта из config/product.json)
        ├── features/
        │   ├── auth/     страницы входа, регистрации, сброса пароля, подтверждения, настроек, политики; меню пользователя; useMe, useForm
        │   ├── orgs/     каталог, страницы организации и подразделения, «Организация» (мои организации и приглашения), управление (данные, подразделения, сотрудники), принятие приглашения; api.ts, labels.ts, RequireUser
        │   ├── vacancies/ страницы «Мои вакансии», вакансия (статья + управление), форма (по типу позиции), список для страниц организаций; api.ts (+ справочники useReference), labels.ts, formValues.ts
        │   ├── applications/ форма отклика, кнопка на вакансии (`ApplyBlock`), «Мои отклики», отклик глазами соискателя (рекомендации: повторить, отменить, добавить), список откликов организации (`CandidatesPage`), карточка отклика для организации (панель решения), приглашения (`Invitations`, окна `InviteModal`, `DecisionModal`, `AnswerModals`), страница рекомендателя; api.ts, labels.ts, review.css
        │   ├── catalog/  раздел «Соискатели» (D-135): страница `/scientists` (поиск, фильтры, чипы, порядок, страницы), запись `ScientistEntry`, фильтры, адрес (`params.ts`)
        │   ├── matching/ избранное, подбор, сохранённые поиски, сроки: четыре страницы-вкладки (`FavoritesPage`, `MatchesPage`, `SavedSearchesPage`, `DeadlinesPage`), закладка `FavoriteButton`, «Сохранить поиск» (`SaveSearch.tsx`), открытие поиска из уведомления (`OpenSavedSearchPage`), письма в настройках аккаунта (`MailSettings`); api.ts, labels.ts, matching.css
        │   ├── offers/   приглашения на вакансии: окно «Пригласить» и кнопка, «Приглашения» учёного (`/offers`, `/offers/:id`), «Отправленные приглашения» организации (`/sent-offers`), вкладки «Отклики / Приглашения»; api.ts, labels.ts
        │   ├── landing/  главная: `LandingPage` (заголовок и поиск, числа, оглавление, свежие вакансии, две дорожки, «Устроено под науку»); api.ts, landing.css
        │   ├── notifications/ колокольчик в шапке (окно с последними, число непрочитанных раз в минуту), страница «Уведомления»; api.ts
        │   ├── profile/  «Мой профиль» (правка разделов в окнах, приватность), форма основного, страница учёного для других, разметка профиля `ProfileView`, поля записей по описанию (`sections.ts`)
        │   ├── shell/    шапка, мобильное меню, подвал, переключатель «Ищу работу / Нанимаю» (RoleProvider), nav.ts
        │   ├── status/   стартовая страница (проверка сервера), 404
        │   └── styleguide/  служебная страница /styleguide (вне покрытия)
        ├── i18n/         ru.ts (все тексты), index.ts (t)
        ├── lib/          plural, deadline (срок подачи словами), normalize (поиск без регистра и ё)
        ├── styles/       tokens.css (цвета, шкалы), base.css (шрифты, сброс), layout.css
        ├── ui/           Button, Field/TextField/TextArea/Select (с группами пунктов), FilterGroup/ScienceFilter (группы фильтров и области науки; поиск и каталог), Combobox, Tag/FilterChip, FilePicker (выбор PDF), Deadline,
        │                 VacancyEntry (слот `action` под сроком: закладка), Modal, ToastProvider/useToast, EmptyState, Skeleton, icons, useLeaveGuard («Уйти без сохранения?», D-120); CSS рядом с компонентом
        └── test/         setup.ts, render.tsx (renderApp, jsonResponse), api.ts (stubApi: подставной сервер), forms.ts
```

Страницы организации и подразделения показывают список опубликованных вакансий (`VacancyList`).

Файлы откликов и писем рекомендателей лежат в S3-совместимом хранилище, в базе только строка с ключом `files/<uuid>`, контрольной суммой и итогом проверки на вирусы (D-157, D-158); папки `storage/uploads/` нет. Журнал сервера пишется в stderr текстом и проходит через `logmask` (D-152).

## Изменения среза 16

- Миграция `00028_vacancy_conditions.sql`: `vacancies.equipment_access`, `equipment_note` (≤ 300), `equipment_unit_id` (→ `units`, `ON DELETE SET NULL`), `publications_paid`, `conference_travel`; `vacancy_view` пересоздана (новые столбцы и `equipment_unit_name` в конце). Таблиц не добавляет.
- Вакансии: `Input`/`Detail` с условиями, `Card.Perks` (`equipment`, `publication`, `conference`), `Target.Facilities` (все ЦКП организации); поиск: `SearchParams.Equipment/Publications/Conferences` (параметры `equipment`, `publications`, `conferences`; в адресе, сохранённом поиске, письмах по нему и ключе кеша).
- Отклики: `Service.ExportCandidates` и `WriteCSV` (`export.go`), `GET /api/my/candidates/export?vacancy=&status=`; `Config.PublicURL` (ссылки в файле); аудит `vacancy.applications_exported`; политика периметра `export_file` (10 в минуту).
- Сайт: блок «Научные условия» в форме и на странице вакансии, строка условий в записи вакансии (`VacancyEntry.perks`), группа фильтров, кнопка «Выгрузить в Excel» на `/candidates`, общий `lib/saveFile.ts`.

## Изменения среза 29

- Миграции: `00025_confirm_binding` (`auth_tokens.binding_hash`: хеш cookie «браузер регистрации»), `00026_inactive_accounts` (`users.last_active_at`, `users.inactive_warned_at`).
- Cookie `scibox_reg` (HttpOnly, SameSite=Lax, на срок ссылки подтверждения): ставят `POST /api/auth/register` и `POST /api/auth/resend-confirmation`, читает и стирает `POST /api/auth/confirm-email`. Ответ `confirm-email` из чужого браузера: `200 {"reset_token": …}` (почта подтверждена, входа нет, сайт открывает `/reset-password?token=…&from=confirm`).
- Лимиты в сервисах: `LockRateKey` (замок на ключ), `RefundRateEvent`; ключи по адресу через `httpguard.NetworkKey`. Новая политика периметра `apply_upload` (`POST /api/applications`), `GET /api/health` под `public_read`; `GET` с `Sec-Fetch-Site: cross-site` отвергается в `auth.SameOrigin`.
- Фоновая уборка аккаунтов (`accounts.cleanup`) вызывает `auth.ProcessInactive` (предупреждение за 30 дней, стирание через 3 года без входа).
- `profiles.Get` считает просмотры чужих профилей сотрудником организации (`profile_view`, 150 в сутки).

## Порты
- web (Vite): 5173 · api: 127.0.0.1:8080 · Postgres в Docker: **5433** · Mailpit UI: 8025, SMTP: 1025 · хранилище файлов (S3): 8333 · ClamAV: 3310
- внутренние порты (срез 26): сервер 127.0.0.1:**9100**, воркер 127.0.0.1:**9101** (`/livez`, `/readyz`, `/metrics`, `/debug/pprof/`); у `make share` 9120 и 9121, у сквозных тестов порт выключен
- мониторинг (`docker-compose.monitoring.yml`): Prometheus 127.0.0.1:**9190**, Grafana **3100**, GlitchTip **8100**
- сквозные тесты (`make e2e`): web 5174 · api 127.0.0.1:8090 · база `scibox_e2e` в том же контейнере · бакет `scibox-e2e`
- показ по ссылке (`make share`, `scripts/share`): сервер вместе с сайтом на 127.0.0.1:8088, туннель tuna или localhost.run (D-131)

## Процессы и фоновые задачи (срез 25)

Процессов может быть несколько каждого вида, все смотрят в одну базу PostgreSQL и в одно хранилище файлов.

| Процесс | Что делает |
|---|---|
| `scibox serve` | отвечает на запросы; с `SCIBOX_BACKGROUND=on` (по умолчанию) ещё и исполняет фоновые задачи |
| `scibox worker` | только фоновые задачи; без HTTP |

Задачи (`internal/cli/jobs.go`, исполняет `internal/jobs`):

| Задача | Вид | Срок | Что делает |
|---|---|---|---|
| `outbox.send` | в каждой копии | 2 с | отправляет письма из очереди (`notifications.Worker.Drain`) |
| `files.check` | в каждой копии | 30 с | проверка файлов на вирусы, перенос старых, удаление объектов из очереди |
| `outbox.report` | на весь кластер | 5 мин | состояние очереди писем в журнал |
| `matching.digests` | на весь кластер | 5 мин | письма о новых вакансиях по сохранённым поискам |
| `matching.reminders` | на весь кластер | 5 мин | напоминания о сроках подачи |
| `outbox.cleanup`, `accounts.cleanup`, `orgs.cleanup`, `rate-counters.cleanup`, `retention` | на весь кластер | 1 ч | уборка по срокам, удаление аккаунтов по сроку |
| `files.sweep` | на весь кластер | 6 ч | чистка осиротевших объектов хранилища |

Соединения с базой: каждая копия сервера до 20, воркер до 10 (`SCIBOX_DB_MAX_CONNS` ниже этих пределов действует как есть); в PostgreSQL по умолчанию 100.

## Наблюдаемость (срез 26)

Что можно узнать о работающей системе и откуда (D-172…D-176).

| Что | Где | Как смотреть |
|---|---|---|
| Журнал | stderr каждого процесса; в бою JSON по строке, локально текст; в каждой строке `service`, `version`, `env`, а у записей запроса `request_id` | `grep request_id=…`; номер человек видит в заголовке `X-Request-Id` |
| Ошибки | записи уровня Error уходят в GlitchTip (`SCIBOX_ERROR_DSN`), сгруппированные, с местом в коде | http://localhost:8100 (`make errors-up`, `make glitchtip-setup`); проверка адреса: `scibox error-test` |
| Метрики | внутренний порт `/metrics`: запросы, время, ошибки по шаблонам адресов, пул базы, очереди писем и файлов, задачи, предохранители, паники, записи журнала | Prometheus 9190, панель Grafana 3100 «ИОН: сервер» (`make monitoring-up`) |
| Тревоги | `monitoring/prometheus/rules/scibox.yml`, 31 правило, тесты `monitoring/prometheus/tests/scibox_test.yml` | Prometheus `/alerts`, панель «Горящие тревоги»; `make monitoring-check` |
| Живость и готовность | внутренний порт `/livez`, `/readyz` (база, миграции, у воркера часы); при остановке «не готов» | оркестратор и балансировщик (срез 28) |
| Профили | внутренний порт `/debug/pprof/` (в разработке включён, в бою выключен; с секретом) | `go tool pprof http://127.0.0.1:9100/debug/pprof/profile?seconds=30` |

Слои журнала (`cli.newLogger`): маска (`logmask`) → номер запроса (`reqid`) → счётчик по уровню (`metrics`) → отправка ошибок (`errreport`) → вывод. Номер запроса берётся из контекста: пишите `logger.ErrorContext(r.Context(), …)`, а не `logger.Error(…)`, когда у вас есть запрос.

Правило метрик: у метки конечное число значений (шаблон адреса, имя задачи, имя запроса), никогда не путь, номер объекта, почта.

## Боевой стенд (срез 28)

Подробно: `docs/DEPLOY.md`. Службы `deploy/docker-compose.prod.yml` (проект `scibox-prod`):

| Служба | Образ | Наружу | Что делает |
|---|---|---|---|
| `caddy` | `caddy:2.11.7` | 80, 443 | HTTPS (Let's Encrypt, при отказе ZeroSSL), распределение между копиями по `/readyz` внутреннего порта, журнал без cookie и с обрезанными адресами |
| `app1`, `app2` | образ сервера | нет | `serve` с `SCIBOX_BACKGROUND=off`, `read_only`, uid 65532 |
| `worker` | образ сервера | нет | `worker` |
| `release` | образ сервера | нет | одноразовый шаг: миграции и справочник журналов |
| `postgres` | `scibox-postgres:16.15-<отпечаток>` | нет | PostgreSQL 16 с `archive_command` pgBackRest, роль приложения без суперпользователя, вход из сети только по TLS |
| `backup` | тот же образ базы | нет | планировщик копий базы и файлов, метрики копий |
| `clamav` | `clamav/clamav-debian:1.5.4` | нет | антивирус с обновлением баз |
| `secrets-init` | образ базы | нет | раскладывает секреты по томам с владельцем нужного процесса |
| `restore-check`, `restore` | образ базы | нет | профиль `tools`: проверка восстановления и восстановление в пустой том |
| `prometheus`, `alertmanager`, `grafana`, `node-exporter` | официальные | 127.0.0.1 | профиль `monitoring` |

Сети: `edge` (Caddy с фиксированным адресом и копии сервера) и `backend`. Внешние: хранилище файлов (S3), хранилище копий (S3), почтовый сервис. Тома: `pgdata`, `pgsocket`, `clamav_db`, `caddy_data`, `backup_state`, `backup_textfile`, `secrets_*`, данные мониторинга.

## Переменные окружения сервера
| Переменная | По умолчанию |
|---|---|
| `SCIBOX_SMTP_ADDR` | `localhost:1025` (Mailpit) |
| `SCIBOX_SMTP_TLS` | `off`; для почтового сервиса `tls` (порт 465) или `starttls` (порт 587) |
| `SCIBOX_SMTP_USER`, `SCIBOX_SMTP_PASSWORD` | пусто (без входа); вход разрешён только с шифрованием |
| `SCIBOX_MAIL_FROM` | `ИОН <no-reply@scibox.local>` |
| `SCIBOX_PUBLIC_URL` | `http://localhost:5173` (из него строятся ссылки в письмах; https включает Secure у cookie) |
| `SCIBOX_HTTP_ADDR` | `127.0.0.1:8080` |
| `DATABASE_URL` | `postgres://scibox:scibox@localhost:5433/scibox?sslmode=disable` |
| `SCIBOX_CROSSREF_URL` | `https://api.crossref.org` (поиск публикаций по DOI) |
| `SCIBOX_CROSSREF_MAILTO` | пусто (почта для «вежливого пула» Crossref, необязательно) |
| `SCIBOX_PRODUCT_CONFIG` | `../config/product.json` (путь от папки `server/`) |
| `SCIBOX_WEB_DIR` | пусто; папка собранного сайта (`web/dist`): тогда сервер сам отдаёт страницы по всем адресам вне `/api` (`httpapi.WebHandler`) |
| `SCIBOX_ENV` | `development`; `production` — сервер не стартует с http-адресом сайта, паролем базы из docker-compose, базой в сети без `sslmode=require/verify-*`, SMTP без шифрования, отправителем `.local` (срез 19, T-08) |
| `SCIBOX_TRUSTED_PROXIES` | пусто (никому); сети и адреса через запятую (`127.0.0.1, 10.0.0.0/8`), от которых сервер принимает `X-Forwarded-For`; `/0` запрещено (срез 19, T-06). `make share` ставит `127.0.0.1,::1` |
| `SCIBOX_S3_ENDPOINT` | `localhost:8333` (SeaweedFS из docker-compose); хост без схемы, например `storage.yandexcloud.net` (срез 23) |
| `SCIBOX_S3_BUCKET` | `scibox-files` |
| `SCIBOX_S3_ACCESS_KEY`, `SCIBOX_S3_SECRET_KEY` | ключи из docker-compose (только разработка; в бою запрещены) |
| `SCIBOX_S3_REGION` | `us-east-1` (у Yandex `ru-central1`, у Selectel `ru-1`) |
| `SCIBOX_S3_TLS` | `off`; в бою `on` обязательно |
| `SCIBOX_S3_PATH_STYLE` | `on` в разработке, `off` в бою (адрес вида endpoint/bucket/key вместо bucket.endpoint/key) |
| `SCIBOX_S3_CREATE_BUCKET` | `on` в разработке, `off` в бою (бакет создаёт администратор) |
| `SCIBOX_CLAMAV_ADDR` | `localhost:3310`; `off` отключает проверку (только разработка, в бою запрещено) |
| `SCIBOX_DB_MAX_CONNS` | `20` (пул одной копии сервера; две копии и процесс фоновых задач = 60 из 100 соединений базы) |
| `SCIBOX_DB_STATEMENT_TIMEOUT` | `10s` (запрос дольше обрывает сама база; 503 `database_busy`) |
| `SCIBOX_DB_IDLE_TX_TIMEOUT` | `30s` (транзакция без команд дольше обрывается) |
| `SCIBOX_DB_SLOW_QUERY` | `200ms` (запросы дольше пишутся в журнал: имя и время) |
| `SCIBOX_BACKGROUND` | `on`: `serve` сам исполняет фоновые задачи; `off` — только HTTP, задачи исполняет `worker` (срез 25, D-170). `make dev`, `make share` и сквозные тесты ставят `off` |
| `SCIBOX_SHUTDOWN_TIMEOUT` | `20s`: сколько после сигнала остановки сервер ждёт текущие запросы, а задачи дописывают порцию (срез 25) |
| `SCIBOX_SEARCH_CACHE_TTL` | `15s`: сколько копия помнит ответ поиска вакансий и каталога учёных (для гостя) на один и тот же запрос; `off` выключает, не больше 5 минут (срез 27, D-179) |
| `SCIBOX_CROSSREF_MAX_CONCURRENT` | `1`: одновременных запросов к Crossref от одной копии, 1–10 (срез 25, D-169) |
| `SCIBOX_LOG_FORMAT` | `json` в production, `text` иначе (срез 26) |
| `SCIBOX_LOG_LEVEL` | `info` (debug, info, warn, error) |
| `SCIBOX_OPS_ADDR` | `127.0.0.1:9100` у сервера, `127.0.0.1:9101` у воркера; `off` выключает внутренний порт. В бою не слушает чужие сети без секрета (D-173) |
| `SCIBOX_OPS_TOKEN` | пусто; от 24 знаков: `/metrics` и pprof требуют `Authorization: Bearer …` |
| `SCIBOX_PPROF` | `on` в разработке, `off` в бою; без секрета в бою не включается |
| `SCIBOX_ERROR_DSN` | пусто (отчёты никуда не уходят); `https://ключ@хост/номер-проекта` GlitchTip; по http только в закрытой сети |
| `<ИМЯ>_FILE` | путь к файлу с секретом вместо значения: `DATABASE_URL`, `SCIBOX_S3_ACCESS_KEY`, `SCIBOX_S3_SECRET_KEY`, `SCIBOX_SMTP_PASSWORD`, `SCIBOX_OPS_TOKEN`, `SCIBOX_ERROR_DSN` (срез 28, D-187); в боевом режиме права не шире 600, `X` и `X_FILE` вместе нельзя |
| `SCIBOX_ALLOW_DESTRUCTIVE` | `off`; `on` разрешает в боевом режиме `migrate down`, `migrate reset` и `seed` (срез 28, D-187) |
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
- Миграция `00004_reference.sql`: `reference_sources` (источники), `science_fields` / `science_groups` / `specialties` (номенклатура ВАК, 5 / 35 / 350), `regions` (89), `positions` (27, с типом). Данные внутри миграции (docs/REFERENCE.md). Миграция `00013_vacancy_kinds.sql` (D-134): семь видов вакансий, `positions` 33 строки.
- Миграция `00005_vacancies.sql`:
  - `vacancies` (id, org_id, unit_id → `ON DELETE RESTRICT`, created_by, status `draft|published|closed|archived`, title, position_code, summary, description, requirements, focus, career_level 1–4, work_format, region_code, city, housing, rate_percent, salary_from/to, contract_type, contract_months, funding_source, funding_note, degree_required, title_required, is_competition, deadline date, published_at, closed_at, archived_at, created_at, updated_at; CHECK на значения);
  - `vacancy_specialties` (vacancy_id, specialty_code);
  - представление `vacancy_view` (в срезе 16 расширено условиями, `00028`) (вакансия + названия должности, региона, организации, подразделения; при добавлении колонок в `vacancies` пересоздать).
  - Счётчик частоты `vacancy_create` (100 в сутки на человека) в общей `rate_events`.
- Миграция `00006_search.sql`: `vacancy_search` (vacancy_id, doc tsvector; индекс GIN), функция `refresh_vacancy_search(uuid)` и триггеры: на `vacancies` (при вставке и смене текстовых полей), `vacancy_specialties`, `organizations` (название, город), `units` (название). Текст в `vacancy_view` не входит.
- Миграция `00007_profiles.sql`:
  - `profiles` (id, user_id уникальный → `users` `ON DELETE CASCADE`, visibility `hidden|orgs|public`, open_to_offers, headline, city, region_code, about, degree `none|candidate|doctor`, degree_specialty_code, degree_year, degree_institution, dissertation_title, academic_title `none|docent|professor`, academic_title_year, orcid, spin, scopus_id, wos_id, h_rsci, h_scopus, h_wos, h_scholar, contact_email, created_at, updated_at; CHECK на длины и перечисления, форматы проверяет сервис);
  - `profile_specialties` (profile_id, specialty_code);
  - `profile_hidden_orgs` (profile_id, org_id, created_at; PK по паре, `ON DELETE CASCADE` с обеих сторон): организации, чьи сотрудники не видят профиль ни в каком режиме (срез 15, D-200); `profiles.visibility` теперь `hidden` / `anon` / `orgs` / `public` (D-199);
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
- Миграция `00012_journals.sql`: `journals` (id = Sourceid SCImago, title, publisher, quartile 1–4 или NULL, sjr, data_year; индекс pg_trgm по названию), `journal_issns` (issn `1234-567X` → journal_id, position — порядок в файле); индекс по `profile_items.data->>'issn'` у публикаций. Строки кладёт `scibox journals load` (D-127), источник и выпуск — строка `journals` в `reference_sources`.
- Миграция `00014_profile_skills.sql` (D-136): `profiles.research_skills`, `profiles.general_skills` (`text[]`, до 30 строк; длину и повторы проверяет сервис).
- Миграция `00015_rate_counters.sql` (срез 19, D-141): `rate_counters` (kind, key, window_start, hits; ключ из трёх первых), таблица UNLOGGED. Счётчики ограничения частоты публичных и тяжёлых адресов по окнам; удаляются через 2 часа после начала окна (фоновая очистка раз в час).
- Миграция `00016_account_security.sql` (срез 20, D-142…D-146): `sessions.reauth_at` (когда в сеансе последний раз вводили пароль); `auth_tokens.new_email` и назначения `change_email`, `revert_email`; `known_devices` (user_id, device_hash, first_seen_at, last_seen_at; ключ из двух первых) — знакомые браузеры, удаляются через 400 дней без входа; `users.deletion_due_at` (когда сотрутся данные), `users.deleted_at` (стёрт, строка-«надгробие»); `profiles.visibility_before_deletion`; `applications.anonymized_at` (письмо и почта могут быть пустыми только у обезличенного отклика).
- Миграция `00017_audit_log.sql` (срез 21, D-147, D-148): `audit_log` (id bigint, at, org_id → `organizations` (пусто у личных действий аккаунта), actor_id и subject_id → `users`, action `вид.действие`, target_kind, target_id, target_label ≤300, details jsonb ≤2000 байт, ip ≤45); индексы `(org_id, id DESC)`, `(actor_id, id DESC)`, `(at)`; триггеры запрещают `UPDATE`, `DELETE` и `TRUNCATE`, кроме обнуления `ip` при прочих равных полях.
- Миграция `00018_retention.sql` (срез 22, D-151, D-152): триггер `audit_log_guard` разрешает `DELETE` внутри транзакции с отметкой `scibox.audit_purge` для записей старше 90 дней (остальное по-прежнему запрещено); индексы под чистку: `audit_log_purge_ip_idx`, `notifications_purge_read_idx`, `notifications_purge_unread_idx`, `users_purge_unconfirmed_idx`. Таблиц не добавляет.
- Миграция `00019_file_storage.sql` (срез 23, D-157): `application_files.data` переименован в `legacy_data` (необязателен, обнуляется после переноса, новые строки содержимого не несут: ограничение `application_files_no_content`), новые столбцы `storage_key` (ключ объекта, уникален), `sha256`, `scan_status` (`pending|clean|infected`, по умолчанию `pending`), `scanned_at`, `scan_signature`, `scan_attempts`, `scan_next_at` (пауза до следующей проверки и «аренда» файла на время проверки); ограничения: чистый файл имеет дату проверки, заражённый не хранит ключ, содержимое есть либо в хранилище, либо в старом столбце; индексы под фоновую проверку и перенос; таблица `blob_deletions` (ключи объектов на удаление из хранилища, их кладёт стирание аккаунта). Столбец `legacy_data` удаляется в срезе 28.
- Миграция `00020_scale.sql` (срез 24, D-162): `profile_search` (profile_id, doc tsvector, fuzzy_text, updated_at, codes text[]; GIN по `doc`, по триграммам `fuzzy_text` и по `codes`) со сборкой триггерами (`refresh_profile_search`: профиль, специальности, имя в аккаунте); `profiles.q12_total` (число статей в Q1–Q2, триггер на `profile_items`), `profiles.h_max` (лучший из четырёх h-index, вычисляемый столбец, `NOT NULL`); индексы `profiles_catalog_idx`, `profiles_h_idx`, `profiles_region_idx`, `users_display_name_idx`.
- Миграция `00021_vacancy_search_open.sql` (срез 24, D-162, D-163): `vacancy_search` + `open` (опубликована), `published_at`, `fuzzy_text` (название, должность, организация), `codes` (коды специальностей вместе с кодами групп и областей); GIN по тексту, триграммам и кодам только для открытых; триггер `vacancy_search_status`; `vacancies_open_order_idx (published_at DESC, id)` вместо `vacancies_published_idx`; `ALTER DATABASE … SET pg_trgm.word_similarity_threshold = 0.5`.
- Миграция `00024_load_indexes.sql` (срез 27, D-181): частичный индекс `profiles_visible_idx (visibility) WHERE headline <> ''` и `vacancies_open_order_idx` пересоздан с `INCLUDE (deadline, org_id)`: числа главной считаются по индексу.
- Миграция `00022_job_runs.sql` (срез 25, D-167): `job_runs` (name — ключ, next_run_at, leased_until, holder, started_at, finished_at, last_ok_at, failures, last_error, runs): общее расписание фоновых задач «на весь кластер»; строка на задачу, ПДн нет.
- Служебная таблица goose: `goose_db_version`. Очистка устаревшего (сессии, ссылки, счётчики; приглашения старше 30 дней после срока) раз в час фоновой задачей (`accounts.cleanup`, `orgs.cleanup`, `rate-counters.cleanup`, `retention`, `outbox.cleanup`).

## API
Формат ошибки для всех адресов (D-032): `{"error": {"code": "...", "message": "..."}}`.
Коды: `invalid_invitation_state` / `too_many_invitations` (409), `unit_has_vacancies` (409, удаление подразделения с вакансиями), `invalid_status_change` / `vacancy_not_draft` / `vacancy_changed` (409), `not_found` (404), `method_not_allowed` (405), `internal` (500), `database_unavailable` (503), `bad_request` (400), `validation_failed` (422, с `fields` по полям), `unauthorized` (401), `forbidden` (403, чужой источник запроса), `rate_limited` (429, `retry_after` в секундах и заголовок Retry-After), `invalid_credentials` (401), `email_not_confirmed` (403), `invalid_token` (400), `reauth_required` (403, нужен свежий пароль: сайт показывает окно пароля и повторяет запрос, срез 20), `email_taken` (409), `deletion_blocked` (409), `unsupported_media_type` (415), `file_pending` (409, файл ещё проверяется на вирусы), `storage_unavailable` (503, хранилище файлов не отвечает), `database_busy` (503, база не уложилась в срок запроса, `Retry-After: 5`, срез 24), `payload_too_large` (413, тело больше лимита адреса: по умолчанию 1 МиБ, загрузки с файлами 64 МиБ, срез 19). Все ответы `/api/*` с `Cache-Control: no-store`, кроме `GET /api/landing` и `GET /api/reference` (общие для всех, срез 24).

Периметр (срез 19, `httpguard`): у всех ответов заголовки защиты (CSP: у API `default-src 'none'`, у сайта только свои скрипты, стили, шрифты и картинки; `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, COOP и CORP `same-origin`; HSTS только при https). Ограничение частоты (`rate_limited`, 429): поиск, каталоги, главная, страницы вакансий, людей, организаций и подразделений, справочник журналов, поиск по DOI — гость 300, вошедший 120 запросов в минуту (`public_read`); резюме PDF — 20 в минуту (`pdf_build`); ссылки из писем (рекомендатель, приглашение в организацию, подтверждение и откат смены почты) — 30 в минуту (`link_page`). Таблица адресов: `httpapi.PerimeterRoutes`. Запросы, меняющие данные, принимают только JSON и только со своего сайта (D-040).

| Метод и адрес | Ответ |
|---|---|
| `GET /api/auth/me` | 200 `{"user": null | {id, email, name, email_confirmed, created_at, deletion_due_at}}` (`deletion_due_at` — когда аккаунт будет удалён, иначе null) |
| `POST /api/auth/register` | 202 `{email}`; 422 с полями; пишет письмо в фоне |
| `POST /api/auth/confirm-email` `{token}` | 200 `{user}` + cookie (вход) |
| `POST /api/auth/resend-confirmation` `{email}` | всегда 202 |
| `POST /api/auth/login` `{email,password}` | 200 `{user}` + cookie сеанса и cookie устройства `scibox_device`; прежний сеанс этого браузера завершается; из незнакомого браузера письмо «вход с нового устройства»; 401/403/429 |
| `POST /api/auth/logout` | 204, cookie стирается |
| `POST /api/auth/forgot-password` `{email}` | всегда 202 |
| `POST /api/auth/reset-password` `{token,password}` | 204, все сессии завершаются |
| `PATCH /api/account` `{name}` | 200 `{user}` (нужен вход) |
| `POST /api/account/password` `{current_password,new_password}` | 204; другие сессии завершаются |
| `POST /api/account/sessions/revoke-others` | 204 |
| `POST /api/account/reauth` `{password}` | 204: 10 минут сеансу разрешены опасные действия; 422 `fields.password`; 429 |
| `GET /api/account/sessions` | 200 `{sessions:[{id, browser, os, mobile, created_at, last_seen_at, current}]}` (коды браузера и системы, до 50, последние сверху) |
| `DELETE /api/account/sessions/{id}` | 204 (свой сеанс; текущий — как выход, cookie стирается); 404 чужой или нет такого |
| `POST /api/account/email` `{email}` | 202 (свежий пароль); ссылка на новый адрес или «почта уже есть»; 422 `fields.email` |
| `POST /api/auth/confirm-email-change` `{token}` | 200 `{user}` (без входа); 400 `invalid_token`; 409 `email_taken` |
| `POST /api/auth/revert-email-change` `{token}` | 204: прежняя почта, все сеансы завершены, удаление отменено, ссылка для пароля на почту; 400; 409 `email_taken` |
| `GET /api/account/deletion` | 200 `{due_at, grace_days, blocking:[{slug,name}], orphaned:[{slug,name}]}` |
| `POST /api/account/deletion` | 200 `{due_at}` (свежий пароль): профиль скрыт, все сеансы завершены, cookie стирается; 409 `deletion_blocked` |
| `POST /api/account/deletion/cancel` | 204 (нет назначенного удаления — тоже 204) |
| `GET /api/organizations?q=&kind=&limit=&offset=` | публичный каталог: `{items:[{id,slug,name,kind,city,summary,unit_count}], total}`; поиск по названию и городу без учёта регистра |
| `GET /api/organizations/{slug}` | публично: `{organization, units:[…], viewer}`; `viewer` (роль и права) только у вошедшего; номера аккаунтов руководителей только сотрудникам |
| `GET /api/organizations/{slug}/units/{id}` | публично: страница подразделения |
| `POST /api/organizations` | 201, создатель становится владельцем; 422 с полями; 429 при пятой в сутки |
| `PATCH /api/organizations/{slug}` | владелец |
| `POST /api/organizations/{slug}/units`, `PATCH …/units/{id}`, `DELETE …/units/{id}`, `PUT …/units/{id}/head {user_id|null}` | создание, удаление и назначение руководителя: владелец; правка: владелец или руководитель этого подразделения |
| `GET /api/organizations/{slug}/audit?before=&limit=` | владелец (иначе 403; нет входа 401): журнал действий организации `{items:[{id, at, action, actor:{id,name}|null, subject:{id,name}|null, target:{kind,id,label}, details}], next}`, от новых к старым, до 100 записей на страницу (по умолчанию 30), `before` — номер записи, с которой продолжать; адреса в ответе нет |
| `GET /api/organizations/{slug}/members` | владелец: сотрудники с почтами и ожидающие приглашения |
| `PATCH …/members/{userId} {role}`, `DELETE …/members/{userId}` | владелец; уйти (DELETE себя) может любой сотрудник; последний владелец 409 `last_owner` |
| `POST …/invitations {email,role,unit_id}`, `DELETE …/invitations/{id}` | владелец; письмо в фоне |
| `POST /api/invitations/lookup {token}` | без входа: что за приглашение (и совпадает ли почта с вошедшим) |
| `POST /api/invitations/accept {token}`, `POST /api/invitations/{id}/accept` | вошедший; по ссылке из письма с чужой почтой 403 `invitation_wrong_email`; по номеру чужое приглашение неотличимо от выдуманного (400 `invalid_invitation`, срез 21); устарело 400 `invalid_invitation`, уже в организации 409 `already_member` |
| `GET /api/my/organizations` | `{organizations:[…с ролью], invitations:[…на почту человека]}` |
| `GET /api/reference` | публично, кеш 10 мин на сервере и час у браузера (`ETag`): `{science:[область → группы → специальности], regions, positions:[{code,type,name}], sources}` |
| `GET /api/vacancies?q=&field=&region=&format=&type=&level=&degree=&org_kind=&funding=&rate=&term=&salary_min=&housing=&competition=&deadline=&sort=&org=&unit=&limit=&offset=` | публичный поиск: опубликованные вакансии без прошедшего срока: `{items:[карточка], total, capped, fuzzy}` (первые 1000 результатов: при `capped: true` найдено больше, `total` равен 1000, дальше листать нельзя, срез 24); все параметры необязательны, повторяющиеся (`field`, `format`, `type`, `level`, `degree`, `org_kind`, `funding`, `rate`, `term`) значат «или»; неизвестное значение 422 с `fields`; `limit` до 100, по умолчанию 50 |
| `POST /api/vacancies` `{organization: slug, …поля}` | 201 `{vacancy}`: черновик; ведущий вакансии этого подразделения; 422 с полями; 429 после 100 в сутки |
| `GET /api/vacancies/{id}` | опубликованную и закрытую видят все; черновик и архив только ведущие вакансии (остальным 404); в ответе `viewer:{can_manage, transitions}` |
| `PATCH /api/vacancies/{id}` | правка (черновик проверяется мягко, остальное строго); перенос в другое подразделение нужно право на оба |
| `POST /api/vacancies/{id}/status` `{status}` | смена статуса по таблице переходов; публикация проверяет готовность; 409 `invalid_status_change`, `vacancy_changed` |
| `DELETE /api/vacancies/{id}` | только черновик; иначе 409 `vacancy_not_draft` |
| `GET /api/my/vacancies?status=&limit=&offset=` | «Мои вакансии»: `{items,total,counts по статусам}` в пределах прав |
| `GET /api/my/vacancy-targets` | где человек может создать вакансию: `[{organization, whole_org, units}]` |
| `GET /api/profile` | вошедший: свой профиль (создаётся пустым и скрытым): `{profile, viewer:{is_owner, can_see_contacts}}`; в `profile` есть `visibility`, `contact_email`, разделы `sections` (всегда списки; у публикации `issn` и подставленный `journal {title,issn,quartile,year}`), `quartiles {q12_total, q12_recent, recent_from, year}`, `skills {research, general}` (то же у `/api/scientists/{id}` и в снимке отклика; в старых снимках `skills` нет) |
| `PUT /api/profile` | основные поля целиком (`headline, city, region_code, about, degree, degree_specialty_code, degree_year, degree_institution, dissertation_title, academic_title, academic_title_year, orcid, spin, scopus_id, wos_id, h_rsci, h_scopus, h_wos, h_scholar, contact_email, specialties, research_skills, general_skills`); 422 с полями; приватность и записи не трогает |
| `PUT /api/profile/privacy` `{visibility, open_to_offers}` | 200 `{profile, viewer, hidden_orgs, anonymous_preview}`; 422 при неизвестном режиме (`hidden`, `anon`, `orgs`, `public`) |
| `PUT /api/profile/hidden-orgs` `{org_ids}` | 200 страница профиля; заменяет список организаций, от которых скрыт профиль (до 30); 422 `org_ids` при неизвестной организации или превышении |
| `POST /api/profile/items` `{kind, …поля вида}` | 201 `{item}`; виды `education, experience, publication, grant, patent, teaching`; 422 с полями; 409 `too_many_items` |
| `PUT /api/profile/items/{id}` `{kind?, …поля}` | 200 `{item}`; чужая или несуществующая запись 404; сменить вид нельзя (422) |
| `DELETE /api/profile/items/{id}` | 204; чужая запись 404 |
| `GET /api/profile/doi?doi=` | 200 `{work:{doi,title,authors,venue,year,type,volume,issue,pages,issns,issn,journal}}` из Crossref; `issn` — найденный в справочнике журналов или первый годный, `journal` `{title,issn,quartile,year}` или null; 422 неверный или уже добавленный DOI; 404 `doi_not_found`; 502 `doi_unavailable`; 429 после 60 в час |
| `GET /api/journals?q=&limit=` | вошедший: `{items:[{id,title,publisher,issn,issns,quartile,year}]}` — поиск по справочнику журналов: по ISSN (с дефисом и без) или по словам названия (все слова, без учёта регистра); короче 2 знаков — пустой список; `limit` до 20, по умолчанию 10; без входа 401 |
| `GET /api/profile/cv` | PDF собственного резюме (`Content-Disposition: attachment`) |
| `GET /api/scientists/{id}` | публично, с учётом приватности: `{profile, viewer}`; скрытый или неизвестный 404; контакты только владельцу и сотрудникам организаций, `visibility` только владельцу |
| `GET /api/scientists/{id}/cv` | PDF резюме по тем же правилам (нужен вход) |
| `POST /api/applications` (multipart, часть `data` идёт первой и проверяется до приёма файлов; файлы принимаются потоком, каждый проверяется на вирусы: найден вирус 422 в поле `files`; антивирус недоступен — файл принят со статусом `pending`; хранилище недоступно 503 `storage_unavailable`; часть `data` — JSON `{vacancy_id, contact_email, cover_letter, referees:[{name,email,relation}]}`, части `file` — PDF) | 201 `{application}`; 422 с полями (`contact_email`, `cover_letter`, `referees`, `files`, `profile`); 409 `vacancy_closed`, `deadline_passed`, `already_applied`; 403 `own_vacancy`; 404; 429 |
| `GET /api/applications?limit=&offset=` | «Мои отклики»: `{items:[{id,status,created_at,status_changed_at,vacancy,references:{total,received}}], total}` |
| `GET /api/applications/for-vacancy/{vacancyId}` | `{can_apply, reason?: own_vacancy|closed|deadline_passed|applied, application}` для вошедшего |
| `GET /api/applications/{id}` | карточка: `{application}` с `viewer` `{role, can_withdraw, decisions, can_invite}`, `decision_note`, `invitations`; `applicant` (рекомендации — только статусы) или `staff` (с письмами; первое открытие ставит «просмотрен» и уведомляет соискателя); чужой и несуществующий — одинаковый 404 |
| `GET /api/applications/{id}/files/{fileId}` | PDF как скачивание потоком из хранилища; письмо рекомендателя соискателю — 404; файл «на проверке» 409 `file_pending`; заражённого нет (404); хранилище не отвечает 503 `storage_unavailable`; у организации выдача записывается в журнал до выдачи |
| `POST /api/applications/{id}/withdraw` | 204; 409 `invalid_status_change` |
| `POST /api/applications/{id}/references` `{name,email,relation}` | 201 `{reference}`; 409 `too_many_references`, `application_closed` |
| `POST …/references/{refId}/resend`, `DELETE …/references/{refId}` | повтор (429 `resend_too_soon` пока не прошли сутки) и отмена (409 `reference_not_pending`) |
| `POST /api/recommendations/lookup {token}` | без входа: `{request:{referee_name,relation,applicant_name,vacancy_title,org_name,status,expires_at}}`; 404 `invalid_link`, 410 `link_expired`, 410 `application_withdrawn` |
| `POST /api/recommendations/submit` (multipart: `data` `{token,text}` первой, не более одной части `file`; ссылка и длина текста проверяются до приёма файла) | 200; 422 (`text`, `file`, вирус в поле `file`); 409 `already_answered`; 503 `storage_unavailable` |
| `POST /api/recommendations/decline {token}` | 200; те же ошибки |
| `GET /api/my/candidates?vacancy=&status=&limit=&offset=` | отклики на вакансии, которые вошедший вправе разбирать (`ViewApplications`: организации целиком и подразделения, которыми руководит): `{items:[{id,status,created_at,applicant_name,headline,vacancy,unit_name,references,pending_invitations,proposed_invitations}], total, counts}`; `counts` по всем статусам (при выбранной вакансии только по ней); 422 на неизвестный статус и неверный номер вакансии; чужая вакансия даёт пустой список |
| `GET /api/my/candidates/export?vacancy=&status=` | файл `text/csv` для Excel (UTF-8 с BOM, разделитель «;», CRLF), `attachment; filename="otkliki-<дата>.csv"`, `Cache-Control: no-store`; те же права и отборы, что у списка, без страниц; не больше 5000 строк (заголовок `X-Export-Truncated: 1`, если больше); пишет `vacancy.applications_exported` в журнал каждой организации; 10 в минуту с аккаунта; 401 без входа, 422 по `vacancy`/`status` |
| `GET /api/my/candidate-vacancies` | `{items:[{id,title,status,org_name,org_slug,total,new}]}`: вакансии с откликами (отозванные не считаются, `new` = «отправлен») |
| `POST /api/applications/{id}/status` `{status: accepted|rejected, note}` | 204; только сотрудник с правом (иначе 404); 409 `invalid_status_change` из недопустимого статуса; 422 `status`, `note`; отказ закрывает открытые приглашения |
| `POST /api/applications/{id}/invitations` `{kind, message, starts_at, place_kind, place, contact_name, contact_email, contact_phone}` | 201 `{invitation}`; 422 по полям (`starts_at` RFC 3339, будущее, не дальше года); 409 `invalid_status_change` (отклик решён/отозван), `too_many_invitations` (10); отклик становится «приглашён», новое собеседование заменяет прежнее открытое |
| `DELETE /api/applications/{id}/invitations/{invId}` | 204; 409 `invalid_invitation_state` |
| `POST /api/applications/{id}/invitations/{invId}/accept-proposal` | 204: время, предложенное соискателем, становится временем собеседования; 409 `invalid_invitation_state` (нет предложения или оно прошло) |
| `POST /api/applications/{id}/invitations/{invId}/answer` `{action: confirm|propose|reply, proposed_at, note, contact, time}` | 204; только автор отклика (иначе 404); один раз, пока приглашение `pending`; 422 по полям; 409 `invalid_invitation_state` |
| `GET /api/notifications?limit=&offset=&unread=1` | `{items:[{id,kind,title,body,link,created_at,read}], total, unread}` (только свои) |
| `GET /api/notifications/unread-count` | `{unread}` |
| `POST /api/notifications/{id}/read`, `POST /api/notifications/read-all` | 204; чужое уведомление 404 |
| `GET /api/scientists?q=&field=&region=&degree=&title=&open=1&h_min=&q12_min=&sort=&limit=&offset=` | публично, с учётом приватности (D-098): `{items:[{id,name,headline,city,region,degree,academic_title,open_to_offers,h_index,publications,q12_total,q12_recent,specialties,updated_at}], total, capped, fuzzy, recent_from}` (первые 1000 результатов, как у вакансий); `q12_min` — статей в Q1–Q2 от (0–300); повторяющиеся `field`, `degree`, `title` значат «или»; `sort`: `relevance`, `updated`, `h_index`, `name`; `limit` до 50, по умолчанию 20; неизвестное значение и не число 422 с `fields`; контактов в ответе нет |
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
| `GET /api/landing` | публично, кеш 30 с (`ETag`, `Cache-Control: public, max-age=30`): `{open_vacancies, hiring_organizations, scientists, fields:[{code,vacancies}], types:[{type,vacancies}]}`; открытая = опубликована и срок не прошёл (по Москве); `scientists` = то, что гость видит в каталоге; вакансия считается в каждой своей области |
| `GET /api/account/export` | ZIP (`application/zip`, скачивание `scibox-data-ГГГГ-ММ-ДД.zip`) со всеми данными человека: `README.txt`, `data.json`, `files/…` (D-153); свежий пароль (иначе 403 `reauth_required`), не больше 3 в сутки (429 `rate_limited` и `Retry-After`); запись `account.data_exported` в личный журнал |
| `GET /api/health` | 200 `{"status":"ok","product":"ИОН","version":"dev","database":{"schema_version":1,"server_version":"16.15"}}`; 503 `database_unavailable`, если база не ответила за 2 с |

## Страницы фронтенда
| Адрес | Что |
|---|---|
| `/` | Лендинг (срез 12): заголовок и строка поиска (уводит на `/vacancies`), числа, оглавление по областям и видам позиций, 6 свежих вакансий (`GET /api/vacancies?limit=6`), «Если вы ищете место» / «Если вы набираете людей», «Устроено под науку» |
| `/vacancies` | Поиск вакансий (срез 6): строка поиска и регион, колонка фильтров (на узком экране раскрывается кнопкой), выбранные фильтры тегами, порядок, список, страницы по 20. Всё выбранное в адресе: `q`, `field`, `region`, `type`, `level`, `format`, `degree`, `org_kind`, `funding`, `rate`, `term`, `salary_min`, `housing`, `competition`, `deadline`, `sort`, `page` |
| `/status` | Статус сервера и базы (проверяем / работает / база недоступна / сервер не отвечает), кнопка «Проверить ещё раз» (бывшая стартовая) |
| `/styleguide` | Служебно: все компоненты со всеми состояниями (в меню нет) |
| `/login` (`?next=` — куда вернуть после входа), `/register`, `/forgot-password`, `/reset-password?token=`, `/confirm-email?token=`, `/account`, `/privacy` | Настоящие страницы аккаунтов (срез 3); `/account` без входа ведёт на `/login` |
| `/account/security` (с блоком «Ваши данные»: скачать архив, срез 22), `/confirm-email-change?token=`, `/revert-email-change?token=` | Срез 20: вкладка «Безопасность» (пароль, почта для входа, «Где вы вошли», удаление аккаунта); подтверждение новой почты (при открытии); откат почты (по кнопке). Окно «Подтвердите, что это вы» одно на сайт (`ReauthProvider` в `Layout`, хук `useReauth`), полоса `DeletionBanner` под шапкой, пока назначено удаление |
| `/organizations` (поиск и тип в адресе, страницы по 20), `/organizations/new`, `/organizations/:slug`, `/organizations/:slug/units/:unitId` | Каталог, создание, публичные страницы организации и подразделения (срез 4) |
| `/my-organization` | Мои организации и приглашения; без входа ведёт на `/login?next=` |
| `/my-organization/:slug` (+ `/units`, `/units/new`, `/units/:unitId`, `/members`, `/audit`) | Управление: данные, подразделения, сотрудники и приглашения, журнал действий (вкладки «Сотрудники» и «Журнал» только владельцу; у `viewer` поля `can_manage_members` и `can_view_audit`) |
| `/invitations/accept?token=` | Страница по ссылке из письма |
| `/my-vacancies` (`?status=`, `?page=`) | Мои вакансии: вкладки по статусам со счётчиками, срок подачи у опубликованных, вакансию открывает название, «Править» спокойной кнопкой; без входа ведёт на `/login?next=` |
| `/my-vacancies/new` (`?org=&unit=`), `/my-vacancies/:id/edit` | Форма вакансии; поля зависят от типа позиции |
| `/vacancies/:id` | Страница вакансии (статья); тем, кто ведёт вакансию, сверху управление: править, сменить статус, удалить черновик; остальным «Откликнуться» под заголовком и ещё раз после требований |
| `/profile` (раздел «Публикации», срез 14) | Квартиль у публикации, строка «N статей в журналах Q1–Q2, из них M за …», подпись источника SCImago; в окне публикации блок «Журнал из справочника» (`JournalPicker`) |
| `/profile` | Мой профиль: имя и должность, под ними кнопки «Изменить основное», «Скачать резюме», «Как видят другие», затем блок приватности и разделы с «Добавить / Изменить / Удалить»; без входа ведёт на `/login?next=` |
| `/profile/edit` | Форма основного: кто вы, степень и звание, специальности, ORCID/SPIN/Scopus/WoS, h-index, контактная почта |
| `/scientists/:id` | Страница учёного для других (приватность решает сервер; «нет профиля» и «скрыт» выглядят одинаково); у вошедшего в режиме «Нанимаю» кнопка «Пригласить на вакансию» |
| `/vacancies/:id/apply` | Форма отклика (письмо, файлы, рекомендатели); без входа ведёт на вход и обратно |
| `/applications`, `/applications/:id` | «Мои отклики» (страницы по 20, отметка «Ждёт вашего ответа») и отклик глазами соискателя: решение, приглашения с ответами (если открыл сотрудник организации — переход на `/candidates/:id`) |
| `/candidates` (`?vacancy=&status=&page=`) | Список откликов организации: отбор по вакансии, вкладки по статусам со счётчиками, страницы по 20 |
| `/candidates/:id` | Карточка отклика для организации: ссылка «Все отклики на эту вакансию», приглашения, письмо, решение (пригласить, принять, отказать), файлы, письма рекомендателей, снимок профиля; если открыл автор отклика — переход на `/applications/:id` |
| `/recommend?token=` | Страница рекомендателя без входа: письмо текстом и/или PDF, отказ |
| `/notifications` | Все уведомления страницами; колокольчик в шапке у вошедших |
| `/scientists` (`?q=&field=&region=&degree=&title=&open=&h_min=&q12_min=&sort=&page=`) | Раздел «Соискатели» (срез 10, название D-135; «статей в Q1–Q2» в записи и фильтр — срез 14): строка поиска и регион, фильтры, чипы, порядок, страницы по 20; у вошедшего в режиме «Нанимаю» на каждой записи «Пригласить на вакансию» |
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
`cli.serve` запускает рядом с HTTP: отправитель писем из `outbox` (раз в 2 с), фоновая работа с файлами `filework.Service.Run` (раз в 30 секунд: удаление объектов по очереди, перенос старых файлов, проверка файлов «на проверке»; раз в 6 часов чистка осиротевших; копии сервера не мешают друг другу: строки берутся `SKIP LOCKED`, проверка арендуется), очистку аккаунтов и организаций (раз в час; уборка аккаунтов заодно стирает аккаунты с прошедшим сроком отмены, по одному в транзакции, `SKIP LOCKED`, срез 20), очистку счётчиков частоты `rate_counters` (раз в час), сроки хранения `retention.Service.Run` (раз в час и сразу при старте: уведомления, журнал действий, адреса в журнале, регистрации без подтверждённой почты) и `matching.Service.Run` (раз в 5 минут): рассылка по сохранённым поискам (`SendDigests`) и напоминания о сроках (`SendReminders`, не раньше 9:00 МСК). Каждый проход самостоятелен, ошибки пишутся в журнал, следующий проход повторяет недоделанное; двойной отправки нет (блокировка строки поиска `SKIP LOCKED`, запись о напоминании и уведомление в одной транзакции).

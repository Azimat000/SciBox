# HANDOFF: записка следующей сессии

Обновлено: 2026-10-14. Срез 32 «Ссылка на GitHub в профиле» готов (`docs/slices/32-github-link.md`, D-210, метка `slice-32`). `ТЕКУЩИЙ: 33`. Очередь: 33 (навыки из справочника с подсказками), затем 30 (модерация организаций).

## Где остановились
- **Что появилось:** поле «GitHub» среди идентификаторов профиля. Человек вводит `github.com/логин` или `https://github.com/логин`, в базе `https://github.com/<логин>` (`profiles.github_url`, миграция 00029). На странице профиля ссылка в новой вкладке, в резюме PDF строка «GitHub», в безымянной карточке поля нет. Разбор строгий, без сети (`profiles/github.go`).
- **Что проверено:** `make test` зелёный (сервер 98,7%, `profiles` 98,6%, сайт 99,78% строк, 1014 тестов), `make lint-new` 0 issues, фаззинг разборщика, страница и форма осмотрены в браузере на 1440 и 375.
- **Что ждёт пользователя вне кода:** хостинг и `docs/DEPLOY.md` (раздел 11); ручная модерация организаций (T-68, D-197, срез 30: пользователь сам опишет, до тех пор не проектировать); юрист: сроки хранения откликов (T-37), политика `/privacy` черновик (абзацы про анонимный режим и про выгрузки откликов, T-89).

## Что дальше: 33, затем 30
- Срез 33: справочник навыков и выбор поиском (как на hh.ru); профиль продолжает хранить навыки строками (`research_skills`, `general_skills`). Перед работой задать пользователю вопросы из ROADMAP (источник справочника, судьба «своих» навыков); источник исследовать через `web-researcher`, условия записать в `reference_sources` и `docs/REFERENCE.md`. Образец поиска: специальности и журналы.
- Модерация (срез 30) по-прежнему последняя, детали опишет пользователь.
- Перед работой с профилями прочитать I-86, I-97…I-101 (анонимность), D-199…D-202.

## Как запустить
- `make dev` (сервер + воркер + сайт; http://localhost:5173), вход `elena.orlova@demo.example.ru` (владелец), `dmitry.korolev@demo.example.ru` (соискатель), пароль в `server/seed/seed.go`. После этого среза нужен `make migrate` (миграция 00029); GitHub у демо-людей появится после `make db-reset` (в seed он есть у двух учёных).
- `make test` (линтер, vulncheck, `monitoring-check`, сервер с порогами, сайт; ≈15 минут), `make e2e`, `make load-test` (после `make load-data`).
- Боевая репетиция: `make prod-up`, `make prod-e2e`, `make prod-restore-check`, `make prod-down` (нужен остановленный dev-стек и ~3 ГБ Docker).

## Неочевидные вещи
- **Новое поле профиля (идентификатор):** столбец с `DEFAULT` (миграции после 00024 только добавляют, D-186), `UpdateProfileCore` в `profiles.sql` + `go tool sqlc generate`, `CoreInput`, `coreFields`, `validateCore`, `Identifiers`, `build`, `SaveCore`, `idFacts` в резюме; на сайте `api.ts` (тип профиля и `CoreFields`), `ProfileEditPage` (`Values`, `initialOf`, `fieldsOf`, поле), `ProfileView`, `ru.ts`, `test/profile.ts`. Снимок отклика (`json.Marshal(View)`), выгрузка данных (`to_jsonb(profiles)`) и стирание (удаление строки) получают поле сами; безымянная карточка не получает (`blindView`, список разрешённого). Старые снимки откликов нового поля не содержат: в типе сайта оно необязательное.
- **Тест миграций** (`migrate_test.go`) перечисляет миграции по именам: новая миграция добавляется в три места (первый `up`, `down` в начале спуска, `up` перед `reset`).
- **Выгрузка:** данные берутся из снимка отклика, а не профиля (D-079); новая колонка файла идёт через `ExportRow`, `exportHeader` и `WriteCSV`, каждая ячейка с текстом людей через `cell` (формулы, T-87). GitHub в файл не входит.
- **Новое поле вакансии:** `Input` и `fields` (`validate.go`), `CreateVacancy`/`UpdateVacancy` в `vacancies.sql` и оба вызова в `service.go`, `inputOf`, `body` в `http.go`, `cardFrom`/`Get`, форма и `VacancyEditPage.valuesOf`, `test/vacancies.ts`. Пропущенный вызов даёт тихую потерю значения.
- **Как смотреть профиль в новом коде:** никогда не сравнивать `visibility` строкой. Собрать `privacy.Viewer` (`ListUserOrgIDs`, `ProfileOrgRelations`), спросить `CanView`/`Detail`/`CanSeeContacts`; в списках организации имя решает `privacy.ShowsName`.
- **iCloud Drive:** Mac выгружает файлы проекта (флаг `dataless`), тесты сайта, `docker build` и `git` зависают. Зависимости сайта в `web/node_modules.nosync`. Лечится: `find web server deploy config -type f -flags +dataless -exec cat {} +`. Свободного места держать не меньше ~12 ГБ (сейчас около 7 ГБ: осторожно с `make load-data`, `prod-up`).
- **Помощники тестов** ставят `Binding: "test-registration-browser"` в `Meta`; новый помощник с `auth.Meta{}` без этого получит `ConfirmedElsewhereError`. Тест, меняющий `exportLimit` (`applications`), возвращает его через `defer`. `testkit.FillProfile` теперь задаёт GitHub (`github.com/scibox-demo-applicant`).
- **Лимиты:** новый счётчик «не больше N» в транзакции начинать с `q.LockRateKey(ctx, kind+":"+key)` (I-80). Тексты человека проверяет периметр и `DecodeJSON` (`textsafe`).
- Каждая сборка Docker даёт новый номер образа базы (`deploy/db-image-tag`). `go build`/`go test` без git-индекса: `GOFLAGS=-buildvcs=false`. Порт базы 5433. Ошибки 500 только через `apierr.WriteInternal`. Тексты сайта только в `web/src/i18n/ru.ts`. `npx prettier` в `web/` не запускать. Запуск dev-сервера через `preview_start` падает («getcwd»): `make dev` или `scripts/dev.sh` из терминала. В zsh `grep --include=*.go` нужно брать в кавычки.
- Mac: Docker (Colima) сейчас 5 ядер; вернуть: `colima stop && colima start --cpu 2 --memory 4`.

## Известные проблемы и долги
- Не сделано: цель 500 зап/с не проверена на боевой машине; полный ClamAV и Let's Encrypt (T-41); автоматический повтор удалений после восстановления из копии (T-55); закрепление образов по хэшам (T-56); удаление `legacy_data`.
- Приняты (D-195, D-199…D-202, D-205, D-210): T-75…T-85, T-88, T-89, T-91. Открытых угроз по профилям нет.
- Описание оборудования не входит в текстовый поиск (D-203).
- Из прежних: окно «свежих подходящих» до проверки приватности (находка 1 в `docs/slices/24-differential-review.md`); D-124; сквозные тесты не входят в CI (D-140); `files.sweep` в dev-базе ругается на чужие объекты хранилища (общий бакет с e2e), на данные не влияет.

## Окружение
Go 1.26.9, golangci-lint 2.12.2, govulncheck v1.8.0, semgrep 1.180, k6 2.1.0, Node 26.8, Docker 29 (Colima), PostgreSQL 16.15, pgBackRest 2.59.3, Caddy 2.11.7, ClamAV 1.5.4, Playwright 1.63.0, OWASP ZAP (образ `zaproxy/zap-stable`).

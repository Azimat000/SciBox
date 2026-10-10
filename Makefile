# Команды SciBox. Подробности: docs/ARCHITECTURE.md, docs/TESTING.md.
.PHONY: prod-up prod-down prod-e2e prod-restore-check prod-config dev share worker load-test load-smoke test test-server test-web e2e install db-up db-down db-reset migrate seed sqlc lint lint-new vulncheck load-data load-check monitoring-up monitoring-down monitoring-check glitchtip-setup errors-up

SCIBOX = cd server && go run ./cmd/api

## dev: поднять базу и почту, накатить миграции, запустить сервер, воркер (фоновые задачи) и сайт
dev: install migrate
	./scripts/dev.sh

## worker: только фоновые задачи (письма, подбор, напоминания, чистка, файлы) без сайта и сервера; `make dev` запускает его сам
worker: db-up
	$(SCIBOX) worker

## share: показать сайт другому человеку по публичной ссылке, пока он работает на этом Mac (D-131)
share: install migrate
	./scripts/share

## test: линтер, проверка уязвимостей и все тесты с проверкой порогов покрытия (срез 18)
test: install db-up lint vulncheck monitoring-check test-server test-web

test-server:
	./scripts/coverage-check

test-web:
	cd web && npm run typecheck && npm run lint && npm run test:coverage

## lint: проверки сервера на ошибки и безопасность (server/.golangci.yml, D-137); lint-new — только незакоммиченные правки
lint:
	cd server && golangci-lint run ./...

lint-new:
	cd server && golangci-lint run --new ./...

## vulncheck: известные уязвимости в Go и библиотеках, через которые реально проходит код сервера (D-137)
vulncheck:
	cd server && go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

## load-data: объёмные данные для замеров в отдельной базе scibox_load (срез 24); масштаб 1 = «вся наука РФ» (~12 ГБ), по умолчанию 0.5
load-data: db-up
	./scripts/load-data $(or $(SCALE),0.5)

## load-check: замер горячих запросов на объёмных данных (нужен `make load-data`); медленнее 50 мс — тест красный
load-check: db-up
	cd server && SCIBOX_LOAD_DATABASE_URL='postgres://scibox:scibox@localhost:5433/scibox_load?sslmode=disable' go test ./internal/cli -run TestHotQueriesOnLoadData -count=1 -v -timeout 20m

## load-test: нагрузочный тест (срез 27): две копии сервера и воркер на базе scibox_load, k6, ступени скорости (нужен `make load-data`).
## Свои ступени: make load-test STEPS=100,300 DURATION=30s
load-test:
	DURATION=$(or $(DURATION),60s) ./scripts/load-test $(or $(STEPS),100,200,300,400,500,600)

## load-smoke: короткая проверка сценария (одна ступень 30 зап/с, 20 секунд), без профилей; годится после правок сервера
load-smoke:
	PROFILE=0 DURATION=20s ./scripts/load-test 30

## e2e: сквозные сценарии в настоящем браузере (Playwright) на отдельной базе scibox_e2e.
## Нужен свободный порт 8090 и 5174; `make dev` может работать параллельно.
e2e: install db-up e2e/node_modules/.package-lock.json
	./scripts/e2e-db
	cd e2e && npx tsc -p . && npx playwright test

e2e/node_modules/.package-lock.json: e2e/package-lock.json
	cd e2e && npm ci && npx playwright install chromium

install: web/node_modules/.package-lock.json

web/node_modules/.package-lock.json: web/package-lock.json
	cd web && npm ci

## db-up / db-down: база PostgreSQL (порт 5433), Mailpit (8025), хранилище файлов (8333) и антивирус (3310)
db-up:
	docker compose up -d --wait

db-down:
	docker compose down

migrate: db-up
	$(SCIBOX) migrate up
	$(SCIBOX) journals load

seed: migrate
	$(SCIBOX) seed

## migrate также загружает справочник журналов SCImago (тот же файл второй раз не грузится).
## db-reset: откатить всё, накатить заново, загрузить справочник журналов и демо-данные
db-reset: db-up
	$(SCIBOX) migrate reset
	$(SCIBOX) migrate up
	$(SCIBOX) journals load
	$(SCIBOX) seed

## sqlc: пересобрать код запросов из server/db/queries
sqlc:
	cd server && go tool sqlc generate

## monitoring-up / monitoring-down: Prometheus (9190), Grafana (3100), GlitchTip (8100) и node-exporter (срез 26)
monitoring-up:
	docker compose -f docker-compose.monitoring.yml up -d --wait

## errors-up: добавить к мониторингу сбор ошибок GlitchTip (8100); первый раз затем `make glitchtip-setup`
errors-up:
	docker compose -f docker-compose.monitoring.yml --profile errors up -d --wait

monitoring-down:
	docker compose -f docker-compose.monitoring.yml --profile errors down

## monitoring-check: синтаксис и тесты правил тревог (promtool из образа Prometheus); входит в `make test`
monitoring-check:
	docker run --rm --entrypoint /bin/promtool -v "$(CURDIR)/monitoring/prometheus:/p:ro" prom/prometheus:v3.15.0 check rules /p/rules/scibox.yml
	docker run --rm --entrypoint /bin/promtool -v "$(CURDIR)/monitoring/prometheus:/p:ro" prom/prometheus:v3.15.0 test rules /p/tests/scibox_test.yml

## glitchtip-setup: создать в локальном GlitchTip администратора, организацию и проект; печатает SCIBOX_ERROR_DSN для сервера
glitchtip-setup:
	./scripts/glitchtip-setup

## prod-up: репетиция боевого стенда на этом компьютере (срез 28): образ, Caddy с самоподписанным сертификатом, две копии
## сервера, воркер, база с архивом, копии, антивирус; https://localhost:8443, письма http://localhost:8026
prod-up:
	./deploy/local/up.sh

## prod-down: остановить репетицию (данные остаются; `make prod-down CLEAN=1` стирает и тома)
prod-down:
	SCIBOX_DEPLOY_LOCAL=1 ./deploy/compose down $(if $(CLEAN),-v,)

## prod-e2e: сквозные сценарии против репетиции боевого стенда (после `make prod-up`)
prod-e2e: e2e/node_modules/.package-lock.json
	cd e2e && npx tsc -p . && E2E_TARGET=https://localhost:8443 E2E_MAILPIT_URL=http://localhost:8026 npx playwright test

## prod-restore-check: проверка восстановления из копии (срез 28, T-26): поднимает копию базы рядом и сверяет с рабочей
prod-restore-check:
	SCIBOX_DEPLOY_LOCAL=1 ./deploy/restore-check.sh

## prod-config: проверить конфигурацию compose боевого стенда и репетиции без запуска
prod-config:
	SCIBOX_DEPLOY_LOCAL=1 ./deploy/compose config -q

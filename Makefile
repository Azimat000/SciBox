# Команды SciBox. Подробности: docs/ARCHITECTURE.md, docs/TESTING.md.
.PHONY: dev share test test-server test-web e2e install db-up db-down db-reset migrate seed sqlc lint lint-new vulncheck load-data load-check

SCIBOX = cd server && go run ./cmd/api

## dev: поднять базу и почту, накатить миграции, запустить сервер и сайт
dev: install migrate
	./scripts/dev.sh

## share: показать сайт другому человеку по публичной ссылке, пока он работает на этом Mac (D-131)
share: install migrate
	./scripts/share

## test: линтер, проверка уязвимостей и все тесты с проверкой порогов покрытия (срез 18)
test: install db-up lint vulncheck test-server test-web

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

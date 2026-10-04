# Команды SciBox. Подробности: docs/ARCHITECTURE.md, docs/TESTING.md.
.PHONY: dev test test-server test-web e2e install db-up db-down db-reset migrate seed sqlc

SCIBOX = cd server && go run ./cmd/api

## dev: поднять базу и почту, накатить миграции, запустить сервер и сайт
dev: install migrate
	./scripts/dev.sh

## test: все тесты с проверкой порогов покрытия
test: install db-up test-server test-web

test-server:
	./scripts/coverage-check

test-web:
	cd web && npm run typecheck && npm run lint && npm run test:coverage

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

## db-up / db-down: база PostgreSQL (порт 5433) и Mailpit (8025)
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

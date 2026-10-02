# Архитектура

Состояние: **кода ещё нет** (после среза 0). Ниже целевая структура; разделы заполняются по мере появления кода.
Правило: если этот файл расходится с кодом, прав код, а файл чинится в том же коммите.

## Структура папок (целевая)

```
SciBox/
├── web/                 React + Vite + TypeScript
│   └── src/
│       ├── app/         роутинг, провайдеры, каркас
│       ├── features/    по разделам: auth, orgs, vacancies, search, profile, applications, catalog...
│       ├── ui/          компоненты дизайн-системы
│       ├── i18n/        словари (ru.ts; позже en.ts)
│       └── api/         клиент к серверу
├── server/              Go
│   ├── cmd/api/         точка входа
│   ├── internal/        http-обработчики, сервисы, фоновые задачи, почта, pdf
│   ├── db/migrations/   goose
│   ├── db/queries/      SQL для sqlc
│   └── seed/            демо-данные и справочники
├── storage/uploads/     файлы пользователей (в git не попадает)
├── docker-compose.yml   PostgreSQL 16, Mailpit
├── Makefile             dev, test, seed, migrate, db-reset
└── docs/                память проекта
```

## Порты (план)
- web: 5173 · api: 8080 · Postgres: 5432 · Mailpit UI: 8025, SMTP: 1025

## Таблицы БД
_пусто_

## API
_пусто_

## Страницы фронтенда
_пусто_

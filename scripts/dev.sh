#!/bin/sh
# Запуск сервера, воркера и сайта вместе. Остановка: Ctrl+C.
# Сервер отвечает на запросы, воркер делает фоновую работу (письма, подбор, напоминания, чистка, файлы): так же,
# как будет в работе, где копий может быть несколько (срез 25).
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)

(cd "$root/server" && go build -o bin/scibox ./cmd/api)

(cd "$root/server" && SCIBOX_BACKGROUND=off exec ./bin/scibox serve) &
api=$!
(cd "$root/server" && exec ./bin/scibox worker) &
worker=$!
(cd "$root/web" && exec npm run dev -- --strictPort) &
web=$!

trap 'kill $api $worker $web 2>/dev/null || true' INT TERM EXIT
echo
echo "  Сайт:      http://localhost:5173"
echo "  Сервер:    http://127.0.0.1:8080/api/health"
echo "  Воркер:    фоновые задачи (журнал в этом же окне)"
echo "  Почта:     http://localhost:8025"
echo "  Остановить: Ctrl+C"
echo
wait

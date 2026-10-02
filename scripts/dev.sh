#!/bin/sh
# Запуск сервера и сайта вместе. Остановка: Ctrl+C.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)

(cd "$root/server" && go build -o bin/scibox ./cmd/api)

(cd "$root/server" && exec ./bin/scibox serve) &
api=$!
(cd "$root/web" && exec npm run dev -- --strictPort) &
web=$!

trap 'kill $api $web 2>/dev/null || true' INT TERM EXIT
echo
echo "  Сайт:      http://localhost:5173"
echo "  Сервер:    http://127.0.0.1:8080/api/health"
echo "  Почта:     http://localhost:8025"
echo "  Остановить: Ctrl+C"
echo
wait

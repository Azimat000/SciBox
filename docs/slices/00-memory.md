# Срез 0. Каркас памяти

Дата: 2026-10-02. Статус: готов.

## План
Перед написанием кода подготовить систему, которая переносит контекст между сессиями.

## Сделано
- Интервью с пользователем (6 раундов) и исследование зарубежных аналогов; итоги в `docs/DECISIONS.md`.
- `git init`, `.gitignore` (исключены `storage/`, `.env`, `.claude/settings.local.json`, `skill-test/`).
- Файлы памяти: `ROADMAP.md`, `HANDOFF.md`, `DECISIONS.md`, `ARCHITECTURE.md`, `DOMAIN.md`, эта папка `slices/`.
- Скилл `.claude/skills/next-slice/SKILL.md` (команда `/next-slice`).
- Хук SessionStart `.claude/hooks/session-context.sh` подключён в `.claude/settings.json`: в начале каждой сессии в контекст попадают HANDOFF и текущий срез.
- Раздел «Работа срезами» в `CLAUDE.md`.

## Отступления от плана
- `skill-test/` (A/B-проверка скиллов на лендинге кофейни) не удалён, только исключён из git.

## Проверки
- Хук запущен вручную, вывод корректен.

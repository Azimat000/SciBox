#!/bin/sh
# SessionStart: подкладывает в контекст новой сессии записку HANDOFF и текущий срез.
root="${CLAUDE_PROJECT_DIR:-$(pwd)}"
handoff="$root/docs/HANDOFF.md"
roadmap="$root/docs/ROADMAP.md"

[ -f "$handoff" ] || exit 0

echo "=== ПАМЯТЬ ПРОЕКТА SciBox ==="
echo "Проект ведётся срезами. Следующий срез запускается командой /next-slice."
echo "Источник правды: docs/ (ROADMAP, HANDOFF, DECISIONS, ARCHITECTURE, DOMAIN, TESTING, slices/)."
echo "Покрытие тестами: >= 90% везде, >= 97% в критичных зонах (docs/TESTING.md)."
echo

if [ -f "$roadmap" ]; then
  current=$(sed -n 's/^ТЕКУЩИЙ: *//p' "$roadmap" | head -1)
  if [ -n "$current" ]; then
    echo "--- Текущий срез по ROADMAP: $current ---"
    awk -v n="$current" '
      $0 ~ "^## \\[.\\] " n "\\. " {p=1; print; next}
      p && /^## / {exit}
      p {print}
    ' "$roadmap"
    echo
  fi
fi

echo "--- docs/HANDOFF.md ---"
cat "$handoff"

if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo
  echo "--- Последние коммиты ---"
  git -C "$root" log --oneline -5 2>/dev/null
  dirty=$(git -C "$root" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
  [ "$dirty" != "0" ] && echo "Внимание: незакоммиченных изменений: $dirty"
fi
exit 0

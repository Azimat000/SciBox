// Список навыков: чистка, ключ сравнения и добавление фраз (поле SkillsInput).

export const clean = (s: string) => s.replace(/\s+/g, ' ').trim()

/**
 * Ключ сравнения навыков: без различий регистра, «ё» и «е», видов тире. Те же правила на сервере (skills.Key):
 * два написания с одним ключом считаются одним навыком.
 */
export const skillKey = (s: string) =>
  clean(s)
    .toLowerCase()
    .replace(/ё/g, 'е')
    .replace(/[‐-―−]/g, '-')

/** Добавляет к списку новые фразы: без пустых, без повторов (регистр, «ё» и тире не важны) и не больше max. */
export function addSkills(list: readonly string[], parts: readonly string[], max: number): string[] {
  const out = [...list]
  const seen = new Set(out.map(skillKey))
  for (const raw of parts) {
    const v = clean(raw)
    if (v === '' || seen.has(skillKey(v)) || out.length >= max) continue
    seen.add(skillKey(v))
    out.push(v)
  }
  return out
}

/** Подсказка справочника: каноническое название, группа и синоним, по которому навык найден. */
export type SkillSuggestion = { name: string; group: string; via?: string }

/** Поиск подсказок: `fuzzy` — точных совпадений нет, показаны похожие написания. */
export type SkillSearch = (query: string, signal?: AbortSignal) => Promise<{ items: SkillSuggestion[]; fuzzy: boolean }>

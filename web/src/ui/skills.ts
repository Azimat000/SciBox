// Список навыков: чистка и добавление фраз (поле SkillsInput).

export const clean = (s: string) => s.replace(/\s+/g, ' ').trim()

/** Добавляет к списку новые фразы: без пустых, без повторов (регистр не важен) и не больше max. */
export function addSkills(list: readonly string[], parts: readonly string[], max: number): string[] {
  const out = [...list]
  const seen = new Set(out.map((s) => s.toLowerCase()))
  for (const raw of parts) {
    const v = clean(raw)
    if (v === '' || seen.has(v.toLowerCase()) || out.length >= max) continue
    seen.add(v.toLowerCase())
    out.push(v)
  }
  return out
}

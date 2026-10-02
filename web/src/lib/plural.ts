/** Русское склонение по числу: plural(1, ['день', 'дня', 'дней']) → 'день'. */
export function plural(n: number, forms: readonly [string, string, string]): string {
  const abs = Math.abs(Math.trunc(n))
  const last = abs % 10
  const lastTwo = abs % 100
  if (lastTwo >= 11 && lastTwo <= 14) return forms[2]
  if (last === 1) return forms[0]
  if (last >= 2 && last <= 4) return forms[1]
  return forms[2]
}

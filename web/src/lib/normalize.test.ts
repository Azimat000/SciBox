import { expect, it } from 'vitest'
import { normalize } from './normalize'

it('ignores case, ё and surrounding spaces', () => {
  expect(normalize('  Йошкар-Ола ')).toBe('йошкар-ола')
  expect(normalize('Учёные')).toBe('ученые')
})

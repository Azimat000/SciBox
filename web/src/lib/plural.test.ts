import { describe, expect, it } from 'vitest'
import { plural } from './plural'

const forms = ['день', 'дня', 'дней'] as const

describe('plural', () => {
  it.each([
    [0, 'дней'],
    [1, 'день'],
    [2, 'дня'],
    [4, 'дня'],
    [5, 'дней'],
    [11, 'дней'],
    [12, 'дней'],
    [14, 'дней'],
    [21, 'день'],
    [22, 'дня'],
    [25, 'дней'],
    [101, 'день'],
    [111, 'дней'],
    [-3, 'дня'],
    [2.9, 'дня'],
  ])('%d → %s', (n, expected) => {
    expect(plural(n, forms)).toBe(expected)
  })
})

import { describe, expect, it } from 'vitest'
import { sizeText } from './fileSize'

describe('sizeText', () => {
  it.each([
    [1, '1 КБ'],
    [512, '1 КБ'],
    [1024, '1 КБ'],
    [16_500, '16 КБ'],
    [29_000, '28 КБ'],
    [1024 * 1024 - 1, '1024 КБ'],
    [1024 * 1024, '1,0 МБ'],
    [2_400_000, '2,3 МБ'],
    [10 * 1024 * 1024, '10,0 МБ'],
  ])('%i bytes → %s', (bytes, text) => {
    expect(sizeText(bytes)).toBe(text)
  })
})

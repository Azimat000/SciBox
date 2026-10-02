import { describe, expect, it } from 'vitest'
import { t } from '.'
import { ru } from './ru'
import { productName } from '../config/product'
import product from '../../../config/product.json'

describe('i18n', () => {
  it('uses the Russian dictionary', () => {
    expect(t).toBe(ru)
  })

  it('formats messages with values', () => {
    expect(t.status.databaseOk(3)).toBe('Работает, схема версии 3')
    expect(t.status.version('1.2.0')).toBe('Версия сервера: 1.2.0')
  })
})

describe('product config', () => {
  it('reads the product name from the shared config file', () => {
    expect(productName).toBe(product.name)
    expect(productName.length).toBeGreaterThan(0)
  })
})

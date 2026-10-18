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
    expect(t.find.filtersCount(3)).toBe('Фильтры (3)')
    expect(t.orgs.mine.accepted('Институт')).toBe('Вы теперь в организации «Институт»')
  })
})

describe('product config', () => {
  it('reads the product name from the shared config file', () => {
    expect(productName).toBe(product.name)
    expect(productName.length).toBeGreaterThan(0)
  })
})

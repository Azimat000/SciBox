import { describe, expect, it } from 'vitest'
import { card } from '../../test/vacancies'
import { contractText, entryFacts, entryTopics, levelParts, placeText, salaryLabel, salaryText, termText, transitionLabel, doneText, statusLabel, positionTypeLabel, formatLabel, housingLabel, fundingLabel, degreeLabel, titleLabel } from './labels'

describe('labels', () => {
  it('formats salary ranges with one or both bounds, and says nothing without them', () => {
    expect(salaryText(80000, 120000)).toBe('80 000 – 120 000 ₽')
    expect(salaryText(80000, null)).toBe('от 80 000 ₽')
    expect(salaryText(null, 120000)).toBe('до 120 000 ₽')
    expect(salaryText(null, null)).toBeNull()
  })

  it('writes contract terms in years when they are whole, otherwise in months', () => {
    expect(termText(12)).toBe('1 год')
    expect(termText(36)).toBe('3 года')
    expect(termText(60)).toBe('5 лет')
    expect(termText(18)).toBe('18 месяцев')
    expect(termText(1)).toBe('1 месяц')
    expect(termText(3)).toBe('3 месяца')
  })

  it('describes the contract', () => {
    expect(contractText('permanent', null)).toBe('Бессрочный договор')
    expect(contractText('fixed', 36)).toBe('Срочный договор, 3 года')
    expect(contractText('fixed', null)).toBe('Срочный договор')
    expect(contractText('', null)).toBe('')
  })

  it('names the level or nothing', () => {
    expect(levelParts(3)).toEqual(['R3', 'самостоятельный исследователь'])
    expect(levelParts(null)).toBeNull()
    expect(levelParts(9)).toBeNull()
  })

  it('shows a stipend for early career positions and a salary for the rest', () => {
    expect(salaryLabel('early_career')).toBe('Стипендия')
    expect(salaryLabel('research')).toBe('Зарплата')
  })

  it('builds the facts line of an entry and skips what is unknown', () => {
    const facts = entryFacts(card)
    expect(facts[0]).toBe('Старший научный сотрудник')
    expect(facts).toContain('1 ставка')
    expect(facts).toContain('Срочный договор, 3 года')
    expect(facts).toContain('Очно')
    expect(facts.find((f) => f.startsWith('Зарплата'))).toContain('95 000')
    const bare = entryFacts({ ...card, rate_percent: null, contract_type: '', work_format: '', salary_from: null, salary_to: null })
    expect(bare).toEqual(['Старший научный сотрудник'])
    expect(entryFacts({ ...card, position: { ...card.position, type: 'early_career' } }).find((f) => f.startsWith('Стипендия'))).toBeDefined()
    expect(entryFacts({ ...card, rate_percent: 50 })).toContain('0,5 ставки')
  })

  it('puts the specialties of an entry on their own line', () => {
    expect(entryTopics(card)).toBe('Физическая химия и ещё 1')
    expect(entryTopics({ ...card, specialties: [{ code: '1.4.3', name: 'Органическая химия' }] })).toBe('Органическая химия')
    expect(entryTopics({ ...card, specialties: [] })).toBe('')
  })

  it('joins city and region and copes with either missing', () => {
    expect(placeText({ city: 'Новосибирск', region: { code: '54', name: 'Новосибирская область' } })).toBe('Новосибирск, Новосибирская область')
    expect(placeText({ city: 'Новосибирск', region: null })).toBe('Новосибирск')
    expect(placeText({ city: '', region: { code: '54', name: 'Новосибирская область' } })).toBe('Новосибирская область')
    expect(placeText({ city: '', region: null })).toBe('')
  })

  it('names the actions of every status change and falls back to the status itself', () => {
    expect(transitionLabel('draft', 'published')).toBe('Опубликовать')
    expect(transitionLabel('closed', 'published')).toBe('Открыть снова')
    expect(transitionLabel('published', 'closed')).toBe('Закрыть набор')
    expect(transitionLabel('closed', 'archived')).toBe('В архив')
    expect(transitionLabel('archived', 'closed')).toBe('Вернуть из архива')
    expect(transitionLabel('x', 'y')).toBe('y')
    expect(doneText('published')).toBe('Вакансия опубликована')
    expect(doneText('weird')).toBe('Статус изменён')
  })

  it('shows an unknown value from a newer server as it is', () => {
    expect(statusLabel('published')).toBe('Опубликована')
    expect(statusLabel('new')).toBe('new')
    expect(positionTypeLabel('x')).toBe('x')
    expect(formatLabel('x')).toBe('x')
    expect(housingLabel('x')).toBe('x')
    expect(fundingLabel('x')).toBe('x')
    expect(degreeLabel('x')).toBe('x')
    expect(titleLabel('x')).toBe('x')
  })
})

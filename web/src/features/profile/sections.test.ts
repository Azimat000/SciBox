import { describe, expect, it } from 'vitest'
import { book, education, experience, grant, patent, publication, teaching } from '../../test/profile'
import { degreeLabel, grantRoleLabel, patentTypeLabel, pubTypeLabel, publicationMeta, teachLevelLabel, titleLabel, years } from './labels'
import { bodyOf, itemTitle, sectionDefs, valuesOf } from './sections'

describe('labels', () => {
  it('names known values and shows an unknown one as it is', () => {
    expect(degreeLabel('doctor')).toBe('Доктор наук')
    expect(degreeLabel('phd')).toBe('phd')
    expect(titleLabel('professor')).toBe('Профессор')
    expect(pubTypeLabel('preprint')).toBe('Препринт')
    expect(grantRoleLabel('participant')).toBe('Исполнитель')
    expect(patentTypeLabel('software')).toBe('Программа для ЭВМ')
    expect(teachLevelLabel('postgraduate')).toBe('Аспирантура')
    expect(teachLevelLabel('school')).toBe('school')
  })

  it('writes years as a range', () => {
    expect(years(2010, 2014)).toBe('2010 — 2014')
    expect(years(2018, null)).toBe('2018 — н. в.')
    expect(years(2018)).toBe('2018 — н. в.')
    expect(years(2020, 2020)).toBe('2020')
    expect(years(undefined, 2020)).toBe('2020')
    expect(years()).toBe('')
  })

  it('sets a publication like a bibliography line, with dots where they are missing', () => {
    expect(publicationMeta(publication)).toBe('Орлова Е. А., Белов И. С. Журнал физической химии, 2023. Т. 97, № 4, С. 512–520.')
    expect(publicationMeta(book)).toBe('Орлова Е. А. Изд-во СО РАН, 2020.')
    expect(publicationMeta({ id: 'x', kind: 'publication' })).toBe('')
  })
})

describe('section definitions', () => {
  it('summarize every kind of item', () => {
    expect(sectionDefs.education.summary(education)).toEqual({ label: '2010 — 2014', lead: education.institution, lines: ['Аспирантура, физика'] })
    expect(sectionDefs.experience.summary(experience).lines).toEqual(['Институт катализа', 'Руковожу группой'])
    expect(sectionDefs.publication.summary(publication)).toMatchObject({ label: '2023', lead: publication.title, doi: '10.1234/abc.2023' })
    expect(sectionDefs.publication.summary(book).lines.at(-1)).toBe('Книга')
    expect(sectionDefs.grant.summary(grant).lines).toEqual(['РНФ, № 24-12-00177, Руководитель'])
    expect(sectionDefs.patent.summary(patent).lines).toEqual(['Изобретение, № RU 2 745 123, Роспатент', 'Орлова Е. А.'])
    expect(sectionDefs.teaching.summary(teaching).lines).toEqual(['НГУ, Магистратура', 'Лекции и семинары'])
  })

  it('survive items with missing fields', () => {
    for (const kind of Object.keys(sectionDefs) as (keyof typeof sectionDefs)[]) {
      const s = sectionDefs[kind].summary({ id: 'x', kind })
      expect(s.label).toBe('')
      expect(s.lead).toBe('')
    }
    expect(itemTitle(education)).toBe(education.institution)
  })

  it('fill the form from an item or from the defaults and turn it back into a request', () => {
    const def = sectionDefs.publication
    expect(valuesOf(def, null)).toMatchObject({ pub_type: 'article', title: '', year: '' })
    expect(valuesOf(def, publication)).toMatchObject({ title: publication.title, year: '2023', volume: '97' })
    expect(bodyOf(def, { ...valuesOf(def, null), title: '  Статья  ', year: '2024', authors: 'Орлова Е.' })).toEqual({ title: 'Статья', authors: 'Орлова Е.', pub_type: 'article', year: 2024 })
    expect(bodyOf(sectionDefs.education, {})).toEqual({})
  })
})

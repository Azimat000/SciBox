import type { Item, Profile, ProfilePage } from '../features/profile/api'

export const PROFILE_ID = '77777777-7777-4777-8777-777777777777'

export const education: Item = { id: 'e1', kind: 'education', institution: 'Новосибирский государственный университет', program: 'Аспирантура, физика', year_from: 2010, year_to: 2014 }
export const experience: Item = { id: 'x1', kind: 'experience', organization: 'Институт катализа', position: 'Старший научный сотрудник', year_from: 2018, description: 'Руковожу группой' }
export const publication: Item = {
  id: 'p1', kind: 'publication', title: 'Активные центры катализаторов', authors: 'Орлова Е. А., Белов И. С.', venue: 'Журнал физической химии', pub_type: 'article', year: 2023,
  volume: '97', issue: '4', pages: '512–520', doi: '10.1234/abc.2023', source: 'manual',
}
export const book: Item = { id: 'p2', kind: 'publication', title: 'Вычислительные методы', authors: 'Орлова Е. А.', venue: 'Изд-во СО РАН.', pub_type: 'book', year: 2020, url: 'https://example.ru/book' }
export const grant: Item = { id: 'g1', kind: 'grant', title: 'Новые материалы', funder: 'РНФ', number: '24-12-00177', role: 'lead', year_from: 2024, year_to: 2027 }
export const patent: Item = { id: 't1', kind: 'patent', title: 'Способ получения катализатора', authors: 'Орлова Е. А.', number: 'RU 2 745 123', office: 'Роспатент', patent_type: 'invention', year: 2022 }
export const teaching: Item = { id: 'c1', kind: 'teaching', course: 'Физическая химия', institution: 'НГУ', level: 'master', year_from: 2019, year_to: 2022, description: 'Лекции и семинары' }

export const profile: Profile = {
  id: PROFILE_ID,
  name: 'Елена Орлова',
  visibility: 'public',
  open_to_offers: true,
  headline: 'Старший научный сотрудник, лаборатория катализа',
  city: 'Новосибирск',
  region: { code: '54', name: 'Новосибирская область' },
  about: 'Изучаю активные центры катализаторов.',
  degree: { level: 'candidate', specialty: { code: '1.4.4', name: 'Физическая химия' }, year: 2016, institution: 'Институт катализа', dissertation: 'Активные центры оксидных катализаторов' },
  academic_title: 'docent',
  academic_title_year: 2021,
  identifiers: { orcid: '0000-0002-1825-0097', spin: '12345678', scopus_id: '57190123456', wos_id: 'A-1234-2008' },
  h_index: { rsci: 12, scopus: 9, wos: null, scholar: 15 },
  specialties: [{ code: '1.4.4', name: 'Физическая химия' }],
  skills: { research: ['ИК-спектроскопия', 'Рентгеновская дифракция'], general: ['Английский B2'] },
  contact_email: 'orlova@example.ru',
  sections: { education: [education], experience: [experience], publications: [publication, book], grants: [grant], patents: [patent], teaching: [teaching] },
  updated_at: '2026-10-03T12:00:00Z',
}

export const emptyProfile: Profile = {
  id: PROFILE_ID,
  name: 'Анна Смирнова',
  visibility: 'hidden',
  open_to_offers: false,
  headline: '',
  city: '',
  region: null,
  about: '',
  degree: { level: 'none', specialty: null, year: null, institution: '', dissertation: '' },
  academic_title: 'none',
  academic_title_year: null,
  identifiers: { orcid: '', spin: '', scopus_id: '', wos_id: '' },
  h_index: { rsci: null, scopus: null, wos: null, scholar: null },
  specialties: [],
  sections: { education: [], experience: [], publications: [], grants: [], patents: [], teaching: [] },
  updated_at: '2026-10-03T12:00:00Z',
}

export const ownPage = (p: Profile = profile): ProfilePage => ({ profile: p, viewer: { is_owner: true, can_see_contacts: true } })
export const otherPage = (p: Profile = profile, contacts = false): ProfilePage => {
  const { visibility: _v, ...rest } = p
  void _v
  return { profile: contacts ? rest : { ...rest, contact_email: undefined }, viewer: { is_owner: false, can_see_contacts: contacts } }
}

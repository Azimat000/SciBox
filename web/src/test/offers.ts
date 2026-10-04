import type { CatalogCard, CatalogResult } from '../features/catalog/api'
import type { MineList, Offer, SentList, Target } from '../features/offers/api'
import { PROFILE_ID } from './profile'
import { VACANCY_ID } from './vacancies'

export const OFFER_ID = 'oooooooo-oooo-4ooo-8ooo-oooooooooooo'

export const catalogCard: CatalogCard = {
  id: PROFILE_ID,
  name: 'Елена Орлова',
  headline: 'Старший научный сотрудник, лаборатория катализа',
  city: 'Новосибирск',
  region: 'Новосибирская область',
  degree: 'doctor',
  academic_title: 'professor',
  open_to_offers: true,
  h_index: 15,
  publications: 12,
  q12_total: 5,
  q12_recent: 3,
  specialties: [
    { code: '1.4.4', name: 'Физическая химия' },
    { code: '1.4.3', name: 'Органическая химия' },
  ],
  updated_at: '2026-10-03T12:00:00Z',
}

export const bareCard: CatalogCard = {
  id: '88888888-8888-4888-8888-888888888888',
  name: 'Борис Лапин',
  headline: 'Аспирант',
  city: '',
  region: '',
  degree: 'none',
  academic_title: 'none',
  open_to_offers: false,
  h_index: null,
  publications: 0,
  q12_total: 0,
  q12_recent: 0,
  specialties: [],
  updated_at: '2026-10-01T12:00:00Z',
}

export const catalogResult = (items: CatalogCard[] = [catalogCard, bareCard], extra: Partial<CatalogResult> = {}): CatalogResult => ({ items, total: items.length, fuzzy: false, recent_from: 2022, ...extra })

const vacancy = { id: VACANCY_ID, title: 'Старший научный сотрудник: сверхпроводящие плёнки', status: 'published', org_name: 'Сибирский институт', org_slug: 'sibirskiy-institut', unit_name: 'Лаборатория катализа', city: 'Новосибирск', deadline: '2099-12-31' }

/** Приглашение глазами учёного: ждёт ответа. */
export const offer: Offer = {
  id: OFFER_ID,
  status: 'pending',
  message: 'Ваши работы по катализаторам близки нашему проекту.\nБудем рады обсудить сотрудничество.',
  answer_note: '',
  answered_at: null,
  created_at: '2026-10-03T09:00:00Z',
  vacancy,
  application_id: null,
  can_answer: true,
  can_cancel: false,
}

export const interested: Offer = { ...offer, status: 'interested', answer_note: 'Откликнусь на этой неделе', answered_at: '2026-10-04T10:00:00Z', can_answer: false }
export const declined: Offer = { ...offer, id: 'oooooooo-oooo-4ooo-8ooo-000000000002', status: 'declined', answer_note: '', answered_at: '2026-10-04T10:00:00Z', can_answer: false }

export const mineList = (items: Offer[] = [offer], extra: Partial<MineList> = {}): MineList => ({
  items,
  total: items.length,
  counts: { pending: items.filter((o) => o.status === 'pending').length, interested: items.filter((o) => o.status === 'interested').length, declined: items.filter((o) => o.status === 'declined').length },
  pending: items.filter((o) => o.status === 'pending').length,
  ...extra,
})

/** Приглашение глазами организации. */
export const sentOffer: Offer = {
  ...offer,
  scientist: { profile_id: PROFILE_ID, name: 'Елена Орлова' },
  application_id: null,
  can_answer: false,
  can_cancel: true,
}
export const sentAnswered: Offer = { ...sentOffer, id: 'oooooooo-oooo-4ooo-8ooo-000000000003', status: 'interested', answer_note: 'Спасибо, откликнусь', answered_at: '2026-10-04T10:00:00Z', can_cancel: false }
export const sentCancelled: Offer = { ...sentOffer, id: 'oooooooo-oooo-4ooo-8ooo-000000000004', status: 'cancelled', message: '', can_cancel: false }

export const sentList = (items: Offer[] = [sentOffer], extra: Partial<SentList> = {}): SentList => {
  const count = (s: string) => items.filter((o) => o.status === s).length
  return { items, total: items.length, counts: { pending: count('pending'), interested: count('interested'), declined: count('declined'), cancelled: count('cancelled') }, ...extra }
}

export const target: Target = {
  id: VACANCY_ID,
  title: 'Старший научный сотрудник: сверхпроводящие плёнки',
  org_name: 'Сибирский институт',
  org_slug: 'sibirskiy-institut',
  unit_name: 'Лаборатория катализа',
  deadline: '2099-12-31',
  offered: false,
  applied: false,
}
export const secondTarget: Target = { ...target, id: 'v2222222-2222-4222-8222-222222222222', title: 'Постдок: геномика', unit_name: '' }

import type { Calendar, DeadlineItem, FavoriteItem, FavoriteList, MatchItem, MatchList, SavedSearch } from '../features/matching/api'
import { card } from './vacancies'

export const cardTwo = { ...card, id: 'b0000000-0000-4000-8000-000000000002', title: 'Постдок: геномика растений', deadline: '' }
export const cardThree = { ...card, id: 'c0000000-0000-4000-8000-000000000003', title: 'Доцент кафедры математики', deadline: '2099-11-20' }

export const favoriteItem = (over: Partial<FavoriteItem> = {}): FavoriteItem => ({
  vacancy: card,
  added_at: '2026-10-03T09:00:00Z',
  state: 'open',
  application_id: null,
  ...over,
})

export const favoriteList = (items: FavoriteItem[] = [favoriteItem()], extra: Partial<FavoriteList> = {}): FavoriteList => ({
  items,
  total: items.length,
  ...extra,
})

export const matchItem = (over: Partial<MatchItem> = {}): MatchItem => ({
  vacancy: card,
  score: 85,
  reasons: ['specialty', 'level', 'degree', 'region'],
  ...over,
})

export const matchList = (items: MatchItem[] = [matchItem()], extra: Partial<MatchList> = {}): MatchList => ({
  items,
  total: items.length,
  ready: true,
  basis: { specialties: 2, level: 2, has_region: true, has_degree: true },
  ...extra,
})

export const deadlineItem = (over: Partial<DeadlineItem> = {}): DeadlineItem => ({
  vacancy: { ...card, deadline: '2026-10-07' },
  days_left: 4,
  application_id: null,
  ...over,
})

export const calendar = (items: DeadlineItem[] = [deadlineItem()], extra: Partial<Calendar> = {}): Calendar => ({
  items,
  without_deadline: 0,
  ...extra,
})

export const SEARCH_ID = 's0000000-0000-4000-8000-000000000001'

export const savedSearch = (over: Partial<SavedSearch> = {}): SavedSearch => ({
  id: SEARCH_ID,
  name: 'Химия в Новосибирске',
  query: 'field=1.4&region=54',
  frequency: 'daily',
  created_at: '2026-10-01T09:00:00Z',
  last_sent_at: null,
  ...over,
})

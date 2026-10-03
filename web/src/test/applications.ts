import type { AppList, Detail, Reference, StaffReference, Summary, ApplyState } from '../features/applications/api'
import type { Notice, NoticeList } from '../features/notifications/api'
import { profile } from './profile'
import { VACANCY_ID } from './vacancies'

export const APP_ID = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
export const REF_ID = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'

export const cvFile = { id: 'f0000000-0000-4000-8000-000000000001', name: 'Анна Смирнова — CV.pdf', size: 29_000 }
export const attachment = { id: 'f0000000-0000-4000-8000-000000000002', name: 'Список публикаций.pdf', size: 16_500 }
export const letterFile = { id: 'f0000000-0000-4000-8000-000000000003', name: 'Письмо Иванова.pdf', size: 2_400_000 }

const base = { created_at: '2026-10-03T09:00:00Z', expires_at: '2026-11-02T09:00:00Z', last_sent_at: '2026-10-03T09:00:00Z', answered_at: null, can_resend: false, resend_at: '2026-10-04T09:00:00Z' }

export const pendingRef: Reference = { id: REF_ID, name: 'Мария Миронова', email: 'mironova@example.ru', relation: 'коллега по лаборатории', status: 'pending', ...base }
export const receivedRef: Reference = {
  id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', name: 'Андрей Козлов', email: 'kozlov@example.ru', relation: 'научный руководитель', status: 'received', ...base,
  answered_at: '2026-10-03T15:30:00Z', resend_at: null,
}
export const declinedRef: Reference = {
  id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', name: 'Павел Ермаков', email: 'ermakov@example.ru', relation: '', status: 'declined', ...base,
  answered_at: '2026-10-03T16:00:00Z', resend_at: null,
}
export const resendableRef: Reference = { ...pendingRef, id: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', name: 'Лидия Орехова', email: 'orehova@example.ru', relation: '', can_resend: true, resend_at: null }

export const application: Detail = {
  id: APP_ID,
  status: 'sent',
  created_at: '2026-10-03T09:00:00Z',
  status_changed_at: '2026-10-03T09:00:00Z',
  vacancy: { id: VACANCY_ID, title: 'Старший научный сотрудник: сверхпроводящие плёнки', status: 'published', org_name: 'Сибирский институт', org_slug: 'sibirskiy-institut', deadline: '2099-12-31' },
  applicant_name: 'Анна Смирнова',
  contact_email: 'anna.apply@example.ru',
  cover_letter: 'Здравствуйте!\nЗанимаюсь эпитаксией плёнок и хотела бы работать в вашей лаборатории.',
  profile: { ...profile, name: 'Анна Смирнова', visibility: undefined, contact_email: 'anna.apply@example.ru' },
  cv: cvFile,
  files: [attachment],
  viewer: { role: 'applicant', can_withdraw: true },
  references: [pendingRef, receivedRef, declinedRef],
}

export const staffReceived: StaffReference = { ...receivedRef, letter: { text: 'Знаю Анну пять лет.\nОтличный экспериментатор.', file: letterFile } }
export const staffPending: StaffReference = { ...pendingRef, letter: null }
export const staffDeclined: StaffReference = { ...declinedRef, letter: null }
export const staffTextOnly: StaffReference = { ...receivedRef, id: 'ffffffff-ffff-4fff-8fff-ffffffffffff', name: 'Борис Лапин', letter: { text: 'Только текст', file: null } }
export const staffFileOnly: StaffReference = { ...receivedRef, id: '99999999-0000-4999-8999-000000000009', name: 'Вера Новикова', letter: { text: '', file: letterFile } }

export const staffApplication: Detail = {
  ...application,
  viewer: { role: 'staff', can_withdraw: false },
  references: [staffReceived, staffPending, staffDeclined],
}

export const summary: Summary = {
  id: APP_ID,
  status: 'sent',
  created_at: '2026-10-03T09:00:00Z',
  status_changed_at: '2026-10-03T09:00:00Z',
  vacancy: application.vacancy,
  references: { total: 3, received: 1 },
}

export const list = (items: Summary[], total = items.length): AppList => ({ items, total })

export const canApply: ApplyState = { can_apply: true, application: null }
export const stateRoute = (s: ApplyState, id = VACANCY_ID) => ({ [`GET /api/applications/for-vacancy/${id}`]: { status: 200, body: s } })

export const notice = (over: Partial<Notice> = {}): Notice => ({
  id: 'n0000000-0000-4000-8000-000000000001', kind: 'application_received', title: 'Новый отклик', body: 'Отклик от Анны Смирновой на вакансию «Старший научный сотрудник».',
  link: `/candidates/${APP_ID}`, created_at: '2026-10-03T09:00:00Z', read: false, ...over,
})

export const noticeList = (items: Notice[], unread = items.filter((n) => !n.read).length, total = items.length): NoticeList => ({ items, total, unread })

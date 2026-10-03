import type { AppList, ApplyState, Candidate, CandidateList, Detail, Invitation, Reference, StaffReference, Summary, VacancyCount } from '../features/applications/api'
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
  decision_note: '',
  invitations: [],
  viewer: { role: 'applicant', can_withdraw: true, decisions: [], can_invite: false },
  references: [pendingRef, receivedRef, declinedRef],
}

export const staffReceived: StaffReference = { ...receivedRef, letter: { text: 'Знаю Анну пять лет.\nОтличный экспериментатор.', file: letterFile } }
export const staffPending: StaffReference = { ...pendingRef, letter: null }
export const staffDeclined: StaffReference = { ...declinedRef, letter: null }
export const staffTextOnly: StaffReference = { ...receivedRef, id: 'ffffffff-ffff-4fff-8fff-ffffffffffff', name: 'Борис Лапин', letter: { text: 'Только текст', file: null } }
export const staffFileOnly: StaffReference = { ...receivedRef, id: '99999999-0000-4999-8999-000000000009', name: 'Вера Новикова', letter: { text: '', file: letterFile } }

export const staffApplication: Detail = {
  ...application,
  viewer: { role: 'staff', can_withdraw: false, decisions: ['accepted', 'rejected'], can_invite: true },
  references: [staffReceived, staffPending, staffDeclined],
}

export const summary: Summary = {
  id: APP_ID,
  status: 'sent',
  created_at: '2026-10-03T09:00:00Z',
  status_changed_at: '2026-10-03T09:00:00Z',
  vacancy: application.vacancy,
  references: { total: 3, received: 1 },
  pending_invitations: 0,
}

export const list = (items: Summary[], total = items.length): AppList => ({ items, total })

export const canApply: ApplyState = { can_apply: true, application: null }
export const stateRoute = (s: ApplyState, id = VACANCY_ID) => ({ [`GET /api/applications/for-vacancy/${id}`]: { status: 200, body: s } })

export const notice = (over: Partial<Notice> = {}): Notice => ({
  id: 'n0000000-0000-4000-8000-000000000001', kind: 'application_received', title: 'Новый отклик', body: 'Отклик от Анны Смирновой на вакансию «Старший научный сотрудник».',
  link: `/candidates/${APP_ID}`, created_at: '2026-10-03T09:00:00Z', read: false, ...over,
})

export const noticeList = (items: Notice[], unread = items.filter((n) => !n.read).length, total = items.length): NoticeList => ({ items, total, unread })

// ---- приглашения и список откликов организации (срез 9) ----

export const INV_ID = 'a1000000-0000-4000-8000-000000000001'

const inv = (over: Partial<Invitation>): Invitation => ({
  id: INV_ID, kind: 'interview', status: 'pending', message: '', starts_at: '2026-10-15T11:00:00Z', place_kind: 'online', place: 'https://meet.example.org/room-1',
  contact_name: '', contact_email: '', contact_phone: '', answer: null, created_at: '2026-10-03T10:00:00Z', can_answer: false, can_cancel: false, can_accept_proposal: false, ...over,
})

/** Собеседование, которое ждёт ответа: соискатель может ответить, организация отменить. */
export const interviewForApplicant = inv({ message: 'Расскажем о проекте', can_answer: true })
export const interviewForStaff = inv({ message: 'Расскажем о проекте', can_cancel: true })
export const interviewOnsite = inv({ id: 'a1000000-0000-4000-8000-000000000002', place_kind: 'onsite', place: 'Новосибирск, пр. Лаврентьева, 5', can_cancel: true })
export const interviewBadLink = inv({ id: 'a1000000-0000-4000-8000-000000000003', place: 'javascript:alert(1)' })
export const interviewConfirmed = inv({
  status: 'confirmed', can_cancel: true, answer: { proposed_at: null, note: 'Буду вовремя', contact: '', time: '', answered_at: '2026-10-04T08:00:00Z' },
})
export const interviewProposedByApplicant = inv({
  status: 'proposed', answer: { proposed_at: '2026-10-17T12:00:00Z', note: 'Занят в этот день', contact: '', time: '', answered_at: '2026-10-04T08:00:00Z' },
})
export const interviewProposedForStaff = { ...interviewProposedByApplicant, can_cancel: true, can_accept_proposal: true }
export const interviewAcceptedProposal = inv({
  status: 'confirmed', can_cancel: true, starts_at: '2026-10-17T12:00:00Z', answer: { proposed_at: '2026-10-17T12:00:00Z', note: '', contact: '', time: '', answered_at: '2026-10-04T08:00:00Z' },
})
export const interviewCancelled = inv({ id: 'a1000000-0000-4000-8000-000000000004', status: 'cancelled' })
export const contactsShared = inv({
  id: 'a1000000-0000-4000-8000-000000000005', kind: 'contacts', status: 'shared', starts_at: null, place_kind: '', place: '',
  contact_name: 'Ольга Кузнецова', contact_email: 'hr@example.ru', contact_phone: '+7 913 000-00-00', message: 'Пишите в любое время',
})
export const contactsPhoneOnly = inv({
  id: 'a1000000-0000-4000-8000-000000000006', kind: 'contacts', status: 'shared', starts_at: null, place_kind: '', place: '', contact_phone: '+7 913 000-00-00',
})
export const requestForApplicant = inv({
  id: 'a1000000-0000-4000-8000-000000000007', kind: 'request_contacts', status: 'pending', starts_at: null, place_kind: '', place: '', message: 'Оставьте телефон', can_answer: true,
})
export const requestAnswered = inv({
  id: 'a1000000-0000-4000-8000-000000000008', kind: 'request_contacts', status: 'answered', starts_at: null, place_kind: '', place: '',
  answer: { proposed_at: null, note: '', contact: '+7 913 555-66-77', time: 'после 15:00', answered_at: '2026-10-04T08:00:00Z' },
})
export const answeredEmpty = inv({
  id: 'a1000000-0000-4000-8000-000000000009', kind: 'request_contacts', status: 'answered', starts_at: null, place_kind: '', place: '',
  answer: { proposed_at: null, note: '', contact: '', time: '', answered_at: '2026-10-04T08:00:00Z' },
})

/** Отклик организации в статусе «просмотрен»: можно приглашать, принимать и отказывать. */
export const reviewedApplication: Detail = { ...staffApplication, status: 'viewed', status_changed_at: '2026-10-03T12:00:00Z' }

export const candidate: Candidate = {
  id: APP_ID, status: 'sent', created_at: '2026-10-03T09:00:00Z', status_changed_at: '2026-10-03T09:00:00Z', applicant_name: 'Анна Смирнова',
  headline: 'Научный сотрудник, лаборатория катализа', vacancy: application.vacancy, unit_name: 'Лаборатория катализа', references: { total: 3, received: 1 },
  pending_invitations: 0, proposed_invitations: 0,
}

export const counts = (over: Partial<CandidateList['counts']> = {}): CandidateList['counts'] => ({ sent: 0, viewed: 0, invited: 0, rejected: 0, accepted: 0, withdrawn: 0, ...over })

export const candidateList = (items: Candidate[], over: Partial<CandidateList> = {}): CandidateList => ({
  items, total: items.length, counts: counts({ sent: items.length }), ...over,
})

export const vacancyCount: VacancyCount = { id: VACANCY_ID, title: application.vacancy.title, status: 'published', org_name: 'Сибирский институт', org_slug: 'sibirskiy-institut', total: 3, new: 2 }

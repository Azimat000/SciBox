import type { Member, MyOrganization, OrgSummary, OrgView, PendingInvitation, Unit, Viewer } from '../features/orgs/api'
import { reply, type Route } from './api'

/** Организация «Сибирский институт» с двумя подразделениями: у первого есть руководитель Анна. */
export const SLUG = 'sibirskiy-institut'

export const lab: Unit = {
  id: '22222222-2222-4222-8222-222222222222',
  name: 'Лаборатория сверхпроводников',
  kind: 'laboratory',
  description: 'Синтез и измерение плёнок.',
  topics: ['Сверхпроводимость', 'Тонкие плёнки'],
  head_name: 'Анна Смирнова',
  head_user_id: '55555555-5555-4555-8555-555555555555',
}

export const dept: Unit = {
  id: '33333333-3333-4333-8333-333333333333',
  name: 'Кафедра физики',
  kind: 'department',
  description: '',
  topics: [],
  head_name: null,
  head_user_id: null,
}

export const org = {
  id: '44444444-4444-4444-8444-444444444444',
  slug: SLUG,
  name: 'Сибирский институт',
  kind: 'institute' as const,
  city: 'Новосибирск',
  website: 'https://sikm.example.ru',
  description: 'Изучаем квантовые материалы.\nПринимаем аспирантов.',
  created_at: '2026-10-01T10:00:00Z',
}

export const viewers: Record<'owner' | 'hr' | 'head' | 'stranger', Viewer> = {
  owner: { role: 'owner', can_edit_organization: true, can_manage_members: true, can_manage_units: true, editable_units: [lab.id, dept.id] },
  hr: { role: 'hr', can_edit_organization: false, can_manage_members: false, can_manage_units: false, editable_units: [] },
  head: { role: 'unit_head', can_edit_organization: false, can_manage_members: false, can_manage_units: false, editable_units: [lab.id] },
  stranger: { role: '', can_edit_organization: false, can_manage_members: false, can_manage_units: false, editable_units: [] },
}

export const orgView = (viewer: Viewer | null, units: Unit[] = [lab, dept]): OrgView => ({ organization: org, units, viewer })

/** Ответы для страницы организации (и страниц управления) от лица человека с таким видом. */
export const orgRoutes = (viewer: Viewer | null, units?: Unit[]): Record<string, Route> => ({
  [`GET /api/organizations/${SLUG}`]: reply(200, orgView(viewer, units)),
})

export const members: Member[] = [
  { user_id: '55555555-5555-4555-8555-555555555555', name: 'Анна Смирнова', email: 'anna@example.ru', role: 'owner', joined_at: '2026-10-01T10:00:00Z' },
  { user_id: '66666666-6666-4666-8666-666666666666', name: 'Борис Кадров', email: 'boris@example.ru', role: 'hr', joined_at: '2026-10-02T10:00:00Z' },
]

export const pending: PendingInvitation = {
  id: '77777777-7777-4777-8777-777777777777',
  email: 'new@example.ru',
  role: 'unit_head',
  unit_id: lab.id,
  unit_name: lab.name,
  created_at: '2026-10-02T10:00:00Z',
  expires_at: '2026-10-09T10:00:00Z',
}

export const membersRoute = (list: Member[] = members, invitations: PendingInvitation[] = [pending]): Record<string, Route> => ({
  [`GET /api/organizations/${SLUG}/members`]: reply(200, { members: list, invitations }),
})

export const summary = (n: number, over: Partial<OrgSummary> = {}): OrgSummary => ({
  id: `00000000-0000-4000-8000-0000000000${String(n).padStart(2, '0')}`,
  slug: `org-${n}`,
  name: `Организация ${n}`,
  kind: 'university',
  city: 'Казань',
  summary: 'Коротко о нас.',
  unit_count: 2,
  ...over,
})

export const mine = (organizations: MyOrganization[] = [], invitations: unknown[] = []) => reply(200, { organizations, invitations })

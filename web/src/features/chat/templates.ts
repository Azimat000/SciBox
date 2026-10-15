import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../api/client'
import { useMe } from '../auth/api'
import { keys } from './api'

// Шаблоны ответов организации (срез 36, contracts/chat/templates-*.json): общие на организацию, до 50 штук.

export type Template = { id: string; title: string; text: string; updated_at: string }
export type TemplateList = { items: Template[]; can_edit: boolean; limit: number }
export type TemplateInput = { title: string; text: string }

export const TEMPLATE_TITLE_MAX = 120
export const TEMPLATE_DEFAULT_LIMIT = 50

const base = (orgId: string) => `/api/orgs/${encodeURIComponent(orgId)}/chat-templates`

/** Шаблоны организации. Меню у поля ввода просит их только после открытия, страница правки сразу. */
export function useTemplates(orgId: string | null | undefined, enabled = true) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.templates(user?.id, orgId ?? ''),
    queryFn: ({ signal }) => apiGet<TemplateList>(base(orgId!), { signal }),
    enabled: enabled && !!orgId && user !== undefined && user !== null,
    retry: false,
  })
}

/** Создание, правка и удаление: после любого шаблоны перечитываются, меню и страница видят одно и то же. */
export function useTemplateActions(orgId: string) {
  const { user } = useMe()
  const client = useQueryClient()
  const done = () => client.invalidateQueries({ queryKey: keys.templates(user?.id, orgId) })
  const create = useMutation({
    mutationFn: (input: TemplateInput) => apiSend<{ template: Template }>('POST', base(orgId), input),
    onSuccess: done,
  })
  const update = useMutation({
    mutationFn: ({ id, ...input }: TemplateInput & { id: string }) => apiSend<{ template: Template }>('PATCH', `${base(orgId)}/${encodeURIComponent(id)}`, input),
    onSuccess: done,
  })
  const remove = useMutation({
    mutationFn: (id: string) => apiSend('DELETE', `${base(orgId)}/${encodeURIComponent(id)}`),
    onSuccess: done,
  })
  return { create, update, remove }
}

/** Поиск по названию и тексту без учёта регистра и «ё». */
export function matchTemplate(tpl: Template, query: string): boolean {
  const norm = (s: string) => s.toLowerCase().replaceAll('ё', 'е')
  const q = norm(query.trim())
  if (!q) return true
  return norm(tpl.title).includes(q) || norm(tpl.text).includes(q)
}

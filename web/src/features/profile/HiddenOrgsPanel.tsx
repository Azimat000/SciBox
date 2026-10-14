import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { apiGet } from '../../api/client'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import type { OrgList } from '../orgs/api'
import { refreshProfile, saveHiddenOrgs, type HiddenOrg } from './api'

const MIN_QUERY = 2
const DEBOUNCE_MS = 300
const MAX_ORGS = 30
const h = t.profile.hiddenOrgs

/**
 * «Скрыть от организаций»: список организаций, чьи сотрудники не видят профиль ни в каком режиме (D-200). Организация
 * находится поиском на сервере (их тысячи). Каждое изменение сохраняется сразу, как переключатель режима.
 */
export function HiddenOrgsPanel({ hidden }: { hidden: readonly HiddenOrg[] }) {
  const client = useQueryClient()
  const toast = useToast()
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(query.trim()), DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [query])

  const ready = debounced.length >= MIN_QUERY
  const found = useQuery({
    queryKey: ['organizations', 'hide-search', debounced],
    queryFn: ({ signal }) => apiGet<OrgList>(`/api/organizations?q=${encodeURIComponent(debounced)}&limit=8`, { signal }),
    enabled: ready,
    staleTime: 60_000,
    retry: false,
  })

  const save = useMutation({
    mutationFn: (ids: string[]) => saveHiddenOrgs(ids),
    onSuccess: async () => {
      await refreshProfile(client)
      toast.show({ kind: 'success', title: h.saved })
    },
    onError: (err) => toast.show({ kind: 'error', title: h.saveFailed, text: describeError(err) }),
  })

  const hiddenIds = hidden.map((o) => o.id)
  const full = hiddenIds.length >= MAX_ORGS
  const items = (found.data?.items ?? []).filter((o) => !hiddenIds.includes(o.id))
  let status = ''
  if (query.trim() !== '' && query.trim().length < MIN_QUERY) status = h.tooShort
  else if (ready && found.isFetching) status = h.searching
  else if (ready && found.isError) status = h.error
  else if (ready && found.isSuccess && (found.data?.items.length ?? 0) === 0) status = h.nothing
  else if (full) status = h.limit(MAX_ORGS)

  return (
    <section className="hidden-orgs" aria-labelledby="hidden-orgs-title">
      <h2 id="hidden-orgs-title">{h.title}</h2>
      <p className="hidden-orgs-text">{h.text}</p>

      {hidden.length === 0 ? (
        <p className="field-hint">{h.none}</p>
      ) : (
        <ul className="hidden-orgs-list" aria-label={h.list}>
          {hidden.map((o) => (
            <li key={o.id} className="hidden-org">
              <span className="hidden-org-name">
                {o.name}
                {o.city && <span className="hidden-org-city">{o.city}</span>}
              </span>
              <Button
                variant="quiet"
                size="sm"
                disabled={save.isPending}
                aria-label={h.showAria(o.name)}
                onClick={() => save.mutate(hiddenIds.filter((id) => id !== o.id))}
              >
                {h.show}
              </Button>
            </li>
          ))}
        </ul>
      )}

      <TextField
        label={h.search}
        hint={h.searchHint}
        type="search"
        name="hidden-org-search"
        autoComplete="off"
        value={query}
        disabled={full}
        onChange={(e) => setQuery(e.target.value)}
      />
      <p className="hidden-orgs-status" role="status">
        {status}
      </p>
      {ready && items.length > 0 && !full && (
        <ul className="hidden-orgs-results" aria-label={h.results} aria-busy={found.isFetching}>
          {items.map((o) => (
            <li key={o.id} className="hidden-org">
              <span className="hidden-org-name">
                {o.name}
                {o.city && <span className="hidden-org-city">{o.city}</span>}
              </span>
              <Button
                variant="secondary"
                size="sm"
                disabled={save.isPending}
                aria-label={h.hideAria(o.name)}
                onClick={() => {
                  save.mutate([...hiddenIds, o.id])
                  setQuery('')
                }}
              >
                {h.hide}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

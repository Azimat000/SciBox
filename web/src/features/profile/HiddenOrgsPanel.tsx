import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { LoadFailed } from '../orgs/states'
import { hiddenOrgsKey, hideFromOrg, MIN_ORG_QUERY, unhideFromOrg, useFindOrganizations, useHiddenOrgs, type HiddenOrg } from './api'

const hp = t.profile.privacy.hiddenOrgs
const DEBOUNCE_MS = 300

/**
 * «Скрыть от организаций»: профиль не виден сотрудникам выбранных организаций ни в каком режиме (D-200). Ищем организацию
 * по названию на сервере, выбранные показываем списком с кнопкой «Вернуть».
 */
export function HiddenOrgsPanel() {
  const list = useHiddenOrgs()
  const client = useQueryClient()
  const toast = useToast()
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(query.trim()), DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [query])
  const ready = debounced.length >= MIN_ORG_QUERY
  const found = useFindOrganizations(debounced)

  const change = useMutation({
    mutationFn: (v: { hide: boolean; slug: string; name: string }) => (v.hide ? hideFromOrg(v.slug) : unhideFromOrg(v.slug)),
    onSuccess: (items, v) => {
      client.setQueriesData({ queryKey: hiddenOrgsKey }, items.items)
      toast.show({ kind: 'success', title: v.hide ? hp.hidden(v.name) : hp.unhidden(v.name) })
    },
    onError: (err) => toast.show({ kind: 'error', title: hp.saveFailed, text: describeError(err) }),
  })

  if (list.isError) return <LoadFailed title={hp.loadError} error={list.error} onRetry={() => void list.refetch()} />
  const hidden: HiddenOrg[] = list.data ?? []
  const hiddenSlugs = new Set(hidden.map((h) => h.slug))
  const items = found.data ?? []
  let status = ''
  if (ready && found.isFetching) status = hp.searching
  else if (ready && found.isError) status = hp.searchFailed
  else if (ready && found.isSuccess && items.length === 0) status = hp.noResults

  return (
    <section className="privacy-panel hidden-orgs" aria-labelledby="hidden-orgs-title">
      <h2 id="hidden-orgs-title">{hp.title}</h2>
      <p className="hidden-orgs-lead">{hp.lead}</p>
      <TextField
        label={hp.search}
        hint={hp.searchHint}
        type="search"
        name="hidden-org-search"
        autoComplete="off"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.preventDefault()
        }}
      />
      <p className="journal-status" role="status">
        {status}
      </p>
      {ready && items.length > 0 && (
        <ul className="hidden-orgs-list" aria-label={hp.search} aria-busy={found.isFetching}>
          {items.map((o) => (
            <li key={o.slug} className="hidden-orgs-row">
              <p className="hidden-orgs-name">
                {o.name}
                <span className="hidden-orgs-city">{o.city}</span>
              </p>
              {hiddenSlugs.has(o.slug) ? (
                <span className="hidden-orgs-done">{hp.alreadyHidden}</span>
              ) : (
                <Button
                  variant="secondary"
                  size="sm"
                  aria-label={hp.hideAria(o.name)}
                  disabled={change.isPending}
                  onClick={() => change.mutate({ hide: true, slug: o.slug, name: o.name })}
                >
                  {hp.hide}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <h3 className="hidden-orgs-subtitle">{hp.listTitle}</h3>
      {list.isPending ? (
        <p className="profile-note" role="status">
          {t.common.loading}
        </p>
      ) : hidden.length === 0 ? (
        <p className="profile-note">{hp.empty}</p>
      ) : (
        <ul className="hidden-orgs-list">
          {hidden.map((o) => (
            <li key={o.slug} className="hidden-orgs-row">
              <p className="hidden-orgs-name">
                {o.name}
                <span className="hidden-orgs-city">{o.city}</span>
              </p>
              <Button variant="quiet" size="sm" aria-label={hp.unhideAria(o.name)} disabled={change.isPending} onClick={() => change.mutate({ hide: false, slug: o.slug, name: o.name })}>
                {hp.unhide}
              </Button>
            </li>
          ))}
        </ul>
      )}
      <p className="field-hint">{hp.publicNote}</p>
    </section>
  )
}

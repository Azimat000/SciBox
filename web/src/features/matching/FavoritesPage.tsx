import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { BookmarkIcon } from '../../ui/icons'
import { Tag } from '../../ui/Tag'
import { useToast } from '../../ui/useToast'
import { dateText } from '../applications/labels'
import { describeError } from '../auth/errors'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { VacancyCard } from '../vacancies/VacancyList'
import { addFavorite, PAGE_SIZE, removeFavorite, useFavorites, type FavoriteItem } from './api'
import { Pager } from './Pager'
import { ShelfTabs } from './ShelfTabs'
import '../../ui/ResultsHeader.css'
import './matching.css'

/** «Избранное»: вакансии, к которым человек хочет вернуться. Убранная вакансия остаётся на странице с кнопкой «Вернуть». */
export function FavoritesPage() {
  return <RequireUser>{() => <Favorites />}</RequireUser>
}

function Favorites() {
  const m = t.matching.favorites
  const [params, setParams] = useSearchParams()
  const page = Math.max(1, Number(params.get('page')) || 1)
  const query = useFavorites(page)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={m.loadError} error={query.error} onRetry={() => void query.refetch()} />

  const { items, total } = query.data
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const go = (n: number) => {
    const next = new URLSearchParams(params)
    if (n > 1) next.set('page', String(n))
    else next.delete('page')
    setParams(next)
  }

  return (
    <div className="page shelf-page">
      <h1>{m.title}</h1>
      <p className="lead">{m.lead}</p>
      <ShelfTabs current="favorites" />
      {total === 0 ? (
        <EmptyState
          headingLevel={2}
          title={m.emptyTitle}
          text={m.emptyText}
          action={<ButtonLink to="/vacancies">{m.emptyAction}</ButtonLink>}
        />
      ) : items.length === 0 ? (
        <EmptyState
          headingLevel={2}
          title={m.pageGoneTitle(page)}
          text={m.pageGoneText}
          action={<Button onClick={() => go(1)}>{m.toFirstPage}</Button>}
        />
      ) : (
        <>
          <p className="results-header num">{m.count(total, plural(total, m.countForms))}</p>
          <ul className="vacancy-list" aria-busy={query.isPlaceholderData}>
            {items.map((item) => (
              <li key={item.vacancy.id}>
                <Row item={item} />
              </li>
            ))}
          </ul>
          <Pager page={page} pages={pages} onPage={go} />
        </>
      )}
    </div>
  )
}

/** Одна вакансия в избранном. После «Убрать» строка не исчезает: человек может передумать. */
function Row({ item }: { item: FavoriteItem }) {
  const m = t.matching.favorites
  const client = useQueryClient()
  const toast = useToast()
  const [removed, setRemoved] = useState(false)
  const card = item.vacancy

  const change = useMutation({
    mutationFn: (keep: boolean) => (keep ? addFavorite(card.id) : removeFavorite(card.id)),
    onSuccess: async (_, keep) => {
      setRemoved(!keep)
      // Список не перечитываем сразу: строка с «Вернуть» должна остаться на месте до следующего открытия страницы.
      await Promise.all([
        client.invalidateQueries({ queryKey: ['matching', 'ids'] }),
        client.invalidateQueries({ queryKey: ['matching', 'calendar'] }),
        client.invalidateQueries({ queryKey: ['matching', 'favorites'], refetchType: 'none' }),
      ])
    },
    onError: (err) => toast.show({ kind: 'error', title: t.matching.favorite.failed, text: describeError(err) }),
  })

  if (removed) {
    return (
      <div className="fav-removed" role="status">
        <p>{m.removed(card.title)}</p>
        <Button variant="quiet" size="sm" loading={change.isPending} onClick={() => change.mutate(true)}>
          {m.restore}
        </Button>
      </div>
    )
  }

  return (
    <VacancyCard
      card={card}
      headingLevel={2}
      showDeadline={item.state !== 'closed'}
      action={
        <button
          type="button"
          className="fav-btn"
          aria-pressed="true"
          aria-label={t.matching.favorite.removeLabel(card.title)}
          disabled={change.isPending}
          onClick={() => change.mutate(false)}
        >
          <BookmarkIcon size={18} filled />
          <span>{t.matching.favorite.remove}</span>
        </button>
      }
      footer={
        <p className="fav-meta">
          {item.state === 'closed' && <Tag>{m.closed}</Tag>}
          {item.state === 'expired' && <Tag>{m.expired}</Tag>}
          {item.application_id && (
            <span>
              {m.applied}: <Link to={`/applications/${item.application_id}`}>{m.seeApplication}</Link>
            </span>
          )}
          <span>{m.addedAt(dateText(item.added_at))}</span>
        </p>
      }
    />
  )
}

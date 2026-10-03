import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation } from 'react-router'
import { t } from '../../i18n'
import { BookmarkIcon } from '../../ui/icons'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { useMe } from '../auth/api'
import { useRole } from '../shell/useRole'
import { addFavorite, keys, refreshFavorites, removeFavorite, useFavoriteIds } from './api'
import './matching.css'

/**
 * Закладка «В избранное» на вакансии. Только ищущему: у нанимающего избранного нет. Не вошедшего ведёт на вход и потом
 * обратно. Состояние меняется сразу, не дожидаясь сервера; если сервер отказал, возвращается прежнее и сказано почему.
 */
export function FavoriteButton({ vacancyId, title }: { vacancyId: string; title: string }) {
  const { role } = useRole()
  const { user } = useMe()
  const ids = useFavoriteIds()
  const client = useQueryClient()
  const toast = useToast()
  const location = useLocation()
  const favorite = ids.data?.has(vacancyId) ?? false

  const toggle = useMutation({
    mutationFn: (next: boolean) => (next ? addFavorite(vacancyId) : removeFavorite(vacancyId)),
    onMutate: async (next) => {
      const key = keys.ids(user?.id)
      await client.cancelQueries({ queryKey: key })
      const before = client.getQueryData<Set<string>>(key)
      const after = new Set(before)
      if (next) after.add(vacancyId)
      else after.delete(vacancyId)
      client.setQueryData(key, after)
      return { before }
    },
    onError: (err, _next, ctx) => {
      client.setQueryData(keys.ids(user?.id), ctx?.before)
      toast.show({ kind: 'error', title: t.matching.favorite.failed, text: describeError(err) })
    },
    onSettled: () => refreshFavorites(client),
  })

  if (role === 'employer') return null
  const m = t.matching.favorite

  if (user === null) {
    const next = encodeURIComponent(location.pathname + location.search)
    return (
      <Link className="fav-btn" to={`/login?next=${next}`} state={{ notice: 'need-login' }} aria-label={m.signIn(title)}>
        <BookmarkIcon size={18} />
        <span>{m.add}</span>
      </Link>
    )
  }
  return (
    <button
      type="button"
      className="fav-btn"
      aria-pressed={favorite}
      aria-label={favorite ? m.removeLabel(title) : m.addLabel(title)}
      disabled={ids.isPending || toggle.isPending}
      onClick={() => toggle.mutate(!favorite)}
    >
      <BookmarkIcon size={18} filled={favorite} />
      <span>{favorite ? m.remove : m.add}</span>
    </button>
  )
}

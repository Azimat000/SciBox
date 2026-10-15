import { useMutation, useQueryClient } from '@tanstack/react-query'
import { t } from '../../i18n'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { keys, setArchived } from './api'

/** Все переписки и значки перечитываются: архив и блокировка меняют списки, шапку, число непрочитанных и настройки. */
export function useRefreshChats() {
  const client = useQueryClient()
  return () => client.invalidateQueries({ queryKey: keys.all })
}

/** «В архив» / «Вернуть»: общее для строки списка и окна. Результат человек видит в списке; здесь только короткое подтверждение. */
export function useArchiveToggle() {
  const refresh = useRefreshChats()
  const toast = useToast()
  const a = t.chat.archive
  return useMutation({
    mutationFn: ({ id, archived }: { id: string; archived: boolean }) => setArchived(id, archived),
    onSuccess: (_, { archived }) => {
      void refresh()
      toast.show(archived ? { kind: 'success', title: a.done, text: a.doneText } : { kind: 'success', title: a.restored })
    },
    onError: (err) => toast.show({ kind: 'error', title: a.failed, text: describeError(err) }),
  })
}


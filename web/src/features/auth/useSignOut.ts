import { useMutation, useQueryClient } from '@tanstack/react-query'
import { t } from '../../i18n'
import { useToast } from '../../ui/useToast'
import { logout, meQueryKey, setMe } from './api'

/** Выход из аккаунта: сервер завершает сессию, а всё личное, что лежало в памяти страницы, стирается. */
export function useSignOut() {
  const client = useQueryClient()
  const toast = useToast()
  return useMutation({
    mutationFn: logout,
    onSuccess: async () => {
      // Всё личное из памяти стираем, а запрос «кто я?» оставляем: шапка следит именно за ним
      client.removeQueries({ predicate: (query) => query.queryKey[0] !== meQueryKey[0] })
      await setMe(client, null)
      toast.show({ kind: 'info', title: t.shell.signedOutTitle })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.shell.signOutFailed, text: err.message }),
  })
}

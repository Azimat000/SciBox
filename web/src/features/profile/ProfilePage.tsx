import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { ButtonLink } from '../../ui/Button'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { deleteItem, ownCvPath, refreshProfile, useOwnProfile, type Item, type ItemKind } from './api'
import { ItemModal } from './ItemModal'
import { PrivacyPanel } from './PrivacyPanel'
import { ProfileView } from './ProfileView'
import { itemTitle } from './sections'
import './profile.css'

/** «Мой профиль»: профиль глазами других людей с кнопками правки, блок приватности, резюме. */
export function ProfilePage() {
  return <RequireUser>{() => <Own />}</RequireUser>
}

type Editing = { kind: ItemKind; item: Item | null }

function Own() {
  const query = useOwnProfile()
  const client = useQueryClient()
  const toast = useToast()
  const [editing, setEditing] = useState<Editing | null>(null)
  const [removing, setRemoving] = useState<Item | null>(null)
  const remove = useMutation({
    mutationFn: (item: Item) => deleteItem(item.id),
    onSuccess: async () => {
      await refreshProfile(client)
      toast.show({ kind: 'success', title: t.profile.item.removed })
      setRemoving(null)
    },
    onError: (err) => {
      setRemoving(null)
      toast.show({ kind: 'error', title: t.profile.item.removeFailed, text: describeError(err) })
    },
  })

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={t.profile.loadError} error={query.error} onRetry={() => void query.refetch()} />
  const page = query.data
  const p = page.profile

  return (
    <div className="page profile-page">
      <div className="profile-toolbar">
        <ButtonLink to="/profile/edit">{t.profile.editCore}</ButtonLink>
        <a className="btn btn-secondary" href={ownCvPath} download>
          {t.profile.downloadCv}
        </a>
        <ButtonLink to={`/scientists/${p.id}`} variant="quiet">
          {t.profile.viewAsOthers}
        </ButtonLink>
      </div>
      {p.visibility === 'hidden' && <Alert kind="info">{t.profile.hiddenNote}</Alert>}
      <PrivacyPanel visibility={p.visibility ?? 'hidden'} openToOffers={p.open_to_offers} />
      <ProfileView page={page} editable onAdd={(kind) => setEditing({ kind, item: null })} onEdit={(item) => setEditing({ kind: item.kind, item })} onRemove={setRemoving} />
      {editing && <ItemModal kind={editing.kind} item={editing.item} onClose={() => setEditing(null)} />}
      <ConfirmModal
        open={removing !== null}
        title={t.profile.item.removeTitle}
        text={removing ? t.profile.item.removeText(itemTitle(removing)) : ''}
        confirmLabel={t.profile.item.remove}
        pending={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing)}
        onClose={() => setRemoving(null)}
      />
    </div>
  )
}

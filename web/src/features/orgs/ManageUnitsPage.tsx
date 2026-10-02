import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router'
import { t } from '../../i18n'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { deleteUnit, refreshOrganizations, type Unit } from './api'
import { ConfirmModal } from './ConfirmModal'
import { longName, unitKindShort } from './labels'
import { useManageContext } from './manageContext'

/** Список подразделений с действиями по правам: владелец правит и удаляет всё, руководитель правит только своё. */
export function ManageUnitsPage() {
  const { slug, view, viewer } = useManageContext()
  const client = useQueryClient()
  const toast = useToast()
  const [removing, setRemoving] = useState<Unit | null>(null)
  const remove = useMutation({
    mutationFn: (id: string) => deleteUnit(slug, id),
    onSuccess: async () => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: t.orgs.units.removed })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.orgs.units.removeFailed, text: describeError(err) }),
    onSettled: () => setRemoving(null),
  })

  return (
    <>
      <p className="manage-intro">{t.orgs.units.lead}</p>
      {viewer.can_manage_units && (
        <div className="org-actions">
          <ButtonLink to={`/my-organization/${slug}/units/new`}>{t.orgs.units.add}</ButtonLink>
        </div>
      )}
      {view.units.length === 0 ? (
        <EmptyState
          headingLevel={2}
          title={t.orgs.units.emptyTitle}
          text={t.orgs.units.emptyText}
          action={
            viewer.can_manage_units ? <ButtonLink to={`/my-organization/${slug}/units/new`}>{t.orgs.units.add}</ButtonLink> : undefined
          }
        />
      ) : (
        <ul className="org-list">
          {view.units.map((u) => {
            const canEdit = viewer.can_manage_units || viewer.editable_units.includes(u.id)
            return (
              <li className="mine-row" key={u.id}>
                <div className="mine-row-main">
                  <p className="mine-row-title" data-long={longName(u.name)}>
                    <Link to={`/organizations/${slug}/units/${u.id}`}>{u.name}</Link>
                  </p>
                  <p className="mine-row-line">
                    <Tag>{unitKindShort(u.kind)}</Tag>
                    <span>{u.head_name ? `${t.orgs.page.head}: ${u.head_name}` : t.orgs.page.noHead}</span>
                  </p>
                </div>
                {canEdit && (
                  <div className="mine-row-actions">
                    <ButtonLink to={`/my-organization/${slug}/units/${u.id}`} variant="secondary" size="sm">
                      {t.orgs.units.edit}
                    </ButtonLink>
                    {viewer.can_manage_units && (
                      <Button variant="quiet" size="sm" onClick={() => setRemoving(u)}>
                        {t.orgs.units.remove}
                      </Button>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}
      <ConfirmModal
        open={removing !== null}
        title={removing ? t.orgs.units.removeTitle(removing.name) : ''}
        text={t.orgs.units.removeText}
        confirmLabel={t.orgs.units.removeConfirm}
        pending={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
        onClose={() => setRemoving(null)}
      />
    </>
  )
}

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { useToast } from '../../ui/useToast'
import { useMe } from '../auth/api'
import { describeError } from '../auth/errors'
import { refreshOrganizations, removeMember, updateOrganization } from './api'
import { ConfirmModal } from './ConfirmModal'
import { roleLabel } from './labels'
import { useManageContext } from './manageContext'
import { OrgForm } from './OrgForm'
import { roles } from './api'

/** Данные организации: форма для владельца, краткая справка для остальных, и выход из организации. */
export function ManageDataPage() {
  const { slug, view, viewer } = useManageContext()
  const org = view.organization
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const { user } = useMe()
  const [leaving, setLeaving] = useState(false)

  const save = useMutation({
    mutationFn: (fields: Parameters<typeof updateOrganization>[1]) => updateOrganization(slug, fields),
    onSuccess: async () => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: t.orgs.manage.saved })
    },
  })
  const leave = useMutation({
    mutationFn: () => removeMember(slug, user!.id),
    onSuccess: async () => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: t.orgs.members.left })
      void navigate('/my-organization')
    },
    onError: (err) => {
      setLeaving(false)
      toast.show({ kind: 'error', title: t.orgs.members.removeFailed, text: describeError(err) })
    },
  })

  const role = roles.find((r) => r === viewer.role)

  return (
    <>
      {viewer.can_edit_organization ? (
        <OrgForm
          initial={{ name: org.name, kind: org.kind, city: org.city, website: org.website, description: org.description }}
          submitLabel={t.orgs.manage.save}
          pending={save.isPending}
          error={save.error}
          onSubmit={(fields) => save.mutate(fields)}
        />
      ) : (
        <>
          <p className="manage-intro">{t.orgs.manage.readOnlyNote}</p>
          {role && (
            <dl className="role-legend">
              <dt>{roleLabel(role)}</dt>
              <dd>{t.orgs.roleDescriptions[role]}</dd>
            </dl>
          )}
        </>
      )}
      <div className="manage-danger">
        <Button variant="secondary" onClick={() => setLeaving(true)}>
          {t.orgs.members.leave}
        </Button>
      </div>
      <ConfirmModal
        open={leaving}
        title={t.orgs.members.leaveTitle}
        text={t.orgs.members.leaveText}
        confirmLabel={t.orgs.members.leaveConfirm}
        pending={leave.isPending}
        onConfirm={() => leave.mutate()}
        onClose={() => setLeaving(false)}
      />
    </>
  )
}

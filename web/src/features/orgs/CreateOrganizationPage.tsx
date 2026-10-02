import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { t } from '../../i18n'
import { useToast } from '../../ui/useToast'
import { createOrganization, refreshOrganizations } from './api'
import { OrgForm } from './OrgForm'
import { RequireUser } from './RequireUser'
import './orgs.css'

export function CreateOrganizationPage() {
  return <RequireUser>{() => <CreateOrganization />}</RequireUser>
}

function CreateOrganization() {
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const create = useMutation({
    mutationFn: createOrganization,
    onSuccess: async (org) => {
      await refreshOrganizations(client, org.slug)
      toast.show({ kind: 'success', title: t.orgs.create.created, text: t.orgs.create.createdText })
      void navigate(`/my-organization/${org.slug}/units`)
    },
  })
  return (
    <div className="page page-narrow org-page">
      <h1>{t.orgs.create.title}</h1>
      <p className="lead">{t.orgs.create.lead}</p>
      <div className="manage-body">
        <OrgForm
          initial={{ name: '', kind: '', city: '', website: '', description: '' }}
          submitLabel={t.orgs.create.submit}
          pending={create.isPending}
          error={create.error}
          onSubmit={(fields) => create.mutate(fields)}
        />
      </div>
    </div>
  )
}

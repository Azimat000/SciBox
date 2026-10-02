import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { t } from '../../i18n'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { acceptInvitationById, refreshOrganizations, useMine, type MyInvitation, type MyOrganization } from './api'
import { formatDate, kindLabel, longName, roleLabel } from './labels'
import { LoadFailed, PageSkeleton } from './states'
import { RequireUser } from './RequireUser'
import './orgs.css'

export function MyOrganizationsPage() {
  return <RequireUser>{() => <MyOrganizations />}</RequireUser>
}

function MyOrganizations() {
  const mine = useMine(true)
  if (mine.isPending) return <PageSkeleton />
  if (mine.isError) return <LoadFailed title={t.orgs.mine.loadError} error={mine.error} onRetry={() => void mine.refetch()} />
  const { organizations, invitations } = mine.data

  return (
    <div className="page org-page">
      <h1>{t.orgs.mine.title}</h1>
      <p className="lead">{t.orgs.mine.lead}</p>

      {invitations.length > 0 && (
        <section className="mine-block" aria-labelledby="mine-invitations">
          <h2 id="mine-invitations">{t.orgs.mine.invitationsTitle}</h2>
          {invitations.map((i) => (
            <InvitationRow key={i.id} invitation={i} />
          ))}
        </section>
      )}

      {organizations.length > 0 ? (
        <section className="mine-block" aria-labelledby="mine-orgs">
          <h2 id="mine-orgs">{t.orgs.mine.listTitle}</h2>
          {organizations.map((o) => (
            <OrganizationRow key={o.id} org={o} />
          ))}
          <div className="org-actions">
            <ButtonLink to="/organizations/new" variant="secondary">
              {t.orgs.mine.create}
            </ButtonLink>
          </div>
        </section>
      ) : (
        invitations.length === 0 && (
          <EmptyState
            headingLevel={2}
            title={t.orgs.mine.emptyTitle}
            text={t.orgs.mine.emptyText}
            action={<ButtonLink to="/organizations/new">{t.orgs.mine.create}</ButtonLink>}
          />
        )
      )}
      {organizations.length === 0 && invitations.length > 0 && (
        <div className="org-actions">
          <ButtonLink to="/organizations/new" variant="secondary">
            {t.orgs.mine.create}
          </ButtonLink>
        </div>
      )}
    </div>
  )
}

function OrganizationRow({ org }: { org: MyOrganization }) {
  return (
    <div className="mine-row">
      <div className="mine-row-main">
        <p className="mine-row-title" data-long={longName(org.name)}>{org.name}</p>
        <p className="mine-row-line">
          <Tag tone="accent">{roleLabel(org.role)}</Tag>
          <span>
            {kindLabel(org.kind)} · {org.city}
          </span>
        </p>
      </div>
      <div className="mine-row-actions">
        <ButtonLink to={`/my-organization/${org.slug}`} size="sm">
          {t.orgs.mine.openManage}
        </ButtonLink>
        <ButtonLink to={`/organizations/${org.slug}`} variant="secondary" size="sm">
          {t.orgs.mine.openPage}
        </ButtonLink>
      </div>
    </div>
  )
}

function InvitationRow({ invitation }: { invitation: MyInvitation }) {
  const client = useQueryClient()
  const toast = useToast()
  const accept = useMutation({
    mutationFn: () => acceptInvitationById(invitation.id),
    onSuccess: async (joined) => {
      await refreshOrganizations(client, joined.slug)
      toast.show({ kind: 'success', title: t.orgs.mine.accepted(joined.name) })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.orgs.mine.acceptFailed, text: describeError(err) }),
  })
  return (
    <div className="mine-row">
      <div className="mine-row-main">
        <p className="mine-row-title">
          <Link to={`/organizations/${invitation.organization.slug}`}>{invitation.organization.name}</Link>
        </p>
        <p className="mine-row-line">
          <Tag tone="accent">{roleLabel(invitation.role)}</Tag>
          {invitation.unit_name && <span>{t.orgs.mine.invitationUnit(invitation.unit_name)}</span>}
          <span className="num">{t.orgs.mine.invitationUntil(formatDate(invitation.expires_at))}</span>
        </p>
      </div>
      <div className="mine-row-actions">
        <Button size="sm" loading={accept.isPending} onClick={() => accept.mutate()}>
          {t.orgs.mine.accept}
        </Button>
      </div>
    </div>
  )
}

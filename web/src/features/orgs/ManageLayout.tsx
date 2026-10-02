import { NavLink, Outlet, useParams } from 'react-router'
import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { NotFoundPage } from '../status/NotFoundPage'
import { useOrganization, isNotFound } from './api'
import { kindLabel, longName, roleLabel } from './labels'
import { RequireUser } from './RequireUser'
import { LoadFailed, PageSkeleton } from './states'
import { Tag } from '../../ui/Tag'
import './orgs.css'

/** Рамка управления организацией: название, роль и разделы. Пускает только сотрудников. */
export function ManageLayout() {
  return <RequireUser>{() => <Manage />}</RequireUser>
}

function Manage() {
  const { slug = '' } = useParams()
  const query = useOrganization(slug)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <NotFoundPage />
    ) : (
      <LoadFailed title={t.orgs.manage.loadError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  const view = query.data
  const viewer = view.viewer
  if (!viewer || viewer.role === '') {
    return (
      <div className="page">
        <EmptyState
          headingLevel={2}
          title={t.orgs.manage.notMemberTitle}
          text={t.orgs.manage.notMemberText}
          action={<ButtonLink to={`/organizations/${slug}`}>{view.organization.name}</ButtonLink>}
        />
      </div>
    )
  }
  const org = view.organization

  return (
    <div className="page org-page">
      <header className="org-head">
        <h1 data-long={longName(org.name)}>{org.name}</h1>
        <p className="org-meta">
          <Tag tone="accent">{roleLabel(viewer.role)}</Tag>
          <span>
            {kindLabel(org.kind)} · {org.city}
          </span>
          <NavLink to={`/organizations/${org.slug}`}>{t.orgs.manage.viewPublic}</NavLink>
        </p>
      </header>
      <nav className="manage-tabs" aria-label={t.orgs.manage.tabs}>
        <NavLink className="manage-tab" to={`/my-organization/${org.slug}`} end>
          {t.orgs.manage.tabData}
        </NavLink>
        <NavLink className="manage-tab" to={`/my-organization/${org.slug}/units`}>
          {t.orgs.manage.tabUnits}
        </NavLink>
        {viewer.can_manage_members && (
          <NavLink className="manage-tab" to={`/my-organization/${org.slug}/members`}>
            {t.orgs.manage.tabMembers}
          </NavLink>
        )}
      </nav>
      <div className="manage-body">
        <Outlet context={{ slug: org.slug, view, viewer }} />
      </div>
    </div>
  )
}

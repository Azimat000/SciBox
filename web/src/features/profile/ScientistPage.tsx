import { usePageTitle } from '../../app/pageTitle'
import { useParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { useMe } from '../auth/api'
import { InviteButton } from '../offers/InviteButton'
import { isNotFound } from '../orgs/api'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { cvPath, useScientist } from './api'
import { ProfileView } from './ProfileView'
import './profile.css'

/** Страница профиля для других людей. Если приватность не пускает, ответ тот же, что у несуществующей страницы. */
export function ScientistPage() {
  const { id = '' } = useParams()
  const query = useScientist(id)
  const { user } = useMe()
  usePageTitle(query.data?.profile.name)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <div className="page">
        <EmptyState
          headingLevel={2}
          title={t.profile.notFoundTitle}
          text={t.profile.notFoundText}
          action={
            <ButtonLink to="/" variant="secondary">
              {t.common.toHome}
            </ButtonLink>
          }
        />
      </div>
    ) : (
      <LoadFailed title={t.profile.scientistLoadError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  const page = query.data
  return (
    <div className="page profile-page">
      {page.viewer.is_owner && (
        <Alert
          kind="info"
          action={
            <ButtonLink to="/profile" variant="secondary" size="sm">
              {t.profile.ownBannerLink}
            </ButtonLink>
          }
        >
          {t.profile.ownBanner}
        </Alert>
      )}
      <ProfileView
        page={page}
        actions={
          <>
            {!page.viewer.is_owner && <InviteButton profileId={page.profile.id} name={page.profile.name} variant="primary" />}
            {user ? (
              <a className="btn btn-secondary" href={cvPath(id)} download>
                {t.profile.downloadCv}
              </a>
            ) : (
              <p className="profile-note">{t.profile.cvNeedLogin}</p>
            )}
          </>
        }
      />
    </div>
  )
}

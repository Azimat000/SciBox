import { Link, Navigate, useParams } from 'react-router'
import { t } from '../../i18n'
import { Tag } from '../../ui/Tag'
import { sizeText } from '../../lib/fileSize'
import { isNotFound } from '../orgs/api'
import { longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { RequireUser } from '../orgs/RequireUser'
import { ProfileView } from '../profile/ProfileView'
import { NotFoundPage } from '../status/NotFoundPage'
import { useApplication, type StaffReference } from './api'
import { FileLink } from './ApplicationPage'
import { dateText, dateTimeText, refStatusLabel, statusLabel, statusTone } from './labels'
import './applications.css'

/**
 * Карточка отклика для организации: кто откликнулся, письмо, файлы, рекомендательные письма и профиль, как он был
 * отправлен. Список откликов и смена статусов появятся со следующим срезом; сюда ведут уведомления.
 */
export function CandidatePage() {
  const { id = '' } = useParams()
  return <RequireUser>{() => <Loader id={id} />}</RequireUser>
}

function Loader({ id }: { id: string }) {
  const c = t.applications.candidate
  const query = useApplication(id)
  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? <NotFoundPage /> : <LoadFailed title={c.loadError} error={query.error} onRetry={() => void query.refetch()} />
  }
  const app = query.data
  // Свой отклик человек читает на своей странице: письма о себе он не видит.
  if (app.viewer.role === 'applicant') return <Navigate to={`/applications/${id}`} replace />
  const refs = app.references as StaffReference[]

  return (
    <article className="page org-page application-page">
      <header className="application-head">
        <h1 data-long={longName(app.applicant_name)}>{app.applicant_name}</h1>
        <p className="application-org">
          <Link to={`/vacancies/${app.vacancy.id}`}>{app.vacancy.title}</Link>
          {', '}
          <Link to={`/organizations/${app.vacancy.org_slug}`}>{app.vacancy.org_name}</Link>
        </p>
        <p className="application-meta">
          <Tag tone={statusTone(app.status)}>{statusLabel(app.status)}</Tag>
          <span>{t.applications.detail.sentAt(dateText(app.created_at))}</span>
        </p>
      </header>

      <section className="application-section" aria-labelledby="cand-letter">
        <h2 id="cand-letter">{c.letter}</h2>
        <p className="application-text">{app.cover_letter}</p>
        <p className="application-contact">
          <span>{c.contact}: </span>
          <a href={`mailto:${app.contact_email}`}>{app.contact_email}</a>
        </p>
      </section>

      <section className="application-section" aria-labelledby="cand-files">
        <h2 id="cand-files">{c.files}</h2>
        <ul className="file-links">
          {app.cv && (
            <li>
              <FileLink appId={app.id} file={app.cv} label={`${c.cv}, ${c.letterFile(app.cv.name, sizeText(app.cv.size))}`} />
            </li>
          )}
          {app.files.map((f) => (
            <li key={f.id}>
              <FileLink appId={app.id} file={f} label={c.letterFile(f.name, sizeText(f.size))} />
            </li>
          ))}
        </ul>
        {app.files.length === 0 && <p className="application-muted">{c.noExtraFiles}</p>}
      </section>

      <section className="application-section" aria-labelledby="cand-refs">
        <h2 id="cand-refs">{c.referencesTitle}</h2>
        <p className="application-muted">{c.referencesText}</p>
        {refs.length === 0 ? (
          <p className="application-muted">{c.referencesEmpty}</p>
        ) : (
          <ul className="ref-list">
            {refs.map((r) => (
              <li key={r.id} className="ref-row ref-row-letter">
                <div className="ref-who">
                  <p className="ref-name">{r.name}</p>
                  <p className="ref-sub">{[r.relation, r.email].filter(Boolean).join(' · ')}</p>
                </div>
                <div className="ref-state">
                  <Tag tone={r.status === 'received' ? 'accent' : 'neutral'}>
                    {r.status === 'pending' ? c.waiting : r.status === 'declined' ? c.declined : refStatusLabel(r.status)}
                  </Tag>
                  {r.answered_at && <span className="ref-when">{dateTimeText(r.answered_at)}</span>}
                </div>
                {r.letter && (
                  <div className="ref-letter">
                    {r.letter.text && <p className="application-text">{r.letter.text}</p>}
                    {r.letter.file && <FileLink appId={app.id} file={r.letter.file} label={c.letterFile(r.letter.file.name, sizeText(r.letter.file.size))} />}
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="application-section" aria-labelledby="cand-profile">
        <h2 id="cand-profile">{c.profileTitle}</h2>
        <p className="application-muted">{c.profileNote}</p>
        <ProfileView nested page={{ profile: app.profile, viewer: { is_owner: false, can_see_contacts: true } }} />
      </section>
    </article>
  )
}

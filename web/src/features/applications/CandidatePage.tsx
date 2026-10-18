import { usePageTitle } from '../../app/pageTitle'
import { useState } from 'react'
import { Link, Navigate, useParams } from 'react-router'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { Tag } from '../../ui/Tag'
import { sizeText } from '../../lib/fileSize'
import { isNotFound } from '../orgs/api'
import { longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { RequireUser } from '../orgs/RequireUser'
import { ProfileView } from '../profile/ProfileView'
import { NotFoundPage } from '../status/NotFoundPage'
import { useApplication, type Detail, type StaffReference } from './api'
import { FileLink } from './ApplicationPage'
import { DecisionModal } from './DecisionModal'
import { dateText, dateTimeText, refStatusLabel, statusLabel, statusTone } from './labels'
import './applications.css'
import { mailtoHref } from '../../lib/mailto'
import { ChatButton } from '../chat/ChatButton'

/**
 * Карточка отклика для организации: решение, письмо, файлы, рекомендательные письма и профиль, как он был
 * отправлен. О собеседовании договариваются в переписке (кнопка рядом с решением); приглашений внутри отклика нет (D-256). Первое открытие отмечает отклик просмотренным (это делает сервер). Список откликов: `/candidates`.
 */
export function CandidatePage() {
  const { id = '' } = useParams()
  return <RequireUser>{() => <Loader id={id} />}</RequireUser>
}

function Loader({ id }: { id: string }) {
  const c = t.applications.candidate
  const query = useApplication(id)
  usePageTitle(query.data?.applicant_name)
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
          <Link to={`/candidates?vacancy=${app.vacancy.id}`}>{c.allForVacancy}</Link>
        </p>
      </header>

      <div className="split">
        {/* Решение справа, рядом с письмом и профилем: кнопки видны всё время, пока организация читает отклик */}
        <aside className="split-aside review-aside" aria-labelledby="cand-decision">
          <ReviewPanel app={app} />
          <ChatButton chat={app.chat} />
        </aside>

        <div className="split-main application-main">
          {app.anonymized ? (
            <section className="application-section" aria-labelledby="cand-erased">
              <h2 id="cand-erased">{c.anonymizedTitle}</h2>
              <p className="application-muted">{c.anonymizedText}</p>
            </section>
          ) : (
            <Submission app={app} refs={refs} />
          )}
        </div>
      </div>
    </article>
  )
}

/** Что прислал соискатель: письмо, файлы, рекомендации и снимок профиля. */
function Submission({ app, refs }: { app: Detail; refs: StaffReference[] }) {
  const c = t.applications.candidate
  return (
    <>
      <section className="application-section" aria-labelledby="cand-letter">
        <h2 id="cand-letter">{c.letter}</h2>
        <p className="application-text">{app.cover_letter}</p>
        <p className="application-contact">
          <span>{c.contact}: </span>
          <a href={mailtoHref(app.contact_email)}>{app.contact_email}</a>
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
                    {r.letter.file && (
                      <FileLink appId={app.id} file={r.letter.file} label={c.letterFile(r.letter.file.name, sizeText(r.letter.file.size))} />
                    )}
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
        <ProfileView
          nested
          page={{
            profile: app.profile,
            viewer: { is_owner: false, can_see_contacts: true },
          }}
        />
      </section>
    </>
  )
}

/** Решение по отклику: пока его нет, кнопки «Принять кандидата» и «Отказать»; потом итог и записка. */
function ReviewPanel({ app }: { app: Detail }) {
  const r = t.applications.review
  const [dialog, setDialog] = useState<'accepted' | 'rejected' | null>(null)
  const { decisions } = app.viewer
  const open = decisions.length > 0
  return (
    <div className="review-panel">
      <h2 id="cand-decision" className="review-title">
        {r.decisionTitle}
      </h2>
      {open ? (
        <div className="review-actions">
          {decisions.includes('accepted') && (
            <Button onClick={() => setDialog('accepted')}>
              {r.accept}
            </Button>
          )}
          {decisions.includes('rejected') && (
            <Button variant="quiet" onClick={() => setDialog('rejected')}>
              {r.reject}
            </Button>
          )}
        </div>
      ) : (
        <p className="review-final">
          {app.status === 'accepted' && r.finalAccepted(dateText(app.status_changed_at))}
          {app.status === 'rejected' && r.finalRejected(dateText(app.status_changed_at))}
          {app.status === 'withdrawn' && r.withdrawnNote}
        </p>
      )}
      {app.decision_note && (
        <p className="review-note">
          <span>{r.noteShown}: </span>
          {app.decision_note}
        </p>
      )}
      {(dialog === 'accepted' || dialog === 'rejected') && <DecisionModal appId={app.id} decision={dialog} onClose={() => setDialog(null)} />}
    </div>
  )
}

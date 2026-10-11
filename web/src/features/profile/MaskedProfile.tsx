import type { ReactNode } from 'react'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Alert } from '../../ui/Alert'
import { Tag } from '../../ui/Tag'
import type { ProfilePage } from './api'
import { degreeLabel, titleLabel } from './labels'
import { Section } from './ProfileView'

const m = t.profile.masked

/**
 * Анонимная карточка учёного (режим «Анонимно», имя организации не открыто): только научный облик. Имени, места работы,
 * города, номеров в научных базах и записей разделов в ответе сервера нет, поэтому показывать здесь нечего, кроме этого.
 */
export function MaskedProfile({ page, actions }: { page: ProfilePage; actions?: ReactNode }) {
  const p = page.profile
  const degree = p.degree.level !== 'none'
  const title = p.academic_title !== 'none'
  const h = [
    [t.profile.identifiers.h.rsci, p.h_index.rsci],
    [t.profile.identifiers.h.scopus, p.h_index.scopus],
    [t.profile.identifiers.h.wos, p.h_index.wos],
    [t.profile.identifiers.h.scholar, p.h_index.scholar],
  ].filter((r): r is [string, number] => r[1] !== null)
  const counts = p.counts
  const works = counts
    ? [
        counts.publications > 0 && m.publications(counts.publications, plural(counts.publications, m.publicationForms)),
        counts.grants > 0 && m.grants(counts.grants, plural(counts.grants, m.grantForms)),
        counts.patents > 0 && m.patents(counts.patents, plural(counts.patents, m.patentForms)),
      ].filter((x): x is string => Boolean(x))
    : []
  const q = p.quartiles

  return (
    <article className="profile profile-masked">
      <header className="profile-head">
        <h1>{p.label || m.badge}</h1>
        <p className="profile-headline">{m.badge}</p>
        {p.open_to_offers && (
          <p className="profile-meta">
            <Tag tone="accent">{t.profile.openToOffers}</Tag>
          </p>
        )}
        {actions && <div className="profile-toolbar">{actions}</div>}
      </header>
      <Alert kind="info">{m.notice}</Alert>

      <Section title={m.degree} id="degree">
        {degree || title ? (
          <ol className="profile-items">
            {degree && (
              <li className="profile-item">
                <span className="profile-item-label" />
                <div className="profile-item-body">
                  <p className="profile-item-lead">{degreeLabel(p.degree.level)}</p>
                  {p.degree.specialty && (
                    <p className="profile-item-line">
                      <span className="num">{p.degree.specialty.code}</span> {p.degree.specialty.name}
                    </p>
                  )}
                </div>
              </li>
            )}
            {title && (
              <li className="profile-item">
                <span className="profile-item-label" />
                <div className="profile-item-body">
                  <p className="profile-item-lead">{titleLabel(p.academic_title)}</p>
                </div>
              </li>
            )}
          </ol>
        ) : (
          <p className="profile-empty">{t.profile.empty.degree}</p>
        )}
      </Section>

      {p.career_level !== undefined && (
        <Section title={m.level} id="level">
          <p className="profile-text">
            <strong className="num">{m.levelValue(p.career_level)}</strong>
          </p>
          <p className="profile-note">{m.levelHint}</p>
        </Section>
      )}

      {p.specialties.length > 0 && (
        <Section title={m.specialties} id="specialties">
          <ul className="profile-tags">
            {p.specialties.map((s) => (
              <li key={s.code}>
                <Tag>
                  <span className="num">{s.code}</span> {s.name}
                </Tag>
              </li>
            ))}
          </ul>
        </Section>
      )}

      <Section title={m.metrics} id="metrics">
        {h.length > 0 ? (
          <ul className="profile-h">
            {h.map(([label, value]) => (
              <li key={label}>
                {label} <span className="num profile-h-value">{value}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="profile-empty">{m.noMetrics}</p>
        )}
      </Section>

      <Section title={m.works} id="works">
        {works.length > 0 ? (
          <ul className="profile-h">
            {works.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        ) : (
          <p className="profile-empty">{m.noWorks}</p>
        )}
        {q && q.q12_total > 0 && (
          <p className="profile-quartiles">
            <strong className="num">{t.profile.journals.count(q.q12_total, plural(q.q12_total, t.profile.journals.countForms))}</strong>
            <span>{q.q12_recent > 0 ? t.profile.journals.recent(q.q12_recent, q.recent_from, q.recent_from + 4) : t.profile.journals.recentNone(q.recent_from, q.recent_from + 4)}</span>
          </p>
        )}
      </Section>
      <p className="profile-note">{m.noCv}</p>
    </article>
  )
}

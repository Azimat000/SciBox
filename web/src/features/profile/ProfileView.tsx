import { createContext, useContext, type ReactNode } from 'react'
import { t } from '../../i18n'
import { Button, ButtonLink } from '../../ui/Button'
import { Tag } from '../../ui/Tag'
import { itemKinds, sectionOf, type Item, type ItemKind, type ProfilePage } from './api'
import { degreeLabel, titleLabel } from './labels'
import { itemTitle, sectionDefs } from './sections'

/** Уровень заголовков разделов: на своей странице профиля h2, внутри чужой страницы (карточка отклика) ниже. */
const SectionLevel = createContext<'h2' | 'h3'>('h2')

type Props = {
  page: ProfilePage
  /** Профиль показан внутри другой страницы (карточка отклика): имя — h2, разделы — h3. */
  nested?: boolean
  /** Владелец правит профиль: у разделов появляются кнопки. */
  editable?: boolean
  onAdd?: (kind: ItemKind) => void
  onEdit?: (item: Item) => void
  onRemove?: (item: Item) => void
  /** Кнопки под именем и должностью: сначала человек видит, чей это профиль, потом действия. */
  actions?: ReactNode
  /** То, что идёт сразу после шапки и до разделов (у владельца: приватность). */
  afterHead?: ReactNode
}

/** Адрес сайта без пути; если ссылку не удалось разобрать, показываем её целиком. */
function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

/** SPIN-код из восьми цифр читается привычно: 1234-5678. */
const spinText = (spin: string) => (/^\d{8}$/.test(spin) ? `${spin.slice(0, 4)}-${spin.slice(4)}` : spin)

const longName = (s: string) => (s.length > 40 ? 'true' : undefined)

/** Профиль учёного как страница журнала. Одна разметка и для владельца (с кнопками правки), и для остальных (без них). */
export function ProfileView({ page, nested = false, editable = false, onAdd, onEdit, onRemove, actions, afterHead }: Props) {
  const p = page.profile
  const place = t.profile.place(p.city, p.region?.name ?? '')
  const hRows = [
    [t.profile.identifiers.h.rsci, p.h_index.rsci],
    [t.profile.identifiers.h.scopus, p.h_index.scopus],
    [t.profile.identifiers.h.wos, p.h_index.wos],
    [t.profile.identifiers.h.scholar, p.h_index.scholar],
  ] as const
  const shownH = hRows.filter((r): r is readonly [string, number] => r[1] !== null)
  const ids = [
    [t.profile.identifiers.orcid, p.identifiers.orcid, 'orcid'],
    [t.profile.identifiers.spin, spinText(p.identifiers.spin), 'spin'],
    [t.profile.identifiers.scopus, p.identifiers.scopus_id, 'scopus'],
    [t.profile.identifiers.wos, p.identifiers.wos_id, 'wos'],
  ].filter((r) => r[1] !== '')
  const degreeLine = p.degree.level !== 'none'
  const titleLine = p.academic_title !== 'none'

  const editCore = (section: string) =>
    editable ? (
      <ButtonLink to="/profile/edit" variant="quiet" size="sm" aria-label={`${t.profile.item.edit}: ${section.toLowerCase()}`}>
        {t.profile.item.edit}
      </ButtonLink>
    ) : null

  // Пустые разделы показываются только владельцу, поэтому подсказка всегда для него
  const empty = (text: string) => <p className="profile-empty">{text}</p>

  return (
    <SectionLevel.Provider value={nested ? 'h3' : 'h2'}>
      <article className="profile">
        <header className="profile-head">
          {nested ? <h2 data-long={longName(p.name)}>{p.name}</h2> : <h1 data-long={longName(p.name)}>{p.name}</h1>}
          {p.headline && <p className="profile-headline">{p.headline}</p>}
          {(place || p.open_to_offers) && (
            <p className="profile-meta">
              {place && <span>{place}</span>}
              {p.open_to_offers && <Tag tone="accent">{t.profile.openToOffers}</Tag>}
            </p>
          )}
          {page.viewer.can_see_contacts && p.contact_email && (
            <p className="profile-contact">
              <a href={`mailto:${p.contact_email}`}>{p.contact_email}</a>
            </p>
          )}
          {actions && <div className="profile-toolbar">{actions}</div>}
        </header>
        {afterHead}

        {(p.about || editable) && (
          <Section title={t.profile.sections.about} action={editCore(t.profile.sections.about)} id="about">
            {p.about ? <p className="profile-text">{p.about}</p> : empty(t.profile.empty.about)}
          </Section>
        )}

        {(degreeLine || titleLine || editable) && (
          <Section title={t.profile.sections.degree} action={editCore(t.profile.sections.degree)} id="degree">
            {degreeLine || titleLine ? (
              <ol className="profile-items">
                {degreeLine && (
                  <li className="profile-item">
                    <span className="profile-item-label num">{p.degree.year ?? ''}</span>
                    <div className="profile-item-body">
                      <p className="profile-item-lead">{degreeLabel(p.degree.level)}</p>
                      {p.degree.specialty && (
                        <p className="profile-item-line">
                          <span className="num">{p.degree.specialty.code}</span> {p.degree.specialty.name}
                        </p>
                      )}
                      {p.degree.dissertation && <p className="profile-item-line">{t.profile.degreeLine.dissertation(p.degree.dissertation)}</p>}
                      {p.degree.institution && <p className="profile-item-line">{p.degree.institution}</p>}
                    </div>
                  </li>
                )}
                {titleLine && (
                  <li className="profile-item">
                    <span className="profile-item-label num">{p.academic_title_year ?? ''}</span>
                    <div className="profile-item-body">
                      <p className="profile-item-lead">{titleLabel(p.academic_title)}</p>
                    </div>
                  </li>
                )}
              </ol>
            ) : (
              empty(t.profile.empty.degree)
            )}
          </Section>
        )}

        {(p.specialties.length > 0 || editable) && (
          <Section title={t.profile.sections.specialties} action={editCore(t.profile.sections.specialties)} id="specialties">
            {p.specialties.length > 0 ? (
              <ul className="profile-tags">
                {p.specialties.map((s) => (
                  <li key={s.code}>
                    <Tag>
                      <span className="num">{s.code}</span> {s.name}
                    </Tag>
                  </li>
                ))}
              </ul>
            ) : (
              empty(t.profile.empty.specialties)
            )}
          </Section>
        )}

        {(ids.length > 0 || shownH.length > 0 || editable) && (
          <Section title={t.profile.sections.identifiers} action={editCore(t.profile.sections.identifiers)} id="identifiers">
            {ids.length > 0 || shownH.length > 0 ? (
              <dl className="profile-facts">
                {ids.map(([label, value, key]) => (
                  <div key={key} className="profile-fact">
                    <dt>{label}</dt>
                    <dd className="num">
                      {key === 'orcid' ? (
                        <a href={`https://orcid.org/${value}`} rel="noreferrer noopener" target="_blank" aria-label={t.profile.identifiers.orcidLink(value)}>
                          {value}
                        </a>
                      ) : (
                        value
                      )}
                    </dd>
                  </div>
                ))}
                {shownH.length > 0 && (
                  <div className="profile-fact profile-fact-wide">
                    <dt>{t.profile.identifiers.hIndex}</dt>
                    <dd>
                      <ul className="profile-h">
                        {shownH.map(([label, value]) => (
                          <li key={label}>
                            {label} <span className="num profile-h-value">{value}</span>
                          </li>
                        ))}
                      </ul>
                    </dd>
                  </div>
                )}
              </dl>
            ) : (
              empty(t.profile.empty.identifiers)
            )}
          </Section>
        )}

        {itemKinds.map((kind) => {
          const def = sectionDefs[kind]
          const items = p.sections[sectionOf[kind]]
          if (items.length === 0 && !editable) return null
          return (
            <Section
              key={kind}
              id={kind}
              title={def.heading}
              action={
                editable ? (
                  <Button variant="secondary" size="sm" aria-label={t.profile.item.addAria(def.heading)} onClick={() => onAdd?.(kind)}>
                    {t.profile.item.add}
                  </Button>
                ) : null
              }
            >
              {items.length === 0 ? (
                empty(def.empty)
              ) : (
                <ol className="profile-items">
                  {items.map((it) => (
                    <ItemRow key={it.id} item={it} editable={editable} onEdit={onEdit} onRemove={onRemove} />
                  ))}
                </ol>
              )}
            </Section>
          )
        })}
      </article>
    </SectionLevel.Provider>
  )
}

function Section({ title, action, id, children }: { title: string; action?: ReactNode; id: string; children: ReactNode }) {
  const level = useContext(SectionLevel)
  return (
    <section className="profile-section" aria-labelledby={`profile-${id}`}>
      <div className="profile-section-head">
        {level === 'h2' ? <h2 id={`profile-${id}`}>{title}</h2> : <h3 id={`profile-${id}`}>{title}</h3>}
        {action}
      </div>
      {children}
    </section>
  )
}

function ItemRow({ item, editable, onEdit, onRemove }: { item: Item; editable: boolean; onEdit?: (i: Item) => void; onRemove?: (i: Item) => void }) {
  const s = sectionDefs[item.kind].summary(item)
  return (
    <li className="profile-item">
      <span className="profile-item-label num">{s.label}</span>
      <div className="profile-item-body">
        <p className="profile-item-lead">{s.lead}</p>
        {s.lines.map((line) => (
          <p key={line} className="profile-item-line">
            {line}
          </p>
        ))}
        {(s.doi || s.url) && (
          <p className="profile-item-line profile-item-links">
            {s.doi && (
              <a href={`https://doi.org/${s.doi}`} rel="noreferrer noopener" target="_blank">
                DOI {s.doi}
              </a>
            )}
            {s.url && (
              <a href={s.url} rel="noreferrer noopener nofollow" target="_blank">
                {hostOf(s.url)}
              </a>
            )}
          </p>
        )}
      </div>
      {editable && (
        <div className="profile-item-actions">
          <Button variant="quiet" size="sm" aria-label={t.profile.item.editAria(itemTitle(item))} onClick={() => onEdit?.(item)}>
            {t.profile.item.edit}
          </Button>
          <Button variant="quiet" size="sm" aria-label={t.profile.item.removeAria(itemTitle(item))} onClick={() => onRemove?.(item)}>
            {t.profile.item.remove}
          </Button>
        </div>
      )}
    </li>
  )
}

import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { Tag } from '../../ui/Tag'

/** Кнопки входа через другие сервисы. Пока не работают, и это сказано прямо (D-019). */
export function SocialSoon({ title = t.auth.social.title, note = t.auth.social.note }: { title?: string; note?: string }) {
  return (
    <section className="social" aria-labelledby="social-title">
      <h2 id="social-title" className="auth-section-title">
        {title}
      </h2>
      <ul className="social-list">
        {t.auth.social.providers.map((name) => (
          <li key={name}>
            <Button variant="secondary" disabled className="social-button">
              {name}
              <Tag>{t.auth.social.soon}</Tag>
            </Button>
          </li>
        ))}
      </ul>
      <p className="auth-note">{note}</p>
    </section>
  )
}

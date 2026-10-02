import { Link } from 'react-router'
import { productName } from '../../config/product'
import { t } from '../../i18n'
import './SiteFooter.css'

export function SiteFooter() {
  return (
    <footer className="site-footer">
      <div className="site-footer-inner">
        <div className="footer-about">
          <p className="footer-name">{productName}</p>
          <p className="footer-note">{t.shell.footerNote}</p>
        </div>
        <nav className="footer-nav" aria-label={t.shell.footerNav}>
          <Link to="/privacy">{t.shell.footerPrivacy}</Link>
        </nav>
      </div>
    </footer>
  )
}

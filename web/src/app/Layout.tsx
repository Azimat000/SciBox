import { Link, Outlet } from 'react-router'
import { productName } from '../config/product'

export function Layout() {
  return (
    <>
      <header className="site-header">
        <Link to="/" className="wordmark">
          {productName}
        </Link>
      </header>
      <main id="main">
        <Outlet />
      </main>
    </>
  )
}

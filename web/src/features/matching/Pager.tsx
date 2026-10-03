import { t } from '../../i18n'
import { Button } from '../../ui/Button'

/** «Назад / Дальше» под списком; ничего не рисует, если страница одна. */
export function Pager({ page, pages, onPage }: { page: number; pages: number; onPage: (n: number) => void }) {
  if (pages <= 1) return null
  const p = t.matching.pager
  return (
    <nav className="pager" aria-label={p.label}>
      <Button variant="secondary" disabled={page <= 1} onClick={() => onPage(page - 1)}>
        {p.prev}
      </Button>
      <span className="pager-text num">{p.page(page, pages)}</span>
      <Button variant="secondary" disabled={page >= pages} onClick={() => onPage(page + 1)}>
        {p.next}
      </Button>
    </nav>
  )
}

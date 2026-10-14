import type { Dataset } from './worst'

// Переключатель наборов данных подставного сервера (break-ui): только в разработке, внизу экрана, не часть дизайна.
// Выбор живёт в адресе (?data=worst), перезагрузка его сохраняет.

const LABELS: Record<Dataset, string> = { demo: 'Демо', worst: 'Худший случай', empty: 'Пусто', many: '1000 строк' }

export function datasetFromUrl(search: string): Dataset {
  const v = new URLSearchParams(search).get('data')
  return v === 'worst' || v === 'empty' || v === 'many' ? v : 'demo'
}

export function mountDataToggle(current: Dataset, doc: Document = document, go: (href: string) => void = (href) => window.location.assign(href)) {
  const bar = doc.createElement('div')
  bar.setAttribute('role', 'group')
  bar.setAttribute('aria-label', 'Данные подставного сервера чата')
  bar.dataset.chatMockToggle = 'true'
  Object.assign(bar.style, {
    position: 'fixed',
    left: '50%',
    bottom: '12px',
    transform: 'translateX(-50%)',
    zIndex: '1000',
    display: 'flex',
    gap: '2px',
    padding: '3px',
    background: '#e5e5e5',
    borderRadius: '999px',
    font: '12px system-ui, sans-serif',
  })
  for (const kind of Object.keys(LABELS) as Dataset[]) {
    const b = doc.createElement('button')
    b.type = 'button'
    b.textContent = LABELS[kind]
    b.setAttribute('aria-pressed', String(kind === current))
    Object.assign(b.style, {
      border: '0',
      borderRadius: '999px',
      padding: '4px 10px',
      cursor: 'pointer',
      background: kind === current ? '#fff' : 'transparent',
      color: '#222',
      font: 'inherit',
    })
    b.addEventListener('click', () => {
      const url = new URL(doc.location.href)
      if (kind === 'demo') url.searchParams.delete('data')
      else url.searchParams.set('data', kind)
      go(url.toString())
    })
    bar.append(b)
  }
  doc.body.append(bar)
  return bar
}

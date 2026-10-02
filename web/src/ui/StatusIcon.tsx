// Значки состояния, нарисованные одним штрихом. Цвет берут из currentColor.
export type StatusKind = 'ok' | 'fail' | 'pending'

const paths: Record<StatusKind, string> = {
  ok: 'M5 10.5l3.2 3.2L15 7',
  fail: 'M6.5 6.5l7 7M13.5 6.5l-7 7',
  pending: 'M6 10h.01M10 10h.01M14 10h.01',
}

export function StatusIcon({ kind }: { kind: StatusKind }) {
  return (
    <svg className="status-icon" viewBox="0 0 20 20" width="20" height="20" aria-hidden="true" focusable="false">
      <circle cx="10" cy="10" r="8.25" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <path d={paths[kind]} fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

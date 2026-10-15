import type { Side } from '../api'
import { author, demoState, orgViewer, scientistViewer } from './data'
import type { MockChat, MockMessage, MockState } from './types'

// Худшие правдоподобные данные для списка переписок (break-ui, срез 34). Пределы из сервера: название вакансии и организации
// до 200 знаков (vacancies.MaxTitleLen, orgs.MaxNameLen), имя человека до 120 (auth.MaxNameLen), сообщение до 4000 (CHAT.md),
// число непрочитанных не ограничено. Включается только в разработке: /chats?data=worst (переключатель внизу экрана).

export type Dataset = 'demo' | 'worst' | 'empty' | 'many'

const MIN = 60_000
const DAY = 24 * 60 * MIN

type Row = { org?: string; person?: string; anonymous?: boolean; vacancy: string; unread: number; text: string; at: number; status?: [('application' | 'offer'), string]; needs?: boolean; closed?: boolean }

const sciRows: Row[] = [
  {
    org: 'Федеральное государственное бюджетное учреждение науки Институт химической биологии и фундаментальной медицины Сибирского отделения Российской академии наук',
    vacancy: 'Старший научный сотрудник лаборатории молекулярной генетики и биоинформатики (CRISPR/Cas-редактирование генома, NGS-секвенирование, анализ данных single-cell RNA-seq)',
    unread: 1500,
    text: 'https://doi.org/10.1103/PhysRevLett.132.012345678901234567890123456789012345678901234567890',
    at: 3 * MIN,
    needs: true,
    status: ['application', 'invited'],
  },
  { org: 'НИЦ «Курчатовский институт» — PNPI / ПИЯФ', vacancy: 'Postdoc in Machine Learning для физики высоких энергий (LHCb, CERN collaboration)', unread: 1, text: 'Dear Dmitry, спасибо за CV! Could we schedule a call?', at: 50 * MIN, status: ['offer', 'pending'], needs: true },
  { org: 'ИОФ', vacancy: 'Лаборант', unread: 0, text: 'Да', at: 2 * DAY },
  { org: 'Технопарк «Академ-Север»', vacancy: 'Инженер', unread: 0, text: '\n\n\nОк\n\n', at: 400 * DAY, status: ['application', 'withdrawn'], closed: true },
  { org: 'Сибирский институт квантовых материалов', vacancy: 'Младший научный сотрудник', unread: 99, text: 'Б'.repeat(4000), at: 26 * 60 * MIN, status: ['application', 'rejected'], closed: true },
]

const orgRows: Row[] = [
  { person: 'Константинопольская-Оберхаузер Александра-Мария Вячеславовна', vacancy: 'Ведущий научный сотрудник отдела фундаментальных исследований в области физики конденсированного состояния и квантовых материалов (группа сверхпроводимости)', unread: 12345, text: 'Добрый день! Прикладываю ссылку на репозиторий: https://github.com/aleksandra-konstantinopolskaya-oberhauser/quantum-materials-simulation-toolkit', at: 1 * MIN },
  { person: 'Jo', vacancy: 'Аспирант', unread: 1, text: 'Ok', at: 20 * MIN },
  { anonymous: true, vacancy: 'Научный сотрудник группы спектроскопии льда и атмосферных аэрозолей Арктического региона', unread: 3, text: 'Расскажите о группе', at: 3 * 60 * MIN, status: ['offer', 'pending'] },
  { person: 'Đặng Thị Ngọc Hân', vacancy: 'Visiting researcher (Vietnam — Russia joint lab)', unread: 0, text: 'Cảm ơn! Спасибо!', at: 30 * DAY, status: ['application', 'accepted'] },
  { person: '王秀英', vacancy: 'Постдок', unread: 0, text: '你好, хорошо', at: 3 * DAY, status: ['offer', 'declined'], closed: true },
  { person: "Seán O'Brien-Ó Súilleabháin", vacancy: 'Научный сотрудник', unread: 0, text: '', at: 5 * DAY, needs: true, status: ['application', 'invited'] },
]


const LONG_LETTER =
  'Здравствуйте! Меня зовут Александра, я занимаюсь низкотемпературной физикой конденсированного состояния уже девять лет. ' +
  'В магистратуре работала над сверхпроводящими плёнками ниобия, в аспирантуре перешла к квантовым материалам и спектроскопии. '.repeat(4) +
  '\n\nОпыт: защитила диссертацию в 2021 году, опубликовала 14 статей (из них 9 в журналах первого квартиля), руководила двумя грантами РНФ. ' +
  'Готова к командировкам и работе на синхротроне. Прилагаю список публикаций и рекомендательное письмо.\n\n'.repeat(3)

const LONG_NOTE =
  'Спасибо за интерес к группе. Мы внимательно рассмотрели ваш отклик и обсудили его на заседании. К сожалению, сейчас мы выбрали кандидата с опытом работы на синхротроне. '.repeat(6)

/**
 * Одна переписка с самой неудобной лентой (break-ui, срез 35): 130 обычных сообщений (подгрузка вверх идёт страницами),
 * 4000 знаков без пробелов, длинная ссылка, одно слово, только переносы, сообщения в полночь, длинное письмо и записка.
 */
function worstThread(side: Side, now: number): { messages: MockMessage[]; counterpart: string } {
  const theirSide: Side = side === 'org' ? 'scientist' : 'org'
  const theirName = side === 'org' ? 'Константинопольская-Оберхаузер Александра-Мария' : 'Институт химической биологии и фундаментальной медицины СО РАН'
  const theirs = author(theirSide, side === 'org' ? theirName : 'Сотрудник кадровой службы', false)
  const mine = author(side, 'Вы', true)
  const midnight = new Date(now)
  midnight.setHours(0, 0, 0, 0)
  const m0 = midnight.getTime()
  const out: MockMessage[] = []
  const add = (at: number, who: 'mine' | 'theirs', text: string, extra: Partial<MockMessage> = {}) => {
    const seq = out.length + 1
    const created = new Date(at)
    out.push({
      id: `${side === 'org' ? '8' : '7'}0000000-0000-4000-8000-${String(seq).padStart(12, '0')}`,
      seq,
      kind: 'text',
      author: who === 'mine' ? mine : theirs,
      text,
      event: null,
      created_at: created.toISOString(),
      edited_at: null,
      deleted: false,
      editable_until: who === 'mine' ? new Date(at + DAY).toISOString() : null,
      client_id: null,
      from: who === 'mine' ? side : theirSide,
      ...extra,
    })
  }
  const system = (at: number, type: string, data: Record<string, unknown>, from: Side) => {
    const seq = out.length + 1
    out.push({
      id: `${side === 'org' ? '8' : '7'}0000000-0000-4000-8000-${String(seq).padStart(12, '0')}`,
      seq,
      kind: 'system',
      author: null,
      text: '',
      event: { type, data },
      created_at: new Date(at).toISOString(),
      edited_at: null,
      deleted: false,
      editable_until: null,
      client_id: null,
      from,
    })
  }
  const appId = side === 'org' ? 'b9000000-0000-4000-8000-000000000150' : 'b9000000-0000-4000-8000-000000000050'
  system(m0 - 20 * DAY, 'application.sent', { application_id: appId, cover_letter: LONG_LETTER }, 'scientist')
  for (let i = 0; i < 130; i++) {
    const at = m0 - 19 * DAY + i * (3 * 60 * MIN + 17 * MIN)
    add(at, i % 3 === 0 ? 'mine' : 'theirs', i % 7 === 0 ? `Сообщение ${i + 1}. Вопрос про график работы и возможность совмещения с преподаванием: как это устроено у вас?` : `Сообщение ${i + 1}`)
  }
  add(m0 - 3 * DAY, 'theirs', 'Да')
  add(m0 - 3 * DAY + MIN, 'mine', 'Первая строка\n\n\n\n\n\nПоследняя строка после пяти пустых')
  add(m0 - 2 * DAY, 'theirs', 'Ж'.repeat(4000))
  add(m0 - 2 * DAY + 5 * MIN, 'mine', `https://example.org/${'a'.repeat(3900)}`)
  add(m0 - 2 * DAY + 9 * MIN, 'theirs', 'Смотрите: https://doi.org/10.1103/PhysRevLett.132.012345?utm_source=chat&utm_medium=email&utm_campaign=quantum-materials-2026-autumn-call, а также (https://ru.wikipedia.org/wiki/Сверхпроводимость_(физика)).')
  add(m0 - 2 * DAY + 20 * MIN, 'theirs', 'Текст с <b>тегами</b> &amp; «кавычками» и javascript:alert(1), mailto:me@example.org', { edited_at: new Date(m0 - 2 * DAY + 25 * MIN).toISOString() })
  add(m0 - DAY, 'theirs', 'Это сообщение удалили', { deleted: true, text: '' })
  system(m0 - DAY + 60 * MIN, 'application.decided', { application_id: appId, decision: 'rejected', note: LONG_NOTE }, 'org')
  system(m0 - DAY + 61 * MIN, 'application.decided', { application_id: appId, decision: 'accepted', note: '' }, 'org')
  add(m0 - MIN, 'theirs', 'Написано в 23:59 вчера')
  add(m0 + MIN, 'mine', 'Написано в 00:01 сегодня')
  add(now - 60 * MIN, 'mine', 'Это моё сообщение, его ещё можно исправить или удалить в течение суток', { edited_at: new Date(now - 30 * MIN).toISOString() })
  add(now - 10 * MIN, 'theirs', 'Последнее входящее')
  return { messages: out, counterpart: theirName }
}

function worstChat(side: Side, now: number): MockChat {
  const { messages, counterpart } = worstThread(side, now)
  const n = side === 'org' ? 150 : 50
  const id = `c9000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const last = messages[messages.length - 1]
  const org = 'Федеральное государственное бюджетное учреждение науки Институт химической биологии и фундаментальной медицины Сибирского отделения РАН'
  return {
    id,
    side,
    vacancy: { id: `a9000000-0000-4000-8000-${String(n).padStart(12, '0')}`, title: 'Старший научный сотрудник лаборатории молекулярной генетики и биоинформатики (CRISPR/Cas-редактирование генома)', org_name: org, org_slug: 'worst', unit_name: 'Лаборатория молекулярной генетики и биоинформатики', status: 'published', deadline: null },
    counterpart: side === 'org' ? { kind: 'scientist', name: counterpart, anonymous: false, profile_id: null } : { kind: 'org', name: org, anonymous: false, profile_id: null },
    application_id: `b9000000-0000-4000-8000-${String(n).padStart(12, '0')}`,
    offer_id: null,
    status: { source: 'application', value: 'sent' },
    needs_answer: false,
    can_write: true,
    closed_reason: null,
    archived: false,
    viewer: side === 'org' ? orgViewer : scientistViewer,
    messages,
    read_mine: last.seq - 3,
    read_theirs: 30,
    updated_at: last.created_at,
  }
}

function build(rows: Row[], side: Side, now: number, offset: number): MockChat[] {
  return rows.map((r, i) => {
    const n = offset + i
    const id = `c9000000-0000-4000-8000-${String(n).padStart(12, '0')}`
    const at = new Date(now - r.at).toISOString()
    const counterpartSide: Side = side === 'org' ? 'scientist' : 'org'
    const name = side === 'org' ? (r.anonymous ? '' : (r.person ?? '')) : (r.org ?? '')
    const status = r.status ?? ['application', 'sent']
    const messages: MockChat['messages'] = []
    for (let k = 0; k < Math.max(1, r.unread); k++) {
      messages.push({
        id: `${id.slice(0, 24)}${String(k).padStart(12, '0')}`,
        seq: k + 1,
        kind: 'text',
        author: author(counterpartSide, name, false, { anonymous: r.anonymous ?? false }),
        text: r.text || 'Пусто',
        event: null,
        created_at: at,
        edited_at: null,
        deleted: false,
        editable_until: null,
        client_id: null,
        from: counterpartSide,
      })
    }
    const last = messages[messages.length - 1]
    return {
      id,
      side,
      vacancy: { id: `a9000000-0000-4000-8000-${String(n).padStart(12, '0')}`, title: r.vacancy, org_name: r.org ?? 'Центр изучения Арктики', org_slug: 'worst', unit_name: '', status: 'published', deadline: null },
      counterpart: side === 'org' ? { kind: 'scientist', name, anonymous: r.anonymous ?? false, profile_id: null } : { kind: 'org', name, anonymous: false, profile_id: null },
      application_id: status[0] === 'application' ? `b9000000-0000-4000-8000-${String(n).padStart(12, '0')}` : null,
      offer_id: status[0] === 'offer' ? `d9000000-0000-4000-8000-${String(n).padStart(12, '0')}` : null,
      status: { source: status[0], value: status[1] },
      needs_answer: r.needs ?? false,
      can_write: !r.closed,
      closed_reason: r.closed ? (status[0] === 'offer' ? 'offer_declined' : status[1] === 'withdrawn' ? 'withdrawn' : 'rejected') : null,
      archived: false,
      viewer: side === 'org' ? orgViewer : scientistViewer,
      messages,
      read_mine: r.unread > 0 ? last.seq - r.unread : last.seq,
      read_theirs: 0,
      updated_at: at,
    }
  })
}

/** Набор переписок для переключателя: демо, худший случай, пусто, 1000 строк у каждой стороны. */
export function dataset(kind: Dataset, nowDate: Date): MockState {
  const now = nowDate.getTime()
  const base = demoState(nowDate)
  if (kind === 'demo') return base
  if (kind === 'empty') return { ...base, chats: [] }
  if (kind === 'worst') return { ...base, chats: [worstChat('scientist', now), worstChat('org', now), ...build(sciRows, 'scientist', now, 1), ...build(orgRows, 'org', now, 100)] }
  const many = Array.from({ length: 1000 }, (_, i) => ({ ...sciRows[i % sciRows.length], at: i * 7 * MIN, unread: i % 4 === 0 ? (i % 17) + 1 : 0 }))
  const manyOrg = Array.from({ length: 1000 }, (_, i) => ({ ...orgRows[i % orgRows.length], at: i * 7 * MIN, unread: i % 5 === 0 ? 2 : 0 }))
  return { ...base, chats: [...build(many, 'scientist', now, 1000), ...build(manyOrg, 'org', now, 5000)] }
}

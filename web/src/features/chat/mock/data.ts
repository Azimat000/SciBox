import type { Author, ChatEvent, Side, Viewer } from '../api'
import type { MockChat, MockMessage, MockState } from './types'

// Демо-переписки подставного сервера (срез 34): вымышленные организации и люди из демо-данных сайта, реальные города.
// Время считается от «сейчас», поэтому переписки всегда свежие.

export const MOCK_ORG_ID = '70000000-0000-4000-8000-000000000001'

const DAY = 24 * 60 * 60 * 1000
const MIN = 60 * 1000

export const scientistViewer: Viewer = { side: 'scientist', can_use_templates: false, can_edit_templates: false, org_id: null, can_block: true, can_complain: true }
export const orgViewer: Viewer = { side: 'org', can_use_templates: true, can_edit_templates: true, org_id: MOCK_ORG_ID, can_block: false, can_complain: false }

const uuid = (prefix: string, n: number) => `${prefix}0000000-0000-4000-8000-${String(n).padStart(12, '0')}`

export const author = (side: Side, name: string, isMe: boolean, extra: Partial<Author> = {}): Author => ({
  side,
  name,
  is_me: isMe,
  anonymous: false,
  former: false,
  ...extra,
})

let serial = 0

type Draft = { from: Side; at: number; text?: string; author?: Author; event?: ChatEvent; edited?: number; deleted?: boolean }

/** Лента из черновиков: номера по порядку, своё сообщение получает срок правки и client_id. */
function feed(now: number, drafts: Draft[]): MockMessage[] {
  return drafts.map((d, i) => {
    const seq = i + 1
    const created = new Date(now - d.at)
    const mine = d.author?.is_me ?? false
    return {
      id: uuid('5', ++serial),
      seq,
      kind: d.event ? 'system' : 'text',
      author: d.event ? null : (d.author ?? null),
      text: d.deleted || d.event ? '' : (d.text ?? ''),
      event: d.event ?? null,
      created_at: created.toISOString(),
      edited_at: d.edited ? new Date(now - d.edited).toISOString() : null,
      deleted: d.deleted ?? false,
      editable_until: mine && !d.deleted && !d.event ? new Date(created.getTime() + DAY).toISOString() : null,
      client_id: mine && !d.event ? uuid('2', seq * 7 + d.at) : null,
      from: d.from,
    }
  })
}

const vacancy = (n: number, title: string, org: string, slug: string, unit: string, deadlineDays: number | null, now: number, status = 'published') => ({
  id: uuid('a', n),
  title,
  org_name: org,
  org_slug: slug,
  unit_name: unit,
  status,
  deadline: deadlineDays === null ? null : new Date(now + deadlineDays * DAY).toISOString().slice(0, 10),
})

const interview = (n: number, now: number, extra: Record<string, unknown> = {}) => ({
  id: uuid('f', n),
  kind: 'interview',
  status: 'pending',
  message: 'Приглашаем на собеседование с заведующим лабораторией. Расскажите о своих работах минут за десять.',
  starts_at: new Date(now + 8 * DAY).toISOString(),
  place_kind: 'online',
  place: 'https://telemost.yandex.ru/j/12345678901234',
  contact_name: '',
  contact_email: '',
  contact_phone: '',
  answer: null,
  created_at: new Date(now - 40 * MIN).toISOString(),
  can_answer: true,
  can_cancel: false,
  can_accept_proposal: false,
  ...extra,
})

function chat(fields: Omit<MockChat, 'read_mine' | 'read_theirs' | 'updated_at'> & { read_mine?: number; read_theirs?: number }): MockChat {
  const last = fields.messages[fields.messages.length - 1]
  const lastMine = [...fields.messages].reverse().find((m) => m.from === fields.side)
  return {
    ...fields,
    read_mine: fields.read_mine ?? fields.messages.length,
    read_theirs: fields.read_theirs ?? lastMine?.seq ?? 0,
    updated_at: last ? last.created_at : new Date().toISOString(),
  }
}

/** Пять переписок учёного и пять организации: разговор, приглашение ждёт ответа, отказ, анонимный соискатель, архив. */
export function demoState(nowDate: Date): MockState {
  const now = nowDate.getTime()
  const me = author('scientist', 'Дмитрий Королёв', true)
  const orlova = author('org', 'Елена Орлова', false)
  const staffMe = author('org', 'Елена Орлова', true)
  const petrov = author('org', 'Иван Петров', false)

  const sciChats: MockChat[] = [
    chat({
      id: uuid('c', 1),
      side: 'scientist',
      vacancy: vacancy(1, 'Младший научный сотрудник лаборатории спинтроники', 'Сибирский институт квантовых материалов', 'sibirskiy-institut-kvantovykh-materialov', 'Лаборатория спинтроники', 18, now),
      counterpart: { kind: 'org', name: 'Сибирский институт квантовых материалов', anonymous: false, profile_id: null },
      application_id: uuid('b', 1),
      offer_id: null,
      status: { source: 'application', value: 'invited' },
      needs_answer: true,
      can_write: true,
      closed_reason: null,
      archived: false,
      viewer: scientistViewer,
      messages: feed(now, [
        { from: 'scientist', at: 4 * DAY, event: { type: 'application.sent', data: { application_id: uuid('b', 1), cover_letter: 'Здравствуйте! Меня заинтересовала вакансия младшего научного сотрудника. Пять лет занимаюсь магнитной микроскопией тонких плёнок, в профиле есть публикации по теме.' } } },
        { from: 'org', at: 3 * DAY, event: { type: 'application.viewed', data: {} } },
        { from: 'org', at: 3 * DAY - 12 * MIN, author: author('org', 'Анна Белова', false), text: 'Дмитрий, добрый день! Ваш опыт с плёнками нам подходит. Пришлите, пожалуйста, ссылку на последнюю статью.', edited: 3 * DAY - 15 * MIN },
        { from: 'scientist', at: 3 * DAY - 40 * MIN, author: me, deleted: true },
        { from: 'scientist', at: 2 * DAY, author: me, text: 'Добрый день! Вот она: https://doi.org/10.5555/12345678\nБуду рад рассказать подробнее.' },
        { from: 'org', at: 40 * MIN, event: { type: 'invitation.created', data: { application_id: uuid('b', 1), invitation: interview(1, now) } } },
      ]),
      read_mine: 5,
    }),
    chat({
      id: uuid('c', 2),
      side: 'scientist',
      vacancy: vacancy(2, 'Научный сотрудник группы палеоклимата и реконструкции ледовых кернов Арктики и Антарктики', 'Центр изучения Арктики', 'tsentr-izucheniya-arktiki', '', 30, now),
      counterpart: { kind: 'org', name: 'Центр изучения Арктики', anonymous: false, profile_id: null },
      application_id: uuid('b', 2),
      offer_id: null,
      status: { source: 'application', value: 'viewed' },
      needs_answer: false,
      can_write: true,
      closed_reason: null,
      archived: false,
      viewer: scientistViewer,
      messages: feed(now, [
        { from: 'scientist', at: 6 * DAY, event: { type: 'application.sent', data: { application_id: uuid('b', 2), cover_letter: 'Добрый день! Изучаю изотопный состав льда, хотел бы продолжить работу в вашей группе.' } } },
        { from: 'org', at: 5 * DAY, event: { type: 'application.viewed', data: {} } },
        { from: 'org', at: 2 * 60 * MIN, author: orlova, text: 'Добрый день! Спасибо за отклик.' },
        { from: 'org', at: 2 * 60 * MIN - 2 * MIN, author: orlova, text: 'Подскажите, когда вам удобно созвониться на этой неделе? Нам важно обсудить экспедицию в марте.' },
      ]),
      read_mine: 2,
    }),
    chat({
      id: uuid('c', 3),
      side: 'scientist',
      vacancy: vacancy(3, 'Инженер-исследователь по оптическим нейроинтерфейсам', 'Нейрофотоника', 'neurofotonika', 'Отдел прототипирования', 12, now),
      counterpart: { kind: 'org', name: 'Нейрофотоника', anonymous: false, profile_id: null },
      application_id: null,
      offer_id: uuid('d', 3),
      status: { source: 'offer', value: 'pending' },
      needs_answer: true,
      can_write: true,
      closed_reason: null,
      archived: false,
      viewer: scientistViewer,
      messages: feed(now, [
        { from: 'org', at: 18 * 60 * MIN, event: { type: 'offer.created', data: { offer_id: uuid('d', 3), message: 'Здравствуйте! Видели ваши работы по флуоресцентной микроскопии. Посмотрите нашу вакансию: мы собираем прототип нейроинтерфейса.', status: 'pending', can_answer: true } } },
      ]),
      read_mine: 0,
    }),
    chat({
      id: uuid('c', 4),
      side: 'scientist',
      vacancy: vacancy(4, 'Старший преподаватель кафедры органической химии', 'Приволжский университет технологий', 'privolzhskiy-universitet-tekhnologiy', 'Кафедра органической химии', null, now, 'closed'),
      counterpart: { kind: 'org', name: 'Приволжский университет технологий', anonymous: false, profile_id: null },
      application_id: uuid('b', 4),
      offer_id: null,
      status: { source: 'application', value: 'rejected' },
      needs_answer: false,
      can_write: false,
      closed_reason: 'rejected',
      archived: false,
      viewer: scientistViewer,
      messages: feed(now, [
        { from: 'scientist', at: 12 * DAY, event: { type: 'application.sent', data: { application_id: uuid('b', 4), cover_letter: 'Здравствуйте! Преподаю органическую химию восемь лет, веду практикумы.' } } },
        { from: 'org', at: 11 * DAY, event: { type: 'application.viewed', data: {} } },
        { from: 'scientist', at: 10 * DAY, author: me, text: 'Добрый день! Подскажите, рассматривается ли совмещение с научной работой?' },
        { from: 'org', at: 9 * DAY, author: petrov, text: 'Да, кафедра поддерживает совмещение, ставка 0,5 возможна.' },
        { from: 'org', at: 2 * DAY, event: { type: 'application.decided', data: { application_id: uuid('b', 4), decision: 'rejected', note: 'Спасибо за интерес к кафедре. Сейчас мы выбрали кандидата с опытом руководства аспирантами.' } } },
      ]),
    }),
    chat({
      id: uuid('c', 5),
      side: 'scientist',
      vacancy: vacancy(5, 'Научный сотрудник лаборатории сверхпроводимости', 'Технопарк «Академ-Север»', 'tekhnopark-akadem-sever', '', null, now),
      counterpart: { kind: 'org', name: 'Технопарк «Академ-Север»', anonymous: false, profile_id: null },
      application_id: uuid('b', 5),
      offer_id: null,
      status: { source: 'application', value: 'accepted' },
      needs_answer: false,
      can_write: true,
      closed_reason: null,
      archived: true,
      viewer: scientistViewer,
      messages: feed(now, [
        { from: 'scientist', at: 40 * DAY, event: { type: 'application.sent', data: { application_id: uuid('b', 5), cover_letter: 'Добрый день! Откликаюсь на вакансию.' } } },
        { from: 'org', at: 30 * DAY, event: { type: 'application.decided', data: { application_id: uuid('b', 5), decision: 'accepted', note: 'Ждём вас первого числа.' } } },
        { from: 'scientist', at: 29 * DAY, author: me, text: 'Спасибо! До встречи.' },
      ]),
    }),
  ]

  const orgChats: MockChat[] = [
    chat({
      id: uuid('c', 6),
      side: 'org',
      vacancy: vacancy(6, 'Научный сотрудник группы спектроскопии льда', 'Центр изучения Арктики', 'tsentr-izucheniya-arktiki', 'Группа спектроскопии', 16, now),
      counterpart: { kind: 'scientist', name: 'Дмитрий Королёв', anonymous: false, profile_id: uuid('e', 1) },
      application_id: uuid('b', 6),
      offer_id: null,
      status: { source: 'application', value: 'sent' },
      needs_answer: false,
      can_write: true,
      closed_reason: null,
      archived: false,
      viewer: orgViewer,
      messages: feed(now, [
        { from: 'scientist', at: 25 * 60 * MIN, event: { type: 'application.sent', data: { application_id: uuid('b', 6), cover_letter: 'Добрый день! Работаю с рамановской спектроскопией льда, готов к экспедициям.' } } },
        { from: 'scientist', at: 50 * MIN, author: author('scientist', 'Дмитрий Королёв', false), text: 'Добавлю: у меня есть допуск к работе на НЭС «Академик Трёшников».' },
        { from: 'scientist', at: 30 * MIN, author: author('scientist', 'Дмитрий Королёв', false), text: 'Могу приехать на собеседование в любой день после 20 октября.' },
      ]),
      read_mine: 0,
    }),
    chat({
      id: uuid('c', 7),
      side: 'org',
      vacancy: vacancy(6, 'Научный сотрудник группы спектроскопии льда', 'Центр изучения Арктики', 'tsentr-izucheniya-arktiki', 'Группа спектроскопии', 16, now),
      counterpart: { kind: 'scientist', name: '', anonymous: true, profile_id: null },
      application_id: null,
      offer_id: uuid('d', 7),
      status: { source: 'offer', value: 'pending' },
      needs_answer: false,
      can_write: true,
      closed_reason: null,
      archived: false,
      viewer: orgViewer,
      messages: feed(now, [
        { from: 'org', at: 3 * DAY, event: { type: 'offer.created', data: { offer_id: uuid('d', 7), message: 'Здравствуйте! Мы ищем человека в группу спектроскопии льда. Посмотрите вакансию.', status: 'pending', can_answer: false } } },
        { from: 'scientist', at: 3 * 60 * MIN, author: author('scientist', '', false, { anonymous: true }), text: 'Добрый день! Расскажите, пожалуйста, о составе группы и об оборудовании.' },
      ]),
      read_mine: 1,
    }),
    chat({
      id: uuid('c', 8),
      side: 'org',
      vacancy: vacancy(8, 'Аспирант: машинное обучение для климатических моделей', 'Центр изучения Арктики', 'tsentr-izucheniya-arktiki', '', 25, now),
      counterpart: { kind: 'scientist', name: 'Алина Гусева', anonymous: false, profile_id: uuid('e', 2) },
      application_id: uuid('b', 8),
      offer_id: null,
      status: { source: 'application', value: 'invited' },
      needs_answer: true,
      can_write: true,
      closed_reason: null,
      archived: false,
      viewer: orgViewer,
      messages: feed(now, [
        { from: 'scientist', at: 5 * DAY, event: { type: 'application.sent', data: { application_id: uuid('b', 8), cover_letter: 'Здравствуйте! Заканчиваю магистратуру, диплом по нейросетевым моделям осадков.' } } },
        { from: 'org', at: 4 * DAY, event: { type: 'application.viewed', data: {} } },
        { from: 'org', at: 2 * DAY, event: { type: 'invitation.created', data: { application_id: uuid('b', 8), invitation: interview(8, now, { status: 'proposed', can_answer: false, can_cancel: true, can_accept_proposal: true, answer: { proposed_at: new Date(now + 9 * DAY).toISOString(), note: 'В этот день у меня защита, удобно на следующий день после обеда.', contact: '', time: '', answered_at: new Date(now - 20 * 60 * MIN).toISOString() } }) } } },
        { from: 'org', at: 2 * DAY - 5 * MIN, author: staffMe, text: 'Алина, приглашение отправили, ждём ответа.' },
        { from: 'scientist', at: 20 * 60 * MIN, event: { type: 'invitation.answered', data: { application_id: uuid('b', 8), invitation_id: uuid('f', 8), kind: 'interview', answer: 'proposed', proposed_at: new Date(now + 9 * DAY).toISOString(), note: 'В этот день у меня защита, удобно на следующий день после обеда.', contact: '', time: '' } } },
      ]),
      read_mine: 4,
    }),
    chat({
      id: uuid('c', 9),
      side: 'org',
      vacancy: vacancy(9, 'Старший преподаватель кафедры органической химии', 'Приволжский университет технологий', 'privolzhskiy-universitet-tekhnologiy', 'Кафедра органической химии', 9, now),
      counterpart: { kind: 'scientist', name: 'Ольга Лебедева', anonymous: false, profile_id: uuid('e', 3) },
      application_id: uuid('b', 9),
      offer_id: null,
      status: { source: 'application', value: 'withdrawn' },
      needs_answer: false,
      can_write: false,
      closed_reason: 'withdrawn',
      archived: false,
      viewer: orgViewer,
      messages: feed(now, [
        { from: 'scientist', at: 8 * DAY, event: { type: 'application.sent', data: { application_id: uuid('b', 9), cover_letter: 'Добрый день! Откликаюсь на должность старшего преподавателя.' } } },
        { from: 'org', at: 7 * DAY, author: author('org', '', false, { former: true }), text: 'Ольга, спасибо! Скоро вернёмся с ответом.' },
        { from: 'scientist', at: 3 * DAY, event: { type: 'application.withdrawn', data: { application_id: uuid('b', 9) } } },
      ]),
    }),
    chat({
      id: uuid('c', 10),
      side: 'org',
      vacancy: vacancy(6, 'Научный сотрудник группы спектроскопии льда', 'Центр изучения Арктики', 'tsentr-izucheniya-arktiki', 'Группа спектроскопии', 16, now),
      counterpart: { kind: 'scientist', name: 'Максим Тарасов', anonymous: false, profile_id: uuid('e', 4) },
      application_id: uuid('b', 10),
      offer_id: null,
      status: { source: 'application', value: 'accepted' },
      needs_answer: false,
      can_write: true,
      closed_reason: null,
      archived: true,
      viewer: orgViewer,
      messages: feed(now, [
        { from: 'scientist', at: 20 * DAY, event: { type: 'application.sent', data: { application_id: uuid('b', 10), cover_letter: 'Здравствуйте! Откликаюсь.' } } },
        { from: 'org', at: 6 * DAY, event: { type: 'application.decided', data: { application_id: uuid('b', 10), decision: 'accepted', note: '' } } },
        { from: 'org', at: 5 * DAY, author: petrov, text: 'Документы для оформления пришлём на почту до пятницы.' },
      ]),
    }),
  ]

  return { version: 1, chats: [...sciChats, ...orgChats], templates: {}, blocks: [], complaints: [], email_messages: true, next_id: 1000 }
}

/** Что «собеседник» пишет сам, пока открыт сайт: по фразе на сторону. */
export const incomingPhrases: Record<Side, string[]> = {
  // Пишет учёный (смотрит организация)
  scientist: [
    'Добрый день! Подскажите, пожалуйста, есть ли возможность частичной удалённой работы?',
    'Спасибо, приглашение получил. Уточню расписание и отвечу сегодня.',
    'Пришлю список публикаций за пять лет в течение часа.',
  ],
  // Пишет организация (смотрит учёный)
  org: [
    'Добрый день! Мы посмотрели ваш профиль, вопросов пока нет.',
    'Напомним: собеседование будет онлайн, ссылка в приглашении.',
    'Если удобно, можем перенести встречу на утро.',
  ],
}

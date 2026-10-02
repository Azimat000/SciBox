// Словарь интерфейса. Все тексты сайта живут здесь, не в разметке (D-021).
// Английский словарь появится позже с той же формой (тип Dictionary).

export const ru = {
  common: {
    toHome: 'На главную',
    retry: 'Проверить ещё раз',
    reload: 'Обновить страницу',
  },
  status: {
    title: 'Вакансии в науке',
    lead: 'Для учёных, которые ищут позицию, и для организаций, которые нанимают.',
    checkTitle: 'Связь с сервером',
    checking: 'Проверяем…',
    server: 'Сервер',
    database: 'База данных',
    serverOk: 'Отвечает',
    serverDown: 'Не отвечает',
    databaseOk: (schema: number) => `Работает, схема версии ${schema}`,
    databaseDown: 'Недоступна',
    databaseUnknown: 'Неизвестно, пока не отвечает сервер',
    serverDownHint: 'Сервер не запущен или упал. Запустите его командой make dev и проверьте ещё раз.',
    databaseDownHint: 'Сервер работает, но не достучался до базы. Проверьте, что Docker запущен: make db-up.',
    unexpectedHint: 'Сервер ответил странно. Подробности в журнале сервера.',
    version: (v: string) => `Версия сервера: ${v}`,
    note: 'Это служебная страница первого этапа. Поиск вакансий появится здесь позже.',
  },
  notFound: {
    title: 'Такой страницы нет',
    text: 'Возможно, ссылка устарела или в адресе опечатка.',
  },
  crash: {
    title: 'Что-то пошло не так',
    text: 'Страница сломалась. Обновите её; если не поможет, напишите нам, что вы делали перед этим.',
  },
  api: {
    unreachable: 'Сервер не отвечает',
    unknown: 'Сервер вернул непонятный ответ',
  },
} as const

type Widen<T> = T extends (...args: infer A) => infer R
  ? (...args: A) => Widen<R>
  : T extends string
    ? string
    : { [K in keyof T]: Widen<T[K]> }

export type Dictionary = Widen<typeof ru>

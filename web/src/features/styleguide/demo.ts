// Образцы данных для /styleguide. Организации и вакансии вымышленные, города настоящие (D-022).

import type { Option } from '../../ui/Select'
import type { VacancyEntryProps } from '../../ui/VacancyEntry'

export const swatches = [
  { name: '--paper', note: 'страница' },
  { name: '--wash', note: 'подложка' },
  { name: '--ink', note: 'текст' },
  { name: '--ink-soft', note: 'вторичный текст' },
  { name: '--line', note: 'линии' },
  { name: '--blue', note: 'действие' },
  { name: '--mark', note: 'маркер' },
  { name: '--ok', note: 'успех' },
  { name: '--fail', note: 'ошибка' },
] as const

export const cities: Option[] = [
  { value: 'msk', label: 'Москва' },
  { value: 'spb', label: 'Санкт-Петербург' },
  { value: 'nsk', label: 'Новосибирск' },
  { value: 'ekb', label: 'Екатеринбург' },
  { value: 'kzn', label: 'Казань' },
  { value: 'nn', label: 'Нижний Новгород' },
  { value: 'tsk', label: 'Томск' },
  { value: 'krs', label: 'Красноярск' },
  { value: 'pus', label: 'Пущино' },
  { value: 'dub', label: 'Дубна' },
  { value: 'chg', label: 'Черноголовка' },
  { value: 'tro', label: 'Троицк' },
  { value: 'yol', label: 'Йошкар-Ола' },
]

export const formats: Option[] = [
  { value: 'onsite', label: 'Очно' },
  { value: 'hybrid', label: 'Гибрид' },
  { value: 'remote', label: 'Удалённо' },
]

export const filterChips = ['Научные должности', 'ППС', 'Аспирантура и постдоки', 'Управление'] as const

type Demo = Omit<VacancyEntryProps, 'now' | 'headingLevel'>

export const entries: Demo[] = [
  {
    to: '#1',
    title: 'Старший научный сотрудник, лаборатория оптики конденсированных сред',
    organization: 'Институт прикладной оптики',
    city: 'Новосибирск',
    abstract:
      'Лаборатория ищет исследователя для работы над нелинейными эффектами в тонких плёнках. Нужен опыт ведения собственного гранта и публикации в рецензируемых журналах.',
    level: ['R3', 'состоявшийся исследователь'],
    facts: ['Физика и астрономия', 'Полная ставка, контракт 3 года'],
    competition: true,
    deadline: '2026-10-09',
  },
  {
    to: '#2',
    title: 'Постдок: машинное обучение в геофизике',
    organization: 'Центр вычислительных наук',
    city: 'Екатеринбург',
    abstract: 'Проект по восстановлению строения недр по сейсмическим данным. Работа в группе из шести человек, гибридный формат, предоставляется жильё.',
    level: ['R2', 'признанный исследователь'],
    facts: ['Науки о Земле', 'Контракт 2 года, от 110 000 ₽'],
    deadline: '2026-11-14',
  },
]

/** Худшие данные: очень длинное название, длинное слово без пробелов, нет аннотации, срок прошёл. */
export const worstEntry: Demo = {
  to: '#3',
  title:
    'Ведущий научный сотрудник отдела экспериментальной и теоретической физики высоких энергий и космических лучей с обязанностями заместителя руководителя научного направления',
  organization: 'Федеральное государственное бюджетное научное учреждение «Межрегиональный исследовательский институт фундаментальных проблем'.concat(' математического моделирования»'),
  city: 'Санкт-Петербург',
  level: ['R4', 'ведущий исследователь'],
  facts: ['Полная ставка', 'Сверхдлинноеназваниеспециальностибезпробеловдляпроверкипереноса'],
  competition: true,
  deadline: '2026-09-29',
}

export const minimalEntry: Demo = {
  to: '#4',
  title: 'Аспирант',
  organization: 'ИМС',
  city: 'Дубна',
}

// Сверка ответа с контрактом «по полям и типам» (contracts/chat/README.md). Сервер (срез 37) делает то же по тем же файлам.
import enumsFile from '../../../contracts/chat/enums.json'

export type Json = null | string | number | boolean | Json[] | { [key: string]: Json }

type Example = { request?: { query?: string; body?: Json }; status?: number; response?: Json }

export type ContractFile = {
  endpoint: string
  enums?: Record<string, string>
  request_enums?: Record<string, string>
  variants?: Record<string, string>
  /** Пути объектов-словарей: имена полей любые, значения одной формы. */
  maps?: string[]
  errors?: string[]
  examples: Record<string, Example>
}

type Field = { shape: Shape; required: boolean }
type Shape = {
  types: Set<string>
  keys?: Map<string, Field>
  items?: Shape
  /** Словарь: имена полей любые, у значений эта форма. */
  values?: Shape
  /** Объекты по этому пути различаются полем `key`: у каждого вида своя форма. */
  variants?: { key: string; byValue: Map<string, Shape> }
}

const enums = enumsFile as unknown as Record<string, string[]>

const typeOf = (v: Json): string => (v === null ? 'null' : Array.isArray(v) ? 'array' : typeof v)
const join = (path: string, key: string) => (path ? `${path}.${key}` : key)
const isObject = (v: Json): v is { [key: string]: Json } => typeOf(v) === 'object'

type Rules = { variants: Record<string, string>; maps: string[] }

function build(values: Json[], path: string, rules: Rules, skipVariant = false): Shape {
  const shape: Shape = { types: new Set(values.map(typeOf)) }
  const objects = values.filter(isObject)
  const variantKey = rules.variants[path]
  if (variantKey !== undefined && !skipVariant && objects.length > 0) {
    const groups = new Map<string, Json[]>()
    for (const o of objects) {
      const v = String(o[variantKey])
      groups.set(v, [...(groups.get(v) ?? []), o])
    }
    shape.variants = { key: variantKey, byValue: new Map([...groups].map(([v, group]) => [v, build(group, path, rules, true)])) }
    return shape
  }
  if (objects.length > 0 && rules.maps.includes(path)) {
    shape.values = build(objects.flatMap((o) => Object.values(o)), `${path}.*`, rules)
  } else if (objects.length > 0) {
    shape.keys = new Map()
    const names = new Set(objects.flatMap((o) => Object.keys(o)))
    for (const name of names) {
      const present = objects.filter((o) => name in o)
      shape.keys.set(name, { shape: build(present.map((o) => o[name]), join(path, name), rules), required: present.length === objects.length })
    }
  }
  const arrays = values.filter((v): v is Json[] => Array.isArray(v))
  const elements = arrays.flat()
  if (elements.length > 0) shape.items = build(elements, `${path}[]`, rules)
  return shape
}

function check(value: Json, shape: Shape, path: string, enumPaths: Record<string, string>, errors: string[]) {
  const where = path || '(корень)'
  const t = typeOf(value)
  if (!shape.types.has(t)) {
    errors.push(`${where}: тип ${t}, ожидался ${[...shape.types].join(' | ')}`)
    return
  }
  const enumName = enumPaths[path]
  if (enumName !== undefined && typeof value === 'string') {
    const allowed = enumName.split('|').flatMap((name) => enums[name] ?? [])
    if (!allowed.includes(value)) errors.push(`${where}: значение «${value}» не из перечня ${enumName}`)
  }
  if (shape.variants && isObject(value)) {
    const kind = String(value[shape.variants.key])
    const variant = shape.variants.byValue.get(kind)
    if (!variant) errors.push(`${where}: неизвестный вид ${shape.variants.key}=«${kind}»`)
    else check(value, variant, path, enumPaths, errors)
    return
  }
  if (shape.values && isObject(value)) {
    for (const v of Object.values(value)) check(v, shape.values, `${path}.*`, enumPaths, errors)
  }
  if (shape.keys && isObject(value)) {
    for (const [name, field] of shape.keys) {
      if (field.required && !(name in value)) errors.push(`${where}: нет поля ${name}`)
    }
    for (const [name, v] of Object.entries(value)) {
      const field = shape.keys.get(name)
      if (!field) errors.push(`${where}: лишнее поле ${name}`)
      else check(v, field.shape, join(path, name), enumPaths, errors)
    }
  }
  if (Array.isArray(value) && shape.items) {
    for (const item of value) check(item, shape.items, `${path}[]`, enumPaths, errors)
  }
}

const responses = (file: ContractFile) => Object.values(file.examples).flatMap((e) => (e.response === undefined ? [] : [e.response]))
const bodies = (file: ContractFile) => Object.values(file.examples).flatMap((e) => (e.request?.body === undefined ? [] : [e.request.body]))

/** Что не так в ответе по сравнению с примерами файла: пустой список, если подходит. */
export function responseErrors(file: ContractFile, value: unknown): string[] {
  const examples = responses(file)
  if (examples.length === 0) return value === undefined ? [] : ['у адреса ответ без тела']
  const errors: string[] = []
  check(value as Json, build(examples, '', { variants: file.variants ?? {}, maps: file.maps ?? [] }), '', file.enums ?? {}, errors)
  return errors
}

/** Что не так в теле запроса по сравнению с примерами файла. */
export function requestErrors(file: ContractFile, value: unknown): string[] {
  const examples = bodies(file)
  if (examples.length === 0) return value === undefined || (isObject(value as Json) && Object.keys(value as object).length === 0) ? [] : ['у адреса запрос без тела']
  const errors: string[] = []
  check(value as Json, build(examples, '', { variants: {}, maps: [] }), '', file.request_enums ?? {}, errors)
  return errors
}

/** Все перечни контракта: имя → допустимые значения. */
export const contractEnums = enums

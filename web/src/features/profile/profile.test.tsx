import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { field } from '../../test/forms'
import { renderApp } from '../../test/render'
import { book, education, emptyProfile, otherPage, ownPage, profile, PROFILE_ID, publication } from '../../test/profile'
import { reference } from '../../test/vacancies'

const own = (page = ownPage(), extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/profile': reply(200, page),
  ...extra,
})
const click = async (name: string | RegExp) => userEvent.click(await screen.findByRole('button', { name }))
const section = async (title: string) => within((await screen.findByRole('heading', { level: 2, name: title })).closest('section')!)

describe('own profile page', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/profile')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('sets the profile out like a journal page with every section and the owner toolbar', async () => {
    stubApi(own())
    renderApp('/profile')
    expect(await screen.findByRole('heading', { level: 1, name: 'Елена Орлова' })).toBeInTheDocument()
    expect(screen.getByText('Старший научный сотрудник, лаборатория катализа')).toBeInTheDocument()
    expect(screen.getByText('Новосибирск, Новосибирская область')).toBeInTheDocument()
    expect(screen.getByText('Открыт к предложениям', { selector: '.tag' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'orlova@example.ru' })).toHaveAttribute('href', 'mailto:orlova@example.ru')
    expect(screen.getByRole('link', { name: 'Изменить основное' })).toHaveAttribute('href', '/profile/edit')
    expect(screen.getByRole('link', { name: 'Скачать резюме (PDF)' })).toHaveAttribute('href', '/api/profile/cv')
    expect(screen.getByRole('link', { name: 'Как видят другие' })).toHaveAttribute('href', `/scientists/${PROFILE_ID}`)

    expect((await section('О себе')).getByText('Изучаю активные центры катализаторов.')).toBeInTheDocument()
    const degree = await section('Степень и звание')
    expect(degree.getByText('Кандидат наук')).toBeInTheDocument()
    expect(degree.getByText('Диссертация: «Активные центры оксидных катализаторов»')).toBeInTheDocument()
    expect(degree.getByText('Институт катализа')).toBeInTheDocument()
    expect(degree.getByText('2016')).toBeInTheDocument()
    expect(degree.getByText('Доцент')).toBeInTheDocument()
    expect((await section('Научные специальности')).getByText('Физическая химия')).toBeInTheDocument()
    expect((await section('Научные навыки')).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['ИК-спектроскопия', 'Рентгеновская дифракция'])
    expect((await section('Общие навыки')).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['Английский B2'])

    const ids = await section('Идентификаторы и метрики')
    expect(ids.getByRole('link', { name: 'Профиль ORCID 0000-0002-1825-0097 на orcid.org' })).toHaveAttribute('href', 'https://orcid.org/0000-0002-1825-0097')
    expect(ids.getByText('1234-5678')).toBeInTheDocument()
    expect(ids.getByText('57190123456')).toBeInTheDocument()
    expect(ids.getByText('A-1234-2008')).toBeInTheDocument()
    expect(ids.getAllByRole('listitem').map((li) => li.textContent)).toEqual(['РИНЦ 12', 'Scopus 9', 'Google Scholar 15'])

    expect((await section('Образование')).getByText('2010 — 2014')).toBeInTheDocument()
    expect((await section('Опыт работы')).getByText('2018 — н. в.')).toBeInTheDocument()
    const pubs = await section('Публикации')
    expect(pubs.getByText('Орлова Е. А., Белов И. С. Журнал физической химии, 2023. Т. 97, № 4, С. 512–520.')).toBeInTheDocument()
    expect(pubs.getByRole('link', { name: 'DOI 10.1234/abc.2023' })).toHaveAttribute('href', 'https://doi.org/10.1234/abc.2023')
    expect(pubs.getByRole('link', { name: 'example.ru' })).toHaveAttribute('href', 'https://example.ru/book')
    expect(pubs.getByText('Книга')).toBeInTheDocument()
    expect((await section('Гранты')).getByText('РНФ, № 24-12-00177, Руководитель')).toBeInTheDocument()
    expect((await section('Патенты и программы')).getByText('Изобретение, № RU 2 745 123, Роспатент')).toBeInTheDocument()
    expect((await section('Преподавание')).getByText('НГУ, Магистратура')).toBeInTheDocument()
  })

  it('shows hints instead of empty sections and says a hidden profile is seen only by its owner', async () => {
    stubApi(own(ownPage(emptyProfile)))
    renderApp('/profile')
    expect(await screen.findByText('Профиль скрыт: эту страницу видите только вы.')).toBeInTheDocument()
    for (const hint of [
      'Расскажите о себе и своей работе в нескольких предложениях.',
      'Методы, приборы и программы, с которыми вы работаете в исследованиях.',
      'То, что пригодится не только в науке: компьютер, языки, работа с людьми.',
      'Степень и звание пока не указаны.',
      'Выберите до пяти специальностей: по ним вас найдут организации.',
      'Идентификаторы и h-index пока не указаны.',
      'Добавьте вуз, аспирантуру, стажировки.',
      'Добавьте места работы, начиная с нынешнего.',
      'Добавьте публикации: можно по DOI, форма заполнится сама.',
      'Добавьте гранты, в которых вы участвовали.',
      'Добавьте патенты и свидетельства на программы и базы данных.',
      'Добавьте курсы, которые вы читаете или читали.',
    ])
      expect(screen.getByText(hint)).toBeInTheDocument()
    expect(screen.queryByText('Открыт к предложениям', { selector: '.tag' })).not.toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /Никто/ })).toBeChecked()
  })

  it('says what went wrong when the profile does not load, and tries again', async () => {
    let fail = true
    stubApi(own(ownPage(), { 'GET /api/profile': () => (fail ? apiError(500, 'internal', 'Что-то сломалось') : reply(200, ownPage())) }))
    renderApp('/profile')
    expect(await screen.findByRole('heading', { name: 'Не удалось загрузить профиль' })).toBeInTheDocument()
    fail = false
    await click('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Елена Орлова' })).toBeInTheDocument()
  })
})

describe('privacy panel', () => {
  it('saves the chosen mode at once and says so', async () => {
    const api = stubApi(own(ownPage(), { 'PUT /api/profile/privacy': reply(200, ownPage({ ...profile, visibility: 'orgs' })) }))
    renderApp('/profile')
    expect(await screen.findByRole('radio', { name: /Все/ })).toBeChecked()
    await userEvent.click(screen.getByRole('radio', { name: /Организации/ }))
    expect(await screen.findByText('Приватность сохранена')).toBeInTheDocument()
    expect(api.called('PUT', '/api/profile/privacy')[0].body).toEqual({ visibility: 'orgs', open_to_offers: true })
  })

  it('saves the "open to offers" mark with the current mode', async () => {
    const api = stubApi(own(ownPage(), { 'PUT /api/profile/privacy': reply(200, ownPage({ ...profile, open_to_offers: false })) }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Открыт к предложениям' }))
    await screen.findByText('Приватность сохранена')
    expect(api.called('PUT', '/api/profile/privacy')[0].body).toEqual({ visibility: 'public', open_to_offers: false })
  })

  it('tells when saving failed', async () => {
    stubApi(own(ownPage(), { 'PUT /api/profile/privacy': apiError(500, 'internal', 'Сбой на сервере') }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('radio', { name: /Никто/ }))
    expect(await screen.findByText('Не удалось сохранить приватность')).toBeInTheDocument()
    expect(screen.getByText('Сбой на сервере')).toBeInTheDocument()
  })
})

describe('items', () => {
  it('adds an education: years go as numbers, empty fields are not sent', async () => {
    const api = stubApi(own(ownPage(emptyProfile), { 'POST /api/profile/items': reply(201, { item: education }) }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Образование»' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Добавить: образование' }))
    await userEvent.type(dialog.getByLabelText(/Учебное заведение/), 'НГУ')
    await userEvent.type(dialog.getByLabelText(/С года/), '2010')
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Запись добавлена')).toBeInTheDocument()
    expect(api.called('POST', '/api/profile/items')[0].body).toEqual({ kind: 'education', institution: 'НГУ', year_from: 2010 })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows the server problems under the fields and keeps the window open', async () => {
    stubApi(own(ownPage(emptyProfile), { 'POST /api/profile/items': apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { institution: 'Укажите учебное заведение', year_from: 'Укажите год начала' } }) }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Образование»' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Укажите учебное заведение')).toBeInTheDocument()
    expect(screen.getByText('Укажите год начала')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('shows a general problem above the form', async () => {
    stubApi(own(ownPage(emptyProfile), { 'POST /api/profile/items': apiError(409, 'too_many_items', 'В этом разделе уже слишком много записей') }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Гранты»' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('В этом разделе уже слишком много записей')).toBeInTheDocument()
  })

  it('closes the window on cancel without saving', async () => {
    const api = stubApi(own(ownPage(emptyProfile)))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Патенты и программы»' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(api.called('POST', '/api/profile/items')).toHaveLength(0)
  })

  it('edits an item: the form holds the saved values and the kind goes with the change', async () => {
    const api = stubApi(own(ownPage(), { [`PUT /api/profile/items/${publication.id}`]: reply(200, { item: publication }) }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Изменить: Активные центры катализаторов' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Изменить: публикации' }))
    expect(dialog.getByLabelText(/Название/)).toHaveValue('Активные центры катализаторов')
    expect(dialog.getByLabelText(/Год/)).toHaveValue(2023)
    expect(dialog.getByLabelText(/DOI/)).toHaveValue('10.1234/abc.2023')
    await userEvent.clear(dialog.getByLabelText(/Название/))
    await userEvent.type(dialog.getByLabelText(/Название/), 'Новое название')
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Запись сохранена')).toBeInTheDocument()
    const body = api.called('PUT', `/api/profile/items/${publication.id}`)[0].body as Record<string, unknown>
    expect(body).toMatchObject({ kind: 'publication', title: 'Новое название', year: 2023, doi: '10.1234/abc.2023', pub_type: 'article', source: 'manual' })
  })

  it('removes an item after a confirmation', async () => {
    const api = stubApi(own(ownPage(), { [`DELETE /api/profile/items/${book.id}`]: reply(204) }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Удалить: Вычислительные методы' }))
    expect(await screen.findByText('«Вычислительные методы» исчезнет из профиля. Вернуть запись нельзя.')).toBeInTheDocument()
    expect(api.called('DELETE', `/api/profile/items/${book.id}`)).toHaveLength(0)
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Удалить' }))
    expect(await screen.findByText('Запись удалена')).toBeInTheDocument()
    expect(api.called('DELETE', `/api/profile/items/${book.id}`)).toHaveLength(1)
  })

  it('does not remove anything when the confirmation is cancelled', async () => {
    const api = stubApi(own())
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Удалить: Вычислительные методы' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    expect(api.calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)
  })

  it('tells when removing failed', async () => {
    stubApi(own(ownPage(), { [`DELETE /api/profile/items/${book.id}`]: apiError(404, 'not_found', 'Такого профиля нет') }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Удалить: Вычислительные методы' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Удалить' }))
    expect(await screen.findByText('Не удалось удалить запись')).toBeInTheDocument()
  })

  it('has a form for every kind of item', async () => {
    stubApi(own(ownPage(emptyProfile)))
    renderApp('/profile')
    const forms: [string, string[]][] = [
      ['Образование', ['Учебное заведение', 'Программа или специальность', 'С года', 'По год', 'Описание']],
      ['Опыт работы', ['Должность', 'Организация', 'С года']],
      ['Публикации', ['DOI', 'Название', 'Авторы', 'Тип', 'Год', 'Том', 'Номер', 'Страницы', 'Ссылка']],
      ['Гранты', ['Название', 'Кто финансирует', 'Номер', 'Роль в проекте']],
      ['Патенты и программы', ['Название', 'Вид документа', 'Номер', 'Ведомство', 'Авторы', 'Год']],
      ['Преподавание', ['Название курса', 'Учебное заведение', 'Уровень']],
    ]
    for (const [title, labels] of forms) {
      await userEvent.click(await screen.findByRole('button', { name: `Добавить в раздел «${title}»` }))
      const dialog = within(await screen.findByRole('dialog'))
      for (const label of labels) expect(dialog.getAllByLabelText(new RegExp(label)).length).toBeGreaterThan(0)
      await userEvent.click(dialog.getByRole('button', { name: 'Отмена' }))
    }
  })
})

describe('publication by DOI', () => {
  const work = { doi: '10.1038/nature12373', title: 'Nanometre-scale thermometry', authors: 'Kucsko G., Maurer P. C.', venue: 'Nature', year: 2013, type: 'article', volume: '500', issue: '7460', pages: '54-58' }
  const open = async () => {
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Публикации»' }))
    return within(await screen.findByRole('dialog'))
  }

  it('fills the form from Crossref and saves it marked as taken from there', async () => {
    const api = stubApi(own(ownPage(emptyProfile), { 'GET /api/profile/doi*': reply(200, { work }), 'POST /api/profile/items': reply(201, { item: publication }) }))
    renderApp('/profile')
    const dialog = await open()
    await userEvent.type(dialog.getByLabelText(/Найти по DOI/), ' https://doi.org/10.1038/nature12373 ')
    await userEvent.click(dialog.getByRole('button', { name: 'Найти' }))
    expect(await dialog.findByText('Нашли. Проверьте поля ниже и сохраните.')).toBeInTheDocument()
    expect(dialog.getByLabelText(/Название/)).toHaveValue('Nanometre-scale thermometry')
    expect(dialog.getByLabelText(/Авторы/)).toHaveValue('Kucsko G., Maurer P. C.')
    expect(dialog.getByLabelText(/Журнал, сборник/)).toHaveValue('Nature')
    expect(dialog.getByLabelText(/Год/)).toHaveValue(2013)
    expect(dialog.getByLabelText(/Тип/)).toHaveValue('article')
    expect(api.called('GET', '/api/profile/doi?doi=https%3A%2F%2Fdoi.org%2F10.1038%2Fnature12373')).toHaveLength(1)
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    await screen.findByText('Запись добавлена')
    expect(api.called('POST', '/api/profile/items')[0].body).toMatchObject({ kind: 'publication', doi: '10.1038/nature12373', source: 'crossref', pub_type: 'article', year: 2013, volume: '500' })
  })

  it('fills in what Crossref knows and leaves the rest empty', async () => {
    stubApi(own(ownPage(emptyProfile), { 'GET /api/profile/doi*': reply(200, { work: { ...work, year: undefined, type: '' } }) }))
    renderApp('/profile')
    const dialog = await open()
    await userEvent.type(dialog.getByLabelText(/Найти по DOI/), '10.1038/nature12373')
    await userEvent.keyboard('{Enter}')
    await dialog.findByText('Нашли. Проверьте поля ниже и сохраните.')
    expect(dialog.getByLabelText(/Год/)).toHaveValue(null)
    expect(dialog.getByLabelText(/Тип/)).toHaveValue('article')
  })

  it('asks for a DOI when the field is empty', async () => {
    const api = stubApi(own(ownPage(emptyProfile)))
    renderApp('/profile')
    const dialog = await open()
    await userEvent.click(dialog.getByRole('button', { name: 'Найти' }))
    expect(await dialog.findByText('Вставьте DOI')).toBeInTheDocument()
    expect(api.calls.filter((c) => c.path.startsWith('/api/profile/doi'))).toHaveLength(0)
  })

  it.each([
    ['unknown DOI', apiError(404, 'doi_not_found', 'Crossref не знает такого DOI. Проверьте номер или заполните публикацию вручную'), 'Crossref не знает такого DOI. Проверьте номер или заполните публикацию вручную'],
    ['Crossref down', apiError(502, 'doi_unavailable', 'Crossref сейчас недоступен. Заполните публикацию вручную'), 'Crossref сейчас недоступен. Заполните публикацию вручную'],
    ['bad format', apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { doi: 'DOI записывают так: 10.1038/nature12373' } }), 'DOI записывают так: 10.1038/nature12373'],
  ])('keeps the form manual when %s', async (_name, response, message) => {
    stubApi(own(ownPage(emptyProfile), { 'GET /api/profile/doi*': response }))
    renderApp('/profile')
    const dialog = await open()
    await userEvent.type(dialog.getByLabelText(/Найти по DOI/), 'x')
    await userEvent.click(dialog.getByRole('button', { name: 'Найти' }))
    expect(await dialog.findByText(message)).toBeInTheDocument()
    await userEvent.type(dialog.getByLabelText(/Название/), 'Вручную')
    expect(dialog.getByLabelText(/Название/)).toHaveValue('Вручную')
  })

  it('says the server cannot be reached', async () => {
    stubApi(own(ownPage(emptyProfile), { 'GET /api/profile/doi*': { status: 500 } }))
    renderApp('/profile')
    const dialog = await open()
    await userEvent.type(dialog.getByLabelText(/Найти по DOI/), 'x')
    await userEvent.click(dialog.getByRole('button', { name: 'Найти' }))
    expect(await dialog.findByText(/Не удалось связаться с сервером|не отвечает|недоступен/i)).toBeInTheDocument()
  })

  it('shows a DOI problem the server found when saving', async () => {
    stubApi(own(ownPage(emptyProfile), { 'POST /api/profile/items': apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { doi: 'Публикация с этим DOI уже есть в вашем профиле' } }) }))
    renderApp('/profile')
    const dialog = await open()
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    expect(await dialog.findByText('Публикация с этим DOI уже есть в вашем профиле')).toBeInTheDocument()
  })
})

describe('details of the profile page', () => {
  it('shows a degree and a title without years', async () => {
    stubApi(own(ownPage({ ...profile, degree: { ...profile.degree, year: null, specialty: null, institution: '', dissertation: '' }, academic_title_year: null })))
    renderApp('/profile')
    const degree = await section('Степень и звание')
    expect(degree.getByText('Кандидат наук')).toBeInTheDocument()
    expect(degree.getByText('Доцент')).toBeInTheDocument()
    expect(degree.queryByText(/Диссертация/)).not.toBeInTheDocument()
  })

  it('treats a profile without a stored mode as hidden', async () => {
    const { visibility: _omit, ...rest } = profile
    void _omit
    stubApi(own(ownPage(rest as typeof profile)))
    renderApp('/profile')
    expect(await screen.findByRole('radio', { name: /Никто/ })).toBeChecked()
  })

  it('shows the chosen mode at once while saving', async () => {
    let release: (r: ReturnType<typeof reply>) => void = () => {}
    const pending = new Promise<ReturnType<typeof reply>>((resolve) => (release = resolve))
    stubApi(own(ownPage(), { 'PUT /api/profile/privacy': () => pending }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('radio', { name: /Никто/ }))
    await waitFor(() => expect(screen.getByRole('radio', { name: /Никто/ })).toBeChecked())
    expect(screen.getByRole('radio', { name: /Все/ })).toBeDisabled()
    release(reply(200, ownPage({ ...profile, visibility: 'hidden' })))
    await screen.findByText('Приватность сохранена')
  })

  it('types into link and description fields of the forms', async () => {
    stubApi(own(ownPage(emptyProfile)))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Публикации»' }))
    await userEvent.type(screen.getByLabelText(/Ссылка/), 'https://example.ru/a')
    expect(screen.getByLabelText(/Ссылка/)).toHaveValue('https://example.ru/a')
    await userEvent.click(screen.getByRole('button', { name: 'Отмена' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Образование»' }))
    await userEvent.type(screen.getByLabelText(/Описание/), 'Диплом с отличием')
    expect(screen.getByLabelText(/Описание/)).toHaveValue('Диплом с отличием')
  })
})

describe('profile of another person', () => {
  const path = `/scientists/${PROFILE_ID}`

  it('shows the profile without owner buttons; a visitor is asked to sign in for the resume', async () => {
    stubApi({ [`GET /api/scientists/${PROFILE_ID}`]: reply(200, otherPage()) })
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: 'Елена Орлова' })).toBeInTheDocument()
    expect(screen.getByText('Изучаю активные центры катализаторов.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Добавить/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Удалить/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Изменить основное' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /@/ })).not.toBeInTheDocument()
    expect(screen.getByText('Войдите, чтобы скачать резюме.')).toBeInTheDocument()
  })

  it('shows the contact mail to those allowed and a resume link to a signed-in person', async () => {
    stubApi({ ...signedInAs(ann), [`GET /api/scientists/${PROFILE_ID}`]: reply(200, otherPage(profile, true)) })
    renderApp(path)
    expect(await screen.findByRole('link', { name: 'orlova@example.ru' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Скачать резюме (PDF)' })).toHaveAttribute('href', `/api/scientists/${PROFILE_ID}/cv`)
  })

  it('hides empty sections from other people', async () => {
    stubApi({ [`GET /api/scientists/${PROFILE_ID}`]: reply(200, otherPage({ ...emptyProfile, name: 'Анна Смирнова' })) })
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: 'Анна Смирнова' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2 })).not.toBeInTheDocument()
  })

  it('says there is no such profile when it is hidden or missing', async () => {
    stubApi({ [`GET /api/scientists/${PROFILE_ID}`]: apiError(404, 'not_found', 'Такого профиля нет') })
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 2, name: 'Такого профиля нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'На главную' })).toHaveAttribute('href', '/')
  })

  it('says what went wrong on another failure, and tries again', async () => {
    let fail = true
    stubApi({ [`GET /api/scientists/${PROFILE_ID}`]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, otherPage())) })
    renderApp(path)
    expect(await screen.findByRole('heading', { name: 'Не удалось загрузить профиль' })).toBeInTheDocument()
    fail = false
    await click('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Елена Орлова' })).toBeInTheDocument()
  })

  it("reminds the owner that this is how others see the page", async () => {
    stubApi({ ...signedInAs(ann), [`GET /api/scientists/${PROFILE_ID}`]: reply(200, ownPage()) })
    renderApp(path)
    expect(await screen.findByText('Так видят вашу страницу другие, если профиль открыт.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Вернуться к редактированию' })).toHaveAttribute('href', '/profile')
  })

  it('handles long names, a very long headline and an unparsable link', async () => {
    const long = 'Иванова-Петрова-Сидорова-Кузнецова Анастасия Константиновна'
    const weird = { ...book, url: 'not a url at all' }
    stubApi({ [`GET /api/scientists/${PROFILE_ID}`]: reply(200, otherPage({ ...profile, name: long, headline: 'я'.repeat(300), sections: { ...profile.sections, publications: [weird] } })) })
    renderApp(path)
    const h1 = await screen.findByRole('heading', { level: 1, name: long })
    expect(h1).toHaveAttribute('data-long', 'true')
    expect(screen.getByRole('link', { name: 'not a url at all' })).toBeInTheDocument()
  })
})

describe('profile edit page', () => {
  const base = (page = ownPage(), extra: Record<string, Route> = {}) => own(page, { 'GET /api/reference': reply(200, reference), ...extra })

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/profile/edit')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('holds the saved values and sends them back with numbers as numbers', async () => {
    const api = stubApi(base(ownPage(), { 'PUT /api/profile': reply(200, ownPage()) }))
    const { router } = renderApp('/profile/edit')
    expect(await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })).toBeInTheDocument()
    expect(field(/Должность и место работы/)).toHaveValue(profile.headline)
    expect(field(/^Город/)).toHaveValue('Новосибирск')
    expect(field(/О себе/)).toHaveValue(profile.about)
    expect(field(/Учёная степень/)).toHaveValue('candidate')
    expect(field(/Год защиты/)).toHaveValue(2016)
    expect(field(/ORCID iD/)).toHaveValue('0000-0002-1825-0097')
    expect(field(/Контактная почта/)).toHaveValue('orlova@example.ru')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Профиль сохранён')).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.pathname).toBe('/profile'))
    expect(api.called('PUT', '/api/profile')[0].body).toEqual({
      headline: profile.headline, city: 'Новосибирск', region_code: '54', about: profile.about,
      degree: 'candidate', degree_specialty_code: '1.4.4', degree_year: 2016, degree_institution: 'Институт катализа',
      dissertation_title: 'Активные центры оксидных катализаторов', academic_title: 'docent', academic_title_year: 2021,
      orcid: '0000-0002-1825-0097', spin: '12345678', scopus_id: '57190123456', wos_id: 'A-1234-2008',
      h_rsci: 12, h_scopus: 9, h_wos: null, h_scholar: 15, contact_email: 'orlova@example.ru', specialties: ['1.4.4'],
      research_skills: ['ИК-спектроскопия', 'Рентгеновская дифракция'], general_skills: ['Английский B2'],
    })
  })

  it('adds skills by Enter, comma, button, paste and leaving the field, drops repeats and removes by the cross', async () => {
    const api = stubApi(base(ownPage(emptyProfile), { 'PUT /api/profile': reply(200, ownPage(emptyProfile)) }))
    renderApp('/profile/edit')
    await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })
    const research = field(/^Научные навыки ·/)
    const general = field(/^Общие навыки ·/)
    await userEvent.type(research, '  ПЦР   в реальном времени {Enter}')
    await userEvent.type(research, 'Python,')
    await userEvent.type(research, 'python{Enter}')
    expect(research).toHaveValue('')
    await userEvent.type(research, 'COMSOL')
    await userEvent.click(screen.getAllByRole('button', { name: 'Добавить' })[0])
    expect(screen.getByRole('list', { name: 'Научные навыки: добавлено' }).textContent).toBe('ПЦР в реальном времениPythonCOMSOL')
    expect(screen.getAllByText('3 из 30')).toHaveLength(1)
    await userEvent.click(screen.getByRole('button', { name: 'Убрать навык «Python»' }))

    await userEvent.click(general)
    await userEvent.paste('Word, Excel; английский B2\nWord')
    await userEvent.type(general, 'Водительские права')
    await userEvent.tab() // недописанное не теряется, когда поле теряет фокус
    expect(within(screen.getByRole('list', { name: 'Общие навыки: добавлено' })).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['Word', 'Excel', 'английский B2', 'Водительские права'])

    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    await screen.findByText('Профиль сохранён')
    expect(api.called('PUT', '/api/profile')[0].body).toMatchObject({
      research_skills: ['ПЦР в реальном времени', 'COMSOL'],
      general_skills: ['Word', 'Excel', 'английский B2', 'Водительские права'],
    })
  })

  it('pastes plain text into a skill as usual and shows the server problem under the list', async () => {
    stubApi(base(ownPage(), { 'PUT /api/profile': apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { general_skills: 'Навыков в списке не больше 30' } }) }))
    renderApp('/profile/edit')
    await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })
    const general = field(/^Общие навыки ·/)
    await userEvent.click(general)
    await userEvent.paste('Публичные выступления')
    expect(general).toHaveValue('Публичные выступления')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Навыков в списке не больше 30')).toBeInTheDocument()
    expect(field(/^Общие навыки ·/)).toHaveAttribute('aria-invalid', 'true')
  })

  it('changes fields, hides degree details when there is no degree, and sends empty as empty', async () => {
    const api = stubApi(base(ownPage(), { 'PUT /api/profile': reply(200, ownPage()) }))
    renderApp('/profile/edit')
    await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })
    await userEvent.selectOptions(field(/Учёная степень/), 'none')
    await userEvent.selectOptions(field(/Учёное звание/), 'none')
    expect(screen.queryByLabelText(/Год защиты/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/Год присвоения/)).not.toBeInTheDocument()
    await userEvent.clear(field(/Должность и место работы/))
    await userEvent.type(field(/Должность и место работы/), 'Профессор')
    await userEvent.clear(field(/Контактная почта/))
    await userEvent.clear(field(/^РИНЦ/))
    await userEvent.type(field(/Web of Science/), '7')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    await screen.findByText('Профиль сохранён')
    expect(api.called('PUT', '/api/profile')[0].body).toMatchObject({ headline: 'Профессор', degree: 'none', academic_title: 'none', contact_email: '', h_rsci: null, h_wos: 7 })
  })

  it('picks a region, a degree specialty and specialties from the lists', async () => {
    const api = stubApi(base(ownPage(emptyProfile), { 'PUT /api/profile': reply(200, ownPage(emptyProfile)) }))
    renderApp('/profile/edit')
    await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })
    await userEvent.type(screen.getByRole('combobox', { name: /Регион/ }), 'Москва')
    await userEvent.click(await screen.findByRole('option', { name: 'Москва' }))
    await userEvent.selectOptions(field(/Учёная степень/), 'doctor')
    await userEvent.type(screen.getByRole('combobox', { name: /Научная специальность диссертации/ }), 'Теоретическая')
    await userEvent.click(await screen.findByRole('option', { name: '1.3.3 Теоретическая физика' }))
    await userEvent.type(screen.getByRole('combobox', { name: /Научные специальности/ }), 'Органическая')
    await userEvent.click(await screen.findByRole('option', { name: /1.4.3 Органическая химия/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    await screen.findByText('Профиль сохранён')
    expect(api.called('PUT', '/api/profile')[0].body).toMatchObject({ region_code: '77', degree: 'doctor', degree_specialty_code: '1.3.3', specialties: ['1.4.3'] })
  })

  it('shows the server problems under the fields and does not leave the page', async () => {
    stubApi(base(ownPage(), { 'PUT /api/profile': apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { orcid: 'В номере ORCID ошибка: проверьте цифры' } }) }))
    const { router } = renderApp('/profile/edit')
    await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('В номере ORCID ошибка: проверьте цифры')).toBeInTheDocument()
    expect(field(/ORCID iD/)).toHaveFocus()
    expect(router.state.location.pathname).toBe('/profile/edit')
  })

  it('shows a general problem above the form', async () => {
    stubApi(base(ownPage(), { 'PUT /api/profile': apiError(500, 'internal', 'Что-то сломалось на сервере') }))
    renderApp('/profile/edit')
    await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText('Что-то сломалось на сервере')).toBeInTheDocument()
  })

  it('goes back to the profile on cancel', async () => {
    stubApi(base())
    const { router } = renderApp('/profile/edit')
    await userEvent.click(await screen.findByRole('link', { name: 'Отмена' }))
    expect(router.state.location.pathname).toBe('/profile')
  })

  it('says what went wrong when the profile or the reference books do not load', async () => {
    stubApi(base(ownPage(), { 'GET /api/profile': apiError(500, 'internal', 'Сбой') }))
    renderApp('/profile/edit')
    expect(await screen.findByRole('heading', { name: 'Не удалось загрузить профиль' })).toBeInTheDocument()
  })

  it('says what went wrong when the reference books do not load, and tries again', async () => {
    let fail = true
    stubApi(base(ownPage(), { 'GET /api/reference': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, reference)) }))
    renderApp('/profile/edit')
    expect(await screen.findByRole('heading', { name: 'Не удалось загрузить профиль' })).toBeInTheDocument()
    fail = false
    await click('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Основное в профиле' })).toBeInTheDocument()
  })

  it('is reachable from every core section of the profile', async () => {
    stubApi(own())
    renderApp('/profile')
    for (const name of ['о себе', 'степень и звание', 'научные специальности', 'идентификаторы и метрики'])
      expect(await screen.findByRole('link', { name: `Изменить: ${name}` })).toHaveAttribute('href', '/profile/edit')
  })
})


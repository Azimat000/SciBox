import { expect } from '@playwright/test'
import { registerAndConfirm, signIn, switchRole, test, uniqueEmail, watchServerErrors } from './helpers'

// Срез 39: переписка двух браузеров вживую на настоящем сервере.
// Отклик → окно переписки у обеих сторон → сообщения, галочки и «печатает» без перезагрузки → о собеседовании
// договариваются сообщениями (приглашений внутри отклика нет, D-256) → отказ закрывает переписку у обеих сторон.
// Организация демо: «Центр изучения Арктики», владелец Ольга Кузнецова.

const owner = 'olga.kuznetsova@demo.example.ru'
const applyTo = 'Стажёр-исследователь: полевой сезон на мерзлоте'

test('переписка двух браузеров идёт вживую, отказ закрывает её', async ({ page, browser, serverErrors }) => {
  const suffix = Date.now().toString(36).slice(-5)
  const name = `Нина Громова ${suffix}`
  const email = uniqueEmail('chat-scientist')

  await test.step('учёный регистрируется, заполняет профиль и откликается', async () => {
    await registerAndConfirm(page, name, email)
    await page.goto('/profile/edit')
    await page.getByLabel('Должность и место работы').fill('Научный сотрудник, лаборатория гляциологии')
    await page.getByLabel('Город').fill('Красноярск')
    await page.getByLabel('О себе', { exact: false }).fill('Изучаю ледники Арктики, работаю с радиолокацией льда и пробами кернов.')
    await page.getByLabel('Учёная степень').selectOption({ label: 'Кандидат наук' })
    await page.getByRole('button', { name: 'Сохранить' }).click()
    await expect(page.getByText('Профиль сохранён')).toBeVisible()

    await page.goto('/vacancies')
    await page.getByRole('searchbox').first().fill('полевой сезон мерзлота')
    await page.getByRole('searchbox').first().press('Enter')
    await page.getByRole('link', { name: applyTo }).first().click()
    await page.getByRole('link', { name: 'Откликнуться' }).or(page.getByRole('button', { name: 'Откликнуться' })).first().click()
    await page.getByLabel('Сопроводительное письмо').fill('Пять лет работаю с ледовой радиолокацией, готова к экспедиции.')
    await page.getByRole('button', { name: 'Отправить отклик' }).click()
    await expect(page.getByText('Отклик отправлен')).toBeVisible()
  })

  const orgSide = await browser.newContext()
  const staff = await orgSide.newPage()
  const staffErrors = watchServerErrors(staff)

  await test.step('организация открывает переписку и видит письмо к отклику', async () => {
    await signIn(staff, owner)
    await switchRole(staff, 'Нанимаю')
    await staff.goto('/chats')
    await staff.getByRole('searchbox', { name: 'Найти переписку' }).fill(name)
    await staff.getByRole('searchbox', { name: 'Найти переписку' }).press('Enter')
    await staff.getByRole('link', { name: new RegExp(name) }).first().click()
    await expect(staff.getByText('Соискатель отправил отклик')).toBeVisible()
    await expect(staff.getByText('Пять лет работаю с ледовой радиолокацией')).toBeVisible()
  })

  await test.step('учёный открывает ту же переписку со страницы отклика', async () => {
    await page.goto('/applications')
    await page.getByRole('link', { name: applyTo }).first().click()
    await page.getByRole('link', { name: /Написать|Перейти к переписке/ }).first().click()
    await expect(page.getByText('Вы отправили отклик')).toBeVisible()
  })

  await test.step('сообщение организации доходит до учёного без перезагрузки', async () => {
    await staff.getByRole('textbox', { name: 'Сообщение' }).fill('Здравствуйте! Расскажите, пожалуйста, про опыт бурения на льду.')
    await staff.getByRole('button', { name: 'Отправить' }).click()
    await expect(staff.getByText('Здравствуйте! Расскажите, пожалуйста, про опыт бурения на льду.')).toBeVisible()
    await expect(page.getByText('Здравствуйте! Расскажите, пожалуйста, про опыт бурения на льду.')).toBeVisible({ timeout: 15_000 })
  })

  await test.step('«прочитано» у организации появляется само', async () => {
    await expect(staff.getByText('Прочитано').first()).toBeVisible({ timeout: 15_000 })
  })

  await test.step('«печатает» видно собеседнику, ответ приходит вживую', async () => {
    const box = page.getByRole('textbox', { name: 'Сообщение' })
    await box.pressSequentially('Бурила на Шпицбергене', { delay: 30 })
    await expect(staff.getByText(/печатает/)).toBeVisible({ timeout: 15_000 })
    await box.fill('Бурила на Шпицбергене и в Якутии, около 200 м керна.')
    await page.getByRole('button', { name: 'Отправить' }).click()
    await expect(staff.getByText('около 200 м керна')).toBeVisible({ timeout: 15_000 })
  })

  await test.step('о собеседовании договариваются в переписке, кнопок приглашения нет', async () => {
    await staff.getByRole('textbox', { name: 'Сообщение' }).fill('Приглашаем на собеседование в четверг в 12:00 по Москве, ссылка: https://telemost.example.ru/j/777')
    await staff.getByRole('button', { name: 'Отправить' }).click()
    await expect(page.getByText(/собеседование в четверг в 12:00/)).toBeVisible({ timeout: 15_000 })
    await staff.goto('/candidates')
    await staff.getByRole('link', { name: name }).first().click()
    await expect(staff.getByRole('button', { name: 'Принять кандидата' })).toBeVisible()
    await expect(staff.getByRole('button', { name: 'Пригласить', exact: true })).toHaveCount(0)
  })

  await test.step('отказ закрывает переписку у обеих сторон сразу', async () => {
    await staff.goto('/candidates')
    await staff.getByRole('link', { name: name }).first().click()
    await staff.getByRole('button', { name: 'Отказать', exact: true }).click()
    await staff.getByRole('dialog').getByRole('button', { name: 'Отказать', exact: true }).click()
    await expect(staff.getByText('Отказ отправлен')).toBeVisible()

    const closed = 'Переписка закрыта: по отклику принято решение «Отказ».'
    await expect(page.getByText(closed)).toBeVisible({ timeout: 15_000 })
    await expect(page.getByRole('textbox', { name: 'Сообщение' })).toHaveCount(0)
    await staff.goto('/chats')
    await staff.getByRole('link', { name: new RegExp(name) }).first().click()
    await expect(staff.getByText(closed)).toBeVisible()
    await expect(staff.getByRole('textbox', { name: 'Сообщение' })).toHaveCount(0)
  })

  await orgSide.close()
  expect([...serverErrors, ...staffErrors]).toEqual([])
})

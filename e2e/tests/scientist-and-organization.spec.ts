import { expect } from '@playwright/test'
import { registerAndConfirm, signIn, switchRole, test, uniqueEmail, watchServerErrors } from './helpers'

// Дорожка «я учёный» целиком и ответы организации на неё:
// регистрация → профиль → отклик → приглашение на собеседование → ответ,
// затем организация находит учёного в каталоге и зовёт на другую вакансию.
// Организация демо: «Центр изучения Арктики», владелец Ольга Кузнецова.

const org = 'Центр изучения Арктики'
const owner = 'olga.kuznetsova@demo.example.ru'
const applyTo = 'Стажёр-исследователь: полевой сезон на мерзлоте'
const offerTo = 'Менеджер научных проектов'

test('учёный регистрируется, откликается и получает приглашения @phone', async ({ page, browser, serverErrors }) => {
  const suffix = Date.now().toString(36).slice(-5)
  const name = `Вера Сомова ${suffix}`
  const email = uniqueEmail('scientist')

  await test.step('регистрация и подтверждение почты', async () => {
    await registerAndConfirm(page, name, email)
  })

  await test.step('профиль: основное и режим «Все»', async () => {
    await page.goto('/profile/edit')
    await page.getByLabel('Должность и место работы').fill('Младший научный сотрудник, лаборатория криолитологии')
    await page.getByLabel('Город').fill('Якутск')
    await page.getByLabel('О себе', { exact: false }).fill('Изучаю многолетнюю мерзлоту и полевые методы бурения. Ищу место в полевой экспедиции.')
    await page.getByLabel('Учёная степень').selectOption({ label: 'Кандидат наук' })
    await page.getByRole('button', { name: 'Сохранить' }).click()
    await expect(page.getByText('Профиль сохранён')).toBeVisible()
    await expect(page).toHaveURL(/\/profile$/)

    // Переключатель сохраняется сразу и отмечается после ответа сервера.
    const everyone = page.getByRole('radio', { name: /Все/ })
    await everyone.click()
    await expect(page.getByText('Приватность сохранена')).toBeVisible()
    await expect(everyone).toBeChecked()
  })

  await test.step('отклик на вакансию из поиска', async () => {
    await page.goto('/vacancies')
    await page.getByRole('searchbox').first().fill('полевой сезон мерзлота')
    await page.getByRole('searchbox').first().press('Enter')
    await page.getByRole('link', { name: applyTo }).first().click()
    await expect(page.getByRole('heading', { level: 1, name: applyTo })).toBeVisible()

    await page.getByRole('link', { name: 'Откликнуться' }).or(page.getByRole('button', { name: 'Откликнуться' })).first().click()
    await page.getByLabel('Сопроводительное письмо').fill('Пять полевых сезонов в Якутии, работаю с буровым оборудованием и пробами льда. Готова к экспедиции.')
    await page.getByRole('button', { name: 'Отправить отклик' }).click()
    await expect(page.getByText('Отклик отправлен')).toBeVisible()
  })

  const orgSide = await browser.newContext()
  const staff = await orgSide.newPage()
  const staffErrors = watchServerErrors(staff)

  await test.step('организация видит отклик и приглашает на собеседование', async () => {
    await signIn(staff, owner)
    await switchRole(staff, 'Нанимаю')
    await staff.goto('/candidates')
    await staff.getByRole('link', { name: name }).first().click()
    await expect(staff.getByRole('heading', { level: 1 })).toContainText(name)

    await staff.getByRole('button', { name: 'Пригласить' }).click()
    const dialog = staff.getByRole('dialog')
    await dialog.getByLabel('Дата').fill(inDays(3))
    await dialog.getByLabel('Время (МСК)').fill('11:00')
    await dialog.getByLabel('Ссылка на встречу').fill('https://telemost.example.ru/j/12345')
    await dialog.getByRole('button', { name: 'Отправить приглашение' }).click()
    await expect(staff.getByText('Приглашение отправлено')).toBeVisible()
  })

  await test.step('учёный подтверждает время', async () => {
    await page.goto('/applications')
    await page.getByRole('link', { name: applyTo }).first().click()
    await expect(page.getByText('Собеседование').first()).toBeVisible()
    await page.getByRole('button', { name: 'Подтвердить время' }).click()
    await expect(page.getByText('Время подтверждено').first()).toBeVisible()
  })

  await test.step('организация видит подтверждение', async () => {
    await staff.reload()
    await expect(staff.getByText('Подтверждено').first()).toBeVisible()
  })

  await test.step('организация находит учёного в каталоге и зовёт на другую вакансию', async () => {
    await staff.goto('/scientists')
    await staff.getByRole('searchbox').first().fill(suffix)
    await staff.getByRole('searchbox').first().press('Enter')
    await staff.getByRole('link', { name: name, exact: true }).first().click()
    await expect(staff.getByRole('heading', { level: 1, name: name })).toBeVisible()
    await staff.getByRole('button', { name: /^Пригласить на вакансию/ }).click()
    const dialog = staff.getByRole('dialog')
    await dialog.getByLabel('Вакансия').selectOption({ label: offerTo })
    await dialog.getByLabel('Сообщение', { exact: false }).fill('Ищем человека, который знает полевую работу изнутри.')
    await dialog.getByRole('button', { name: 'Отправить приглашение' }).click()
    await expect(staff.getByText('Приглашение отправлено')).toBeVisible()
  })

  await test.step('учёный отвечает «Интересно»', async () => {
    await page.goto('/offers')
    await page.getByRole('link', { name: offerTo }).first().click()
    await expect(page.getByText(org).first()).toBeVisible()
    await page.getByRole('button', { name: 'Интересно' }).click()
    await expect(page.getByText('Вы ответили: Интересно')).toBeVisible()
  })

  await orgSide.close()
  expect([...serverErrors, ...staffErrors]).toEqual([])
})

/** Дата через n дней в формате поля даты (ГГГГ-ММ-ДД), по Москве. */
function inDays(n: number): string {
  const d = new Date(Date.now() + n * 86_400_000)
  return d.toLocaleDateString('sv-SE', { timeZone: 'Europe/Moscow' })
}

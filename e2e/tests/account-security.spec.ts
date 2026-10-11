import { expect } from '@playwright/test'
import { linkFromMail, newPassword, registerAndConfirm, signIn, test, uniqueEmail, watchServerErrors } from './helpers'

// Срез 20 (T-10): сменил пароль на одном устройстве — на другом устройстве сеанс завершён.
// «Другое устройство» — отдельный контекст браузера со своими cookie.

test('смена пароля завершает сеанс на другом устройстве @phone', async ({ page, browser }) => {
  const suffix = Date.now().toString(36).slice(-5)
  const email = uniqueEmail('security')
  const changed = 'e2e-changed-2026'

  await test.step('регистрация на первом устройстве', async () => {
    await registerAndConfirm(page, `Ирина Белова ${suffix}`, email)
  })

  const laptop = await browser.newContext()
  const other = await laptop.newPage()
  const otherErrors = watchServerErrors(other)

  await test.step('вход на втором устройстве, оба сеанса видны в списке', async () => {
    await signIn(other, email, newPassword)
    await page.goto('/account/security')
    const sessions = page.getByRole('region', { name: 'Где вы вошли' })
    await expect(sessions.getByRole('listitem')).toHaveCount(2)
    await expect(sessions.getByText('это устройство')).toBeVisible()
  })

  await test.step('смена пароля на первом устройстве', async () => {
    await page.getByLabel('Текущий пароль', { exact: false }).fill(newPassword)
    await page.getByLabel('Новый пароль', { exact: false }).fill(changed)
    await page.getByRole('button', { name: 'Сменить пароль' }).click()
    await expect(page.getByText('Пароль изменён')).toBeVisible()
  })

  await test.step('второе устройство вышло из аккаунта', async () => {
    await other.goto('/account/security')
    await expect(other).toHaveURL(/\/login\?next=\/account\/security/)
    await expect(other.getByText('Войдите, чтобы открыть эту страницу.')).toBeVisible()
  })

  await test.step('первое устройство осталось, в списке один сеанс', async () => {
    await page.reload()
    await expect(page.getByText('Аккаунт открыт только здесь.')).toBeVisible()
  })

  await test.step('новый пароль работает на втором устройстве', async () => {
    await signIn(other, email, changed)
  })

  expect(otherErrors, 'сервер ответил ошибкой второму устройству').toEqual([])
  await laptop.close()
})

// Срез 29 (D-190, предзахват аккаунта): ссылка подтверждения, открытая в другом браузере, подтверждает почту, но не
// входит в аккаунт и не оставляет пароль регистрации; пароль человек придумывает заново и только тогда входит.
test('ссылка подтверждения в другом браузере не входит в аккаунт', async ({ page, browser }) => {
  const suffix = Date.now().toString(36).slice(-5)
  const email = uniqueEmail('binding')
  const chosen = 'e2e-after-confirm-2026'

  await page.goto('/register')
  await page.getByLabel('Имя и фамилия').fill(`Олег Тихонов ${suffix}`)
  await page.getByLabel('Почта').fill(email)
  await page.getByLabel('Пароль', { exact: false }).first().fill(newPassword)
  await page.getByRole('checkbox', { name: /согласие на обработку/ }).check()
  await page.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(page.getByText(`Мы отправили письмо на ${email}`)).toBeVisible()
  const link = await linkFromMail(page.request, email, '/confirm-email?token=')

  const phone = await browser.newContext()
  const other = await phone.newPage()
  const errors = watchServerErrors(other)
  await test.step('в другом браузере откроется выбор пароля, входа нет', async () => {
    await other.goto(new URL(link).pathname + new URL(link).search)
    await expect(other).toHaveURL(/\/reset-password\?token=.*from=confirm/)
    await expect(other.getByText('Почта подтверждена. Ссылка открыта не в том браузере')).toBeVisible()
  })
  await test.step('пароль регистрации больше не работает, новый работает', async () => {
    await other.getByLabel('Новый пароль', { exact: false }).fill(chosen)
    await other.getByRole('button', { name: 'Сохранить пароль' }).click()
    await expect(other).toHaveURL(/\/login/)
    await signIn(other, email, chosen)
  })
  expect(errors, 'сервер ответил ошибкой').toEqual([])
  await phone.close()
})

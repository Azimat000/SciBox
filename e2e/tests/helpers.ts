import { test as base, expect, type APIRequestContext, type Page } from '@playwright/test'
import { mailpitURL } from '../playwright.config'

// Пароль демо-аккаунтов (server/seed/seed.go, DemoPassword). Только для локальной базы.
export const demoPassword = 'demo-password-2026'
export const newPassword = 'e2e-password-2026'

/** Уникальная почта для человека, которого сценарий создаёт сам. */
export function uniqueEmail(who: string): string {
  return `${who}-${Date.now()}-${Math.floor(Math.random() * 1e6)}@e2e.example.ru`
}

type MailpitList = { messages: { ID: string; Subject: string }[] }

/**
 * Ждёт письмо на адрес и возвращает первую ссылку сайта с нужным путём.
 * Письма уходят из очереди сервера раз в пару секунд, поэтому ждём, а не читаем сразу.
 */
export async function linkFromMail(request: APIRequestContext, to: string, path: string): Promise<string> {
  let link = ''
  await expect
    .poll(
      async () => {
        const list = (await (await request.get(`${mailpitURL}/api/v1/search`, { params: { query: `to:"${to}"` } })).json()) as MailpitList
        for (const m of list.messages) {
          const msg = (await (await request.get(`${mailpitURL}/api/v1/message/${m.ID}`)).json()) as { Text: string }
          const found = msg.Text.match(new RegExp(`https?://[^\\s]+${path.replace(/[/?]/g, '\\$&')}[^\\s]*`))
          if (found) {
            link = found[0]
            return true
          }
        }
        return false
      },
      { timeout: 30_000, intervals: [500, 1000, 2000] },
    )
    .toBe(true)
  return link
}

/** Регистрация через форму и подтверждение почты по ссылке из письма. После неё человек вошёл. */
export async function registerAndConfirm(page: Page, name: string, email: string): Promise<void> {
  await page.goto('/register')
  await page.getByLabel('Имя и фамилия').fill(name)
  await page.getByLabel('Почта').fill(email)
  await page.getByLabel('Пароль', { exact: false }).first().fill(newPassword)
  await page.getByRole('checkbox', { name: /согласие на обработку/ }).check()
  await page.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(page.getByText(`Мы отправили письмо на ${email}`)).toBeVisible()

  const link = await linkFromMail(page.request, email, '/confirm-email?token=')
  await page.goto(new URL(link).pathname + new URL(link).search)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/подтверждена/i)
}

export async function signIn(page: Page, email: string, password = demoPassword): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Почта').fill(email)
  await page.getByLabel('Пароль', { exact: false }).first().fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

/** Переключатель «Ищу работу / Нанимаю» в шапке. На телефоне он внутри меню. */
export async function switchRole(page: Page, role: 'Ищу работу' | 'Нанимаю'): Promise<void> {
  const menu = page.getByRole('button', { name: 'Открыть меню' })
  if (await menu.isVisible()) await menu.click()
  const tab = page.getByRole('group', { name: 'Режим' }).getByRole('button', { name: role }).locator('visible=true')
  await tab.click()
  await expect(tab).toHaveAttribute('aria-pressed', 'true')
}

/**
 * Следит, чтобы сервер ни разу не ответил ошибкой 5xx за время сценария.
 * Возвращает список таких ответов; сценарий проверяет, что он пуст.
 */
export function watchServerErrors(page: Page): string[] {
  const failures: string[] = []
  page.on('response', (res) => {
    if (res.status() >= 500) failures.push(`${res.status()} ${res.request().method()} ${new URL(res.url()).pathname}`)
  })
  return failures
}

/** Обычный test, но страница по умолчанию сразу под присмотром watchServerErrors. */
export const test = base.extend<{ serverErrors: string[] }>({
  serverErrors: async ({ page }, use) => {
    const failures = watchServerErrors(page)
    await use(failures)
    expect(failures, 'сервер ответил ошибкой').toEqual([])
  },
})

import { defineConfig, devices } from '@playwright/test'

// Сквозные тесты гоняют настоящий сервер и сайт на своих портах и своей базе (scibox_e2e),
// чтобы не мешать `make dev`. Базу готовит scripts/e2e-db (его вызывает `make e2e`).
const apiPort = 8090
const webPort = 5174
export const mailpitURL = 'http://localhost:8025'

export default defineConfig({
  testDir: './tests',
  // Сценарии одной базы идут друг за другом: у сервера лимиты на отклики и приглашения в сутки.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 10_000 },
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'report' }]],
  outputDir: 'results',
  use: {
    baseURL: `http://localhost:${webPort}`,
    locale: 'ru-RU',
    timezoneId: 'Europe/Moscow',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'phone', use: { ...devices['Pixel 7'] }, grep: /@phone/ },
  ],
  webServer: [
    {
      // Сервер отвечает на запросы, воркер рядом делает фоновую работу (письма уходят в Mailpit именно им). Журналы
      // пишутся в e2e/server.log и e2e/worker.log, чтобы не засорять вывод тестов. Playwright останавливает группу
      // процессов целиком, поэтому воркер, запущенный в фоне командной оболочки, останавливается вместе с сервером.
      command: './bin/scibox worker > ../e2e/worker.log 2>&1 & exec ./bin/scibox serve > ../e2e/server.log 2>&1',
      cwd: '../server',
      url: `http://127.0.0.1:${apiPort}/api/health`,
      reuseExistingServer: Boolean(process.env.E2E_REUSE),
      timeout: 60_000,
      env: {
        DATABASE_URL: 'postgres://scibox:scibox@localhost:5433/scibox_e2e?sslmode=disable',
        SCIBOX_S3_BUCKET: 'scibox-e2e',
        SCIBOX_HTTP_ADDR: `127.0.0.1:${apiPort}`,
        SCIBOX_BACKGROUND: 'off',
        SCIBOX_PUBLIC_URL: `http://localhost:${webPort}`,
      },
    },
    {
      command: `npm run dev -- --port ${webPort} --strictPort`,
      cwd: '../web',
      url: `http://localhost:${webPort}`,
      reuseExistingServer: Boolean(process.env.E2E_REUSE),
      timeout: 60_000,
      env: { SCIBOX_API_URL: `http://127.0.0.1:${apiPort}` },
    },
  ],
})

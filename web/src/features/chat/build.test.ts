// @vitest-environment node
import { mkdtempSync, readdirSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'
import { afterAll, describe, expect, it } from 'vitest'
import { CHAT_MOCK_MARKER } from './mock'

// T-104: подставной сервер чата не попадает в боевую сборку, даже если VITE_CHAT_MOCK=1 оставили включённым.
// Проверка настоящей сборкой Vite; вторая сборка (режим разработки) доказывает, что тест вообще умеет его найти.

const root = fileURLToPath(new URL('../../..', import.meta.url))
const dirs: string[] = []

async function bundleText(mode: 'production' | 'development'): Promise<string> {
  const outDir = mkdtempSync(join(tmpdir(), `scibox-build-${mode}-`))
  dirs.push(outDir)
  // Vitest ставит NODE_ENV=test, а по нему Vite решает, боевая ли сборка (import.meta.env.DEV).
  const nodeEnv = process.env.NODE_ENV
  process.env.NODE_ENV = mode
  process.env.VITE_CHAT_MOCK = '1'
  try {
    await build({ root, mode, logLevel: 'silent', build: { outDir, emptyOutDir: true, minify: mode === 'production', sourcemap: false } })
  } finally {
    delete process.env.VITE_CHAT_MOCK
    process.env.NODE_ENV = nodeEnv
  }
  const files = readdirSync(join(outDir, 'assets')).filter((f) => f.endsWith('.js'))
  return files.map((f) => readFileSync(join(outDir, 'assets', f), 'utf8')).join('\n')
}

afterAll(() => {
  for (const d of dirs) rmSync(d, { recursive: true, force: true })
})

describe('production build', () => {
  it('has no chat mock server even with VITE_CHAT_MOCK=1', async () => {
    const text = await bundleText('production')
    expect(text.includes('/api/chats'), 'адреса чата в сборке').toBe(true)
    expect(text.includes(CHAT_MOCK_MARKER), 'метка подставного сервера').toBe(false)
    expect(text.includes('__chatMock'), 'window.__chatMock').toBe(false)
  }, 120_000)

  it('the development build does carry it, so the check above can see it', async () => {
    const text = await bundleText('development')
    expect(text.includes(CHAT_MOCK_MARKER), 'метка в сборке разработки').toBe(true)
  }, 120_000)
})

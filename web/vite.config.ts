/// <reference types="vitest/config" />
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

// Название продукта живёт в одном файле, общем с сервером (D-029).
const productFile = fileURLToPath(new URL('../config/product.json', import.meta.url))
const product = JSON.parse(readFileSync(productFile, 'utf8')) as { name: string }

function productName(): Plugin {
  return {
    name: 'scibox-product-name',
    transformIndexHtml: (html) => html.replaceAll('%PRODUCT_NAME%', product.name),
  }
}

export default defineConfig({
  plugins: [react(), productName()],
  server: {
    port: 5173,
    fs: { allow: ['.', '../config'] },
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      // Исключения перечислены в docs/TESTING.md.
      exclude: ['src/main.tsx', 'src/test/**', 'src/features/styleguide/**', 'src/**/*.test.{ts,tsx}', 'src/vite-env.d.ts'],
      reporter: ['text', 'html'],
      thresholds: {
        lines: 90,
        statements: 90,
        functions: 90,
        branches: 85,
        // Критичные зоны (≥ 97% строк, ≥ 95% ветвлений) добавляются сюда
        // по мере появления папок: access, privacy, references,
        // applications, files, matching.
        'src/features/auth/**': { lines: 97, statements: 97, branches: 95, functions: 97 },
        // Организации: кто что может видеть и менять (показ кнопок по ролям; решает сервер, но интерфейс не должен обманывать).
        'src/features/orgs/**': { lines: 97, statements: 97, branches: 95, functions: 97 },
        // Вакансии: показ управления по правам и жизненный цикл (решает сервер, интерфейс не должен обманывать).
        'src/features/vacancies/**': { lines: 97, statements: 97, branches: 95, functions: 97 },
      },
    },
  },
})

import '@testing-library/jest-dom/vitest'
import { cleanup, configure } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// Под полной нагрузкой (десятки параллельных файлов) страница с несколькими переходами не всегда успевает за секунду
// по умолчанию: ожидание findBy/waitFor растянуто до трёх секунд. Проверки от этого не мягче, медленный прогон не падает.
configure({ asyncUtilTimeout: 3000 })

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
